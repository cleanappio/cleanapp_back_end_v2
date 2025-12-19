#!/usr/bin/env python3
"""
Reddit Funnel - Stage 2.5 v2: Improved Deterministic Clustering

Creates two-tier deterministic cluster IDs:
- cluster_id_coarse: Groups broadly (3 bucket tokens)
- cluster_id_fine: Groups specifically (6 bucket tokens)

Usage:
  python3 stage2_postprocess_v2.py \
    --input output_full/shards/*.jsonl \
    --output output_full/enriched_clustered.jsonl
"""

import argparse
import json
import hashlib
import re
import unicodedata
from pathlib import Path
from typing import Optional, List, Set, Tuple
from urllib.parse import urlparse
import glob

# English stopwords + filler words that cause false collisions
STOPWORDS = {
    'a', 'an', 'the', 'and', 'or', 'but', 'in', 'on', 'at', 'to', 'for',
    'of', 'with', 'by', 'from', 'as', 'is', 'was', 'are', 'were', 'been',
    'be', 'have', 'has', 'had', 'do', 'does', 'did', 'will', 'would',
    'could', 'should', 'may', 'might', 'must', 'can', 'this', 'that',
    'these', 'those', 'i', 'you', 'he', 'she', 'it', 'we', 'they', 'my',
    'your', 'his', 'her', 'its', 'our', 'their', 'what', 'which', 'who',
    'when', 'where', 'why', 'how', 'all', 'each', 'every', 'both', 'few',
    'more', 'most', 'other', 'some', 'such', 'no', 'not', 'only', 'own',
    'im', 'youre', 'dont', 'cant', 'wont', 'isnt'
}

# Controlled vocabulary of issue atoms for low-entropy bucketing
# Each entry: (canonical_atom, [trigger_words])
ISSUE_ATOMS = {
    # Authentication / Account
    "login": ["login", "log in", "signin", "sign in", "logout", "logged out", "cant login", "cant log in"],
    "password": ["password", "passwd", "reset password", "forgot password", "change password"],
    "account": ["account", "my account", "account banned", "account suspended", "account locked"],
    "ban": ["banned", "ban", "suspended", "suspension", "terminated", "termination"],
    "verify": ["verify", "verification", "2fa", "two factor", "authenticator", "otp", "code"],
    
    # Technical Issues
    "crash": ["crash", "crashes", "crashing", "crashed", "freezes", "freeze", "frozen", "hang", "hangs"],
    "bug": ["bug", "bugs", "glitch", "glitches", "broken", "doesnt work", "not working", "wont work"],
    "error": ["error", "errors", "error code", "error message", "failed", "failure"],
    "update": ["update", "updates", "updating", "updated", "upgrade", "patch", "patched"],
    "install": ["install", "installing", "installation", "uninstall", "reinstall", "download"],
    "load": ["loading", "load", "loads", "wont load", "not loading", "stuck loading", "infinite load"],
    "sync": ["sync", "syncing", "synchronize", "not syncing", "out of sync"],
    "connect": ["connect", "connection", "disconnect", "disconnected", "cant connect", "no connection"],
    
    # Performance
    "slow": ["slow", "slower", "lag", "laggy", "lagging", "latency", "delay", "delayed"],
    "performance": ["performance", "fps", "frame rate", "framerate", "stuttering", "stutter"],
    
    # Payment / Billing
    "payment": ["payment", "pay", "paid", "paying", "charge", "charged", "charges"],
    "refund": ["refund", "refunded", "refunds", "money back", "chargeback"],
    "subscription": ["subscription", "subscribe", "subscribed", "cancel subscription", "recurring"],
    "billing": ["billing", "bill", "billed", "invoice", "receipt"],
    "price": ["price", "pricing", "cost", "expensive", "cheap", "free", "premium"],
    
    # Content Moderation
    "moderation": ["moderation", "moderator", "mod", "mods", "removed", "removal", "taken down"],
    "spam": ["spam", "spammer", "spamming", "bot", "bots", "fake"],
    "report": ["report", "reported", "reporting", "flag", "flagged"],
    "appeal": ["appeal", "appealed", "appealing", "dispute", "contested"],
    
    # UI/UX
    "ui": ["ui", "interface", "design", "layout", "button", "buttons", "menu"],
    "display": ["display", "screen", "showing", "not showing", "visible", "invisible", "hidden"],
    "notification": ["notification", "notifications", "notify", "alert", "alerts", "push"],
    "settings": ["settings", "preferences", "options", "configure", "configuration"],
    
    # Data / Privacy
    "data": ["data", "my data", "personal data", "information", "privacy"],
    "delete": ["delete", "deleted", "deletion", "remove", "removed", "erase"],
    "backup": ["backup", "restore", "recovery", "recover", "lost data"],
    
    # Support
    "support": ["support", "customer support", "help", "assistance", "contact"],
    "response": ["response", "respond", "reply", "no response", "waiting"],
    
    # Common Product Issues
    "shipping": ["shipping", "shipped", "delivery", "delivered", "tracking", "package"],
    "quality": ["quality", "defective", "damaged", "broken", "faulty"],
    "return": ["return", "returns", "returned", "exchange", "replacement"],
    
    # Platform Specific
    "app": ["app", "application", "mobile app", "ios app", "android app"],
    "website": ["website", "site", "web", "browser", "webpage"],
    "server": ["server", "servers", "down", "outage", "maintenance", "offline"],
}

# Build reverse lookup: word -> atom
ATOM_LOOKUP = {}
for atom, triggers in ISSUE_ATOMS.items():
    for trigger in triggers:
        # Normalize trigger
        trigger_norm = trigger.lower().strip()
        ATOM_LOOKUP[trigger_norm] = atom


def extract_atoms(text: str, max_atoms: int = 3) -> List[str]:
    """
    Extract issue atoms from text using controlled vocabulary.
    Returns up to max_atoms matched atoms, sorted alphabetically.
    """
    if not text:
        return []
    
    text_lower = text.lower()
    
    # Find all matching atoms
    matched = set()
    
    # Check multi-word triggers first (longer matches)
    for trigger, atom in sorted(ATOM_LOOKUP.items(), key=lambda x: -len(x[0])):
        if trigger in text_lower:
            matched.add(atom)
            if len(matched) >= max_atoms * 2:  # Get more than needed, then take top
                break
    
    # Sort and take top max_atoms
    atoms = sorted(matched)[:max_atoms]
    
    return atoms


def atoms_bucket(text: str, max_atoms: int = 3) -> str:
    """
    Create atoms bucket string.
    Returns sorted atoms joined by underscore, or "generic" if none match.
    """
    atoms = extract_atoms(text, max_atoms)
    if not atoms:
        return "generic"
    return "_".join(atoms)


# Brand-specific subreddits (lowercase)
BRAND_SUBREDDITS = {
    'apple', 'iphone', 'ipad', 'mac', 'macos', 'ios', 'watchos',
    'google', 'android', 'pixel', 'chrome', 'chromeos', 'gmail',
    'microsoft', 'windows', 'windows10', 'windows11', 'xbox', 'office365',
    'amazon', 'aws', 'alexa', 'kindle', 'prime',
    'steam', 'valve', 'steamdeck',
    'discord', 'slack', 'zoom', 'teams',
    'spotify', 'netflix', 'hulu', 'disneyplus',
    'youtube', 'twitch', 'tiktok', 'instagram', 'facebook', 'twitter',
    'nvidia', 'amd', 'intel', 'asus', 'msi', 'gigabyte', 'corsair',
    'samsung', 'sony', 'lg', 'oneplus', 'xiaomi', 'huawei',
    'nintendo', 'playstation', 'ps4', 'ps5',
    'tesla', 'uber', 'lyft', 'doordash', 'grubhub', 'instacart',
    'airbnb', 'dropbox', 'paypal', 'venmo', 'cashapp', 'robinhood',
    'openai', 'chatgpt', 'midjourney', 'stablediffusion',
    'github', 'gitlab', 'bitbucket', 'notion', 'figma', 'canva'
}


def parse_args():
    parser = argparse.ArgumentParser(description="Stage 2.5 v2: Improved Clustering")
    parser.add_argument("--input", required=True, help="Input JSONL (supports glob)")
    parser.add_argument("--output", required=True, help="Output JSONL file")
    parser.add_argument("--max-items", type=int, default=None, help="Max items (testing)")
    parser.add_argument("--skip-existing", action="store_true", help="Skip processed IDs")
    return parser.parse_args()


def normalize_text(text: str) -> str:
    """Unicode normalize and lowercase."""
    if not text:
        return ""
    text = unicodedata.normalize('NFKC', text)
    return text.lower()


def extract_registrable_domain(url: str) -> Optional[str]:
    """
    Extract registrable domain from URL.
    e.g., "https://www.paypal.com/path" -> "paypal"
    """
    if not url:
        return None
    try:
        parsed = urlparse(url)
        host = parsed.netloc or parsed.path.split('/')[0]
        host = host.lower()
        
        # Remove www prefix
        if host.startswith('www.'):
            host = host[4:]
        
        # Remove common TLDs to get domain name
        parts = host.split('.')
        if len(parts) >= 2:
            # Handle common multi-part TLDs
            if parts[-1] in ('uk', 'au', 'jp', 'de', 'fr', 'ca', 'br'):
                if len(parts) >= 3:
                    return parts[-3]
            return parts[-2]
        return parts[0] if parts else None
    except:
        return None


def get_brand_effective(item: dict) -> str:
    """
    Get effective brand with fallbacks:
    1. canonical_brand (if present and not unknown)
    2. registrable_domain from URL
    3. inferred from subreddit name
    4. "unknown"
    """
    # 1. Check canonical_brand
    brand = item.get('canonical_brand')
    if brand and brand.lower() not in ('unknown', 'null', 'none', ''):
        return brand.lower()
    
    # 2. Check URL domain
    url = item.get('url', '')
    domain = extract_registrable_domain(url)
    if domain and len(domain) > 2:
        # Clean up common non-brand domains
        if domain not in ('reddit', 'redd', 'imgur', 'gfycat', 'giphy', 'youtube', 'youtu'):
            return domain
    
    # 3. Check subreddit
    subreddit = (item.get('subreddit') or '').lower()
    if subreddit in BRAND_SUBREDDITS:
        # Map some subreddits to canonical brands
        subreddit_map = {
            'iphone': 'apple', 'ipad': 'apple', 'mac': 'apple', 'macos': 'apple',
            'ios': 'apple', 'watchos': 'apple',
            'android': 'google', 'pixel': 'google', 'chrome': 'google',
            'chromeos': 'google', 'gmail': 'google',
            'windows': 'microsoft', 'windows10': 'microsoft', 'windows11': 'microsoft',
            'xbox': 'microsoft', 'office365': 'microsoft',
            'aws': 'amazon', 'alexa': 'amazon', 'kindle': 'amazon', 'prime': 'amazon',
            'valve': 'steam', 'steamdeck': 'steam',
            'ps4': 'playstation', 'ps5': 'playstation',
            'chatgpt': 'openai'
        }
        return subreddit_map.get(subreddit, subreddit)
    
    # 4. Check brand_hints
    hints = item.get('original_brand_hints') or item.get('brand_hints') or []
    if hints and len(hints) > 0:
        return hints[0].lower()
    
    return "unknown"


def bucket_tokens(text: str, k: int) -> str:
    """
    Extract k bucket tokens from text:
    1. Normalize and tokenize
    2. Remove stopwords and short tokens
    3. Take first k unique tokens
    4. Sort alphabetically for stability
    5. Join with underscore
    """
    if not text:
        return "empty"
    
    # Normalize
    text = normalize_text(text)
    
    # Replace punctuation with spaces
    text = re.sub(r'[^a-z0-9\s]', ' ', text)
    
    # Collapse whitespace
    text = re.sub(r'\s+', ' ', text).strip()
    
    # Split into words
    words = text.split()
    
    # Filter: remove stopwords and short tokens
    filtered = []
    seen = set()
    for w in words:
        if w in seen:
            continue
        if w in STOPWORDS:
            continue
        if len(w) < 3:
            continue
        filtered.append(w)
        seen.add(w)
        if len(filtered) >= k:
            break
    
    if not filtered:
        return "empty"
    
    # Sort alphabetically for stability
    tokens_sorted = sorted(filtered)
    
    return '_'.join(tokens_sorted)


def get_source_text(item: dict) -> str:
    """
    Get source text in order of preference:
    1. body/text (if substantial)
    2. title
    3. summary
    """
    body = item.get('body', '') or item.get('text', '')
    if body and len(body.strip()) > 50:
        return body[:500]  # Limit for efficiency
    
    title = item.get('title', '')
    if title and len(title.strip()) > 10:
        return title
    
    summary = item.get('summary', '')
    return summary or ""


def compute_sha1(text: str) -> str:
    """Compute SHA1 hash, hex encoded."""
    return hashlib.sha1(text.encode('utf-8')).hexdigest()


def slugify(text: str, max_len: int = 64) -> str:
    """Create URL-safe slug."""
    if not text:
        return ""
    text = normalize_text(text)
    text = re.sub(r'[^a-z0-9]+', '-', text)
    text = re.sub(r'-+', '-', text).strip('-')
    return text[:max_len]


def process_item(item: dict) -> dict:
    """
    Add deterministic clustering fields:
    - brand_effective
    - cluster_bucket3, cluster_bucket6
    - cluster_signature_coarse, cluster_signature_fine
    - cluster_id_coarse, cluster_id_fine
    - cluster_key_llm, cluster_key_norm
    """
    # Rename original cluster_key
    llm_key = item.get('cluster_key', '')
    item['cluster_key_llm'] = llm_key
    item['cluster_key_norm'] = slugify(llm_key)
    
    # Remove old field
    if 'cluster_key' in item:
        del item['cluster_key']
    
    # Get effective brand
    brand_effective = get_brand_effective(item)
    item['brand_effective'] = brand_effective
    
    # Get issue type
    issue_type = item.get('issue_type', 'other') or 'other'
    
    # Get source text and compute atoms buckets
    source_text = get_source_text(item)
    atoms_coarse = atoms_bucket(source_text, max_atoms=2)  # 2 atoms for coarse
    atoms_fine = atoms_bucket(source_text, max_atoms=4)    # 4 atoms for fine
    
    # Also keep cluster_key_norm for fine grain
    cluster_key_norm = item.get('cluster_key_norm', '')
    
    item['cluster_atoms_coarse'] = atoms_coarse
    item['cluster_atoms_fine'] = atoms_fine
    
    # Build signatures
    # COARSE: brand + issue_type + 2 atoms (if matched, else "generic")
    sig_coarse = f"{brand_effective}|{issue_type}|{atoms_coarse}"
    # FINE: brand + issue_type + 4 atoms + cluster_key_norm for extra specificity
    sig_fine = f"{brand_effective}|{issue_type}|{atoms_fine}|{cluster_key_norm}"
    
    item['cluster_signature_coarse'] = sig_coarse
    item['cluster_signature_fine'] = sig_fine
    
    # Compute cluster IDs
    item['cluster_id_coarse'] = compute_sha1(sig_coarse)
    item['cluster_id_fine'] = compute_sha1(sig_fine)
    
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
    
    # Expand glob pattern
    input_files = glob.glob(args.input)
    if not input_files:
        print(f"No files match pattern: {args.input}")
        return
    
    print(f"Processing {len(input_files)} input file(s)")
    
    output_path = Path(args.output)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    
    # Load existing IDs
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
