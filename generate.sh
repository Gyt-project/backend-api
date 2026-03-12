#!/bin/bash
# generate.sh — Régénère le code gRPC ET GraphQL depuis service.proto
#
# Pré-requis (à installer une seule fois) :
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
#   protoc : https://github.com/protocolbuffers/protobuf/releases
#
# Usage :
#   ./generate.sh          → régénère tout (gRPC + GraphQL)
#   ./generate.sh proto    → seulement gRPC
#   ./generate.sh graphql  → seulement GraphQL

set -e

STEP="${1:-all}"

# ─── Étape 1 : Régénérer les stubs gRPC ──────────────────────────────────────
generate_proto() {
  echo "▶ [1/2] Génération des stubs gRPC depuis service.proto..."

  protoc \
    --go_out=pkg/grpc \
    --go_opt=paths=source_relative \
    --go-grpc_out=pkg/grpc \
    --go-grpc_opt=paths=source_relative \
    -I pkg/grpc \
    pkg/grpc/service.proto

  echo "✅ gRPC stubs régénérés dans pkg/grpc/"
  echo ""
  echo "  ⚠️  Actions manuelles requises si tu as ajouté/modifié des RPCs :"
  echo "     → Implémenter les nouvelles méthodes dans pkg/server/server.go"
  echo "     → Mettre à jour les mappers dans pkg/server/mappers.go si nouveaux types"
}

# ─── Étape 2 : Régénérer le code GraphQL ─────────────────────────────────────
generate_graphql() {
  echo "▶ [2/2] Génération du code GraphQL depuis pkg/gql/schema.graphql..."

  go run github.com/99designs/gqlgen generate

  echo "✅ Code GraphQL régénéré dans pkg/gql/"
  echo ""
  echo "  ⚠️  Actions manuelles requises si tu as ajouté/modifié des RPCs :"
  echo "     → Mettre à jour pkg/gql/schema.graphql pour refléter le proto"
  echo "     → Implémenter les nouveaux resolvers dans pkg/gql/schema.resolvers.go"
  echo "     → Ajouter les mappers correspondants dans pkg/gql/mappers.go"
}

# ─── Dispatch ────────────────────────────────────────────────────────────────
case "$STEP" in
  proto)   generate_proto   ;;
  graphql) generate_graphql ;;
  all)
    generate_proto
    echo ""
    generate_graphql
    echo ""
    echo "🎉 Génération complète terminée."
    echo "   Lance 'go build ./...' pour vérifier qu'il n'y a pas d'erreurs."
    ;;
  *)
    echo "Usage: $0 [all|proto|graphql]"
    exit 1
    ;;
esac
