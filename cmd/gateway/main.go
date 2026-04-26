package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Gyt-project/backend-api/internal/auth"
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

	// ── HTTP routes ───────────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// GraphQL endpoint — wrappé par le middleware JWT
	graphqlHandler := gql.AuthMiddleware(gql.NewHandler(grpcClient, nil, nil))
	mux.Handle("/graphql", graphqlHandler)

	// GraphQL Playground (dev uniquement — désactiver en production)
	if os.Getenv("ENV") != "production" {
		mux.Handle("/playground", gql.NewPlaygroundHandler("/graphql"))
		log.Println("GraphQL Playground disponible sur http://localhost:" + gatewayPort + "/playground")
	}

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
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
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		resp, err := grpcClient.HandleBranchPush(r.Context(), &pb.BranchPushRequest{
			Owner: payload.Owner, Repo: payload.Repo, Branch: payload.Branch,
		})
		if err == nil {
			for _, n := range resp.GetPrNumbers() {
				key := fmt.Sprintf("%s/%s/%d", payload.Owner, payload.Repo, n)
				events.PublishPR(key, "new_commits")
			}
		}
		events.PublishRepo(fmt.Sprintf("%s/%s", payload.Owner, payload.Repo), "push")
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
		return
	}
	defer conn.Close()

	ch := events.PR.Subscribe(key)
	defer events.PR.Unsubscribe(key, ch)

	// Read pump: detect client disconnection
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

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
				return
			}
		case e, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteJSON(e); err != nil {
				return
			}
		}
	}
}

// wsHandleRepo upgrades the connection to WebSocket and streams repo events.
func wsHandleRepo(w http.ResponseWriter, r *http.Request, key string) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ch := events.Repo.Subscribe(key)
	defer events.Repo.Unsubscribe(key, ch)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

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
				return
			}
		case e, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteJSON(e); err != nil {
				return
			}
		}
	}
}
