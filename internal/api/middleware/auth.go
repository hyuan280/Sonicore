package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/auth"
)

func writeCodedError(w http.ResponseWriter, status int, code domain.ErrorCode) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": code.DefaultMessage(),
		"code":  int(code),
	})
}

type ctxKey string

const (
	CtxUserID   ctxKey = "user_id"
	CtxUsername ctxKey = "username"
	CtxUserRole ctxKey = "user_role"
)

func AuthMiddleware(jwtService *auth.JWTService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeCodedError(w, http.StatusUnauthorized, domain.ErrMissingAuthHeader)
				return
			}

			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenStr == authHeader {
				writeCodedError(w, http.StatusUnauthorized, domain.ErrInvalidAuthFormat)
				return
			}

			claims, err := jwtService.Validate(tokenStr)
			if err != nil {
				writeCodedError(w, http.StatusUnauthorized, domain.ErrInvalidToken)
				return
			}

			ctx := context.WithValue(r.Context(), CtxUserID, claims.UserID)
			ctx = context.WithValue(ctx, CtxUsername, claims.Username)
			ctx = context.WithValue(ctx, CtxUserRole, string(claims.Role))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUserID(ctx context.Context) string {
	v, _ := ctx.Value(CtxUserID).(string)
	return v
}

func GetUsername(ctx context.Context) string {
	v, _ := ctx.Value(CtxUsername).(string)
	return v
}

func GetUserRole(ctx context.Context) string {
	v, _ := ctx.Value(CtxUserRole).(string)
	return v
}

func HasRole(ctx context.Context, role domain.Role) bool {
	return GetUserRole(ctx) == string(role)
}
