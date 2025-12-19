#!/usr/bin/env python3
"""
Stage 2A: Stratified Calibration Sample

Creates a stratified sample from routed.jsonl for calibration:
- Sample by brand (top brands get proportional representation)
- Sample by subreddit diversity
- Sample by priority score buckets

Then runs Stage 2 enrichment and generates calibration report.
"""

import argparse
import json
import random
import os
from collections import defaultdict
from pathlib import Path


def parse_args():
    parser = argparse.ArgumentParser(description="Create stratified calibration sample")
    parser.add_argument("--input", required=True, help="Path to routed.jsonl")
    parser.add_argument("--output", required=True, help="Path to write sample.jsonl")
    parser.add_argument("--sample-size", type=int, default=75000, help="Target sample size")
    parser.add_argument("--seed", type=int, default=42, help="Random seed")
    return parser.parse_args()


def create_sample(args):
    random.seed(args.seed)
    
    # First pass: collect statistics
    print("Pass 1: Collecting statistics...")
    brand_items = defaultdict(list)  # brand -> list of (line_num, priority)
    subreddit_items = defaultdict(list)
    priority_buckets = defaultdict(list)  # bucket -> list of line_nums
    
    line_count = 0
    with open(args.input, "r") as f:
        for line_num, line in enumerate(f):
            if not line.strip():
                continue
            try:
                item = json.loads(line.strip())
            except json.JSONDecodeError:
                continue
            
            line_count += 1
            brands = item.get("brand_hints", [])
            subreddit = item.get("subreddit", "")
            priority = item.get("priority", 0)
            
            # Primary brand for stratification
            primary_brand = brands[0] if brands else "unknown"
            brand_items[primary_brand].append(line_num)
            
            subreddit_items[subreddit].append(line_num)
            
            # Priority buckets: low (1-10), medium (11-20), high (21+)
            if priority <= 10:
                priority_buckets["low"].append(line_num)
            elif priority <= 20:
                priority_buckets["medium"].append(line_num)
            else:
                priority_buckets["high"].append(line_num)
    
    print(f"Total items: {line_count}")
    print(f"Unique brands: {len(brand_items)}")
    print(f"Unique subreddits: {len(subreddit_items)}")
    print(f"Priority buckets: low={len(priority_buckets['low'])}, medium={len(priority_buckets['medium'])}, high={len(priority_buckets['high'])}")
    
    # Stratified sampling strategy:
    # - 40% from top 20 brands (proportional)
    # - 20% from remaining brands (random)
    # - 20% from priority bucket distribution
    # - 20% pure random
    
    sample_size = args.sample_size
    selected = set()
    
    # 1. Top 20 brands: 40%
    top_brands_budget = int(sample_size * 0.4)
    top_brands = sorted(brand_items.items(), key=lambda x: len(x[1]), reverse=True)[:20]
    total_top = sum(len(items) for _, items in top_brands)
    
    print(f"\nTop 20 brands (sampling {top_brands_budget}):")
    for brand, items in top_brands[:10]:
        print(f"  {brand}: {len(items)} items")
    
    for brand, items in top_brands:
        brand_quota = int(top_brands_budget * len(items) / total_top)
        brand_quota = min(brand_quota, len(items))
        sampled = random.sample(items, brand_quota)
        selected.update(sampled)
    
    print(f"After top brands: {len(selected)} selected")
    
    # 2. Remaining brands: 20%
    remaining_budget = int(sample_size * 0.2)
    remaining_brands = [b for b in brand_items.keys() if b not in dict(top_brands)]
    remaining_items = []
    for brand in remaining_brands:
        remaining_items.extend(brand_items[brand])
    random.shuffle(remaining_items)
    remaining_sample = [ln for ln in remaining_items[:remaining_budget] if ln not in selected]
    selected.update(remaining_sample)
    
    print(f"After remaining brands: {len(selected)} selected")
    
    # 3. Priority bucket distribution: 20%
    priority_budget = int(sample_size * 0.2)
    for bucket, items in priority_buckets.items():
        bucket_quota = priority_budget // 3
        available = [ln for ln in items if ln not in selected]
        random.shuffle(available)
        sample = available[:bucket_quota]
        selected.update(sample)
    
    print(f"After priority buckets: {len(selected)} selected")
    
    # 4. Pure random to fill remaining: 20%
    random_budget = sample_size - len(selected)
    if random_budget > 0:
        all_items = list(range(line_count))
        available = [ln for ln in all_items if ln not in selected]
        random.shuffle(available)
        selected.update(available[:random_budget])
    
    print(f"Final sample size: {len(selected)}")
    
    # Second pass: extract selected items
    print("\nPass 2: Extracting selected items...")
    selected_list = sorted(selected)
    selected_set = set(selected_list)
    
    extracted = []
    with open(args.input, "r") as f:
        for line_num, line in enumerate(f):
            if line_num in selected_set:
                extracted.append(line)
    
    # Shuffle to avoid ordering bias
    random.shuffle(extracted)
    
    # Write output
    with open(args.output, "w") as f:
        for line in extracted:
            f.write(line)
    
    print(f"Wrote {len(extracted)} items to {args.output}")
    
    # Print sample stats
    print("\n=== Sample Statistics ===")
    brand_dist = defaultdict(int)
    priority_dist = defaultdict(int)
    for line in extracted:
        try:
            item = json.loads(line.strip())
            brands = item.get("brand_hints", [])
            primary = brands[0] if brands else "unknown"
            brand_dist[primary] += 1
            priority = item.get("priority", 0)
            if priority <= 10:
                priority_dist["low"] += 1
            elif priority <= 20:
                priority_dist["medium"] += 1
            else:
                priority_dist["high"] += 1
        except:
            pass
    
    print(f"Top brands in sample:")
    for brand, count in sorted(brand_dist.items(), key=lambda x: -x[1])[:15]:
        print(f"  {brand}: {count} ({100*count/len(extracted):.1f}%)")
    
    print(f"\nPriority distribution:")
    for bucket, count in priority_dist.items():
        print(f"  {bucket}: {count} ({100*count/len(extracted):.1f}%)")


if __name__ == "__main__":
    args = parse_args()
    create_sample(args)
