#!/usr/bin/env python3
"""
Reddit Funnel - Stage 3: Clustering & Aggregation

Reads enriched.jsonl from Stage 2, clusters by brand and issue type,
and generates summary reports.
"""

import argparse
import json
import csv
from collections import defaultdict
from pathlib import Path
from datetime import datetime


def parse_args():
    parser = argparse.ArgumentParser(description="Reddit Funnel Stage 3: Clustering")
    parser.add_argument("--input", required=True, help="Path to enriched.jsonl from Stage 2")
    parser.add_argument("--output-dir", required=True, help="Directory for output files")
    parser.add_argument("--top-n", type=int, default=50, help="Top N brands/clusters to export")
    return parser.parse_args()


def process(args):
    input_path = Path(args.input)
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    
    # Aggregation structures
    brand_stats = defaultdict(lambda: {
        "count": 0,
        "actionable": 0,
        "issue_types": defaultdict(int),
        "severities": defaultdict(int),
        "subreddits": defaultdict(int),
        "sample_ids": [],
        "clusters": defaultdict(int)
    })
    
    cluster_stats = defaultdict(lambda: {
        "count": 0,
        "brand": None,
        "issue_type": None,
        "severity_max": "none",
        "subreddits": defaultdict(int),
        "sample_ids": []
    })
    
    total = 0
    actionable_total = 0
    no_brand = 0
    
    severity_order = {"none": 0, "low": 1, "medium": 2, "high": 3}
    
    with open(input_path, "r") as f:
        for line in f:
            if not line.strip():
                continue
            try:
                item = json.loads(line.strip())
            except json.JSONDecodeError:
                continue
            
            total += 1
            brand = item.get("canonical_brand")
            is_actionable = item.get("is_actionable", False)
            issue_type = item.get("issue_type", "other")
            severity = item.get("severity", "none")
            subreddit = item.get("subreddit", "")
            cluster_key = item.get("cluster_key", "")
            item_id = item.get("id", "")
            
            if is_actionable:
                actionable_total += 1
            
            if not brand:
                no_brand += 1
                continue
            
            brand_lower = brand.lower()
            
            # Update brand stats
            bs = brand_stats[brand_lower]
            bs["count"] += 1
            if is_actionable:
                bs["actionable"] += 1
            bs["issue_types"][issue_type] += 1
            bs["severities"][severity] += 1
            bs["subreddits"][subreddit] += 1
            if len(bs["sample_ids"]) < 50:
                bs["sample_ids"].append(item_id)
            if cluster_key:
                bs["clusters"][cluster_key] += 1
            
            # Update cluster stats
            if cluster_key:
                cs = cluster_stats[cluster_key]
                cs["count"] += 1
                cs["brand"] = brand_lower
                cs["issue_type"] = issue_type
                if severity_order.get(severity, 0) > severity_order.get(cs["severity_max"], 0):
                    cs["severity_max"] = severity
                cs["subreddits"][subreddit] += 1
                if len(cs["sample_ids"]) < 50:
                    cs["sample_ids"].append(item_id)
    
    print(f"=== Stage 3 Complete ===")
    print(f"Total items: {total}")
    print(f"Actionable: {actionable_total} ({100*actionable_total/max(1,total):.1f}%)")
    print(f"No brand: {no_brand}")
    print(f"Unique brands: {len(brand_stats)}")
    print(f"Unique clusters: {len(cluster_stats)}")
    
    # Export top brands
    top_brands = sorted(brand_stats.items(), key=lambda x: x[1]["count"], reverse=True)[:args.top_n]
    
    brands_csv = output_dir / "brands_top.csv"
    with open(brands_csv, "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["brand", "count", "actionable", "top_issue_type", "top_severity", "top_subreddit"])
        for brand, stats in top_brands:
            top_issue = max(stats["issue_types"].items(), key=lambda x: x[1])[0] if stats["issue_types"] else ""
            top_sev = max(stats["severities"].items(), key=lambda x: severity_order.get(x[0], 0))[0] if stats["severities"] else ""
            top_sub = max(stats["subreddits"].items(), key=lambda x: x[1])[0] if stats["subreddits"] else ""
            writer.writerow([brand, stats["count"], stats["actionable"], top_issue, top_sev, top_sub])
    print(f"Wrote: {brands_csv}")
    
    # Export top clusters
    top_clusters = sorted(cluster_stats.items(), key=lambda x: x[1]["count"], reverse=True)[:args.top_n]
    
    clusters_csv = output_dir / "clusters_top.csv"
    with open(clusters_csv, "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["cluster_key", "brand", "count", "issue_type", "severity_max", "top_subreddit"])
        for key, stats in top_clusters:
            top_sub = max(stats["subreddits"].items(), key=lambda x: x[1])[0] if stats["subreddits"] else ""
            writer.writerow([key, stats["brand"], stats["count"], stats["issue_type"], stats["severity_max"], top_sub])
    print(f"Wrote: {clusters_csv}")
    
    # Export sample enriched items
    sample_jsonl = output_dir / "enriched_sample.jsonl"
    with open(input_path, "r") as fin, open(sample_jsonl, "w") as fout:
        count = 0
        for line in fin:
            if count >= 1000:
                break
            if not line.strip():
                continue
            try:
                item = json.loads(line.strip())
                if item.get("is_actionable"):
                    fout.write(line)
                    count += 1
            except json.JSONDecodeError:
                continue
    print(f"Wrote: {sample_jsonl} ({count} actionable samples)")
    
    # Export full brand stats as JSON
    brands_json = output_dir / "brand_stats.json"
    export_stats = {}
    for brand, stats in brand_stats.items():
        export_stats[brand] = {
            "count": stats["count"],
            "actionable": stats["actionable"],
            "issue_types": dict(stats["issue_types"]),
            "severities": dict(stats["severities"]),
            "top_subreddits": dict(sorted(stats["subreddits"].items(), key=lambda x: -x[1])[:10]),
            "top_clusters": dict(sorted(stats["clusters"].items(), key=lambda x: -x[1])[:10])
        }
    with open(brands_json, "w") as f:
        json.dump(export_stats, f, indent=2)
    print(f"Wrote: {brands_json}")


if __name__ == "__main__":
    args = parse_args()
    process(args)
