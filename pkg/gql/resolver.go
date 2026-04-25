package gql

import (
	"context"
	"net/http"
	"strings"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/Gyt-project/backend-api/internal/auth"
	"github.com/Gyt-project/backend-api/internal/service"
	pb "github.com/Gyt-project/backend-api/pkg/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

type contextKey string

const ContextKeyToken contextKey = "gql_token"

// Resolver holds the gRPC client and direct services for new features.
type Resolver struct {
	Client     pb.GytServiceClient
	BranchProt *service.BranchProtectionService
	PRSvc      *service.PRService
}

// NewHandler returns an http.Handler that serves the GraphQL API.
func NewHandler(client pb.GytServiceClient, branchProt *service.BranchProtectionService, prSvc *service.PRService) http.Handler {
	return handler.NewDefaultServer(NewExecutableSchema(Config{
		Resolvers: &Resolver{Client: client, BranchProt: branchProt, PRSvc: prSvc},
	}))
}

// NewPlaygroundHandler returns an http.Handler for the GraphQL playground UI.
func NewPlaygroundHandler(endpoint string) http.Handler {
	return playground.Handler("GraphQL Playground", endpoint)
}

// ContextWithToken injects a Bearer token into the context (called by the HTTP middleware).
func ContextWithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, ContextKeyToken, token)
}

// grpcCtx forwards the Bearer token from the GraphQL context to gRPC outgoing metadata.
func grpcCtx(ctx context.Context) context.Context {
	token, _ := ctx.Value(ContextKeyToken).(string)
	if token == "" {
		return ctx
	}
	md := metadata.Pairs("authorization", "Bearer "+token)
	return metadata.NewOutgoingContext(ctx, md)
}

// extractCallerID extracts the caller's user ID from the JWT in the GraphQL context.
func extractCallerID(ctx context.Context) (uint, error) {
	token, _ := ctx.Value(ContextKeyToken).(string)
	if token == "" {
		return 0, grpcstatus.Error(codes.Unauthenticated, "user not authenticated")
	}
	token = strings.TrimPrefix(token, "Bearer ")
	claims, err := auth.ParseAccessToken(token)
	if err != nil {
		return 0, grpcstatus.Error(codes.Unauthenticated, "invalid token")
	}
	return claims.UserID, nil
}
