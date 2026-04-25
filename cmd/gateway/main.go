package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	gql "github.com/Gyt-project/backend-api/pkg/gql"
	pb "github.com/Gyt-project/backend-api/pkg/grpc"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

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

	// ── Démarrage du serveur ──────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + gatewayPort,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("API Gateway GraphQL démarré sur :%s", gatewayPort)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Erreur serveur: %v", err)
	}
}
