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

// ParseToken 校验 HMAC 签名与有效期，返回声明。
func ParseToken(tokenStr string) (*CustomClaims, error) {
	claims := &CustomClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(JWTSecret()), nil
	})
	if err != nil || !token.Valid {
		if err == nil {
			err = jwt.ErrTokenInvalidClaims
		}
		return nil, err
	}
	return claims, nil
}
