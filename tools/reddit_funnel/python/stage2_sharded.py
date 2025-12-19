#!/usr/bin/env python3
"""
Reddit Funnel - Stage 2 Sharded: Optimized High-Throughput LLM Enrichment

=============================================================================
RUNBOOK
=============================================================================
Optimized for 40+ workers:
  - Per-worker output shards (no lock contention)
  - O(1) skip-existing using ID set loaded at startup
  - Stable partition: line_num % total_workers
  - Run detached via nohup with per-worker logs

Start 40 workers:
  for i in $(seq 1 40); do
    nohup python3 python/stage2_sharded.py \
      --input output_full/routed.jsonl \
      --output-dir output_full/shards \
      --gemini-key "$GEMINI_KEY" \
      --batch-size 10 --rps 5 \
      --worker-id $i --total-workers 40 \
      > worker_$i.log 2>&1 &
  done

Merge shards after completion:
  cat output_full/shards/enriched.worker_*.jsonl > output_full/enriched_final.jsonl

Expected throughput: 40 workers × 10 items/batch × 3 batches/sec = ~1200 items/sec
ETA for 2.5M: ~35 min at peak, ~2-3 hours with rate limits
=============================================================================
"""

import argparse
import json
import os
import time
import random
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

SEVERITY_MAPPINGS = {
    "minor": "low",
    "moderate": "medium",
    "major": "high",
    "critical": "high",
    "severe": "high",
    "serious": "high",
    "urgent": "high"
}

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
You will receive MULTIPLE items to analyze. Return one result per item with matching id.

## BRAND EXTRACTION (CRITICAL)
You MUST extract a brand if ANY of these are present:
- A company or product name is mentioned (Apple, Steam, Discord, Google, Amazon, etc.)
- The subreddit is brand-specific (e.g. r/apple → brand="apple")
- Brand hints are provided from Stage 1 detection

ONLY return canonical_brand=null if truly generic with NO brand context.

## ISSUE TYPE (REQUIRED)
MUST be one of: bug, outage, ux, account, billing, policy, security, performance, feature_request, other, not_applicable
If uncertain, use "other" - NEVER leave issue_type empty.

## SEVERITY (REQUIRED)
MUST be one of: none, low, medium, high

## ACTIONABLE
is_actionable = true ONLY if user reports a real problem a brand could address.

## OUTPUT FORMAT
Return JSON with "results" array. Each result must have matching "id" from input."""


def parse_args():
    parser = argparse.ArgumentParser(description="Stage 2 Sharded: Optimized LLM Enrichment")
    parser.add_argument("--input", required=True, help="Path to routed.jsonl")
    parser.add_argument("--output-dir", required=True, help="Directory for worker shard files")
    parser.add_argument("--gemini-key", required=True, help="Gemini API key")
    parser.add_argument("--gemini-model", default="gemini-2.0-flash", help="Gemini model")
    parser.add_argument("--batch-size", type=int, default=10, help="Items per Gemini call")
    parser.add_argument("--max-items", type=int, default=None, help="Max items (for testing)")
    parser.add_argument("--rps", type=float, default=5.0, help="Requests per second")
    parser.add_argument("--max-retries", type=int, default=5, help="Max retries per batch")
    parser.add_argument("--worker-id", type=int, required=True, help="Worker ID (1-indexed)")
    parser.add_argument("--total-workers", type=int, required=True, help="Total workers")
    parser.add_argument("--log-interval", type=int, default=50, help="Log every N batches")
    return parser.parse_args()


def load_existing_ids_from_shard(shard_path: Path) -> Set[str]:
    """Load IDs from this worker's shard file for O(1) resume."""
    existing = set()
    if shard_path.exists():
        with open(shard_path, "r") as f:
            for line in f:
                try:
                    item = json.loads(line.strip())
                    if "id" in item:
                        existing.add(item["id"])
                except:
                    continue
    return existing


def build_batch_prompt(items: List[Dict]) -> str:
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
    
    return f"""Analyze these {len(items)} Reddit posts.
Return JSON with "results" array containing exactly {len(items)} items.

ITEMS:
{''.join(prompt_items)}

Each result needs: id, canonical_brand, brand_confidence, issue_type, severity, is_actionable, summary, cluster_key."""


def call_gemini_batch(api_key: str, model: str, prompt: str, max_retries: int = 3) -> Optional[Dict]:
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
            resp = requests.post(f"{url}?key={api_key}", json=payload, timeout=120)
            
            if resp.status_code == 429:
                wait_time = (2 ** attempt) + random.uniform(0, 1)
                time.sleep(wait_time)
                continue
                
            if resp.status_code != 200:
                time.sleep(1)
                continue
                
            data = resp.json()
            if "candidates" not in data or not data["candidates"]:
                continue
                
            text = data["candidates"][0]["content"]["parts"][0]["text"]
            result = json.loads(text)
            
            if isinstance(result, list):
                result = {"results": result}
            elif isinstance(result, dict) and "results" not in result:
                result = {"results": [result]}
            
            return result if isinstance(result, dict) else None
            
        except Exception as e:
            if attempt < max_retries - 1:
                time.sleep(1)
    
    return None


def validate_batch_response(response: Dict, input_ids: List[str]) -> Tuple[bool, str]:
    if not response or "results" not in response:
        return False, "missing results"
    
    results = response["results"]
    if len(results) != len(input_ids):
        return False, f"count mismatch"
    
    result_ids = {r.get("id") for r in results if isinstance(r, dict)}
    if result_ids != set(input_ids):
        return False, "id mismatch"
    
    return True, "ok"


def normalize_result(result: Dict) -> Dict:
    raw_issue = result.get("issue_type", "other") or "other"
    raw_issue_lower = raw_issue.lower().strip()
    
    if raw_issue_lower in ISSUE_TYPE_MAPPINGS:
        result["issue_type"] = ISSUE_TYPE_MAPPINGS[raw_issue_lower]
    elif raw_issue_lower in CANONICAL_ISSUE_TYPES:
        result["issue_type"] = raw_issue_lower
    else:
        result["issue_type"] = "other"
    result["raw_issue_type"] = raw_issue
    
    raw_severity = result.get("severity", "none") or "none"
    raw_severity_lower = raw_severity.lower().strip()
    
    if raw_severity_lower in SEVERITY_MAPPINGS:
        result["severity"] = SEVERITY_MAPPINGS[raw_severity_lower]
    elif raw_severity_lower in CANONICAL_SEVERITIES:
        result["severity"] = raw_severity_lower
    else:
        result["severity"] = "medium"
    
    result["is_actionable"] = bool(result.get("is_actionable", False))
    return result


def process_batch_with_retry(items: List[Dict], api_key: str, model: str, 
                              max_retries: int, worker_id: int) -> List[Dict]:
    if not items:
        return []
    
    input_ids = [item["id"] for item in items]
    prompt = build_batch_prompt(items)
    
    for attempt in range(max_retries):
        response = call_gemini_batch(api_key, model, prompt, max_retries=2)
        
        if response:
            valid, _ = validate_batch_response(response, input_ids)
            if valid:
                results = []
                for r in response["results"]:
                    r = normalize_result(r)
                    r["worker_id"] = worker_id
                    r["processed_at"] = datetime.utcnow().isoformat() + "Z"
                    results.append(r)
                return results
    
    # Split on failure
    if len(items) > 1:
        mid = len(items) // 2
        left = process_batch_with_retry(items[:mid], api_key, model, max_retries, worker_id)
        right = process_batch_with_retry(items[mid:], api_key, model, max_retries, worker_id)
        return left + right
    
    return []


def main():
    args = parse_args()
    worker_id = args.worker_id
    total_workers = args.total_workers
    
    input_path = Path(args.input)
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    
    # Each worker writes to its own shard file (no locking needed)
    shard_path = output_dir / f"enriched.worker_{worker_id:02d}.jsonl"
    
    print(f"[Worker {worker_id}/{total_workers}] Starting Stage 2 Sharded")
    print(f"  Output shard: {shard_path}")
    print(f"  Batch size: {args.batch_size}, RPS: {args.rps}")
    
    # O(1) skip-existing: load IDs from this worker's shard only
    existing_ids = load_existing_ids_from_shard(shard_path)
    print(f"  Existing IDs to skip: {len(existing_ids)}")
    
    # Load items for this worker's partition (stable: line_num % total_workers)
    all_items = []
    with open(input_path, "r") as f:
        for line_num, line in enumerate(f):
            if not line.strip():
                continue
            # Stable partition by line number
            if line_num % total_workers != (worker_id - 1):
                continue
            try:
                item = json.loads(line.strip())
                # O(1) lookup
                if item.get("id") not in existing_ids:
                    all_items.append(item)
            except:
                continue
    
    print(f"  Partition items to process: {len(all_items)}")
    
    if args.max_items:
        all_items = all_items[:args.max_items]
        print(f"  Limited to: {len(all_items)}")
    
    # Process in batches
    batch_size = args.batch_size
    min_interval = 1.0 / args.rps if args.rps > 0 else 0
    
    processed = 0
    enriched = 0
    batch_count = 0
    start_time = time.time()
    last_request = 0
    
    # Open shard file for append (no locking needed - exclusive to this worker)
    with open(shard_path, "a") as fout:
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
            
            # Write results to shard (no lock needed)
            for r in results:
                item_id = r.get("id", "")
                original = batch_ids.get(item_id, {})
                enriched_item = {
                    "id": item_id,
                    "subreddit": original.get("subreddit", ""),
                    "title": original.get("title", ""),
                    "url": original.get("url", ""),
                    "created_utc": original.get("created_utc", 0),
                    "original_brand_hints": original.get("brand_hints", []),
                    "priority": original.get("priority", 0),
                    **r
                }
                fout.write(json.dumps(enriched_item) + "\n")
            fout.flush()
            
            processed += len(batch)
            enriched += len(results)
            batch_count += 1
            
            if batch_count % args.log_interval == 0:
                elapsed_total = time.time() - start_time
                rate = processed / elapsed_total if elapsed_total > 0 else 0
                print(f"[W{worker_id}] Batch {batch_count}: processed={processed}, "
                      f"enriched={enriched}, rate={rate:.1f}/sec")
    
    # Final stats
    elapsed_total = time.time() - start_time
    rate = processed / max(1, elapsed_total)
    print(f"\n[Worker {worker_id}] === Complete ===")
    print(f"  Processed: {processed}, Enriched: {enriched}")
    print(f"  Duration: {elapsed_total:.1f}s ({rate:.1f} items/sec)")
    print(f"  Output: {shard_path}")


if __name__ == "__main__":
    main()
