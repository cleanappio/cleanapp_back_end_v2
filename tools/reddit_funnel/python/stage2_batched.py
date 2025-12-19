#!/usr/bin/env python3
"""
Reddit Funnel - Stage 2 Batched: High-Throughput LLM Enrichment

=============================================================================
RUNBOOK
=============================================================================
Recommended settings for production:
  --workers 20
  --batch-size 10
  --rps 5 (per worker)

Expected throughput:
  - 10 items/batch × 5 batches/sec/worker × 20 workers = ~1000 items/sec peak
  - Realistic with rate limits: 100-500 items/sec
  - 2.5M items in 1-7 hours

10K validation command:
  python3 stage2_batched.py \
    --input output_full/routed.jsonl \
    --output output_full/batched_test.jsonl \
    --gemini-key "$GEMINI_KEY" \
    --max-items 10000 \
    --batch-size 10 \
    --rps 10

Sample output JSONL line:
{"id":"abc123","subreddit":"apple","canonical_brand":"apple","brand_confidence":0.95,
 "issue_type":"bug","severity":"medium","is_actionable":true,"summary":"iPhone crashes...",
 "cluster_key":"apple-ios-crash","raw_issue_type":"bug","worker_id":1,"processed_at":"2024-12-19T09:00:00Z"}
=============================================================================
"""

import argparse
import json
import os
import time
import random
import fcntl
from datetime import datetime
from pathlib import Path
from typing import Optional, List, Dict, Set, Tuple
import requests

# Canonical enums
CANONICAL_ISSUE_TYPES = [
    "bug", "outage", "ux", "account", "billing", "policy",
    "security", "performance", "feature_request", "other", "not_applicable"
]

CANONICAL_SEVERITIES = ["none", "low", "medium", "high"]

# Issue type mappings (Option B)
ISSUE_TYPE_MAPPINGS = {
    "promotion": "not_applicable",
    "promotional": "not_applicable",
    "gameplay": "ux",
    "hardware": "bug",
    "compatibility": "bug",
    "discussion": "not_applicable",
    "general discussion": "not_applicable",
    "general": "other",
    "question": "other",
    "community": "not_applicable",
    "none": "not_applicable",
    "na": "not_applicable",
    "": "other"
}

# Severity normalization
SEVERITY_MAPPINGS = {
    "minor": "low",
    "moderate": "medium",
    "major": "high",
    "critical": "high",
    "severe": "high",
    "serious": "high",
    "urgent": "high"
}

# Gemini response schema for batched output
# Note: Gemini API uses simplified schema format, not full JSON Schema
BATCH_RESPONSE_SCHEMA = {
    "type": "object",
    "properties": {
        "results": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "id": {"type": "string"},
                    "canonical_brand": {"type": "string"},
                    "brand_confidence": {"type": "number"},
                    "issue_type": {"type": "string"},
                    "severity": {"type": "string"},
                    "is_actionable": {"type": "boolean"},
                    "summary": {"type": "string"},
                    "cluster_key": {"type": "string"}
                },
                "required": ["id", "issue_type", "severity", "is_actionable", "summary", "cluster_key"]
            }
        }
    },
    "required": ["results"]
}

BATCH_SYSTEM_PROMPT = """You are extracting brand-addressable issue reports for CleanApp.
CleanApp crowdsources user feedback about specific brands and forwards it to those brands.

You will receive MULTIPLE items to analyze. Return one result per item with matching id.

## BRAND EXTRACTION (CRITICAL)

You MUST extract a brand if ANY of these are present:
- A company or product name is mentioned (Apple, Steam, Discord, Google, Amazon, etc.)
- The subreddit is brand-specific (e.g. r/apple → brand="apple", r/steam → brand="steam")
- Brand hints are provided from Stage 1 detection

ONLY return canonical_brand=null if the post is truly generic with NO brand context.
Cross-reference with the brand hints provided and the subreddit name.

## ISSUE TYPE (REQUIRED - must be one of these)

MUST be one of: bug, outage, ux, account, billing, policy, security, performance, feature_request, other, not_applicable

- bug: software errors, crashes, glitches, defects
- outage: service down, unavailable, server issues  
- ux: user experience problems, confusing UI, hard to use
- account: login issues, banned, suspended, password problems
- billing: payment issues, charges, refunds, subscription problems
- policy: terms violations, moderation issues, content removal
- security: privacy concerns, hacks, data breaches
- performance: slow, laggy, timeout issues
- feature_request: suggestions, wishes, improvement requests
- other: doesn't fit above categories
- not_applicable: not a brand issue, promotional, general discussion

If uncertain, use "other" - NEVER leave issue_type empty.

## SEVERITY (REQUIRED - must be one of these)

MUST be one of: none, low, medium, high

## ACTIONABLE

is_actionable = true ONLY if user is reporting a real problem a brand could address.
is_actionable = false for general discussion, memes, promotional content.

## OUTPUT FORMAT

Return JSON with "results" array. Each result must have matching "id" from input.
Example: {"results": [{"id": "abc", "canonical_brand": "apple", ...}, {"id": "def", ...}]}"""


def parse_args():
    parser = argparse.ArgumentParser(description="Stage 2 Batched: High-Throughput LLM Enrichment")
    parser.add_argument("--input", required=True, help="Path to routed.jsonl from Stage 1")
    parser.add_argument("--output", required=True, help="Path to write enriched JSONL")
    parser.add_argument("--gemini-key", required=True, help="Gemini API key")
    parser.add_argument("--gemini-model", default="gemini-2.0-flash", help="Gemini model")
    parser.add_argument("--batch-size", type=int, default=10, help="Items per Gemini call")
    parser.add_argument("--max-items", type=int, default=None, help="Max items to process (for testing)")
    parser.add_argument("--rps", type=float, default=5.0, help="Requests per second")
    parser.add_argument("--max-retries", type=int, default=5, help="Max retries per batch")
    parser.add_argument("--worker-id", type=int, default=1, help="Worker ID (1-indexed)")
    parser.add_argument("--total-workers", type=int, default=1, help="Total number of workers")
    parser.add_argument("--skip-existing", action="store_true", help="Skip already processed IDs")
    parser.add_argument("--log-interval", type=int, default=100, help="Log progress every N batches")
    return parser.parse_args()


def load_existing_ids(output_path: Path) -> Set[str]:
    """Load IDs already processed for resume capability."""
    existing = set()
    if output_path.exists():
        with open(output_path, "r") as f:
            for line in f:
                try:
                    item = json.loads(line.strip())
                    if "id" in item:
                        existing.add(item["id"])
                except:
                    continue
    return existing


def build_batch_prompt(items: List[Dict]) -> str:
    """Build prompt for batch of items."""
    prompt_items = []
    for item in items:
        item_str = f"""---
ID: {item.get('id', '')}
Subreddit: r/{item.get('subreddit', '')}
Title: {item.get('title', '')}
Content: {item.get('body', '')[:1500]}
Brand hints: {', '.join(item.get('brand_hints', [])) or 'none'}
---"""
        prompt_items.append(item_str)
    
    return f"""Analyze these {len(items)} Reddit posts for CleanApp.
Return a JSON object with "results" array containing exactly {len(items)} items, one per input ID.

ITEMS TO ANALYZE:
{''.join(prompt_items)}

Return JSON with "results" array. Each result must have:
- id (matching input)
- canonical_brand (string or null)
- brand_confidence (0.0-1.0)
- issue_type (one of: bug, outage, ux, account, billing, policy, security, performance, feature_request, other, not_applicable)
- severity (one of: none, low, medium, high)
- is_actionable (boolean)
- summary (under 240 chars)
- cluster_key (grouping key)

Example output format:
{{"results": [{{"id": "abc", "canonical_brand": "apple", "brand_confidence": 0.9, "issue_type": "bug", "severity": "medium", "is_actionable": true, "summary": "App crashes on launch", "cluster_key": "apple-ios-crash"}}]}}"""


def call_gemini_batch(api_key: str, model: str, prompt: str, max_retries: int = 3) -> Optional[Dict]:
    """Call Gemini API with schema enforcement for batched output."""
    url = f"https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent"
    
    payload = {
        "generationConfig": {
            "response_mime_type": "application/json",
            "response_schema": BATCH_RESPONSE_SCHEMA
        },
        "contents": [{
            "role": "user",
            "parts": [
                {"text": BATCH_SYSTEM_PROMPT},
                {"text": prompt}
            ]
        }]
    }
    
    for attempt in range(max_retries):
        try:
            resp = requests.post(
                f"{url}?key={api_key}",
                json=payload,
                timeout=120
            )
            
            if resp.status_code == 429:
                wait_time = (2 ** attempt) + random.uniform(0, 1)
                print(f"  Rate limited, waiting {wait_time:.1f}s...")
                time.sleep(wait_time)
                continue
                
            if resp.status_code != 200:
                print(f"  API error {resp.status_code}: {resp.text[:200]}")
                time.sleep(1)
                continue
                
            data = resp.json()
            if "candidates" not in data or not data["candidates"]:
                continue
                
            text = data["candidates"][0]["content"]["parts"][0]["text"]
            result = json.loads(text)
            
            # Handle various response formats
            if isinstance(result, list):
                result = {"results": result}
            elif isinstance(result, dict) and "results" not in result:
                # Single result, wrap it
                result = {"results": [result]}
            
            return result if isinstance(result, dict) else None
            
        except json.JSONDecodeError as e:
            print(f"  JSON parse error: {e}")
            time.sleep(1)
        except Exception as e:
            print(f"  Error: {e}")
            if attempt < max_retries - 1:
                time.sleep(1)
    
    return None


def validate_batch_response(response: Dict, input_ids: List[str]) -> Tuple[bool, str]:
    """Validate that response has exactly one result per input ID."""
    if not response or "results" not in response:
        return False, "missing results array"
    
    results = response["results"]
    if not isinstance(results, list):
        return False, "results is not an array"
    
    if len(results) != len(input_ids):
        return False, f"expected {len(input_ids)} results, got {len(results)}"
    
    result_ids = set()
    for r in results:
        if not isinstance(r, dict):
            return False, "result is not an object"
        if "id" not in r:
            return False, "result missing id"
        result_ids.add(r["id"])
    
    input_id_set = set(input_ids)
    if result_ids != input_id_set:
        missing = input_id_set - result_ids
        extra = result_ids - input_id_set
        return False, f"id mismatch: missing={missing}, extra={extra}"
    
    return True, "ok"


def normalize_result(result: Dict) -> Dict:
    """Apply taxonomy mappings and normalize result."""
    # Issue type mapping
    raw_issue = result.get("issue_type", "other") or "other"
    raw_issue_lower = raw_issue.lower().strip()
    
    # Apply mapping
    if raw_issue_lower in ISSUE_TYPE_MAPPINGS:
        normalized_issue = ISSUE_TYPE_MAPPINGS[raw_issue_lower]
    elif raw_issue_lower in CANONICAL_ISSUE_TYPES:
        normalized_issue = raw_issue_lower
    else:
        normalized_issue = "other"
    
    # Severity mapping
    raw_severity = result.get("severity", "none") or "none"
    raw_severity_lower = raw_severity.lower().strip()
    
    if raw_severity_lower in SEVERITY_MAPPINGS:
        normalized_severity = SEVERITY_MAPPINGS[raw_severity_lower]
    elif raw_severity_lower in CANONICAL_SEVERITIES:
        normalized_severity = raw_severity_lower
    else:
        normalized_severity = "medium"
    
    result["raw_issue_type"] = raw_issue
    result["issue_type"] = normalized_issue
    result["raw_severity"] = raw_severity
    result["severity"] = normalized_severity
    
    # Ensure is_actionable is boolean
    result["is_actionable"] = bool(result.get("is_actionable", False))
    
    return result


def process_batch_with_retry(
    items: List[Dict],
    api_key: str,
    model: str,
    max_retries: int,
    worker_id: int
) -> List[Dict]:
    """Process a batch with adaptive retry and split on failure."""
    
    if not items:
        return []
    
    input_ids = [item["id"] for item in items]
    prompt = build_batch_prompt(items)
    
    # Try full batch
    for attempt in range(max_retries):
        response = call_gemini_batch(api_key, model, prompt, max_retries=2)
        
        if response:
            valid, reason = validate_batch_response(response, input_ids)
            if valid:
                # Normalize and return results
                results = []
                for r in response["results"]:
                    r = normalize_result(r)
                    r["worker_id"] = worker_id
                    r["processed_at"] = datetime.utcnow().isoformat() + "Z"
                    results.append(r)
                return results
            else:
                print(f"  Batch validation failed: {reason}")
    
    # Batch failed - split if possible
    if len(items) > 1:
        mid = len(items) // 2
        print(f"  Splitting batch {len(items)} -> {mid} + {len(items) - mid}")
        left = process_batch_with_retry(items[:mid], api_key, model, max_retries, worker_id)
        right = process_batch_with_retry(items[mid:], api_key, model, max_retries, worker_id)
        return left + right
    
    # Single item failed - last resort with stricter prompt
    print(f"  Single item failed: {items[0].get('id')}")
    return []


def append_results_with_lock(output_path: Path, results: List[Dict], original_items: Dict[str, Dict]):
    """Append results to output file with file locking."""
    if not results:
        return
    
    lines = []
    for r in results:
        item_id = r.get("id", "")
        original = original_items.get(item_id, {})
        
        # Merge with original metadata
        enriched = {
            "id": item_id,
            "subreddit": original.get("subreddit", ""),
            "title": original.get("title", ""),
            "url": original.get("url", ""),
            "created_utc": original.get("created_utc", 0),
            "original_brand_hints": original.get("brand_hints", []),
            "priority": original.get("priority", 0),
            **r
        }
        lines.append(json.dumps(enriched) + "\n")
    
    with open(output_path, "a") as f:
        fcntl.flock(f.fileno(), fcntl.LOCK_EX)
        try:
            for line in lines:
                f.write(line)
            f.flush()
        finally:
            fcntl.flock(f.fileno(), fcntl.LOCK_UN)


def main():
    args = parse_args()
    worker_id = args.worker_id
    total_workers = args.total_workers
    
    input_path = Path(args.input)
    output_path = Path(args.output)
    
    print(f"[Worker {worker_id}/{total_workers}] Starting Stage 2 Batched")
    print(f"  Batch size: {args.batch_size}")
    print(f"  RPS: {args.rps}")
    print(f"  Max retries: {args.max_retries}")
    
    # Load existing IDs for resume
    existing_ids = set()
    if args.skip_existing and output_path.exists():
        existing_ids = load_existing_ids(output_path)
        print(f"  Loaded {len(existing_ids)} existing IDs to skip")
    
    # Load all items for this worker's partition
    all_items = []
    with open(input_path, "r") as f:
        for line_num, line in enumerate(f):
            if not line.strip():
                continue
            # Partition by line number
            if line_num % total_workers != (worker_id - 1):
                continue
            try:
                item = json.loads(line.strip())
                if item.get("id") not in existing_ids:
                    all_items.append(item)
            except:
                continue
    
    print(f"  Partition size: {len(all_items)} items")
    
    if args.max_items:
        all_items = all_items[:args.max_items]
        print(f"  Limited to: {len(all_items)} items")
    
    # Process in batches
    batch_size = args.batch_size
    min_interval = 1.0 / args.rps if args.rps > 0 else 0
    
    processed = 0
    enriched = 0
    errors = 0
    batch_count = 0
    start_time = time.time()
    last_request = 0
    
    # Metrics tracking
    issue_dist = {}
    brand_null_count = 0
    
    for i in range(0, len(all_items), batch_size):
        batch = all_items[i:i + batch_size]
        batch_ids = {item["id"]: item for item in batch}
        
        # Rate limiting
        elapsed = time.time() - last_request
        if elapsed < min_interval:
            time.sleep(min_interval - elapsed)
        last_request = time.time()
        
        # Process batch
        results = process_batch_with_retry(
            batch, args.gemini_key, args.gemini_model, 
            args.max_retries, worker_id
        )
        
        # Write results
        append_results_with_lock(output_path, results, batch_ids)
        
        processed += len(batch)
        enriched += len(results)
        errors += len(batch) - len(results)
        batch_count += 1
        
        # Track metrics
        for r in results:
            issue = r.get("issue_type", "other")
            issue_dist[issue] = issue_dist.get(issue, 0) + 1
            if not r.get("canonical_brand"):
                brand_null_count += 1
        
        # Log progress
        if batch_count % args.log_interval == 0:
            elapsed_total = time.time() - start_time
            rate = processed / elapsed_total if elapsed_total > 0 else 0
            brand_null_pct = 100 * brand_null_count / max(1, enriched)
            print(f"[W{worker_id}] Batch {batch_count}: processed={processed}, "
                  f"enriched={enriched}, errors={errors}, rate={rate:.1f}/sec, "
                  f"brand_null={brand_null_pct:.1f}%")
    
    # Final stats
    elapsed_total = time.time() - start_time
    print(f"\n[Worker {worker_id}] === Complete ===")
    print(f"  Processed: {processed}")
    print(f"  Enriched: {enriched}")
    print(f"  Errors: {errors}")
    print(f"  Duration: {elapsed_total:.1f}s ({processed/max(1,elapsed_total):.1f} items/sec)")
    print(f"  Brand null rate: {100*brand_null_count/max(1,enriched):.1f}%")
    print(f"  Issue distribution: {dict(sorted(issue_dist.items(), key=lambda x: -x[1])[:5])}")


if __name__ == "__main__":
    main()
