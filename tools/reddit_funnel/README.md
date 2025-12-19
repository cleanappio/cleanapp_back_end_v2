# Reddit Funnel: Brand-Addressable Issue Extraction

High-throughput pipeline for extracting brand-addressable issues from Reddit data dumps, using Rust for Stage 1 scanning and Python + Gemini for Stage 2 LLM enrichment.

## Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Reddit Dumps (ZST)                                                          │
│ RS_2025-09.zst (~100GB) + RC_2025-09.zst (~200GB)                          │
└───────────────────────────────────┬─────────────────────────────────────────┘
                                    │
                    ┌───────────────▼───────────────┐
                    │ Stage 1: Rust Scanner         │
                    │ • 23K items/sec throughput    │
                    │ • Keyword + brand filtering   │
                    │ • Priority scoring            │
                    │ Output: routed.jsonl (~2.5M)  │
                    └───────────────┬───────────────┘
                                    │
                    ┌───────────────▼───────────────┐
                    │ Stage 2: Python + Gemini      │
                    │ • 40 sharded workers          │
                    │ • 10 items/batch per call     │
                    │ • Schema enforcement          │
                    │ Output: enriched.*.jsonl      │
                    └───────────────┬───────────────┘
                                    │
                    ┌───────────────▼───────────────┐
                    │ Stage 3: Clustering           │
                    │ • Brand × Issue aggregation   │
                    │ • CSV reports                 │
                    └───────────────────────────────┘
```

## Prerequisites

### Local Development
- Rust 1.70+ (for Stage 1)
- Python 3.10+ (for Stage 2/3)
- `pip install requests`

### VM Requirements
- GCP Compute Engine VM (`cleanapp-processing`)
- 16+ vCPUs, 64GB+ RAM recommended
- 500GB+ disk for Reddit dumps
- Network access to Gemini API

## Directory Structure

```
tools/reddit_funnel/
├── Cargo.toml                    # Rust dependencies
├── src/
│   └── main.rs                   # Stage 1: Rust scanner
├── python/
│   ├── stage2_sharded.py         # Stage 2: Production LLM enrichment (40 workers)
│   ├── stage2_batched.py         # Stage 2: Batched version (legacy)
│   ├── stage2_enrich.py          # Stage 2: Single-item version (legacy)
│   ├── stage2_v2.py              # Stage 2: Schema-enforced version (legacy)
│   ├── stage2_worker.py          # Stage 2: Worker process (legacy)
│   ├── stage3_cluster.py         # Stage 3: Clustering and reports
│   ├── calibration_analysis.py   # Analysis: Calibration metrics
│   └── create_calibration_sample.py  # Analysis: Stratified sampling
└── data/
    ├── brand_dictionary.json     # 160+ tech brands with aliases
    ├── subreddit_priors.json     # Subreddit priority mappings
    └── issue_keywords.txt        # Issue detection keywords
```

---

## Stage 1: Rust Scanner

### Build

```bash
cd tools/reddit_funnel
cargo build --release
```

### Deploy to VM

```bash
# Copy binary and data to VM
gcloud compute scp target/release/reddit_funnel deployer@cleanapp-processing:~/reddit_funnel/
gcloud compute scp -r data/ deployer@cleanapp-processing:~/reddit_funnel/data/
```

### Run Stage 1

```bash
# SSH to VM
gcloud compute ssh deployer@cleanapp-processing --zone=us-central1-a --project=cleanup-mysql-v2

# Run scanner on Reddit dumps
cd ~/reddit_funnel
RUST_LOG=info ./reddit_funnel \
  --inputs ~/reddit_dumps/2025-09/reddit/submissions/RS_2025-09.zst \
  --inputs ~/reddit_dumps/2025-09/reddit/comments/RC_2025-09.zst \
  --output-dir ./output_full \
  --log-interval 1000000

# Output: output_full/routed.jsonl (~2.5M items)
```

### Stage 1 Performance

| Metric | Value |
|--------|-------|
| Throughput | ~23,000 items/sec |
| Reddit Sept 2025 | 116M items → 2.5M routed (2.1%) |
| Processing time | ~85 minutes |

---

## Stage 2: LLM Enrichment

### Environment Setup

```bash
# On VM
cd ~/reddit_funnel
mkdir -p python output_full/shards

# Copy Python scripts
gcloud compute scp python/*.py deployer@cleanapp-processing:~/reddit_funnel/python/
```

### API Key Setup

```bash
# Store Gemini API key in environment (never commit!)
export GEMINI_KEY="your-gemini-api-key-here"

# Or fetch from GCP Secret Manager
export GEMINI_KEY=$(gcloud secrets versions access latest --secret=GEMINI_API_KEY_PROD)
```

### Run Stage 2 (Production - 40 Workers)

```bash
# Launch 40 sharded workers
for i in $(seq 1 40); do
  nohup python3 python/stage2_sharded.py \
    --input output_full/routed.jsonl \
    --output-dir output_full/shards \
    --gemini-key "$GEMINI_KEY" \
    --batch-size 10 \
    --rps 5 \
    --worker-id $i \
    --total-workers 40 \
    > worker_$i.log 2>&1 &
done

# Monitor progress
watch -n 10 'wc -l output_full/shards/*.jsonl | tail -1'

# Check worker count
ps aux | grep stage2_sharded | grep python3 | wc -l
```

### Merge Shards After Completion

```bash
cat output_full/shards/enriched.worker_*.jsonl > output_full/enriched_final.jsonl
wc -l output_full/enriched_final.jsonl
```

### Stage 2 Performance

| Workers | Batch Size | Throughput | 2.5M ETA |
|---------|------------|------------|----------|
| 4 | 10 | ~20 items/sec | 35 hours |
| 20 | 10 | ~50 items/sec | 14 hours |
| 40 | 10 | ~100 items/sec | 7 hours |

### Stage 2 Architecture

- **Per-worker shards**: Each worker writes to `enriched.worker_XX.jsonl` (no lock contention)
- **O(1) skip-existing**: IDs loaded into set at startup for fast resume
- **Stable partition**: `line_num % total_workers` ensures deterministic assignment
- **Adaptive retry**: Failed batches split recursively (10→5→2→1)

---

## Stage 3: Clustering

```bash
python3 python/stage3_cluster.py \
  --input output_full/enriched_final.jsonl \
  --output-dir output_full/reports

# Outputs:
# - top_brands.csv
# - top_clusters.csv
# - sample_issues.jsonl
```

---

## Configuration Reference

### brand_dictionary.json

```json
{
  "brands": [
    {
      "canonical": "apple",
      "aliases": ["apple inc", "iphone", "ipad", "macbook", "ios"],
      "domains": ["apple.com"]
    }
  ]
}
```

### subreddit_priors.json

```json
{
  "apple": {"signals_brand": "apple", "priority_boost": 0.3},
  "steam": {"signals_brand": "steam", "priority_boost": 0.3}
}
```

### Issue Type Mappings (Option B)

| Raw Value | Canonical |
|-----------|-----------|
| promotion | not_applicable |
| gameplay | ux |
| hardware | bug |
| compatibility | bug |
| discussion | not_applicable |

---

## Calibration & Validation

### Create Calibration Sample

```bash
python3 python/create_calibration_sample.py \
  --input output_full/routed.jsonl \
  --output output_full/calibration_sample.jsonl \
  --size 75000
```

### Run Calibration Analysis

```bash
python3 python/calibration_analysis.py \
  --input output_full/calibration_enriched.jsonl \
  --output-dir output_full/analysis

# Outputs:
# - top_issue_types.json
# - brand_issue_heatmap.csv
# - calibration_summary.json
```

### Key Metrics from Calibration (84K items)

| Metric | Value |
|--------|-------|
| Issue type EXACT match | 100% (after schema enforcement) |
| Unknown brand rate | 35% |
| Actionable rate | 24% |
| Severity EXACT match | 100% |

---

## Cost Estimation

### Gemini 2.5 Flash Pricing

| Category | Rate |
|----------|------|
| Input tokens | $0.30 / 1M |
| Output tokens | $2.50 / 1M |

### Estimated Costs

| Items | Est. Input | Est. Output | Total |
|-------|------------|-------------|-------|
| 10K | ~10M | ~2M | ~$8 |
| 100K | ~100M | ~20M | ~$80 |
| 2.5M | ~2.5B | ~500M | ~$2,000 |

---

## Troubleshooting

### SSH Drops

SSH connections to the VM may drop during long operations. All Stage 2 workers use `nohup` and survive SSH disconnects.

```bash
# Reconnect and check status
gcloud compute ssh deployer@cleanapp-processing ...
ps aux | grep stage2_sharded | grep python3 | wc -l
```

### Rate Limiting

If Gemini API returns 429 errors, reduce `--rps` per worker:

```bash
--rps 3  # Instead of 5
```

### Resume After Failure

Workers automatically skip existing IDs (O(1) lookup from shard file):

```bash
# Just restart the same command - it will resume
nohup python3 python/stage2_sharded.py ... &
```

---

## VM Reference

| Setting | Value |
|---------|-------|
| Project | `cleanup-mysql-v2` |
| VM Name | `cleanapp-processing` |
| Zone | `us-central1-a` |
| SSH Command | `gcloud compute ssh deployer@cleanapp-processing --zone=us-central1-a --project=cleanup-mysql-v2` |

---

## Security Notes

- **Never commit API keys** - Use environment variables or GCP Secret Manager
- **Reddit dumps** are stored on VM, not in repo
- **Output files** contain user-generated content - handle with care

---

## Quick Reference Commands

```bash
# Stage 1: Run Rust scanner
RUST_LOG=info ./reddit_funnel --inputs ... --output-dir ./output_full

# Stage 2: Launch 40 sharded workers
for i in $(seq 1 40); do nohup python3 python/stage2_sharded.py --worker-id $i --total-workers 40 ... & done

# Monitor progress
wc -l output_full/shards/*.jsonl | tail -1

# Merge shards
cat output_full/shards/enriched.worker_*.jsonl > output_full/enriched_final.jsonl

# Stage 3: Generate reports
python3 python/stage3_cluster.py --input output_full/enriched_final.jsonl --output-dir output_full/reports
```
