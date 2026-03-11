package edge

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type CORSConfig struct {
	AllowedOrigins   []string
	AllowCredentials bool
	AllowedHeaders   []string
	AllowedMethods   []string
	ExposedHeaders   []string
}

type RateLimitConfig struct {
	RPS   float64
	Burst int
	TTL   time.Duration
}

type clientBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func originAllowed(origin string, allowed []string) bool {
	if origin == "" {
		return false
	}
	for _, candidate := range allowed {
		switch {
		case candidate == "*":
			return true
		case strings.EqualFold(candidate, origin):
			return true
		case strings.HasPrefix(candidate, "*."):
			suffix := strings.TrimPrefix(candidate, "*")
			if strings.HasSuffix(strings.ToLower(origin), strings.ToLower(suffix)) {
				return true
			}
		}
	}
	return false
}

func CORSMiddleware(cfg CORSConfig) gin.HandlerFunc {
	headers := cfg.AllowedHeaders
	if len(headers) == 0 {
		headers = []string{"Content-Type", "Content-Length", "Accept-Encoding", "X-CSRF-Token", "Authorization", "Accept", "Origin", "Cache-Control", "X-Requested-With", "X-Request-Id", "X-Internal-Admin-Token"}
	}
	methods := cfg.AllowedMethods
	if len(methods) == 0 {
		methods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && originAllowed(origin, cfg.AllowedOrigins) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			if cfg.AllowCredentials {
				c.Header("Access-Control-Allow-Credentials", "true")
			}
		}
		c.Header("Access-Control-Allow-Headers", strings.Join(headers, ", "))
		c.Header("Access-Control-Allow-Methods", strings.Join(methods, ", "))
		if len(cfg.ExposedHeaders) > 0 {
			c.Header("Access-Control-Expose-Headers", strings.Join(cfg.ExposedHeaders, ", "))
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		c.Header("Cross-Origin-Opener-Policy", "same-origin")
		c.Header("Cross-Origin-Resource-Policy", "same-site")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Next()
	}
}

func RequestBodyLimit(maxBytes int64) gin.HandlerFunc {
	if maxBytes <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}

func RateLimitMiddleware(cfg RateLimitConfig) gin.HandlerFunc {
	if cfg.RPS <= 0 || cfg.Burst <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 10 * time.Minute
	}
	var (
		mu      sync.Mutex
		buckets = map[string]*clientBucket{}
	)
	cleanup := func(now time.Time) {
		for key, bucket := range buckets {
			if now.Sub(bucket.lastSeen) > cfg.TTL {
				delete(buckets, key)
			}
		}
	}
	return func(c *gin.Context) {
		key := c.ClientIP()
		now := time.Now()
		mu.Lock()
		bucket, ok := buckets[key]
		if !ok {
			bucket = &clientBucket{limiter: rate.NewLimiter(rate.Limit(cfg.RPS), cfg.Burst)}
			buckets[key] = bucket
		}
		bucket.lastSeen = now
		cleanup(now)
		allowed := bucket.limiter.Allow()
		mu.Unlock()
		if !allowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}

func WebSocketOriginChecker(allowed []string) func(r *http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return false
		}
		return originAllowed(origin, allowed)
	}
}
