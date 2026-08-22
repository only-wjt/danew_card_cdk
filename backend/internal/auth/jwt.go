package auth

import (
	"os"

	"github.com/golang-jwt/jwt/v5"
)

type CustomClaims struct {
	UserID   int64  `json:"user_id"`
	IsAdmin  bool   `json:"is_admin"`
	IsAgent  bool   `json:"is_agent"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func JWTSecret() string {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "your-secret-key-change-in-production"
	}
	return secret
}
