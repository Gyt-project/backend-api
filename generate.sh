#!/bin/bash
# generate.sh — Génère le code Go depuis le proto GYT
#
# Pré-requis :
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
#   protoc installé (https://github.com/protocolbuffers/protobuf/releases)

set -e

PROTO_DIR="pkg/grpc"
OUT_DIR="pkg/grpc/gen"

mkdir -p "$OUT_DIR"

protoc \
  --go_out="$OUT_DIR" \
  --go_opt=paths=source_relative \
  --go-grpc_out="$OUT_DIR" \
  --go-grpc_opt=paths=source_relative \
  "$PROTO_DIR/service.proto"

echo "✓ Proto generated in $OUT_DIR"
echo ""
echo "Après la génération :"
echo "  1. Ajouter l'import '\"github.com/Gyt-project/backend-api/pkg/grpc/gen\"' dans cmd/main.go"
echo "  2. Décommenter pb.RegisterGytServiceServer(grpcServer, server.NewGytServer())"
echo "  3. Supprimer pkg/server/types.go (remplacé par les types générés)"
echo "  4. Mettre à jour pkg/server/server.go pour implémenter GytServiceServer généré"
