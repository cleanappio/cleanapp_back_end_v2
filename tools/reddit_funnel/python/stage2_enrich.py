#!/usr/bin/env python3
"""
Reddit Funnel - Stage 2: LLM Enrichment

Reads routed.jsonl from Stage 1, sends items to Gemini API for brand extraction,
and writes enriched.jsonl with normalized brand names, issue types, and severity.
"""

import argparse
import json
import os
import time
import hashlib
from pathlib import Path
from typing import Optional
import requests

# LLM Schema for Gemini response
LLM_SCHEMA = {
    "type": "object",
    "properties": {
        "canonical_brand": {"type": ["string", "null"]},
        "brand_confidence": {"type": "number"},
        "brand_alias_detected": {"type": ["string", "null"]},
        "issue_type": {
            "type": "string",
            "enum": ["bug", "outage", "ux", "account", "billing", "policy", 
                     "security", "performance", "feature_request", "other", "not_applicable"]
        },
        "severity": {"type": "string", "enum": ["none", "low", "medium", "high"]},
        "summary": {"type": "string"},
        "cluster_key": {"type": "string"},
        "responsible_party": {"type": ["string", "null"]},
        "is_actionable": {"type": "boolean"}
    },
    "required": ["canonical_brand", "brand_confidence", "issue_type", "severity", 
                 "summary", "cluster_key", "is_actionable"]
}

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
    parser = argparse.ArgumentParser(description="Reddit Funnel Stage 2: LLM Enrichment")
    parser.add_argument("--input", required=True, help="Path to routed.jsonl from Stage 1")
    parser.add_argument("--output", required=True, help="Path to write enriched.jsonl")
    parser.add_argument("--gemini-key", required=True, help="Gemini API key")
    parser.add_argument("--gemini-model", default="gemini-2.0-flash", help="Gemini model")
    parser.add_argument("--batch-size", type=int, default=50, help="Batch size for processing")
    parser.add_argument("--max-items", type=int, default=None, help="Max items to process")
    parser.add_argument("--rps", type=float, default=10.0, help="Requests per second")
    parser.add_argument("--skip-existing", action="store_true", help="Skip items already in output")
    parser.add_argument("--dry-run", action="store_true", help="Print stats without processing")
    return parser.parse_args()


def load_existing_ids(output_path: Path) -> set:
    """Load IDs of already-processed items to enable resume."""
    existing = set()
    if output_path.exists():
        with open(output_path, "r") as f:
            for line in f:
                try:
                    item = json.loads(line.strip())
                    if "id" in item:
                        existing.add(item["id"])
                except json.JSONDecodeError:
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
                # Rate limited, wait and retry
                wait_time = 2 ** attempt
                print(f"  Rate limited, waiting {wait_time}s...")
                time.sleep(wait_time)
                continue
                
            if resp.status_code != 200:
                print(f"  API error {resp.status_code}: {resp.text[:200]}")
                return None
                
            data = resp.json()
            if "candidates" not in data or not data["candidates"]:
                return None
                
            text = data["candidates"][0]["content"]["parts"][0]["text"]
            return json.loads(text)
            
        except Exception as e:
            print(f"  Error on attempt {attempt+1}: {e}")
            if attempt < max_retries - 1:
                time.sleep(1)
    
    return None


def build_prompt(item: dict) -> str:
    """Build LLM prompt from routed item."""
    title = item.get("title", "")
    body = item.get("body", "")[:2000]  # Limit body size
    subreddit = item.get("subreddit", "")
    brand_hints = item.get("brand_hints", [])
    
    return f"""Analyze this Reddit post for CleanApp:

Subreddit: r/{subreddit}
Title: {title}
Content: {body}

Brand hints from detection (may include false positives): {', '.join(brand_hints) if brand_hints else 'none'}

Return JSON with: canonical_brand, brand_confidence (0-1), issue_type, severity, summary (<240 chars), cluster_key, is_actionable.
If no real brand issue exists, set canonical_brand=null, severity="none", is_actionable=false."""


def process_items(args):
    """Main processing loop."""
    input_path = Path(args.input)
    output_path = Path(args.output)
    
    # Load existing IDs if resuming
    existing_ids = set()
    if args.skip_existing and output_path.exists():
        existing_ids = load_existing_ids(output_path)
        print(f"Loaded {len(existing_ids)} existing IDs to skip")
    
    # Count items
    total_items = 0
    with open(input_path, "r") as f:
        for line in f:
            if line.strip():
                total_items += 1
    print(f"Total items in input: {total_items}")
    
    if args.dry_run:
        print("Dry run, exiting")
        return
    
    # Process items
    processed = 0
    enriched = 0
    actionable = 0
    skipped = 0
    
    min_interval = 1.0 / args.rps if args.rps > 0 else 0
    last_request = 0
    
    with open(input_path, "r") as fin, open(output_path, "a") as fout:
        for line in fin:
            if not line.strip():
                continue
                
            if args.max_items and processed >= args.max_items:
                break
            
            try:
                item = json.loads(line.strip())
            except json.JSONDecodeError:
                continue
            
            item_id = item.get("id", "")
            
            # Skip if already processed
            if item_id in existing_ids:
                skipped += 1
                continue
            
            # Rate limiting
            elapsed = time.time() - last_request
            if elapsed < min_interval:
                time.sleep(min_interval - elapsed)
            
            # Build prompt and call LLM
            prompt = build_prompt(item)
            last_request = time.time()
            
            result = call_gemini(args.gemini_key, args.gemini_model, prompt)
            processed += 1
            
            if result:
                # Validate result is a dict (Gemini sometimes returns list)
                if isinstance(result, list):
                    result = result[0] if result else None
                
                if result and isinstance(result, dict):
                    # Merge with original item
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
                    
                    fout.write(json.dumps(enriched_item) + "\n")
                    enriched += 1
                    
                    if result.get("is_actionable"):
                        actionable += 1
            
            if processed % 100 == 0:
                print(f"Processed {processed}, enriched {enriched}, actionable {actionable}")
    
    print(f"\n=== Stage 2 Complete ===")
    print(f"Processed: {processed}")
    print(f"Skipped (existing): {skipped}")
    print(f"Enriched: {enriched}")
    print(f"Actionable: {actionable} ({100*actionable/max(1,enriched):.1f}%)")
    print(f"Output: {output_path}")


if __name__ == "__main__":
    args = parse_args()
    process_items(args)
