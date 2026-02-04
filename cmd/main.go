package main

import (
	"context"
	"github.com/Gyt-project/backend-api/internal/gitClient"
	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
	"github.com/joho/godotenv"
	"log"
	"os"
	"path/filepath"
	"time"
)

func main() {
	p, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	err = godotenv.Load(filepath.Join(p, ".env"))
	if err != nil {
		log.Println("No .env file found")
	}

	// read from env file or env variables for database connection
	HOST := os.Getenv("DB_HOST")
	PORT := os.Getenv("DB_PORT")
	USER := os.Getenv("DB_USER")
	PASSWORD := os.Getenv("DB_PASSWORD")
	DBNAME := os.Getenv("DB_NAME")

	// init the orm and database
	err = orm.InitORM(HOST, PORT, USER, PASSWORD, DBNAME)
	if err != nil {
		panic(err)
	}
	err = orm.RegisterModels(
		models.User{},
	)
	if err != nil {
		panic(err)
	}

	CLIENT_ADDR := os.Getenv("GIT_SERVER_ADDR")

	err = gitClient.InitGitClient(CLIENT_ADDR)
	if err != nil {
		panic(err)
	}

	// wait for grpc client to be ready
	maxRetries := 5
	timeout := 2000 // milliseconds
	ready := false
	for i := 0; i < maxRetries; i++ {
		// request the status of the git server
		resp, err := gitClient.GitClient.HealthCheck(context.Background())
		if err == nil && resp.GetStatus() == "ok" {
			ready = true
			break
		} else {
			log.Printf("Status check failed retrying... (%d/%d)\n", i+1, maxRetries)
			time.Sleep(time.Duration(timeout) * time.Millisecond)
		}
	}
	if !ready {
		log.Fatal("Failed to connect to git server grpc after max retries")
	} else {
		log.Println("Git server is ready, health check passed")
	}

	// start the grpc server
}
