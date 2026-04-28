package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Gyt-project/backend-api/internal/auth"
	"github.com/Gyt-project/backend-api/internal/cache"
	"github.com/Gyt-project/backend-api/internal/pubsub"
	"github.com/Gyt-project/backend-api/pkg/events"
	gql "github.com/Gyt-project/backend-api/pkg/gql"
	pb "github.com/Gyt-project/backend-api/pkg/grpc"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func main() {
	// ── Config ────────────────────────────────────────────────────────────────
	p, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	if err := godotenv.Load(filepath.Join(p, ".env")); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	grpcAddr := os.Getenv("GRPC_BACKEND_ADDR")
	if grpcAddr == "" {
		grpcAddr = "localhost:50051"
	}

	gatewayPort := os.Getenv("GATEWAY_PORT")
	if gatewayPort == "" {
		gatewayPort = "8080"
	}

	// ── gRPC client ───────────────────────────────────────────────────────────
	conn, err := grpc.NewClient(
		grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("Failed to connect to gRPC backend at %s: %v", grpcAddr, err)
	}
	defer conn.Close()

	grpcClient := pb.NewGytServiceClient(conn)
	log.Printf("Connected to gRPC backend at %s", grpcAddr)

	// ── Redis (pub/sub for git server events) ─────────────────────────────────
	if err := cache.Init(os.Getenv("REDIS_ADDR"), os.Getenv("REDIS_PASSWORD")); err != nil {
		log.Printf("gateway: Redis unavailable (%v) — repo push events are single-instance only", err)
	} else {
		pubsub.Init(cache.Client)
		log.Println("gateway: Redis connected (repo event pub/sub enabled)")
	}

	// ── HTTP routes ───────────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// GraphQL endpoint — wrappé par le middleware JWT
	graphqlHandler := gql.AuthMiddleware(gql.NewHandler(grpcClient, nil, nil))
	mux.Handle("/graphql", graphqlHandler)

	// GraphQL Playground (dev uniquement — désactiver en production)
	if os.Getenv("ENV") != "production" {
		mux.Handle("/graphql/playground", gql.NewPlaygroundHandler("/graphql"))
		log.Println("GraphQL Playground disponible sur http://localhost:" + gatewayPort + "/graphql/playground")
	}

	// Health check — includes Redis pub/sub status
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		redisStatus := "unavailable"
		if cache.Client != nil {
			if err := cache.Client.Ping(r.Context()).Err(); err == nil {
				redisStatus = "ok"
			} else {
				redisStatus = "error: " + err.Error()
			}
		}
		_, _ = fmt.Fprintf(w, `{"status":"ok","redis":%q}`, redisStatus)
	})

	// WebSocket routing probe — GET /ws/health
	// Because HAProxy routes /ws/* to this backend, a 200 here proves:
	//   1. HAProxy is correctly forwarding /ws/* to the gateway
	//   2. The gateway is reachable
	//   3. Redis pub/sub is operational (tested with a PING round-trip)
	// Usage:  curl -i https://<host>/ws/health
	mux.HandleFunc("/ws/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		redisOK := false
		redisMsg := "unavailable"
		if cache.Client != nil {
			if err := cache.Client.Ping(r.Context()).Err(); err == nil {
				redisOK = true
				redisMsg = "ok"
			} else {
				redisMsg = err.Error()
			}
		}
		if redisOK {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_, _ = fmt.Fprintf(w,
			`{"routing":"ok","redis":%q,"ws_path_routed_by_haproxy":true}`,
			redisMsg,
		)
	})

	// ── WebSocket: PR-level events (/ws/pr/{owner}/{repo}/{number}?token=...) ──
	mux.HandleFunc("/ws/pr/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path[len("/ws/pr/"):] // "owner/repo/number"
		parts := splitN(path, "/", 3)
		if len(parts) != 3 {
			http.Error(w, "invalid path: expected /ws/pr/{owner}/{repo}/{number}", http.StatusBadRequest)
			return
		}
		owner, repo, numberStr := parts[0], parts[1], parts[2]
		if _, err := strconv.Atoi(numberStr); err != nil {
			http.Error(w, "invalid PR number", http.StatusBadRequest)
			return
		}

		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		if _, err := auth.ParseAccessToken(token); err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		key := fmt.Sprintf("%s/%s/%s", owner, repo, numberStr)
		wsHandlePR(w, r, key)
	})

	// ── WebSocket: Repo-level events (/ws/repo/{owner}/{repo}?token=...) ─────
	mux.HandleFunc("/ws/repo/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path[len("/ws/repo/"):] // "owner/repo"
		parts := splitN(path, "/", 2)
		if len(parts) != 2 {
			http.Error(w, "invalid path: expected /ws/repo/{owner}/{repo}", http.StatusBadRequest)
			return
		}
		owner, repo := parts[0], parts[1]

		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		if _, err := auth.ParseAccessToken(token); err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		key := fmt.Sprintf("%s/%s", owner, repo)
		wsHandleRepo(w, r, key)
	})

	// ── Internal push hook (/hooks/push) called by soft-serve on git push ────
	mux.HandleFunc("/hooks/push", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Owner  string `json:"owner"`
			Repo   string `json:"repo"`
			Branch string `json:"branch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Owner == "" || payload.Repo == "" || payload.Branch == "" {
			log.Printf("[push] bad request body: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// soft-serve names repos as "owner/reponame" (git_repo_name).
		// Strip the owner prefix so our DB lookups and cache keys use just "reponame".
		repoName := strings.TrimPrefix(payload.Repo, payload.Owner+"/")
		log.Printf("[push] hook received: owner=%s repo=%s (normalized: %s) branch=%s", payload.Owner, payload.Repo, repoName, payload.Branch)

		resp, err := grpcClient.HandleBranchPush(r.Context(), &pb.BranchPushRequest{
			Owner: payload.Owner, Repo: repoName, Branch: payload.Branch,
		})
		if err != nil {
			log.Printf("[push] gRPC HandleBranchPush error: %v", err)
		} else {
			prNums := resp.GetPrNumbers()
			log.Printf("[push] gRPC HandleBranchPush ok: affected_prs=%v", prNums)
			for _, n := range prNums {
				key := fmt.Sprintf("%s/%s/%d", payload.Owner, repoName, n)
				log.Printf("[push] publishing new_commits to PR ws key=%s", key)
				events.PublishPR(key, "new_commits")
			}
		}

		repoKey := fmt.Sprintf("%s/%s", payload.Owner, repoName)
		evtJSON, _ := json.Marshal(events.RepoEvent{Type: "push"})
		redisChannel := fmt.Sprintf("git:repo:%s:events", repoKey)
		if pubErr := pubsub.Publish(r.Context(), redisChannel, evtJSON); pubErr != nil {
			log.Printf("[push] Redis PUBLISH %s failed: %v — falling back to in-memory only", redisChannel, pubErr)
		} else {
			log.Printf("[push] Redis PUBLISH %s ok", redisChannel)
		}
		events.PublishRepo(repoKey, "push")
		log.Printf("[push] in-memory hub notified for repo=%s", repoKey)
		w.WriteHeader(http.StatusOK)
	})

	// ── Démarrage du serveur ──────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + gatewayPort,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // 0 = no timeout, required for SSE
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("API Gateway GraphQL démarré sur :%s", gatewayPort)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Erreur serveur: %v", err)
	}
}

// splitN splits s by sep up to n parts (like strings.SplitN but avoids import clutter).
func splitN(s, sep string, n int) []string {
	var result []string
	for i := 0; i < n-1; i++ {
		idx := indexOf(s, sep)
		if idx < 0 {
			break
		}
		result = append(result, s[:idx])
		s = s[idx+len(sep):]
	}
	result = append(result, s)
	return result
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// wsHandlePR upgrades the connection to WebSocket and streams PR events.
func wsHandlePR(w http.ResponseWriter, r *http.Request, key string) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws:pr] upgrade failed key=%s: %v", key, err)
		return
	}
	defer conn.Close()

	ch := events.PR.Subscribe(key)
	defer events.PR.Unsubscribe(key, ch)

	// Read pump: detect client disconnection
	done := make(chan struct{})
	go func() {
		defer func() {
			log.Printf("[ws:pr] client disconnected key=%s", key)
			close(done)
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	log.Printf("[ws:pr] client connected key=%s", key)
	_ = conn.WriteJSON(map[string]string{"type": "connected"})

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-done:
			return
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[ws:pr] heartbeat write failed key=%s: %v", key, err)
				return
			}
		case e, ok := <-ch:
			if !ok {
				return
			}
			log.Printf("[ws:pr] forwarding event type=%s key=%s", e.Type, key)
			if err := conn.WriteJSON(e); err != nil {
				log.Printf("[ws:pr] write failed key=%s: %v", key, err)
				return
			}
		}
	}
}

// wsHandleRepo upgrades the connection to WebSocket and streams repo-level git
// events (push, branch create/delete, etc.).
//
// Fan-out strategy:
//   - When Redis is available, the handler subscribes to the Redis channel
//     "git:repo:{owner}/{repo}:events" so events published by ANY gateway
//     instance are delivered to ALL connected clients (multi-instance safe).
//   - When Redis is unavailable the handler falls back to the local in-memory
//     hub, which works correctly for single-instance deployments.
func wsHandleRepo(w http.ResponseWriter, r *http.Request, key string) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws:repo] upgrade failed for key=%s: %v", key, err)
		return
	}
	defer conn.Close()

	// Read pump — detects client disconnection.
	done := make(chan struct{})
	go func() {
		defer func() {
			log.Printf("[ws:repo] client disconnected key=%s", key)
			close(done)
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	_ = conn.WriteJSON(map[string]string{"type": "connected"})

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	channel := fmt.Sprintf("git:repo:%s:events", key)
	redisSub, redisErr := pubsub.Subscribe(r.Context(), channel)
	if redisErr == nil {
		log.Printf("[ws:repo] client connected key=%s via=redis channel=%s", key, channel)
		// ── Redis path: cross-instance fan-out ───────────────────────────────
		defer redisSub.Close()
		redisCh := redisSub.Channel()
		for {
			select {
			case <-done:
				return
			case <-r.Context().Done():
				return
			case <-heartbeat.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Printf("[ws:repo] heartbeat write failed key=%s: %v", key, err)
					return
				}
			case msg, ok := <-redisCh:
				if !ok {
					log.Printf("[ws:repo] redis channel closed key=%s", key)
					return
				}
				var e events.RepoEvent
				if err := json.Unmarshal([]byte(msg.Payload), &e); err != nil {
					log.Printf("[ws:repo] unmarshal error key=%s: %v", key, err)
					continue
				}
				log.Printf("[ws:repo] forwarding event type=%s to client key=%s via=redis", e.Type, key)
				if err := conn.WriteJSON(e); err != nil {
					log.Printf("[ws:repo] write failed key=%s: %v", key, err)
					return
				}
			}
		}
	}

	// ── In-memory fallback (Redis unavailable) ────────────────────────────────
	log.Printf("[ws:repo] client connected key=%s via=in-memory (Redis unavailable: %v)", key, redisErr)
	ch := events.Repo.Subscribe(key)
	defer events.Repo.Unsubscribe(key, ch)
	for {
		select {
		case <-done:
			return
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[ws:repo] heartbeat write failed key=%s: %v", key, err)
				return
			}
		case e, ok := <-ch:
			if !ok {
				return
			}
			log.Printf("[ws:repo] forwarding event type=%s to client key=%s via=in-memory", e.Type, key)
			if err := conn.WriteJSON(e); err != nil {
				log.Printf("[ws:repo] write failed key=%s: %v", key, err)
				return
			}
		}
	}
}
