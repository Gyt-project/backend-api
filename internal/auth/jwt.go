package auth

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Clés de contexte pour injecter les claims dans le contexte gRPC
type contextKey string

const (
	ContextKeyUserUUID contextKey = "user_uuid"
	ContextKeyUserID   contextKey = "user_id"
	ContextKeyIsAdmin  contextKey = "is_admin"
)

// Claims JWT personnalisés
type Claims struct {
	UserUUID string `json:"user_uuid"`
	UserID   uint   `json:"user_id"`
	IsAdmin  bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

// jwtSecret retourne le secret depuis l'environnement
func jwtSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "changeme-in-production" // fallback dev uniquement
	}
	return []byte(secret)
}

// AccessTokenDuration durée de vie du token d'accès
const AccessTokenDuration = 15 * time.Minute

// RefreshTokenDuration durée de vie du refresh token
const RefreshTokenDuration = 7 * 24 * time.Hour

// GenerateAccessToken génère un JWT d'accès
func GenerateAccessToken(userUUID string, userID uint, isAdmin bool) (string, error) {
	claims := Claims{
		UserUUID: userUUID,
		UserID:   userID,
		IsAdmin:  isAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userUUID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(AccessTokenDuration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret())
}

// GenerateRefreshToken génère un JWT de rafraîchissement (longue durée, claims minimaux)
func GenerateRefreshToken(userUUID string) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   userUUID,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(RefreshTokenDuration)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret())
}

// ParseAccessToken valide et parse un token d'accès
func ParseAccessToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jwtSecret(), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// ParseRefreshToken valide un refresh token et retourne le subject (userUUID)
func ParseRefreshToken(tokenStr string) (string, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &jwt.RegisteredClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jwtSecret(), nil
	})
	if err != nil {
		return "", err
	}
	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok || !token.Valid {
		return "", errors.New("invalid refresh token")
	}
	return claims.Subject, nil
}

// méthodes publiques (pas d'auth requise)
var publicMethods = map[string]bool{
	"/gyt.GytService/Register": true,
	"/gyt.GytService/Login":    true,
}

// UnaryAuthInterceptor est un intercepteur gRPC qui valide le JWT sur toutes les
// méthodes sauf celles de la liste publique.
func UnaryAuthInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	if publicMethods[info.FullMethod] {
		return handler(ctx, req)
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	authHeaders := md.Get("authorization")
	if len(authHeaders) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization header")
	}

	bearer := authHeaders[0]
	if !strings.HasPrefix(bearer, "Bearer ") {
		return nil, status.Error(codes.Unauthenticated, "invalid authorization format, expected Bearer <token>")
	}

	tokenStr := strings.TrimPrefix(bearer, "Bearer ")
	claims, err := ParseAccessToken(tokenStr)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
	}

	// Injecter les claims dans le contexte
	ctx = context.WithValue(ctx, ContextKeyUserUUID, claims.UserUUID)
	ctx = context.WithValue(ctx, ContextKeyUserID, claims.UserID)
	ctx = context.WithValue(ctx, ContextKeyIsAdmin, claims.IsAdmin)

	return handler(ctx, req)
}

// ExtractUserUUID extrait le UUID de l'utilisateur depuis le contexte
func ExtractUserUUID(ctx context.Context) (string, error) {
	v := ctx.Value(ContextKeyUserUUID)
	if v == nil {
		return "", status.Error(codes.Unauthenticated, "user not authenticated")
	}
	return v.(string), nil
}

// ExtractUserID extrait l'ID DB de l'utilisateur depuis le contexte
func ExtractUserID(ctx context.Context) (uint, error) {
	v := ctx.Value(ContextKeyUserID)
	if v == nil {
		return 0, status.Error(codes.Unauthenticated, "user not authenticated")
	}
	return v.(uint), nil
}

// ExtractIsAdmin extrait le flag admin depuis le contexte
func ExtractIsAdmin(ctx context.Context) bool {
	v := ctx.Value(ContextKeyIsAdmin)
	if v == nil {
		return false
	}
	return v.(bool)
}
