#!/usr/bin/env python3
"""
Reddit Funnel - Stage 2.5: Deterministic Cluster Key Post-Processing

Post-processes Stage 2 output to add deterministic clustering keys:
- cluster_key_llm: Original LLM-provided cluster_key (renamed)
- cluster_key_norm: Normalized slug of cluster_key_llm
- cluster_signature: "{brand}|{issue_type}|{slug}" for debugging
- cluster_id: SHA1 hash of cluster_signature for grouping

Usage:
  python3 stage2_postprocess.py \
    --input output_full/shards/*.jsonl \
    --output output_full/enriched_clustered.jsonl

Or process single shard:
  python3 stage2_postprocess.py \
    --input output_full/shards/enriched.worker_01.jsonl \
    --output output_full/clustered/enriched.worker_01.jsonl
"""

import argparse
import json
import hashlib
import re
import unicodedata
from pathlib import Path
from typing import Optional, List, Set
import glob

# English stopwords for slug generation
STOPWORDS = {
    'a', 'an', 'the', 'and', 'or', 'but', 'in', 'on', 'at', 'to', 'for',
    'of', 'with', 'by', 'from', 'as', 'is', 'was', 'are', 'were', 'been',
    'be', 'have', 'has', 'had', 'do', 'does', 'did', 'will', 'would',
    'could', 'should', 'may', 'might', 'must', 'can', 'this', 'that',
    'these', 'those', 'i', 'you', 'he', 'she', 'it', 'we', 'they', 'my',
    'your', 'his', 'her', 'its', 'our', 'their', 'what', 'which', 'who',
    'when', 'where', 'why', 'how', 'all', 'each', 'every', 'both', 'few',
    'more', 'most', 'other', 'some', 'such', 'no', 'not', 'only', 'own',
    'same', 'so', 'than', 'too', 'very', 'just', 'also', 'now', 'here',
    'there', 'then', 'if', 'about', 'into', 'through', 'during', 'before',
    'after', 'above', 'below', 'between', 'under', 'again', 'further',
    'once', 'any', 'being', 'get', 'got', 'getting', 'im', 'ive', 'dont',
    'doesnt', 'didnt', 'wont', 'cant', 'couldnt', 'shouldnt', 'wouldnt',
    'isnt', 'arent', 'wasnt', 'werent', 'hasnt', 'havent', 'hadnt'
}


def parse_args():
    parser = argparse.ArgumentParser(description="Stage 2.5: Deterministic Cluster Post-Processing")
    parser.add_argument("--input", required=True, help="Input JSONL file(s) - supports glob patterns")
    parser.add_argument("--output", required=True, help="Output JSONL file")
    parser.add_argument("--max-items", type=int, default=None, help="Max items to process (for testing)")
    parser.add_argument("--skip-existing", action="store_true", help="Skip already processed IDs")
    return parser.parse_args()


def normalize_text(text: str) -> str:
    """Unicode normalize and lowercase text."""
    if not text:
        return ""
    # Unicode normalize (decompose then compose)
    text = unicodedata.normalize('NFKC', text)
    # Lowercase
    text = text.lower()
    return text


def slugify(text: str, max_length: int = 64) -> str:
    """
    Create normalized slug:
    - lowercase, unicode normalize
    - replace non-alphanumerics with '-'
    - collapse dashes, trim
    - max length 64
    """
    if not text:
        return ""
    
    # Unicode normalize and lowercase
    text = normalize_text(text)
    
    # Replace non-alphanumerics with dash
    text = re.sub(r'[^a-z0-9]+', '-', text)
    
    # Collapse multiple dashes
    text = re.sub(r'-+', '-', text)
    
    # Trim leading/trailing dashes
    text = text.strip('-')
    
    # Max length
    if len(text) > max_length:
        text = text[:max_length].rstrip('-')
    
    return text


def extract_tokens(text: str, max_tokens: int = 8) -> List[str]:
    """
    Extract top tokens from text for slug:
    - normalize, remove stopwords, keep top N tokens
    """
    if not text:
        return []
    
    # Normalize
    text = normalize_text(text)
    
    # Split into words (alphanumeric only)
    words = re.findall(r'[a-z0-9]+', text)
    
    # Remove stopwords and short words
    words = [w for w in words if w not in STOPWORDS and len(w) > 2]
    
    # Take first max_tokens
    return words[:max_tokens]


def build_deterministic_slug(item: dict) -> str:
    """
    Build deterministic slug from title (preferred) else summary else text.
    Normalize, remove stopwords, keep top 8 tokens, join with '_'.
    """
    # Try title first
    text = item.get('title', '')
    
    # Fallback to summary
    if not text or len(text) < 10:
        text = item.get('summary', '')
    
    # Fallback to body (first 160 chars)
    if not text or len(text) < 10:
        body = item.get('body', '') or item.get('text', '')
        text = body[:160] if body else ''
    
    # Extract tokens
    tokens = extract_tokens(text, max_tokens=8)
    
    if not tokens:
        return "unknown"
    
    return '_'.join(tokens)


def infer_brand(item: dict) -> str:
    """
    Infer brand from item:
    1. canonical_brand if present
    2. brand_hints from Stage 1
    3. subreddit name (if brand-specific)
    4. "unknown"
    """
    # Check canonical_brand
    brand = item.get('canonical_brand')
    if brand and brand.lower() not in ('unknown', 'null', 'none', ''):
        return brand.lower()
    
    # Check brand_hints
    hints = item.get('original_brand_hints') or item.get('brand_hints') or []
    if hints and len(hints) > 0:
        return hints[0].lower()
    
    # Check subreddit (some are brand-specific)
    subreddit = (item.get('subreddit') or '').lower()
    brand_subreddits = {
        'apple', 'google', 'microsoft', 'amazon', 'steam', 'discord',
        'spotify', 'netflix', 'youtube', 'instagram', 'tiktok', 'twitter',
        'facebook', 'meta', 'nvidia', 'amd', 'intel', 'samsung', 'sony',
        'nintendo', 'playstation', 'xbox', 'tesla', 'uber', 'lyft',
        'doordash', 'grubhub', 'airbnb', 'dropbox', 'slack', 'zoom'
    }
    if subreddit in brand_subreddits:
        return subreddit
    
    return "unknown"


def build_cluster_signature(item: dict) -> str:
    """
    Build cluster_signature = "{brand}|{issue_type}|{slug}"
    """
    brand = infer_brand(item)
    issue_type = item.get('issue_type', 'other') or 'other'
    slug = build_deterministic_slug(item)
    
    return f"{brand}|{issue_type}|{slug}"


def compute_cluster_id(signature: str) -> str:
    """
    Compute cluster_id = sha1(cluster_signature), hex encoded.
    """
    return hashlib.sha1(signature.encode('utf-8')).hexdigest()


def process_item(item: dict) -> dict:
    """
    Add deterministic clustering fields to item:
    - cluster_key_llm: Original LLM cluster_key
    - cluster_key_norm: Normalized slug
    - cluster_signature: For debugging
    - cluster_id: SHA1 hash for grouping
    """
    # Rename cluster_key to cluster_key_llm
    llm_key = item.get('cluster_key', '')
    item['cluster_key_llm'] = llm_key
    
    # Add normalized slug of LLM key
    item['cluster_key_norm'] = slugify(llm_key)
    
    # Build deterministic signature
    signature = build_cluster_signature(item)
    item['cluster_signature'] = signature
    
    # Compute cluster_id
    item['cluster_id'] = compute_cluster_id(signature)
    
    # Remove old cluster_key (now in cluster_key_llm)
    if 'cluster_key' in item:
        del item['cluster_key']
    
    return item


def load_existing_ids(output_path: Path) -> Set[str]:
    """Load IDs already processed for resume."""
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


def main():
    args = parse_args()
    
    # Expand glob pattern for input
    input_files = glob.glob(args.input)
    if not input_files:
        print(f"No files match pattern: {args.input}")
        return
    
    print(f"Processing {len(input_files)} input file(s)")
    
    output_path = Path(args.output)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    
    # Load existing IDs for skip
    existing_ids = set()
    if args.skip_existing:
        existing_ids = load_existing_ids(output_path)
        print(f"  Loaded {len(existing_ids)} existing IDs to skip")
    
    processed = 0
    skipped = 0
    
    with open(output_path, "a") as fout:
        for input_file in sorted(input_files):
            print(f"  Processing: {input_file}")
            
            with open(input_file, "r") as fin:
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
                        skipped += 1
                        continue
                    
                    # Process item
                    item = process_item(item)
                    
                    # Write output
                    fout.write(json.dumps(item) + "\n")
                    processed += 1
                    
                    if processed % 10000 == 0:
                        print(f"    Processed: {processed}, Skipped: {skipped}")
            
            if args.max_items and processed >= args.max_items:
                break
    
    print(f"\n=== Complete ===")
    print(f"  Processed: {processed}")
    print(f"  Skipped: {skipped}")
    print(f"  Output: {output_path}")


if __name__ == "__main__":
    main()
