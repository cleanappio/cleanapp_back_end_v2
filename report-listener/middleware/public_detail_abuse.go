package middleware

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type PublicDetailAbuseConfig struct {
	Window    time.Duration
	MaxHits   int
	MaxMisses int
}

type publicDetailAbuseState struct {
	hits        []time.Time
	misses      []time.Time
	lastHitLog  time.Time
	lastMissLog time.Time
}

// PublicDetailAbuseMonitor emits server-side logs for suspicious detail walking.
func PublicDetailAbuseMonitor(cfg PublicDetailAbuseConfig) gin.HandlerFunc {
	if cfg.Window <= 0 {
		cfg.Window = 10 * time.Minute
	}
	if cfg.MaxHits <= 0 {
		cfg.MaxHits = 60
	}
	if cfg.MaxMisses <= 0 {
		cfg.MaxMisses = 12
	}

	var (
		mu     sync.Mutex
		states = map[string]*publicDetailAbuseState{}
	)

	trim := func(times []time.Time, cutoff time.Time) []time.Time {
		idx := 0
		for idx < len(times) && times[idx].Before(cutoff) {
			idx++
		}
		if idx == 0 {
			return times
		}
		return append([]time.Time(nil), times[idx:]...)
	}

	return func(c *gin.Context) {
		c.Next()

		if c.Request.Method != http.MethodGet {
			return
		}

		ip := c.ClientIP()
		now := time.Now()
		cutoff := now.Add(-cfg.Window)

		mu.Lock()
		state := states[ip]
		if state == nil {
			state = &publicDetailAbuseState{}
			states[ip] = state
		}

		state.hits = append(trim(state.hits, cutoff), now)
		if c.Writer.Status() == http.StatusNotFound {
			state.misses = append(trim(state.misses, cutoff), now)
		} else {
			state.misses = trim(state.misses, cutoff)
		}

		if len(state.hits) >= cfg.MaxHits && now.Sub(state.lastHitLog) >= time.Minute {
			log.Printf("public_detail_abuse_high_rate ip=%s path=%s hits=%d window=%s", ip, c.FullPath(), len(state.hits), cfg.Window)
			state.lastHitLog = now
		}
		if len(state.misses) >= cfg.MaxMisses && now.Sub(state.lastMissLog) >= time.Minute {
			log.Printf("public_detail_abuse_failed_lookups ip=%s path=%s misses=%d window=%s", ip, c.FullPath(), len(state.misses), cfg.Window)
			state.lastMissLog = now
		}

		for key, candidate := range states {
			candidate.hits = trim(candidate.hits, cutoff)
			candidate.misses = trim(candidate.misses, cutoff)
			if len(candidate.hits) == 0 && len(candidate.misses) == 0 {
				delete(states, key)
			}
		}
		mu.Unlock()
	}
}
