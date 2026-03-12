package gql

import (
	"net/http"
	"strings"
)

// AuthMiddleware extrait le Bearer token du header HTTP Authorization
// et l'injecte dans le context Go via ContextWithToken.
// Les resolvers GraphQL le transmettent ensuite aux métadonnées gRPC.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}
		ctx := ContextWithToken(r.Context(), token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

