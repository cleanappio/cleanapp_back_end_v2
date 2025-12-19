#!/usr/bin/env python3
"""
Stage 2 Concurrent Worker

Additional worker process that reads from calibration sample,
skips already-processed IDs, and appends to same output file.
Uses file locking for safe concurrent append.
"""

import argparse
import json
import os
import time
import fcntl
from pathlib import Path
from typing import Optional, Set
import requests

SYSTEM_PROMPT = """You are extracting brand-addressable issue reports for CleanApp.
CleanApp crowdsources user feedback about specific brands and forwards it to those brands.

Your task is to analyze Reddit posts/comments and extract:
1. The specific BRAND being discussed (company, app, platform, service)
2. The type of issue (bug, outage, ux, account, billing, etc.)
3. A severity level
4. Whether this is an actionable complaint that CleanApp can forward to the brand

BRAND RULES:
- Extract the ACTUAL company/brand name, not generic categories
- "my Steam account won't download" → brand = "steam"
- "Discord keeps crashing" → brand = "discord"
- If discussing Windows issues → brand = "microsoft"
- If discussing iPhone issues → brand = "apple"
- If NO specific brand is identifiable, set canonical_brand = null

IS_ACTIONABLE RULES:
- TRUE if: user is reporting a real problem with a product/service that the brand could address
- FALSE if: general discussion, memes, promotional content, questions without issues

Return JSON ONLY, no prose."""


def parse_args():
    parser = argparse.ArgumentParser(description="Stage 2 Concurrent Worker")
    parser.add_argument("--input", required=True, help="Path to calibration_sample.jsonl")
    parser.add_argument("--output", required=True, help="Path to calibration_enriched.jsonl (append)")
    parser.add_argument("--gemini-key", required=True, help="Gemini API key")
    parser.add_argument("--gemini-model", default="gemini-2.0-flash", help="Gemini model")
    parser.add_argument("--rps", type=float, default=10.0, help="Requests per second")
    parser.add_argument("--worker-id", type=int, default=1, help="Worker ID for offset")
    parser.add_argument("--total-workers", type=int, default=4, help="Total workers")
    parser.add_argument("--max-items", type=int, default=None, help="Max items to process")
    return parser.parse_args()


def load_existing_ids(output_path: Path) -> Set[str]:
    """Load IDs already processed."""
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


def call_gemini(api_key: str, model: str, prompt: str, max_retries: int = 3) -> Optional[dict]:
    """Call Gemini API with retry logic."""
    url = f"https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent"
    
    payload = {
        "generationConfig": {
            "response_mime_type": "application/json"
        },
        "contents": [{
            "role": "user",
            "parts": [
                {"text": SYSTEM_PROMPT},
                {"text": prompt}
            ]
        }]
    }
    
    for attempt in range(max_retries):
        try:
            resp = requests.post(
                f"{url}?key={api_key}",
                json=payload,
                timeout=60
            )
            
            if resp.status_code == 429:
                wait_time = 2 ** (attempt + 1)
                print(f"  [W{os.environ.get('WORKER_ID', '?')}] Rate limited, waiting {wait_time}s...")
                time.sleep(wait_time)
                continue
                
            if resp.status_code != 200:
                print(f"  [W{os.environ.get('WORKER_ID', '?')}] API error {resp.status_code}")
                return None
                
            data = resp.json()
            if "candidates" not in data or not data["candidates"]:
                return None
                
            text = data["candidates"][0]["content"]["parts"][0]["text"]
            result = json.loads(text)
            
            # Handle list response
            if isinstance(result, list):
                result = result[0] if result else None
            
            return result if isinstance(result, dict) else None
            
        except Exception as e:
            print(f"  [W{os.environ.get('WORKER_ID', '?')}] Error: {e}")
            if attempt < max_retries - 1:
                time.sleep(1)
    
    return None


def append_with_lock(output_path: Path, line: str):
    """Append to file with file locking for concurrency safety."""
    with open(output_path, "a") as f:
        fcntl.flock(f.fileno(), fcntl.LOCK_EX)
        try:
            f.write(line)
            f.flush()
        finally:
            fcntl.flock(f.fileno(), fcntl.LOCK_UN)


def build_prompt(item: dict) -> str:
    title = item.get("title", "")
    body = item.get("body", "")[:2000]
    subreddit = item.get("subreddit", "")
    brand_hints = item.get("brand_hints", [])
    
    return f"""Analyze this Reddit post for CleanApp:

Subreddit: r/{subreddit}
Title: {title}
Content: {body}

Brand hints from detection (may include false positives): {', '.join(brand_hints) if brand_hints else 'none'}

Return JSON with: canonical_brand, brand_confidence (0-1), issue_type, severity, summary (<240 chars), cluster_key, is_actionable.
If no real brand issue exists, set canonical_brand=null, severity="none", is_actionable=false."""


def process_worker(args):
    worker_id = args.worker_id
    total_workers = args.total_workers
    os.environ["WORKER_ID"] = str(worker_id)
    
    input_path = Path(args.input)
    output_path = Path(args.output)
    
    print(f"[Worker {worker_id}/{total_workers}] Starting...")
    
    # Load existing IDs
    existing_ids = load_existing_ids(output_path)
    print(f"[Worker {worker_id}] Found {len(existing_ids)} existing IDs")
    
    # Read all input lines and filter to this worker's partition
    all_items = []
    with open(input_path, "r") as f:
        for idx, line in enumerate(f):
            if not line.strip():
                continue
            # Partition by line number
            if idx % total_workers == (worker_id - 1):
                try:
                    item = json.loads(line.strip())
                    all_items.append((line, item))
                except:
                    continue
    
    print(f"[Worker {worker_id}] Partition size: {len(all_items)} items")
    
    # Filter out already processed
    to_process = [(line, item) for line, item in all_items if item.get("id", "") not in existing_ids]
    print(f"[Worker {worker_id}] To process: {len(to_process)} items (skipping {len(all_items) - len(to_process)})")
    
    if args.max_items:
        to_process = to_process[:args.max_items]
    
    # Process
    min_interval = 1.0 / args.rps if args.rps > 0 else 0
    last_request = 0
    processed = 0
    enriched = 0
    errors = 0
    
    for line, item in to_process:
        item_id = item.get("id", "")
        
        # Rate limiting
        elapsed = time.time() - last_request
        if elapsed < min_interval:
            time.sleep(min_interval - elapsed)
        
        prompt = build_prompt(item)
        last_request = time.time()
        
        result = call_gemini(args.gemini_key, args.gemini_model, prompt)
        processed += 1
        
        if result:
            enriched_item = {
                "id": item_id,
                "subreddit": item.get("subreddit", ""),
                "title": item.get("title", ""),
                "url": item.get("url", ""),
                "created_utc": item.get("created_utc", 0),
                "original_brand_hints": item.get("brand_hints", []),
                "priority": item.get("priority", 0),
                **result
            }
            
            append_with_lock(output_path, json.dumps(enriched_item) + "\n")
            enriched += 1
        else:
            errors += 1
        
        if processed % 50 == 0:
            print(f"[Worker {worker_id}] Processed {processed}, enriched {enriched}, errors {errors}")
    
    print(f"[Worker {worker_id}] Done! Processed {processed}, enriched {enriched}, errors {errors}")


if __name__ == "__main__":
    args = parse_args()
    process_worker(args)
