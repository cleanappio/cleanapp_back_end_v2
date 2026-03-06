package jwtx

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type AccessClaims struct {
	UserID string
	Type   string
	Exp    time.Time
}

func ParseAccessToken(tokenString, secret string) (*AccessClaims, error) {
	if secret == "" {
		return nil, errors.New("jwt secret not configured")
	}
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %T", token.Method)
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}
	tokenType, _ := claims["type"].(string)
	if tokenType == "refresh" {
		return nil, errors.New("refresh token not allowed")
	}
	userID, ok := claims["user_id"].(string)
	if !ok || userID == "" {
		return nil, errors.New("invalid user id in token")
	}
	expFloat, ok := claims["exp"].(float64)
	if !ok {
		return nil, errors.New("invalid exp in token")
	}
	exp := time.Unix(int64(expFloat), 0)
	if time.Now().After(exp) {
		return nil, errors.New("token expired")
	}
	return &AccessClaims{UserID: userID, Type: tokenType, Exp: exp}, nil
}
