package authx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"cleanapp-common/jwtx"
)

var ErrInvalidToken = errors.New("invalid or expired token")

type Result struct {
	UserID string
	Exp    time.Time
}

func VerifyAccessToken(ctx context.Context, db *sql.DB, token, jwtSecret string) (*Result, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	claims, err := jwtx.ParseAccessToken(token, jwtSecret)
	if err != nil {
		return nil, ErrInvalidToken
	}

	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM auth_tokens
		WHERE user_id = ?
		  AND token_hash = ?
		  AND token_type = 'access'
		  AND expires_at > NOW()
	`, claims.UserID, jwtx.HashToken(token)).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("verify access token in db: %w", err)
	}
	if count == 0 {
		return nil, ErrInvalidToken
	}

	return &Result{UserID: claims.UserID, Exp: claims.Exp}, nil
}
