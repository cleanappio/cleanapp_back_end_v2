#!/usr/bin/env python3
"""
Stage 2A Calibration Analysis

Analyzes calibration_enriched.jsonl to validate:
1. Brand normalization
2. Cluster formation
3. Issue taxonomy
4. Cost & throughput

Generates reports for calibration review.
"""

import argparse
import json
from collections import defaultdict, Counter
from pathlib import Path
import csv

# Canonical enums for post-processing
CANONICAL_ISSUE_TYPES = {
    "bug": ["bug", "error", "crash", "broken", "glitch", "defect", "malfunction"],
    "outage": ["outage", "down", "offline", "unavailable", "service disruption", "server"],
    "ux": ["ux", "usability", "ui", "interface", "design", "confusing", "hard to use", "annoying"],
    "account": ["account", "login", "password", "authentication", "access", "banned", "suspended", "locked"],
    "billing": ["billing", "payment", "charge", "refund", "subscription", "price", "cost", "invoice"],
    "policy": ["policy", "terms", "moderation", "censorship", "content", "rules", "violation"],
    "security": ["security", "privacy", "hack", "breach", "vulnerability", "data", "leak"],
    "performance": ["performance", "slow", "lag", "latency", "speed", "loading", "timeout", "hang"],
    "feature_request": ["feature", "request", "suggestion", "wish", "want", "need", "improvement"],
    "other": ["other", "general", "misc", "unknown"],
    "not_applicable": ["not_applicable", "na", "none", "n/a", "unrelated"]
}

CANONICAL_SEVERITIES = {
    "none": ["none", "na", "n/a", "not applicable", "unrelated"],
    "low": ["low", "minor", "trivial", "cosmetic", "small"],
    "medium": ["medium", "moderate", "normal", "standard", "average"],
    "high": ["high", "major", "critical", "severe", "urgent", "serious", "blocking"]
}


def parse_args():
    parser = argparse.ArgumentParser(description="Stage 2A Calibration Analysis")
    parser.add_argument("--input", required=True, help="Path to calibration_enriched.jsonl")
    parser.add_argument("--output-dir", required=True, help="Directory for analysis reports")
    return parser.parse_args()


def map_to_canonical(raw_value: str, mapping: dict) -> tuple:
    """Map raw value to canonical enum. Returns (canonical, confidence, is_exact)."""
    if not raw_value:
        return ("other", 0.0, False)
    
    raw_lower = raw_value.lower().strip()
    
    # Check for exact match in canonical keys
    for canonical, variants in mapping.items():
        if raw_lower == canonical:
            return (canonical, 1.0, True)
        if raw_lower in variants:
            return (canonical, 0.9, True)
    
    # Fuzzy match: check if any variant is contained in raw value
    for canonical, variants in mapping.items():
        for variant in variants:
            if variant in raw_lower or raw_lower in variant:
                return (canonical, 0.7, False)
    
    return ("other", 0.3, False)


def analyze(args):
    input_path = Path(args.input)
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    
    # Collect data
    items = []
    raw_issue_types = Counter()
    raw_severities = Counter()
    brand_counts = Counter()
    brand_issue_heatmap = defaultdict(Counter)
    actionable_count = 0
    total_count = 0
    
    # Mapping tracking
    issue_mappings = []  # (raw, canonical, confidence, is_exact)
    severity_mappings = []
    
    print("Loading enriched items...")
    with open(input_path, "r") as f:
        for line in f:
            if not line.strip():
                continue
            try:
                item = json.loads(line.strip())
            except json.JSONDecodeError:
                continue
            
            items.append(item)
            total_count += 1
            
            # Raw value tracking
            raw_issue = item.get("issue_type") or ""
            raw_severity = item.get("severity") or ""
            raw_issue_types[raw_issue] += 1
            raw_severities[raw_severity] += 1
            
            # Brand tracking
            brand = item.get("canonical_brand") or "unknown"
            brand = brand.lower()
            brand_counts[brand] += 1
            brand_issue_heatmap[brand][raw_issue] += 1
            
            # Actionable
            if item.get("is_actionable"):
                actionable_count += 1
            
            # Map to canonical
            issue_canonical, issue_conf, issue_exact = map_to_canonical(raw_issue, CANONICAL_ISSUE_TYPES)
            severity_canonical, sev_conf, sev_exact = map_to_canonical(raw_severity, CANONICAL_SEVERITIES)
            
            issue_mappings.append((raw_issue, issue_canonical, issue_conf, issue_exact))
            severity_mappings.append((raw_severity, severity_canonical, sev_conf, sev_exact))
    
    print(f"Total items: {total_count}")
    print(f"Actionable: {actionable_count} ({100*actionable_count/max(1,total_count):.1f}%)")
    print(f"Unique brands: {len(brand_counts)}")
    
    # === Report 1: Top 20 Raw Issue Types ===
    print("\n=== TOP 20 RAW ISSUE_TYPE VALUES ===")
    report_lines = []
    for issue_type, count in raw_issue_types.most_common(20):
        canonical, conf, exact = map_to_canonical(issue_type, CANONICAL_ISSUE_TYPES)
        pct = 100 * count / total_count
        match_type = "EXACT" if exact else "FUZZY" if conf > 0.5 else "MISS"
        line = f"{issue_type:30s} → {canonical:15s} [{match_type}] {count:6d} ({pct:5.1f}%)"
        print(line)
        report_lines.append({"raw": issue_type, "canonical": canonical, "match_type": match_type, 
                            "count": count, "percent": pct, "confidence": conf})
    
    with open(output_dir / "top_issue_types.json", "w") as f:
        json.dump(report_lines, f, indent=2)
    
    # === Report 2: Raw Severity Values ===
    print("\n=== RAW SEVERITY VALUES ===")
    for severity, count in raw_severities.most_common(20):
        canonical, conf, exact = map_to_canonical(severity, CANONICAL_SEVERITIES)
        pct = 100 * count / total_count
        match_type = "EXACT" if exact else "FUZZY" if conf > 0.5 else "MISS"
        print(f"{severity:20s} → {canonical:10s} [{match_type}] {count:6d} ({pct:5.1f}%)")
    
    # === Report 3: Brand × Issue Type Heatmap ===
    print("\n=== BRAND × ISSUE_TYPE HEATMAP (Top 15 brands) ===")
    top_brands = brand_counts.most_common(15)
    top_issues = [t[0] for t in raw_issue_types.most_common(8)]
    
    # CSV header
    with open(output_dir / "brand_issue_heatmap.csv", "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["brand", "total"] + top_issues)
        
        for brand, total in top_brands:
            row = [brand, total]
            for issue in top_issues:
                row.append(brand_issue_heatmap[brand].get(issue, 0))
            writer.writerow(row)
            print(f"{brand:20s} {total:6d} | " + " | ".join(f"{brand_issue_heatmap[brand].get(issue, 0):4d}" for issue in top_issues[:5]))
    
    # === Report 4: Raw → Canonical Collision Rates ===
    print("\n=== RAW → CANONICAL COLLISION ANALYSIS ===")
    
    # Issue type mapping quality
    issue_exact = sum(1 for _, _, _, e in issue_mappings if e)
    issue_fuzzy = sum(1 for _, _, c, e in issue_mappings if not e and c > 0.5)
    issue_miss = sum(1 for _, _, c, e in issue_mappings if not e and c <= 0.5)
    
    print(f"Issue type mappings:")
    print(f"  EXACT:  {issue_exact:6d} ({100*issue_exact/total_count:.1f}%)")
    print(f"  FUZZY:  {issue_fuzzy:6d} ({100*issue_fuzzy/total_count:.1f}%)")
    print(f"  MISS:   {issue_miss:6d} ({100*issue_miss/total_count:.1f}%)")
    
    severity_exact = sum(1 for _, _, _, e in severity_mappings if e)
    severity_fuzzy = sum(1 for _, _, c, e in severity_mappings if not e and c > 0.5)
    severity_miss = sum(1 for _, _, c, e in severity_mappings if not e and c <= 0.5)
    
    print(f"\nSeverity mappings:")
    print(f"  EXACT:  {severity_exact:6d} ({100*severity_exact/total_count:.1f}%)")
    print(f"  FUZZY:  {severity_fuzzy:6d} ({100*severity_fuzzy/total_count:.1f}%)")
    print(f"  MISS:   {severity_miss:6d} ({100*severity_miss/total_count:.1f}%)")
    
    # === Report 5: Brand Normalization ===
    print("\n=== BRAND NORMALIZATION ===")
    print("Top 20 brands:")
    for brand, count in brand_counts.most_common(20):
        pct = 100 * count / total_count
        print(f"  {brand:25s} {count:6d} ({pct:5.1f}%)")
    
    # Check for potential duplicates/mismatches
    print("\nPotential brand normalization issues (similar names):")
    brand_list = list(brand_counts.keys())
    for i, b1 in enumerate(brand_list[:50]):
        for b2 in brand_list[i+1:100]:
            if b1 in b2 or b2 in b1:
                if b1 != b2:
                    print(f"  '{b1}' ({brand_counts[b1]}) <-> '{b2}' ({brand_counts[b2]})")
    
    # === Summary Stats ===
    collision_report = {
        "total_items": total_count,
        "actionable": actionable_count,
        "actionable_pct": 100 * actionable_count / max(1, total_count),
        "unique_brands": len(brand_counts),
        "unique_raw_issue_types": len(raw_issue_types),
        "unique_raw_severities": len(raw_severities),
        "issue_mapping": {
            "exact": issue_exact,
            "fuzzy": issue_fuzzy, 
            "miss": issue_miss
        },
        "severity_mapping": {
            "exact": severity_exact,
            "fuzzy": severity_fuzzy,
            "miss": severity_miss
        }
    }
    
    with open(output_dir / "calibration_summary.json", "w") as f:
        json.dump(collision_report, f, indent=2)
    
    print(f"\n=== Reports written to {output_dir} ===")
    print(f"  top_issue_types.json")
    print(f"  brand_issue_heatmap.csv") 
    print(f"  calibration_summary.json")


if __name__ == "__main__":
    args = parse_args()
    analyze(args)
