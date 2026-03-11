use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
    time::{Duration, Instant},
};

use axum::http::HeaderMap;

#[derive(Clone)]
pub struct TokenBucketLimiter {
    inner: Arc<TokenBucketLimiterInner>,
}

struct TokenBucketLimiterInner {
    rate_per_second: f64,
    burst: f64,
    buckets: Mutex<HashMap<String, BucketState>>,
}

struct BucketState {
    tokens: f64,
    last_refill: Instant,
}

impl TokenBucketLimiter {
    pub fn new(rate_per_second: f64, burst: usize) -> Self {
        Self {
            inner: Arc::new(TokenBucketLimiterInner {
                rate_per_second: rate_per_second.max(0.1),
                burst: burst.max(1) as f64,
                buckets: Mutex::new(HashMap::new()),
            }),
        }
    }

    pub fn allow(&self, key: &str) -> bool {
        let now = Instant::now();
        let mut buckets = self.inner.buckets.lock().expect("rate limiter mutex poisoned");
        let bucket = buckets.entry(key.to_string()).or_insert(BucketState {
            tokens: self.inner.burst,
            last_refill: now,
        });

        let elapsed = now.duration_since(bucket.last_refill).as_secs_f64();
        bucket.last_refill = now;
        bucket.tokens = (bucket.tokens + elapsed * self.inner.rate_per_second).min(self.inner.burst);
        if bucket.tokens < 1.0 {
            return false;
        }
        bucket.tokens -= 1.0;
        true
    }
}

#[derive(Clone)]
pub struct DetailAbuseMonitor {
    inner: Arc<DetailAbuseMonitorInner>,
}

struct DetailAbuseMonitorInner {
    window: Duration,
    max_hits: usize,
    max_misses: usize,
    entries: Mutex<HashMap<String, DetailWindow>>,
}

struct DetailWindow {
    started_at: Instant,
    hits: usize,
    misses: usize,
}

impl DetailAbuseMonitor {
    pub fn new(window: Duration, max_hits: usize, max_misses: usize) -> Self {
        Self {
            inner: Arc::new(DetailAbuseMonitorInner {
                window,
                max_hits,
                max_misses,
                entries: Mutex::new(HashMap::new()),
            }),
        }
    }

    pub fn record(&self, key: &str, found: bool) {
        let now = Instant::now();
        let mut entries = self.inner.entries.lock().expect("abuse monitor mutex poisoned");
        let entry = entries.entry(key.to_string()).or_insert(DetailWindow {
            started_at: now,
            hits: 0,
            misses: 0,
        });

        if now.duration_since(entry.started_at) > self.inner.window {
            entry.started_at = now;
            entry.hits = 0;
            entry.misses = 0;
        }

        if found {
            entry.hits += 1;
        } else {
            entry.misses += 1;
        }

        if entry.hits > self.inner.max_hits || entry.misses > self.inner.max_misses {
            tracing::warn!(
                client = %key,
                hits = entry.hits,
                misses = entry.misses,
                "suspicious public detail access pattern detected"
            );
        }
    }
}

pub fn client_key(headers: &HeaderMap) -> String {
    for header_name in ["x-forwarded-for", "x-real-ip", "cf-connecting-ip"] {
        if let Some(value) = headers.get(header_name) {
            if let Ok(raw) = value.to_str() {
                let ip = raw.split(',').next().unwrap_or("").trim();
                if !ip.is_empty() {
                    return ip.to_string();
                }
            }
        }
    }
    "unknown".to_string()
}
