#!/usr/bin/env python3
"""
Reddit Funnel - Stage 3 v2: Clustering by Deterministic cluster_id

Groups by cluster_id_coarse (primary) and cluster_id_fine (subclusters).

Usage:
  python3 stage3_cluster_v2.py \
    --input output_full/enriched_clustered.jsonl \
    --output-dir output_full/reports
"""

import argparse
import json
import csv
from collections import defaultdict, Counter
from pathlib import Path


def parse_args():
    parser = argparse.ArgumentParser(description="Stage 3 v2: Cluster by cluster_id")
    parser.add_argument("--input", required=True, help="Path to post-processed enriched JSONL")
    parser.add_argument("--output-dir", required=True, help="Directory for output files")
    parser.add_argument("--top-n", type=int, default=50, help="Top N brands/clusters")
    parser.add_argument("--max-items", type=int, default=None, help="Max items (for testing)")
    return parser.parse_args()


def process(args):
    input_path = Path(args.input)
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    
    # Aggregation by cluster_id_coarse
    coarse_data = defaultdict(lambda: {
        "count": 0,
        "brand": None,
        "issue_type": None,
        "cluster_key_llm_counts": Counter(),
        "cluster_bucket3": None,
        "cluster_signature_coarse": None,
        "fine_clusters": Counter(),
        "severity_max": "none",
        "actionable": 0,
        "subreddits": Counter(),
        "sample_ids": []
    })
    
    # Aggregation by cluster_id_fine
    fine_data = defaultdict(lambda: {"count": 0})
    
    # Brand aggregation
    brand_stats = defaultdict(lambda: {
        "count": 0,
        "actionable": 0,
        "issue_types": Counter(),
        "coarse_clusters": set()
    })
    
    severity_order = {"none": 0, "low": 1, "medium": 2, "high": 3}
    
    total = 0
    actionable_total = 0
    
    with open(input_path, "r") as f:
        for line in f:
            if not line.strip():
                continue
            if args.max_items and total >= args.max_items:
                break
            
            try:
                item = json.loads(line.strip())
            except json.JSONDecodeError:
                continue
            
            total += 1
            
            # Get cluster IDs (use coarse, fallback to old cluster_id)
            cluster_id_coarse = item.get("cluster_id_coarse") or item.get("cluster_id")
            cluster_id_fine = item.get("cluster_id_fine") or cluster_id_coarse
            
            if not cluster_id_coarse:
                continue
            
            brand = item.get("brand_effective") or item.get("canonical_brand") or "unknown"
            brand = brand.lower() if brand else "unknown"
            issue_type = item.get("issue_type", "other")
            severity = item.get("severity", "none")
            is_actionable = item.get("is_actionable", False)
            subreddit = item.get("subreddit", "")
            item_id = item.get("id", "")
            cluster_key_llm = item.get("cluster_key_llm", "")
            cluster_bucket3 = item.get("cluster_bucket3", "")
            cluster_signature = item.get("cluster_signature_coarse", "")
            
            if is_actionable:
                actionable_total += 1
            
            # Update coarse data
            cd = coarse_data[cluster_id_coarse]
            cd["count"] += 1
            cd["brand"] = brand
            cd["issue_type"] = issue_type
            cd["cluster_bucket3"] = cluster_bucket3
            cd["cluster_signature_coarse"] = cluster_signature
            
            if cluster_key_llm:
                cd["cluster_key_llm_counts"][cluster_key_llm] += 1
            
            cd["fine_clusters"][cluster_id_fine] += 1
            
            if is_actionable:
                cd["actionable"] += 1
            
            if severity_order.get(severity, 0) > severity_order.get(cd["severity_max"], 0):
                cd["severity_max"] = severity
            
            cd["subreddits"][subreddit] += 1
            
            if len(cd["sample_ids"]) < 10:
                cd["sample_ids"].append(item_id)
            
            # Update fine data
            fine_data[cluster_id_fine]["count"] += 1
            
            # Update brand stats
            bs = brand_stats[brand]
            bs["count"] += 1
            if is_actionable:
                bs["actionable"] += 1
            bs["issue_types"][issue_type] += 1
            bs["coarse_clusters"].add(cluster_id_coarse)
    
    # Compute display labels for each coarse cluster
    for cid, cd in coarse_data.items():
        if cd["cluster_key_llm_counts"]:
            cd["display_label"] = cd["cluster_key_llm_counts"].most_common(1)[0][0]
        else:
            # Fallback to brand + issue_type + bucket3
            cd["display_label"] = f"{cd['brand']}_{cd['issue_type']}_{cd['cluster_bucket3']}"
    
    # Cluster size distributions
    coarse_sizes = [cd["count"] for cd in coarse_data.values()]
    fine_sizes = [fd["count"] for fd in fine_data.values()]
    
    coarse_singleton = sum(1 for s in coarse_sizes if s == 1)
    fine_singleton = sum(1 for s in fine_sizes if s == 1)
    
    coarse_singleton_pct = 100 * coarse_singleton / max(1, len(coarse_sizes))
    fine_singleton_pct = 100 * fine_singleton / max(1, len(fine_sizes))
    
    def size_distribution(sizes):
        dist = Counter()
        for s in sizes:
            if s == 1:
                dist["1"] += 1
            elif s <= 5:
                dist["2-5"] += 1
            elif s <= 10:
                dist["6-10"] += 1
            elif s <= 50:
                dist["11-50"] += 1
            elif s <= 100:
                dist["51-100"] += 1
            else:
                dist["100+"] += 1
        return dist
    
    coarse_dist = size_distribution(coarse_sizes)
    fine_dist = size_distribution(fine_sizes)
    
    print(f"\n=== Stage 3 v2 Complete ===")
    print(f"Total items: {total}")
    print(f"Actionable: {actionable_total} ({100*actionable_total/max(1,total):.1f}%)")
    print(f"\n--- COARSE Clusters (cluster_id_coarse) ---")
    print(f"Unique: {len(coarse_data)}")
    print(f"Singleton: {coarse_singleton} ({coarse_singleton_pct:.1f}%)")
    print(f"Size distribution:")
    for bucket in ["1", "2-5", "6-10", "11-50", "51-100", "100+"]:
        if bucket in coarse_dist:
            print(f"  {bucket}: {coarse_dist[bucket]} ({100*coarse_dist[bucket]/len(coarse_data):.1f}%)")
    
    print(f"\n--- FINE Clusters (cluster_id_fine) ---")
    print(f"Unique: {len(fine_data)}")
    print(f"Singleton: {fine_singleton} ({fine_singleton_pct:.1f}%)")
    
    print(f"\nUnique brands: {len(brand_stats)}")
    
    # Top 20 coarse clusters
    top_coarse = sorted(coarse_data.items(), key=lambda x: x[1]["count"], reverse=True)[:args.top_n]
    
    print(f"\n=== Top 20 Coarse Clusters ===")
    for i, (cid, cd) in enumerate(top_coarse[:20]):
        fine_count = len(cd["fine_clusters"])
        print(f"{i+1:2}. [{cd['count']:5}] {cd['brand']:15} | {cd['issue_type']:15} | {cd['display_label'][:40]} (fine:{fine_count})")
    
    # Export coarse clusters CSV
    clusters_csv = output_dir / "clusters_coarse.csv"
    with open(clusters_csv, "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["cluster_id_coarse", "display_label", "brand", "issue_type", "count", 
                         "actionable", "fine_subclusters", "severity_max", "top_subreddit", "signature"])
        for cid, cd in top_coarse:
            top_sub = cd["subreddits"].most_common(1)[0][0] if cd["subreddits"] else ""
            writer.writerow([
                cid[:16] + "...", cd["display_label"][:60], cd["brand"], cd["issue_type"],
                cd["count"], cd["actionable"], len(cd["fine_clusters"]), cd["severity_max"], top_sub,
                (cd["cluster_signature_coarse"] or "")[:80]
            ])
    print(f"\nWrote: {clusters_csv}")
    
    # Export brands CSV
    top_brands = sorted(brand_stats.items(), key=lambda x: x[1]["count"], reverse=True)[:args.top_n]
    brands_csv = output_dir / "brands_by_cluster.csv"
    with open(brands_csv, "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["brand", "count", "actionable", "coarse_clusters", "top_issue_type"])
        for brand, bs in top_brands:
            top_issue = bs["issue_types"].most_common(1)[0][0] if bs["issue_types"] else ""
            writer.writerow([brand, bs["count"], bs["actionable"], len(bs["coarse_clusters"]), top_issue])
    print(f"Wrote: {brands_csv}")
    
    # Export stats JSON
    stats_json = output_dir / "cluster_stats.json"
    stats = {
        "total_items": total,
        "actionable": actionable_total,
        "coarse": {
            "unique": len(coarse_data),
            "singleton": coarse_singleton,
            "singleton_pct": round(coarse_singleton_pct, 2),
            "distribution": dict(coarse_dist)
        },
        "fine": {
            "unique": len(fine_data),
            "singleton": fine_singleton,
            "singleton_pct": round(fine_singleton_pct, 2),
            "distribution": dict(fine_dist)
        },
        "unique_brands": len(brand_stats),
        "top_20_coarse": [
            {
                "cluster_id": cid[:16],
                "display_label": cd["display_label"],
                "brand": cd["brand"],
                "issue_type": cd["issue_type"],
                "count": cd["count"],
                "actionable": cd["actionable"],
                "fine_subclusters": len(cd["fine_clusters"])
            }
            for cid, cd in top_coarse[:20]
        ]
    }
    with open(stats_json, "w") as f:
        json.dump(stats, f, indent=2)
    print(f"Wrote: {stats_json}")


if __name__ == "__main__":
    args = parse_args()
    process(args)
