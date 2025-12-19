#!/usr/bin/env python3
"""
Reddit Funnel - Stage 3 Final: Aggregation Only

Groups by cluster_id_coarse, aggregates stats, outputs reports.
No identity logic - clustering is done in Stage 2.

Usage:
  python3 stage3_final.py \
    --input output_full/enriched_final.jsonl \
    --output-dir output_full/reports
"""

import argparse
import json
import csv
from collections import defaultdict, Counter
from pathlib import Path


def parse_args():
    parser = argparse.ArgumentParser(description="Stage 3: Aggregation")
    parser.add_argument("--input", required=True, help="Path to enriched_final.jsonl")
    parser.add_argument("--output-dir", required=True, help="Directory for outputs")
    parser.add_argument("--top-n", type=int, default=100, help="Top N for reports")
    parser.add_argument("--max-items", type=int, default=None, help="Max items (testing)")
    return parser.parse_args()


def process(args):
    input_path = Path(args.input)
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    
    severity_order = {"none": 0, "low": 1, "medium": 2, "high": 3}
    
    # Cluster aggregation
    clusters = defaultdict(lambda: {
        "count": 0,
        "actionable": 0,
        "brand": None,
        "issue_type": None,
        "cluster_signature": None,
        "severity_counts": Counter(),
        "subreddits": Counter(),
        "issue_atoms": Counter(),
        "cluster_key_llm_counts": Counter(),
        "sample_ids": []
    })
    
    # Brand aggregation
    brands = defaultdict(lambda: {
        "count": 0,
        "actionable": 0,
        "issue_types": Counter(),
        "severities": Counter(),
        "cluster_ids": set()
    })
    
    total = 0
    actionable_total = 0
    
    print(f"Reading: {input_path}")
    
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
            
            cluster_id = item.get("cluster_id_coarse")
            if not cluster_id:
                continue
            
            brand = item.get("brand_effective", "unknown")
            issue_type = item.get("issue_type", "other")
            severity = item.get("severity", "none")
            is_actionable = item.get("is_actionable", False)
            subreddit = item.get("subreddit", "")
            item_id = item.get("id", "")
            cluster_key_llm = item.get("cluster_key_llm", "")
            issue_atoms = item.get("issue_atoms", [])
            cluster_signature = item.get("cluster_signature", "")
            
            if is_actionable:
                actionable_total += 1
            
            # Update cluster
            c = clusters[cluster_id]
            c["count"] += 1
            c["brand"] = brand
            c["issue_type"] = issue_type
            c["cluster_signature"] = cluster_signature
            
            if is_actionable:
                c["actionable"] += 1
            
            c["severity_counts"][severity] += 1
            c["subreddits"][subreddit] += 1
            
            for atom in issue_atoms:
                c["issue_atoms"][atom] += 1
            
            if cluster_key_llm:
                c["cluster_key_llm_counts"][cluster_key_llm] += 1
            
            if len(c["sample_ids"]) < 10:
                c["sample_ids"].append(item_id)
            
            # Update brand
            b = brands[brand]
            b["count"] += 1
            if is_actionable:
                b["actionable"] += 1
            b["issue_types"][issue_type] += 1
            b["severities"][severity] += 1
            b["cluster_ids"].add(cluster_id)
            
            if total % 100000 == 0:
                print(f"  Processed: {total}")
    
    # Compute display labels
    for cid, c in clusters.items():
        if c["cluster_key_llm_counts"]:
            c["display_label"] = c["cluster_key_llm_counts"].most_common(1)[0][0]
        else:
            c["display_label"] = f"{c['brand']} | {c['issue_type']}"
    
    # Cluster size distribution
    sizes = [c["count"] for c in clusters.values()]
    singleton_count = sum(1 for s in sizes if s == 1)
    singleton_pct = 100 * singleton_count / max(1, len(sizes))
    
    size_dist = Counter()
    for s in sizes:
        if s == 1:
            size_dist["1"] += 1
        elif s <= 5:
            size_dist["2-5"] += 1
        elif s <= 10:
            size_dist["6-10"] += 1
        elif s <= 50:
            size_dist["11-50"] += 1
        elif s <= 100:
            size_dist["51-100"] += 1
        else:
            size_dist["100+"] += 1
    
    print(f"\n=== Stage 3 Complete ===")
    print(f"Total items: {total}")
    print(f"Actionable: {actionable_total} ({100*actionable_total/max(1,total):.1f}%)")
    print(f"\n--- Clusters (cluster_id_coarse) ---")
    print(f"Unique: {len(clusters)}")
    print(f"Singleton: {singleton_count} ({singleton_pct:.1f}%)")
    print(f"Size distribution:")
    for bucket in ["1", "2-5", "6-10", "11-50", "51-100", "100+"]:
        if bucket in size_dist:
            pct = 100 * size_dist[bucket] / len(clusters)
            print(f"  {bucket}: {size_dist[bucket]} ({pct:.1f}%)")
    
    print(f"\nUnique brands: {len(brands)}")
    
    # Top clusters
    top_clusters = sorted(clusters.items(), key=lambda x: x[1]["count"], reverse=True)[:args.top_n]
    
    print(f"\n=== Top 20 Clusters ===")
    for i, (cid, c) in enumerate(top_clusters[:20]):
        top_atoms = ", ".join([a for a, _ in c["issue_atoms"].most_common(3)]) or "-"
        print(f"{i+1:2}. [{c['count']:6}] {c['brand']:15} | {c['issue_type']:15} | atoms: {top_atoms}")
    
    # Export clusters CSV
    clusters_csv = output_dir / "clusters_top.csv"
    with open(clusters_csv, "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["cluster_id", "display_label", "brand", "issue_type", "count", 
                         "actionable", "actionable_pct", "top_severity", "top_atoms", 
                         "top_subreddit", "signature"])
        for cid, c in top_clusters:
            top_sub = c["subreddits"].most_common(1)[0][0] if c["subreddits"] else ""
            top_sev = max(c["severity_counts"].keys(), key=lambda x: severity_order.get(x, 0)) if c["severity_counts"] else "none"
            top_atoms = "_".join([a for a, _ in c["issue_atoms"].most_common(3)])
            act_pct = round(100 * c["actionable"] / max(1, c["count"]), 1)
            writer.writerow([
                cid[:16], c["display_label"][:60], c["brand"], c["issue_type"],
                c["count"], c["actionable"], act_pct, top_sev, top_atoms, top_sub,
                c["cluster_signature"][:50]
            ])
    print(f"\nWrote: {clusters_csv}")
    
    # Export brands CSV
    top_brands = sorted(brands.items(), key=lambda x: x[1]["count"], reverse=True)[:args.top_n]
    brands_csv = output_dir / "brands_top.csv"
    with open(brands_csv, "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["brand", "count", "actionable", "actionable_pct", "clusters", "top_issue_type"])
        for brand, b in top_brands:
            top_issue = b["issue_types"].most_common(1)[0][0] if b["issue_types"] else ""
            act_pct = round(100 * b["actionable"] / max(1, b["count"]), 1)
            writer.writerow([brand, b["count"], b["actionable"], act_pct, len(b["cluster_ids"]), top_issue])
    print(f"Wrote: {brands_csv}")
    
    # Export brand stats JSON
    brand_stats = {}
    for brand, b in brands.items():
        brand_stats[brand] = {
            "count": b["count"],
            "actionable": b["actionable"],
            "clusters": len(b["cluster_ids"]),
            "issue_types": dict(b["issue_types"]),
            "severities": dict(b["severities"])
        }
    brand_json = output_dir / "brand_stats.json"
    with open(brand_json, "w") as f:
        json.dump(brand_stats, f, indent=2)
    print(f"Wrote: {brand_json}")
    
    # Export cluster stats JSON
    stats = {
        "total_items": total,
        "actionable": actionable_total,
        "actionable_pct": round(100 * actionable_total / max(1, total), 2),
        "clusters": {
            "unique": len(clusters),
            "singleton": singleton_count,
            "singleton_pct": round(singleton_pct, 2),
            "size_distribution": dict(size_dist)
        },
        "brands": {
            "unique": len(brands)
        },
        "top_20": [
            {
                "cluster_id": cid[:16],
                "display_label": c["display_label"],
                "brand": c["brand"],
                "issue_type": c["issue_type"],
                "count": c["count"],
                "actionable": c["actionable"],
                "top_atoms": [a for a, _ in c["issue_atoms"].most_common(5)]
            }
            for cid, c in top_clusters[:20]
        ]
    }
    stats_json = output_dir / "cluster_stats.json"
    with open(stats_json, "w") as f:
        json.dump(stats, f, indent=2)
    print(f"Wrote: {stats_json}")
    
    # Export actionable sample
    sample_jsonl = output_dir / "enriched_sample.jsonl"
    sample_count = 0
    with open(input_path, "r") as fin, open(sample_jsonl, "w") as fout:
        for line in fin:
            if sample_count >= 1000:
                break
            try:
                item = json.loads(line.strip())
                if item.get("is_actionable"):
                    fout.write(line)
                    sample_count += 1
            except:
                continue
    print(f"Wrote: {sample_jsonl} ({sample_count} samples)")


if __name__ == "__main__":
    args = parse_args()
    process(args)
