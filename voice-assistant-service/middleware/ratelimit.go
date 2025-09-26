package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

// RateLimiter implements a simple in-memory rate limiter
type RateLimiter struct {
	requests map[string][]time.Time
	mutex    sync.RWMutex
	limit    int
	window   time.Duration
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

// Allow checks if a request from the given key should be allowed
func (rl *RateLimiter) Allow(key string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Clean up old requests
	if requests, exists := rl.requests[key]; exists {
		var validRequests []time.Time
		for _, reqTime := range requests {
			if reqTime.After(cutoff) {
				validRequests = append(validRequests, reqTime)
			}
		}
		rl.requests[key] = validRequests
	} else {
		rl.requests[key] = []time.Time{}
	}

	// Check if we're under the limit
	if len(rl.requests[key]) < rl.limit {
		rl.requests[key] = append(rl.requests[key], now)
		return true
	}

	return false
}

// RateLimitMiddleware creates a rate limiting middleware
func RateLimitMiddleware(limit int, window time.Duration) gin.HandlerFunc {
	limiter := NewRateLimiter(limit, window)

	return func(c *gin.Context) {
		// Use IP address as the key for rate limiting
		clientIP := c.ClientIP()
		
		if !limiter.Allow(clientIP) {
			log.Warnf("Rate limit exceeded for IP: %s", clientIP)
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded",
				"retry_after": int(window.Seconds()),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// UserRateLimitMiddleware creates a rate limiting middleware based on user ID
func UserRateLimitMiddleware(limit int, window time.Duration) gin.HandlerFunc {
	limiter := NewRateLimiter(limit, window)

	return func(c *gin.Context) {
		// Use IP address for rate limiting since we don't have authentication
		userID := c.ClientIP()
		
		if !limiter.Allow(userID) {
			log.Warnf("Rate limit exceeded for user: %s", userID)
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded",
				"retry_after": int(window.Seconds()),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
