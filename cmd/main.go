package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/Gyt-project/backend-api/internal/auth"
	"github.com/Gyt-project/backend-api/internal/cache"
	"github.com/Gyt-project/backend-api/internal/gitClient"
	"github.com/Gyt-project/backend-api/internal/orm"
	pb "github.com/Gyt-project/backend-api/pkg/grpc"
	"github.com/Gyt-project/backend-api/pkg/models"
	"github.com/Gyt-project/backend-api/pkg/server"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"
)

// grpcDebugInterceptor logs every gRPC call: method, response type, and any error.
// This runs after the handler returns, before gRPC marshals the response — so if
// the marshal step panics, the log shows the last method that returned successfully.
func grpcDebugInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	resp, err := handler(ctx, req)
	if err != nil {
		log.Printf("[grpc:error] method=%s req=%T err=%v", info.FullMethod, req, err)
	} else {
		log.Printf("[grpc:response] method=%s req=%T resp=%T", info.FullMethod, req, resp)
		if resp == nil {
			log.Printf("[grpc:warn] method=%s returned nil response", info.FullMethod)
		}
	}
	_ = fmt.Sprintf // keep fmt import used
	return resp, err
}

func main() {
	p, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	err = godotenv.Load(filepath.Join(p, ".env"))
	if err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// ── Database ──────────────────────────────────────────────────────────────
	HOST := os.Getenv("DB_HOST")
	PORT := os.Getenv("DB_PORT")
	USER := os.Getenv("DB_USER")
	PASSWORD := os.Getenv("DB_PASSWORD")
	DBNAME := os.Getenv("DB_NAME")

	err = orm.InitORM(HOST, PORT, USER, PASSWORD, DBNAME)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	log.Println("Database connected")

	err = orm.RegisterModels(
		// core
		&models.User{},
		&models.Organization{},
		&models.OrgMembership{},
		&models.Repository{},
		&models.RepoCollaborator{},
		&models.SSHKey{},
		// stars
		&models.Star{},
		// labels (doit précéder Issue et PullRequest)
		&models.Label{},
		// issues
		&models.Issue{},
		&models.IssueComment{},
		// pull requests
		&models.PullRequest{},
		&models.PRComment{},
		&models.PRReview{},
		&models.ReviewRequest{},
		// webhooks
		&models.Webhook{},
		// branch protection
		&models.BranchProtection{},
	)
	if err != nil {
		log.Fatalf("Failed to migrate models: %v", err)
	}
	log.Println("Database migrations applied")

	// ── Redis cache ───────────────────────────────────────────────────────────
	redisAddr := os.Getenv("REDIS_ADDR")
	redisPassword := os.Getenv("REDIS_PASSWORD")
	if err := cache.Init(redisAddr, redisPassword); err != nil {
		log.Printf("Redis cache unavailable, running without cache: %v", err)
	} else {
		log.Println("Redis cache connected")
	}

	// ── Git server client ─────────────────────────────────────────────────────
	CLIENT_ADDR := os.Getenv("GIT_SERVER_ADDR")
	err = gitClient.InitGitClient(CLIENT_ADDR)
	if err != nil {
		log.Fatalf("Failed to connect to git server: %v", err)
	}

	// Wait for git server readiness
	maxRetries := 5
	timeout := 2000 // milliseconds
	ready := false
	for i := 0; i < maxRetries; i++ {
		resp, err := gitClient.GitClient.HealthCheck(context.Background())
		if err == nil && resp.GetStatus() == "ok" {
			ready = true
			break
		}
		log.Printf("Git server health check failed, retrying... (%d/%d)\n", i+1, maxRetries)
		time.Sleep(time.Duration(timeout) * time.Millisecond)
	}
	if !ready {
		log.Fatal("Failed to connect to git server gRPC after max retries")
	}
	log.Println("Git server is ready")

	// ── gRPC server ───────────────────────────────────────────────────────────
	grpcPort := os.Getenv("GRPC_PORT")
	if grpcPort == "" {
		grpcPort = "50051"
	}

	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", grpcPort, err)
	}

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			auth.UnaryAuthInterceptor,
			grpcDebugInterceptor,
		),
	)

	pb.RegisterGytServiceServer(grpcServer, server.NewGytServer())

	log.Printf("gRPC server listening on :%s", grpcPort)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve gRPC: %v", err)
	}
}
