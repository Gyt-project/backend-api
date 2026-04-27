// cmd/live/main.go — Live PR API server
//
// This binary is the entry point for the real-time review collaboration
// service.  It exposes:
//   - WebSocket endpoint  /live/ws?session=<id>
//   - SSE endpoint        /live/events?session=<id>&token=<jwt>
//   - REST session CRUD   POST/GET /live/sessions, POST /live/sessions/{id}/close
//   - PR session list     GET /live/pr/{prId}/sessions
//
// It shares the same PostgreSQL database as the main backend (adding its own
// tables via AutoMigrate) and the same Redis instance (for both Pub/Sub and
// the cache layer).
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Gyt-project/backend-api/internal/cache"
	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/internal/pubsub"
	pb "github.com/Gyt-project/backend-api/pkg/grpc"
	"github.com/Gyt-project/backend-api/pkg/live"
	"github.com/Gyt-project/backend-api/pkg/models"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// ── Config ────────────────────────────────────────────────────────────────
	p, _ := os.Getwd()
	if err := godotenv.Load(filepath.Join(p, ".env")); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// ── Database ──────────────────────────────────────────────────────────────
	if err := orm.InitORM(
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_SSLMODE"),
	); err != nil {
		log.Fatalf("live: DB connect failed: %v", err)
	}

	// Migrate only the Live-specific tables.  The main backend owns the other
	// models and migrates them on its own startup.
	if err := orm.RegisterModels(
		&models.LiveSession{},
		&models.LiveParticipant{},
		&models.LiveChatMessage{},
		&models.LiveReviewEvent{},
	); err != nil {
		log.Fatalf("live: DB migrate failed: %v", err)
	}

	// Drop the legacy repo_id column if it still exists from a previous schema.
	// AutoMigrate never removes columns, so we handle it explicitly.
	orm.DB.Exec(`ALTER TABLE live_sessions DROP COLUMN IF EXISTS repo_id`)

	log.Println("live: database connected and migrated")

	// ── Redis (cache + pub/sub) ───────────────────────────────────────────────
	if err := cache.Init(os.Getenv("REDIS_ADDR"), os.Getenv("REDIS_PASSWORD")); err != nil {
		log.Printf("live: Redis unavailable (%v) — running without pub/sub (single instance only)", err)
	} else {
		pubsub.Init(cache.Client) // share the same *redis.Client
		log.Println("live: Redis connected (pub/sub enabled)")
	}

	// ── gRPC client → main backend API ───────────────────────────────────────
	grpcAddr := os.Getenv("GRPC_BACKEND_ADDR")
	if grpcAddr == "" {
		grpcAddr = "localhost:50051"
	}
	grpcConn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("live: gRPC connect failed (%s): %v", grpcAddr, err)
	}
	defer grpcConn.Close()
	live.InitGrpc(pb.NewGytServiceClient(grpcConn))
	log.Printf("live: connected to backend gRPC at %s", grpcAddr)

	// ── HTTP router ───────────────────────────────────────────────────────────
	port := os.Getenv("LIVE_PORT")
	if port == "" {
		port = "8090"
	}

	// Allowed frontend origins (space or comma separated; default: localhost dev).
	corsOrigin := os.Getenv("CORS_ORIGIN")
	if corsOrigin == "" {
		corsOrigin = "http://localhost:3000"
	}

	mux := http.NewServeMux()

	// Session management
	mux.HandleFunc("POST /live/sessions", live.CreateSessionHandler)
	mux.HandleFunc("GET /live/sessions/{id}", live.GetSessionHandler)
	mux.HandleFunc("POST /live/sessions/{id}/close", live.CloseSessionHandler)
	mux.HandleFunc("GET /live/pr/{prId}/sessions", live.ListSessionsForPRHandler)

	// Real-time endpoints
	mux.HandleFunc("GET /live/ws", live.WSHandler)
	mux.HandleFunc("GET /live/events", live.SSEHandler)

	// Health probe
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"live"}`))
	})

	handler := corsMiddleware(corsOrigin, mux)

	// WriteTimeout must be 0 for SSE connections (they stream indefinitely).
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("live: API server listening on :%s (CORS origin: %s)", port, corsOrigin)
	log.Fatal(srv.ListenAndServe())
}

// corsMiddleware adds permissive CORS headers for the configured frontend origin.
// WebSocket upgrade requests and SSE connections pass through unchanged because
// the browser does not enforce CORS on those protocols; only plain HTTP requests
// (the REST session endpoints) need the headers.
func corsMiddleware(origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")

		// Handle preflight.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
