package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"montenegro-areas/config"
)

// AuthMiddleware validates JWT tokens for protected routes by calling auth-service
func AuthMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				log.Printf("WARNING: Missing authorization header from %s", r.RemoteAddr)
				http.Error(w, "missing authorization header", http.StatusUnauthorized)
				return
			}

			tokenString := extractToken(authHeader)
			if tokenString == "" {
				log.Printf("WARNING: Invalid authorization format from %s", r.RemoteAddr)
				http.Error(w, "invalid authorization format", http.StatusUnauthorized)
				return
			}

			log.Printf("DEBUG: Validating token from %s", r.RemoteAddr)

			// Call auth-service to validate token
			valid, userID, err := validateTokenWithAuthService(tokenString, cfg.AuthServiceURL)
			if err != nil {
				log.Printf("ERROR: Failed to validate token with auth-service from %s: %v", r.RemoteAddr, err)
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			if !valid {
				log.Printf("WARNING: Invalid token from %s", r.RemoteAddr)
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			log.Printf("DEBUG: Token validated successfully for user %s from %s", userID, r.RemoteAddr)

			// Add user ID to request context
			ctx := r.Context()
			ctx = context.WithValue(ctx, "user_id", userID)
			ctx = context.WithValue(ctx, "token", tokenString)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractToken(authHeader string) string {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ""
	}
	return parts[1]
}

func validateTokenWithAuthService(token string, authServiceURL string) (bool, string, error) {
	url := authServiceURL + "/api/v3/validate-token"
	payload := map[string]string{"token": token}
	body, _ := json.Marshal(payload)

	log.Printf("DEBUG: Calling auth-service to validate token: %s", url)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Printf("ERROR: Failed to call auth-service for token validation: %v", err)
		return false, "", err
	}
	defer resp.Body.Close()

	log.Printf("DEBUG: Auth-service token validation response: %d", resp.StatusCode)

	var result struct {
		Valid      bool   `json:"valid"`
		UserID     string `json:"user_id"`
		CustomerID string `json:"customer_id"`
		Error      string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("ERROR: Failed to decode auth-service response: %v", err)
		return false, "", err
	}

	// Accept either user_id or customer_id for compatibility
	id := result.CustomerID
	if id == "" {
		id = result.UserID
	}

	log.Printf("DEBUG: Token validation result - Valid: %t, ID: %s", result.Valid, id)
	return result.Valid, id, nil
}

// GetUserIDFromContext extracts user ID from request context
func GetUserIDFromContext(r *http.Request) string {
	if userID, ok := r.Context().Value("user_id").(string); ok {
		return userID
	}
	return ""
}

// GetTokenFromContext extracts token from request context
func GetTokenFromContext(r *http.Request) string {
	if token, ok := r.Context().Value("token").(string); ok {
		return token
	}
	return ""
}
