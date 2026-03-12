# ═══════════════════════════════════════════════════════════════
# Makefile — Backend API + GraphQL Gateway
# ═══════════════════════════════════════════════════════════════

.PHONY: help proto graphql generate build run-backend run-gateway tidy

# ─── Aide ────────────────────────────────────────────────────────────────────
help:
	@echo ""
	@echo "  Gyt Backend — Commandes disponibles"
	@echo "  ────────────────────────────────────"
	@echo "  make proto        → Régénère les stubs gRPC depuis service.proto"
	@echo "  make graphql      → Régénère le code GraphQL depuis schema.graphql"
	@echo "  make generate     → Fait les deux (proto + graphql)"
	@echo "  make build        → Compile tous les services"
	@echo "  make run-backend  → Lance le serveur gRPC"
	@echo "  make run-gateway  → Lance l'API gateway GraphQL"
	@echo "  make tidy         → go mod tidy"
	@echo ""

# ─── Génération ──────────────────────────────────────────────────────────────

## 1. Régénère service.pb.go + service_grpc.pb.go depuis service.proto
proto:
	@echo "▶ [proto] Régénération des stubs gRPC..."
	protoc \
		--go_out=pkg/grpc \
		--go_opt=paths=source_relative \
		--go-grpc_out=pkg/grpc \
		--go-grpc_opt=paths=source_relative \
		-I pkg/grpc \
		pkg/grpc/service.proto
	@echo "✅ gRPC stubs régénérés dans pkg/grpc/"

## 2. Régénère generated.go + models_gen.go depuis schema.graphql
graphql:
	@echo "▶ [graphql] Régénération du code GraphQL..."
	go run github.com/99designs/gqlgen generate
	@echo "✅ Code GraphQL régénéré dans pkg/gql/"

## 3. Les deux en séquence
generate: proto graphql
	@echo ""
	@echo "🎉 Génération complète. Lance 'make build' pour vérifier."

# ─── Build & Run ─────────────────────────────────────────────────────────────

build:
	@echo "▶ Compilation de tous les services..."
	go build ./...
	@echo "✅ Build OK"

run-backend:
	go run ./cmd/main.go

run-gateway:
	go run ./cmd/gateway/main.go

tidy:
	go mod tidy

