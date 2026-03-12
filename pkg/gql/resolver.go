package gql
import (
"context"
"net/http"
"github.com/99designs/gqlgen/graphql/handler"
"github.com/99designs/gqlgen/graphql/playground"
pb "github.com/Gyt-project/backend-api/pkg/grpc"
"google.golang.org/grpc/metadata"
)
type contextKey string
const ContextKeyToken contextKey = "gql_token"
// Resolver holds the gRPC client and serves as the root GraphQL resolver.
type Resolver struct {
Client pb.GytServiceClient
}
// NewHandler returns an http.Handler that serves the GraphQL API.
func NewHandler(client pb.GytServiceClient) http.Handler {
return handler.NewDefaultServer(NewExecutableSchema(Config{
Resolvers: &Resolver{Client: client},
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
