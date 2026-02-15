package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"report-listener/database"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

const (
	FetcherKeyPrefixLive = "cleanapp_fk_live_"
	FetcherKeyPrefixTest = "cleanapp_fk_test_"

	ctxFetcherID                = "fetcher_id"
	ctxFetcherKeyID             = "fetcher_key_id"
	ctxFetcherScopes            = "fetcher_scopes"
	ctxFetcherOwnerType         = "fetcher_owner_type"
	ctxFetcherStatus            = "fetcher_status"
	ctxFetcherTier              = "fetcher_tier"
	ctxFetcherDailyCap          = "fetcher_daily_cap_items"
	ctxFetcherMinuteCap         = "fetcher_per_minute_cap_items"
	ctxFetcherDefaultVisibility = "fetcher_default_visibility"
	ctxFetcherDefaultTrustLevel = "fetcher_default_trust_level"
	ctxFetcherRoutingEnabled    = "fetcher_routing_enabled"
	ctxFetcherRewardsEnabled    = "fetcher_rewards_enabled"
)

type FetcherScopes map[string]bool

func parseFetcherKey(raw string) (prefix string, keyID string, secret string, ok bool) {
	switch {
	case strings.HasPrefix(raw, FetcherKeyPrefixLive):
		prefix = FetcherKeyPrefixLive
		raw = strings.TrimPrefix(raw, FetcherKeyPrefixLive)
	case strings.HasPrefix(raw, FetcherKeyPrefixTest):
		prefix = FetcherKeyPrefixTest
		raw = strings.TrimPrefix(raw, FetcherKeyPrefixTest)
	default:
		return "", "", "", false
	}

	parts := strings.SplitN(raw, "_", 2)
	if len(parts) != 2 {
		return "", "", "", false
	}
	keyID = parts[0]
	secret = parts[1]
	if keyID == "" || secret == "" {
		return "", "", "", false
	}
	return prefix, keyID, secret, true
}

func allowedPrefix(fetcherKeyEnv string, prefix string) bool {
	switch strings.ToLower(strings.TrimSpace(fetcherKeyEnv)) {
	case "live", "prod", "production":
		return prefix == FetcherKeyPrefixLive
	case "test", "dev", "development":
		return prefix == FetcherKeyPrefixTest
	default:
		// Allow both in unknown envs (useful for local scripts).
		return prefix == FetcherKeyPrefixLive || prefix == FetcherKeyPrefixTest
	}
}

func hasAllScopes(scopes FetcherScopes, required []string) bool {
	for _, s := range required {
		if s == "" {
			continue
		}
		if !scopes[s] {
			return false
		}
	}
	return true
}

// FetcherKeyAuthV1 authenticates v1 fetcher API keys:
// Authorization: Bearer cleanapp_fk_{live|test}_{key_id}_{secret}
func FetcherKeyAuthV1(db *database.Database, fetcherKeyEnv string, requiredScopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid authorization header"})
			c.Abort()
			return
		}
		rawKey := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))

		prefix, keyID, secret, ok := parseFetcherKey(rawKey)
		if !ok || !allowedPrefix(fetcherKeyEnv, prefix) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			c.Abort()
			return
		}

		key, fetcher, err := db.GetFetcherKeyAndFetcherV1(c.Request.Context(), keyID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			c.Abort()
			return
		}

		// Ensure the key is bound to the expected environment prefix.
		if subtle.ConstantTimeCompare([]byte(key.KeyPrefix), []byte(prefix)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			c.Abort()
			return
		}

		// Key/fetcher status gating.
		if strings.ToLower(key.Status) != "active" {
			c.JSON(http.StatusForbidden, gin.H{"error": "api key revoked"})
			c.Abort()
			return
		}
		switch strings.ToLower(fetcher.Status) {
		case "active", "limited":
			// ok
		default:
			c.JSON(http.StatusForbidden, gin.H{"error": "fetcher suspended"})
			c.Abort()
			return
		}

		// Verify secret against hash.
		if err := bcrypt.CompareHashAndPassword([]byte(key.KeyHash), []byte(secret)); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			c.Abort()
			return
		}

		// Build scope map.
		scopeMap := make(FetcherScopes, len(key.Scopes))
		for _, s := range key.Scopes {
			s = strings.TrimSpace(s)
			if s != "" {
				scopeMap[s] = true
			}
		}
		if !hasAllScopes(scopeMap, requiredScopes) {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient scope"})
			c.Abort()
			return
		}

		// Compute effective caps (key overrides win).
		perMinute := fetcher.PerMinuteCapItems
		daily := fetcher.DailyCapItems
		if key.PerMinuteCapItems.Valid && int(key.PerMinuteCapItems.Int64) > 0 {
			perMinute = int(key.PerMinuteCapItems.Int64)
		}
		if key.DailyCapItems.Valid && int(key.DailyCapItems.Int64) > 0 {
			daily = int(key.DailyCapItems.Int64)
		}

		// Best-effort touch.
		db.TouchFetcherKeyV1(c.Request.Context(), fetcher.FetcherID, key.KeyID)

		c.Set(ctxFetcherID, fetcher.FetcherID)
		c.Set(ctxFetcherKeyID, key.KeyID)
		c.Set(ctxFetcherScopes, scopeMap)
		c.Set(ctxFetcherOwnerType, fetcher.OwnerType)
		c.Set(ctxFetcherStatus, fetcher.Status)
		c.Set(ctxFetcherTier, fetcher.Tier)
		c.Set(ctxFetcherMinuteCap, perMinute)
		c.Set(ctxFetcherDailyCap, daily)
		c.Set(ctxFetcherDefaultVisibility, strings.ToLower(strings.TrimSpace(fetcher.DefaultVisibility)))
		c.Set(ctxFetcherDefaultTrustLevel, strings.ToLower(strings.TrimSpace(fetcher.DefaultTrustLevel)))
		c.Set(ctxFetcherRoutingEnabled, fetcher.RoutingEnabled)
		c.Set(ctxFetcherRewardsEnabled, fetcher.RewardsEnabled)

		c.Next()
	}
}
