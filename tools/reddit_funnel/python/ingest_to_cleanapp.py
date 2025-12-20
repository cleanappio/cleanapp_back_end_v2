#!/usr/bin/env python3
"""
Reddit Report Ingestion - Ingest actionable Reddit items into CleanApp DB

Ingests ~658K actionable items from cleanapp_signal_corpus_v1 into the
CleanApp reports database as first-class digital reports.

Usage:
  # Dry-run (default): Count and summarize, no DB writes
  python3 ingest_to_cleanapp.py \
    --input enriched_final.jsonl \
    --db-host 127.0.0.1 \
    --db-port 3306

  # Commit mode: Actually insert into DB
  python3 ingest_to_cleanapp.py \
    --input enriched_final.jsonl \
    --db-host 127.0.0.1 \
    --db-port 3306 \
    --commit
"""

import argparse
import json
import hashlib
import os
import sys
from collections import Counter
from datetime import datetime
from pathlib import Path
from typing import Optional

try:
    import mysql.connector
except ImportError:
    print("ERROR: mysql-connector-python required. Install: pip install mysql-connector-python")
    sys.exit(1)


# Constants
SOURCE_NAME = "reddit_archive"
SOURCE_CORPUS = "cleanapp_signal_corpus_v1"
SOURCE_RUN = "2025-09"

# Severity mapping: Reddit severity → severity_level float
SEVERITY_MAP = {
    "low": 0.3,
    "medium": 0.5,
    "high": 0.8,
    "none": 0.1
}


def parse_args():
    parser = argparse.ArgumentParser(description="Ingest Reddit reports into CleanApp DB")
    parser.add_argument("--input", required=True, help="Path to enriched_final.jsonl")
    parser.add_argument("--db-host", default=os.environ.get("DB_HOST", "127.0.0.1"))
    parser.add_argument("--db-port", type=int, default=int(os.environ.get("DB_PORT", "3306")))
    parser.add_argument("--db-user", default=os.environ.get("DB_USER", "server"))
    parser.add_argument("--db-password", default=os.environ.get("DB_PASSWORD", "secret_app"))
    parser.add_argument("--db-name", default=os.environ.get("DB_NAME", "cleanapp"))
    parser.add_argument("--commit", action="store_true", help="Actually commit to DB (default: dry-run)")
    parser.add_argument("--batch-size", type=int, default=1000, help="Batch commit size")
    parser.add_argument("--max-items", type=int, default=None, help="Max items (for testing)")
    return parser.parse_args()


def compute_external_id(reddit_id: str) -> str:
    """Compute stable external ID: sha1('reddit:' + reddit_id)[:32]"""
    return hashlib.sha1(f"reddit:{reddit_id}".encode()).hexdigest()[:32]


def format_title(item: dict) -> str:
    """Format title from brand + issue_type."""
    brand = item.get("brand_effective", "Unknown")
    issue_type = item.get("issue_type", "other")
    # Capitalize brand
    brand_display = brand.title() if brand else "Unknown"
    # Format issue type
    issue_display = issue_type.replace("_", " ").title()
    return f"{brand_display} - {issue_display}"


def get_severity_level(severity: str) -> float:
    """Convert severity string to float."""
    return SEVERITY_MAP.get(severity.lower() if severity else "none", 0.1)


def get_reddit_timestamp(item: dict) -> Optional[datetime]:
    """Parse Reddit timestamp."""
    ts = item.get("created_utc")
    if ts:
        try:
            if isinstance(ts, (int, float)):
                return datetime.fromtimestamp(ts)
            return datetime.fromisoformat(str(ts).replace("Z", "+00:00"))
        except:
            pass
    return datetime.now()


def ingest(args):
    input_path = Path(args.input)
    if not input_path.exists():
        print(f"ERROR: Input file not found: {input_path}")
        return

    print(f"=== Reddit Report Ingestion ===")
    print(f"Input: {input_path}")
    print(f"Mode: {'COMMIT' if args.commit else 'DRY-RUN'}")
    print(f"DB: {args.db_host}:{args.db_port}/{args.db_name}")
    print()

    # Connect to DB
    if args.commit:
        try:
            conn = mysql.connector.connect(
                host=args.db_host,
                port=args.db_port,
                user=args.db_user,
                password=args.db_password,
                database=args.db_name,
                charset="utf8mb4",
                collation="utf8mb4_unicode_ci"
            )
            cursor = conn.cursor()
            print("Database connected.")
        except Exception as e:
            print(f"ERROR: Database connection failed: {e}")
            return
    else:
        conn = None
        cursor = None
        print("Dry-run mode: No database connection.")

    # Counters
    total = 0
    actionable = 0
    skipped_not_actionable = 0
    skipped_duplicate = 0
    inserted = 0
    errors = 0
    brand_counts = Counter()

    # Process
    print("\nProcessing...")
    
    with open(input_path, "r") as f:
        batch = []
        
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
            
            # Skip non-actionable
            if not item.get("is_actionable"):
                skipped_not_actionable += 1
                continue
            
            actionable += 1
            reddit_id = item.get("id", "")
            external_id = compute_external_id(reddit_id)
            
            # Track brand
            brand = item.get("brand_effective", "unknown")
            brand_counts[brand] += 1
            
            if args.commit:
                # Check for duplicate
                cursor.execute(
                    "SELECT seq FROM external_ingest_index WHERE source = %s AND external_id = %s",
                    (SOURCE_NAME, external_id)
                )
                if cursor.fetchone():
                    skipped_duplicate += 1
                    continue
                
                # Prepare record
                record = {
                    "reddit_id": reddit_id,
                    "external_id": external_id,
                    "brand": brand,
                    "brand_display": brand.title() if brand else "Unknown",
                    "title": format_title(item),
                    "summary": (item.get("summary") or "")[:4000],
                    "description": (item.get("summary") or "")[:255],
                    "severity_level": get_severity_level(item.get("severity", "")),
                    "issue_type": item.get("issue_type", "other"),
                    "ts": get_reddit_timestamp(item),
                    "url": item.get("url", ""),
                    "subreddit": item.get("subreddit", ""),
                    "cluster_id": item.get("cluster_id_coarse", "")
                }
                batch.append(record)
                
                # Batch insert
                if len(batch) >= args.batch_size:
                    inserted += insert_batch(cursor, conn, batch)
                    batch = []
                    
                    if inserted % 10000 == 0:
                        print(f"  Inserted: {inserted}")
            
            if total % 100000 == 0:
                print(f"  Processed: {total}, Actionable: {actionable}")
    
    # Insert remaining batch
    if args.commit and batch:
        inserted += insert_batch(cursor, conn, batch)
    
    # Final stats
    print(f"\n=== Summary ===")
    print(f"Total processed: {total}")
    print(f"Actionable: {actionable}")
    print(f"Skipped (not actionable): {skipped_not_actionable}")
    print(f"Skipped (duplicate): {skipped_duplicate}")
    print(f"Inserted: {inserted}")
    print(f"Errors: {errors}")
    
    # Top brands
    print(f"\n=== Top 20 Brands (by new reports) ===")
    for brand, count in brand_counts.most_common(20):
        print(f"  {brand}: {count}")
    
    # Get new total count
    if args.commit:
        cursor.execute("SELECT COUNT(*) FROM reports")
        new_total = cursor.fetchone()[0]
        print(f"\n=== CleanApp Total Reports: {new_total:,} ===")
        
        if new_total >= 1000000:
            print("\n🎉 CleanApp total reports now exceed 1,000,000!")
        
        cursor.close()
        conn.close()
    else:
        print(f"\n[DRY-RUN] Would insert {actionable - skipped_duplicate} reports")
        print("[DRY-RUN] Re-run with --commit to insert")


def insert_batch(cursor, conn, batch):
    """Insert batch of records into DB."""
    inserted = 0
    
    for record in batch:
        try:
            # 1. Insert into reports
            cursor.execute("""
                INSERT INTO reports (ts, id, team, latitude, longitude, x, y, image, description)
                VALUES (%s, %s, 0, 0.0, 0.0, 0.5, 0.5, '', %s)
            """, (
                record["ts"],
                f"reddit_{record['reddit_id']}",
                record["description"]
            ))
            seq = cursor.lastrowid
            
            # 2. Insert into report_analysis
            cursor.execute("""
                INSERT INTO report_analysis (
                    seq, source, analysis_text, title, description,
                    brand_name, brand_display_name,
                    litter_probability, hazard_probability, digital_bug_probability,
                    severity_level, summary, language, is_valid, classification
                )
                VALUES (%s, %s, %s, %s, %s, %s, %s, 0.0, 0.0, %s, %s, %s, 'en', TRUE, 'digital')
            """, (
                seq,
                f"{SOURCE_NAME}_{SOURCE_RUN}",
                f"Source: Reddit r/{record['subreddit']}",
                record["title"],
                record["summary"][:4000],
                record["brand"],
                record["brand_display"],
                record["severity_level"],  # digital_bug_probability
                record["severity_level"],
                record["summary"][:4000]
            ))
            
            # 3. Insert into external_ingest_index
            cursor.execute("""
                INSERT INTO external_ingest_index (source, external_id, seq)
                VALUES (%s, %s, %s)
            """, (SOURCE_NAME, record["external_id"], seq))
            
            inserted += 1
            
        except Exception as e:
            print(f"  ERROR inserting {record['reddit_id']}: {e}")
    
    conn.commit()
    return inserted


if __name__ == "__main__":
    args = parse_args()
    ingest(args)
