package port

import "context"

type AuthService interface {
	Register(ctx context.Context, username, email, password string) (string, error)
	Login(ctx context.Context, username, password string) (string, error)
	ValidateToken(tokenStr string) (string, error)
}
