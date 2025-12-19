#!/usr/bin/env python3
"""
Reddit Funnel - Stage 2 Final Post-Processing

Adds deterministic cluster_id_coarse based on brand_effective|issue_type only.
Atoms and LLM labels are kept as attributes, not identity.

Usage:
  python3 stage2_final.py \
    --input 'output_full/shards/*.jsonl' \
    --output output_full/enriched_final.jsonl
"""

import argparse
import json
import hashlib
import re
import unicodedata
from pathlib import Path
from typing import Optional, List, Set
from urllib.parse import urlparse
import glob

# Controlled vocabulary of issue atoms (for attributes, not identity)
ISSUE_ATOMS = {
    "login": ["login", "log in", "signin", "sign in", "logout", "logged out"],
    "password": ["password", "reset password", "forgot password"],
    "account": ["account", "my account", "account banned", "account locked"],
    "ban": ["banned", "ban", "suspended", "suspension", "terminated"],
    "verify": ["verify", "verification", "2fa", "two factor", "authenticator"],
    "crash": ["crash", "crashes", "crashing", "crashed", "freezes", "freeze", "frozen"],
    "bug": ["bug", "bugs", "glitch", "glitches", "broken", "not working"],
    "error": ["error", "errors", "error code", "failed", "failure"],
    "update": ["update", "updates", "updating", "upgrade", "patch"],
    "install": ["install", "installing", "installation", "uninstall", "download"],
    "load": ["loading", "wont load", "not loading", "stuck loading"],
    "sync": ["sync", "syncing", "not syncing"],
    "connect": ["connect", "connection", "disconnect", "disconnected"],
    "slow": ["slow", "lag", "laggy", "lagging", "latency", "delay"],
    "performance": ["performance", "fps", "stuttering", "stutter"],
    "payment": ["payment", "pay", "paid", "charge", "charged"],
    "refund": ["refund", "refunded", "money back", "chargeback"],
    "subscription": ["subscription", "subscribe", "cancel subscription"],
    "billing": ["billing", "bill", "billed", "invoice"],
    "moderation": ["moderation", "moderator", "removed", "taken down"],
    "spam": ["spam", "spammer", "bot", "bots", "fake"],
    "report": ["report", "reported", "flag", "flagged"],
    "notification": ["notification", "notifications", "alert", "alerts"],
    "settings": ["settings", "preferences", "options"],
    "data": ["data", "my data", "privacy"],
    "delete": ["delete", "deleted", "remove", "removed"],
    "shipping": ["shipping", "shipped", "delivery", "delivered", "tracking"],
    "return": ["return", "returns", "exchange", "replacement"],
    "app": ["app", "application", "mobile app"],
    "website": ["website", "site", "browser"],
    "server": ["server", "servers", "down", "outage", "offline"],
}

# Build reverse lookup
ATOM_LOOKUP = {}
for atom, triggers in ISSUE_ATOMS.items():
    for trigger in triggers:
        ATOM_LOOKUP[trigger.lower().strip()] = atom

# Brand-specific subreddits
BRAND_SUBREDDITS = {
    'apple', 'iphone', 'ipad', 'mac', 'macos', 'ios',
    'google', 'android', 'pixel', 'chrome', 'gmail',
    'microsoft', 'windows', 'windows10', 'windows11', 'xbox',
    'amazon', 'aws', 'alexa', 'kindle', 'prime',
    'steam', 'valve', 'steamdeck', 'discord', 'slack', 'zoom',
    'spotify', 'netflix', 'youtube', 'twitch', 'tiktok', 'instagram', 'facebook', 'twitter',
    'nvidia', 'amd', 'intel', 'asus', 'samsung', 'sony',
    'nintendo', 'playstation', 'ps4', 'ps5',
    'tesla', 'uber', 'lyft', 'doordash', 'airbnb', 'paypal',
    'openai', 'chatgpt', 'github', 'notion', 'figma'
}

SUBREDDIT_TO_BRAND = {
    'iphone': 'apple', 'ipad': 'apple', 'mac': 'apple', 'macos': 'apple', 'ios': 'apple',
    'android': 'google', 'pixel': 'google', 'chrome': 'google', 'gmail': 'google',
    'windows': 'microsoft', 'windows10': 'microsoft', 'windows11': 'microsoft', 'xbox': 'microsoft',
    'aws': 'amazon', 'alexa': 'amazon', 'kindle': 'amazon', 'prime': 'amazon',
    'valve': 'steam', 'steamdeck': 'steam',
    'ps4': 'playstation', 'ps5': 'playstation',
    'chatgpt': 'openai'
}


def parse_args():
    parser = argparse.ArgumentParser(description="Stage 2 Final: Deterministic Clustering")
    parser.add_argument("--input", required=True, help="Input JSONL (supports glob)")
    parser.add_argument("--output", required=True, help="Output JSONL file")
    parser.add_argument("--max-items", type=int, default=None, help="Max items (testing)")
    parser.add_argument("--skip-existing", action="store_true", help="Skip processed IDs")
    return parser.parse_args()


def extract_registrable_domain(url: str) -> Optional[str]:
    """Extract registrable domain from URL."""
    if not url:
        return None
    try:
        parsed = urlparse(url)
        host = parsed.netloc or parsed.path.split('/')[0]
        host = host.lower()
        if host.startswith('www.'):
            host = host[4:]
        parts = host.split('.')
        if len(parts) >= 2:
            if parts[-1] in ('uk', 'au', 'jp', 'de', 'fr', 'ca', 'br'):
                if len(parts) >= 3:
                    return parts[-3]
            return parts[-2]
        return parts[0] if parts else None
    except:
        return None


def get_brand_effective(item: dict) -> str:
    """
    Get effective brand:
    1. canonical_brand
    2. registrable_domain(url)
    3. inferred_brand_from_subreddit
    4. "unknown"
    """
    brand = item.get('canonical_brand')
    if brand and brand.lower() not in ('unknown', 'null', 'none', ''):
        return brand.lower()
    
    url = item.get('url', '')
    domain = extract_registrable_domain(url)
    if domain and len(domain) > 2 and domain not in ('reddit', 'redd', 'imgur', 'gfycat'):
        return domain
    
    subreddit = (item.get('subreddit') or '').lower()
    if subreddit in BRAND_SUBREDDITS:
        return SUBREDDIT_TO_BRAND.get(subreddit, subreddit)
    
    hints = item.get('original_brand_hints') or item.get('brand_hints') or []
    if hints:
        return hints[0].lower()
    
    return "unknown"


def extract_issue_atoms(text: str) -> List[str]:
    """Extract matched issue atoms from text."""
    if not text:
        return []
    text_lower = text.lower()
    matched = set()
    for trigger, atom in sorted(ATOM_LOOKUP.items(), key=lambda x: -len(x[0])):
        if trigger in text_lower:
            matched.add(atom)
            if len(matched) >= 5:
                break
    return sorted(matched)


def get_source_text(item: dict) -> str:
    """Get text for atom extraction."""
    body = item.get('body', '') or item.get('text', '')
    if body and len(body.strip()) > 50:
        return body[:500]
    title = item.get('title', '')
    if title:
        return title
    return item.get('summary', '') or ""


def compute_sha1(text: str) -> str:
    """Compute SHA1 hash, hex encoded."""
    return hashlib.sha1(text.encode('utf-8')).hexdigest()


def slugify(text: str, max_len: int = 64) -> str:
    """Create URL-safe slug."""
    if not text:
        return ""
    text = unicodedata.normalize('NFKC', text).lower()
    text = re.sub(r'[^a-z0-9]+', '-', text)
    text = re.sub(r'-+', '-', text).strip('-')
    return text[:max_len]


def process_item(item: dict) -> dict:
    """
    Add final clustering fields:
    - brand_effective
    - cluster_id_coarse (primary identity)
    - issue_atoms (attribute)
    - cluster_key_llm, cluster_key_norm (attributes)
    """
    # Rename original cluster_key
    llm_key = item.get('cluster_key', '')
    item['cluster_key_llm'] = llm_key
    item['cluster_key_norm'] = slugify(llm_key)
    if 'cluster_key' in item:
        del item['cluster_key']
    
    # Compute brand_effective
    brand_effective = get_brand_effective(item)
    item['brand_effective'] = brand_effective
    
    # Get issue_type
    issue_type = item.get('issue_type', 'other') or 'other'
    
    # Extract issue atoms (attribute only)
    source_text = get_source_text(item)
    atoms = extract_issue_atoms(source_text)
    item['issue_atoms'] = atoms
    
    # Compute cluster_id_coarse (PRIMARY IDENTITY)
    # ONLY brand_effective and issue_type, nothing else
    cluster_signature = f"{brand_effective}|{issue_type}"
    item['cluster_signature'] = cluster_signature
    item['cluster_id_coarse'] = compute_sha1(cluster_signature)
    
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
    
    input_files = glob.glob(args.input)
    if not input_files:
        print(f"No files match pattern: {args.input}")
        return
    
    print(f"Processing {len(input_files)} input file(s)")
    
    output_path = Path(args.output)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    
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
                    
                    item = process_item(item)
                    fout.write(json.dumps(item) + "\n")
                    processed += 1
                    
                    if processed % 50000 == 0:
                        print(f"    Processed: {processed}")
            
            if args.max_items and processed >= args.max_items:
                break
    
    print(f"\n=== Complete ===")
    print(f"  Processed: {processed}")
    print(f"  Skipped: {skipped}")
    print(f"  Output: {output_path}")


if __name__ == "__main__":
    main()
