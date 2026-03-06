package middleware

import (
	"bytes"
	"cleanapp-common/edge"
	"cleanapp-common/jwtx"
	"context"
	"crypto/sha256"
	"customer-service/config"
	"customer-service/database"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var authServiceHTTPClient = &http.Client{
	Timeout: 3 * time.Second,
}

type tokenValidationCacheEntry struct {
	UserID    string
	ExpiresAt time.Time
}

var tokenValidationCache = struct {
	sync.RWMutex
	entries map[string]tokenValidationCacheEntry
}{
	entries: map[string]tokenValidationCacheEntry{},
}

// AuthMiddleware validates JWT tokens for protected routes by calling auth-service
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			log.Printf("WARNING: Missing authorization header from %s", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		tokenString := extractToken(authHeader)
		if tokenString == "" {
			log.Printf("WARNING: Invalid authorization format from %s", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format"})
			c.Abort()
			return
		}

		customerID, err := validateToken(c.Request.Context(), tokenString, cfg)
		if err != nil {
			log.Printf("ERROR: Failed to validate token with auth-service from %s: %v", c.ClientIP(), err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}
		c.Set("customer_id", customerID)
		c.Set("token", tokenString)
		c.Next()
	}
}

func extractToken(authHeader string) string {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ""
	}
	return parts[1]
}

func validateToken(ctx context.Context, token string, cfg *config.Config) (string, error) {
	claims, err := jwtx.ParseAccessToken(token, cfg.JWTSecret)
	if err != nil {
		return "", err
	}
	cacheTTL, err := time.ParseDuration(cfg.AuthValidationCacheTTL)
	if err != nil || cacheTTL <= 0 {
		cacheTTL = 30 * time.Second
	}
	cacheKey := hashTokenForCache(token)
	if userID, ok := getCachedValidation(cacheKey); ok {
		return userID, nil
	}
	valid, customerID, err := validateTokenWithAuthService(ctx, token, cfg.AuthServiceURL)
	if err != nil {
		return "", err
	}
	if !valid {
		return "", errInvalidToken
	}
	expiresAt := time.Now().Add(cacheTTL)
	if claims.Exp.Before(expiresAt) {
		expiresAt = claims.Exp
	}
	setCachedValidation(cacheKey, tokenValidationCacheEntry{
		UserID:    customerID,
		ExpiresAt: expiresAt,
	})
	return customerID, nil
}

var errInvalidToken = errors.New("invalid token")

func validateTokenWithAuthService(ctx context.Context, token string, authServiceURL string) (bool, string, error) {
	url := authServiceURL + "/api/v3/validate-token"
	payload := map[string]string{"token": token}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		log.Printf("ERROR: Failed to create auth-service request for token validation: %v", err)
		return false, "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := authServiceHTTPClient.Do(req)
	if err != nil {
		log.Printf("ERROR: Failed to call auth-service for token validation: %v", err)
		return false, "", err
	}
	defer resp.Body.Close()

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

	return result.Valid, id, nil
}

// RequireSubscription middleware checks if customer has active subscription
func RequireSubscription(service *database.CustomerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		customerID := c.GetString("customer_id")
		if customerID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}

		// Check subscription status (implement this method in service)
		// hasActive := service.HasActiveSubscription(c.Request.Context(), customerID)
		// if !hasActive {
		//     c.JSON(http.StatusPaymentRequired, gin.H{"error": "active subscription required"})
		//     c.Abort()
		//     return
		// }

		c.Next()
	}
}

// RateLimitMiddleware implements basic rate limiting
func RateLimitMiddleware(rps float64, burst int) gin.HandlerFunc {
	return edge.RateLimitMiddleware(edge.RateLimitConfig{
		RPS:   rps,
		Burst: burst,
	})
}

// CORSMiddleware handles CORS headers
func CORSMiddleware(allowedOrigins []string) gin.HandlerFunc {
	return edge.CORSMiddleware(edge.CORSConfig{
		AllowedOrigins:   allowedOrigins,
		AllowCredentials: true,
	})
}

// SecurityHeaders adds security headers to responses
func SecurityHeaders() gin.HandlerFunc {
	return edge.SecurityHeaders()
}

func hashTokenForCache(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func getCachedValidation(key string) (string, bool) {
	now := time.Now()
	tokenValidationCache.RLock()
	entry, ok := tokenValidationCache.entries[key]
	tokenValidationCache.RUnlock()
	if !ok || now.After(entry.ExpiresAt) {
		return "", false
	}
	return entry.UserID, true
}

func setCachedValidation(key string, entry tokenValidationCacheEntry) {
	tokenValidationCache.Lock()
	tokenValidationCache.entries[key] = entry
	tokenValidationCache.Unlock()
}
