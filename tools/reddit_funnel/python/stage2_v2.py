#!/usr/bin/env python3
"""
Reddit Funnel - Stage 2 v2: LLM Enrichment with Schema Enforcement

Improvements over v1:
- Enforced JSON schema (no empty issue_type)
- Stronger brand extraction prompt
- Post-processing taxonomy mappings
"""

import argparse
import json
import os
import time
import fcntl
from pathlib import Path
from typing import Optional, Set
import requests

# Gemini Response Schema (enforced)
RESPONSE_SCHEMA = {
    "type": "object",
    "properties": {
        "canonical_brand": {
            "type": "string",
            "description": "Exact brand/company name, or 'unknown' if truly no brand context"
        },
        "brand_confidence": {
            "type": "number",
            "description": "Confidence 0.0-1.0 in brand extraction"
        },
        "issue_type": {
            "type": "string",
            "enum": ["bug", "outage", "ux", "account", "billing", "policy", 
                     "security", "performance", "feature_request", "promotion",
                     "hardware", "gameplay", "compatibility", "discussion",
                     "general", "other", "not_applicable", "none"]
        },
        "severity": {
            "type": "string",
            "enum": ["none", "minor", "low", "medium", "moderate", "major", "high", "critical"]
        },
        "summary": {
            "type": "string",
            "description": "Brief summary under 240 chars"
        },
        "cluster_key": {
            "type": "string",
            "description": "Grouping key for similar issues"
        },
        "is_actionable": {
            "type": "boolean",
            "description": "True if brand can address this issue"
        }
    },
    "required": ["canonical_brand", "issue_type", "severity", "is_actionable"]
}

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

SYSTEM_PROMPT = """You are extracting brand-addressable issue reports for CleanApp.
CleanApp crowdsources user feedback about specific brands and forwards it to those brands.

## BRAND EXTRACTION (CRITICAL)

You MUST extract a brand if ANY of these are present:
- A company or product name is mentioned (Apple, Steam, Discord, Google, Amazon, etc.)
- The subreddit is brand-specific (e.g. r/apple → brand="apple", r/steam → brand="steam")
- Brand hints are provided from Stage 1 detection

ONLY return canonical_brand="unknown" if the post is truly generic with NO brand context.

Cross-reference with the brand hints provided and the subreddit name.
If subreddit matches a brand name, use that as canonical_brand.

## ISSUE TYPE

Classify into one of:
- bug: software errors, crashes, glitches, defects
- outage: service down, unavailable, server issues
- ux: user experience problems, confusing UI, hard to use
- account: login issues, banned, suspended, password problems
- billing: payment issues, charges, refunds, subscription problems
- policy: terms violations, moderation issues, content removal
- security: privacy concerns, hacks, data breaches
- performance: slow, laggy, timeout issues
- feature_request: suggestions, wishes, improvement requests
- promotion: promotional/spam content (not actionable)
- hardware: physical device issues
- gameplay: game-specific issues
- compatibility: cross-platform/version issues
- discussion: general discussion (not actionable)
- other: doesn't fit above categories
- not_applicable: not a brand issue

## ACTIONABLE

is_actionable = true ONLY if:
- User is reporting a real problem with a product/service
- The brand could reasonably address or respond to it

is_actionable = false if:
- General discussion, memes, promotional content
- Questions without issues
- Not related to a specific brand

Return JSON matching the schema. No prose."""


def parse_args():
    parser = argparse.ArgumentParser(description="Stage 2 v2: LLM Enrichment")
    parser.add_argument("--input", required=True, help="Path to routed.jsonl or sample")
    parser.add_argument("--output", required=True, help="Path to write enriched.jsonl")
    parser.add_argument("--gemini-key", required=True, help="Gemini API key")
    parser.add_argument("--gemini-model", default="gemini-2.0-flash", help="Gemini model")
    parser.add_argument("--max-items", type=int, default=None, help="Max items to process")
    parser.add_argument("--rps", type=float, default=10.0, help="Requests per second")
    parser.add_argument("--skip-existing", action="store_true", help="Skip already processed IDs")
    return parser.parse_args()


def load_existing_ids(output_path: Path) -> Set[str]:
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
    """Call Gemini API with schema enforcement."""
    url = f"https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent"
    
    payload = {
        "generationConfig": {
            "response_mime_type": "application/json",
            "response_schema": RESPONSE_SCHEMA
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
            result = json.loads(text)
            
            if isinstance(result, list):
                result = result[0] if result else None
            
            return result if isinstance(result, dict) else None
            
        except Exception as e:
            print(f"  Error: {e}")
            if attempt < max_retries - 1:
                time.sleep(1)
    
    return None


def apply_mappings(result: dict) -> dict:
    """Apply taxonomy mappings to normalize values."""
    if not result:
        return result
    
    # Issue type mapping
    raw_issue = result.get("issue_type", "other") or "other"
    raw_issue_lower = raw_issue.lower().strip()
    result["raw_issue_type"] = raw_issue
    result["issue_type"] = ISSUE_TYPE_MAPPINGS.get(raw_issue_lower, raw_issue_lower)
    
    # Ensure issue_type is never empty
    if not result["issue_type"]:
        result["issue_type"] = "other"
    
    # Severity mapping
    raw_severity = result.get("severity", "none") or "none"
    raw_severity_lower = raw_severity.lower().strip()
    result["raw_severity"] = raw_severity
    result["severity"] = SEVERITY_MAPPINGS.get(raw_severity_lower, raw_severity_lower)
    
    # Normalize severity to canonical values
    if result["severity"] not in ["none", "low", "medium", "high"]:
        result["severity"] = "medium"  # Default for unmapped
    
    # Brand normalization: "unknown" is explicit null
    brand = result.get("canonical_brand", "unknown")
    if brand and brand.lower() in ["unknown", "null", "none", ""]:
        result["canonical_brand"] = None
    
    return result


def build_prompt(item: dict) -> str:
    title = item.get("title", "")
    body = item.get("body", "")[:2000]
    subreddit = item.get("subreddit", "")
    brand_hints = item.get("brand_hints", [])
    
    return f"""Analyze this Reddit post for CleanApp:

Subreddit: r/{subreddit}
Title: {title}
Content: {body}

Brand hints from Stage 1 detection: {', '.join(brand_hints) if brand_hints else 'none'}

IMPORTANT: If subreddit matches a known brand (r/apple, r/steam, r/discord, etc.), use that as canonical_brand.
Cross-reference brand hints with the content.

Return JSON with: canonical_brand, brand_confidence, issue_type, severity, summary, cluster_key, is_actionable."""


def append_with_lock(output_path: Path, line: str):
    with open(output_path, "a") as f:
        fcntl.flock(f.fileno(), fcntl.LOCK_EX)
        try:
            f.write(line)
            f.flush()
        finally:
            fcntl.flock(f.fileno(), fcntl.LOCK_UN)


def process_items(args):
    input_path = Path(args.input)
    output_path = Path(args.output)
    
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
    
    if args.max_items:
        print(f"Processing max {args.max_items} items")
    
    # Process
    processed = 0
    enriched = 0
    actionable = 0
    brand_detected = 0
    errors = 0
    
    min_interval = 1.0 / args.rps if args.rps > 0 else 0
    last_request = 0
    start_time = time.time()
    
    with open(input_path, "r") as fin:
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
            
            if item_id in existing_ids:
                continue
            
            # Rate limiting
            elapsed = time.time() - last_request
            if elapsed < min_interval:
                time.sleep(min_interval - elapsed)
            
            prompt = build_prompt(item)
            last_request = time.time()
            
            result = call_gemini(args.gemini_key, args.gemini_model, prompt)
            processed += 1
            
            if result:
                # Apply mappings
                result = apply_mappings(result)
                
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
                
                if result.get("is_actionable"):
                    actionable += 1
                if result.get("canonical_brand"):
                    brand_detected += 1
            else:
                errors += 1
            
            if processed % 100 == 0:
                elapsed_total = time.time() - start_time
                rate = processed / elapsed_total if elapsed_total > 0 else 0
                print(f"Processed {processed}/{args.max_items or total_items}, "
                      f"enriched {enriched}, actionable {actionable}, "
                      f"brand_detected {brand_detected}, errors {errors}, "
                      f"rate {rate:.1f}/sec")
    
    elapsed_total = time.time() - start_time
    print(f"\n=== Stage 2 v2 Complete ===")
    print(f"Processed: {processed}")
    print(f"Enriched: {enriched}")
    print(f"Errors: {errors}")
    print(f"Actionable: {actionable} ({100*actionable/max(1,enriched):.1f}%)")
    print(f"Brand detected: {brand_detected} ({100*brand_detected/max(1,enriched):.1f}%)")
    print(f"Duration: {elapsed_total:.1f}s ({processed/elapsed_total:.1f} items/sec)")
    print(f"Output: {output_path}")


if __name__ == "__main__":
    args = parse_args()
    process_items(args)
