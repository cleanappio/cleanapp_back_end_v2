//! Reddit Funnel - Stage 1: Cheap Brand-First Scan
//!
//! Streams Reddit dumps (zst), extracts brand candidates via dictionary matching,
//! computes priority scores, and routes items for LLM enrichment or archive-only.
//!
//! Outputs:
//! - candidates.jsonl: all items with features + priority + route decision
//! - routed.jsonl: only items routed to LLM (for Stage 2 Python consumption)

use aho_corasick::{AhoCorasick, AhoCorasickBuilder, MatchKind};
use anyhow::{Context, Result, anyhow};
use async_compression::tokio::bufread::ZstdDecoder;
use chrono::{DateTime, TimeZone, Utc};
use clap::Parser;
use log::{info, warn};
use regex::Regex;
use serde::{Deserialize, Serialize};
use serde_json::{Value as JsonValue, json};
use sha1::{Sha1, Digest};
use std::collections::{HashMap, HashSet};
use std::path::PathBuf;
use std::sync::atomic::{AtomicUsize, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Instant;
use tokio::fs::{self, File};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader, BufWriter};
use tokio::sync::Mutex;

#[derive(Parser, Debug, Clone)]
#[command(author, version, about = "Reddit Funnel Stage 1: Cheap brand-first scan", long_about = None)]
struct Args {
    /// Input zst dump files (comments or submissions)
    #[arg(long = "inputs", required = true)]
    inputs: Vec<String>,

    /// Brand dictionary JSON file
    #[arg(long = "brand-dict", default_value = "data/brand_dictionary.json")]
    brand_dict: PathBuf,

    /// Issue keywords file (one per line)
    #[arg(long = "issue-keywords", default_value = "data/issue_keywords.txt")]
    issue_keywords: PathBuf,

    /// Subreddit priors JSON file
    #[arg(long = "subreddit-priors", default_value = "data/subreddit_priors.json")]
    subreddit_priors: PathBuf,

    /// Output directory for JSONL files
    #[arg(long = "output-dir", default_value = "./output")]
    output_dir: PathBuf,

    /// Target percent of items to route to LLM (0.0-1.0)
    #[arg(long = "target-llm-percent", default_value_t = 0.20)]
    target_llm_percent: f64,

    /// Discovery route max percent (for unknown brands)
    #[arg(long = "discovery-percent-cap", default_value_t = 0.02)]
    discovery_percent_cap: f64,

    /// Max items to process (for testing)
    #[arg(long = "max-items")]
    max_items: Option<usize>,

    /// Dry run - print stats but don't write output
    #[arg(long = "dry-run")]
    dry_run: bool,

    /// Log every N items processed
    #[arg(long = "log-interval", default_value_t = 100000)]
    log_interval: usize,
}

#[derive(Debug, Deserialize)]
struct BrandDictionary {
    brands: Vec<BrandEntry>,
}

#[derive(Debug, Deserialize, Clone)]
struct BrandEntry {
    canonical: String,
    aliases: Vec<String>,
    domains: Vec<String>,
    confidence: f64,
}

#[derive(Debug, Deserialize)]
struct SubredditPriors {
    high_relevance: Vec<String>,
    medium_relevance: Vec<String>,
    weights: HashMap<String, i32>,
}

#[derive(Debug, Deserialize)]
struct RedditRecord {
    id: Option<String>,
    name: Option<String>,
    body: Option<String>,      // comments
    selftext: Option<String>,  // submissions
    title: Option<String>,     // submissions
    permalink: Option<String>,
    created_utc: Option<f64>,
    score: Option<f64>,
    subreddit: Option<String>,
    author: Option<String>,
    link_id: Option<String>,
    parent_id: Option<String>,
    url: Option<String>,
}

#[derive(Debug, Serialize)]
struct CandidateRow {
    id: String,
    item_type: String,  // "comment" or "submission"
    subreddit: String,
    created_utc: i64,
    brand_hits: Vec<String>,
    weak_candidates: Vec<String>,
    domains: Vec<String>,
    issue_kw_count: i32,
    first_person: bool,
    question_help: bool,
    error_artifacts: bool,
    update_regress: bool,
    priority: i32,
    route: String,
    content_hash: String,
}

#[derive(Debug, Serialize)]
struct RoutedRow {
    id: String,
    item_type: String,
    subreddit: String,
    title: String,
    body: String,
    url: String,
    created_utc: i64,
    brand_hints: Vec<String>,
    priority: i32,
    route: String,
}

struct BrandMatcher {
    alias_automaton: AhoCorasick,
    alias_to_canonical: HashMap<usize, String>,
    domain_to_canonical: HashMap<String, String>,
}

struct Processor {
    brand_matcher: BrandMatcher,
    issue_keywords: HashSet<String>,
    issue_automaton: AhoCorasick,
    subreddit_weights: HashMap<String, i32>,
    first_person_patterns: Vec<&'static str>,
    question_help_patterns: Vec<&'static str>,
    error_regex: Regex,
    update_patterns: Vec<&'static str>,
    domain_regex: Regex,
}

#[derive(Default)]
struct Stats {
    total_processed: AtomicUsize,
    total_comments: AtomicUsize,
    total_submissions: AtomicUsize,
    routed_llm_enrich: AtomicUsize,
    routed_discovery: AtomicUsize,
    routed_archive: AtomicUsize,
    dedupe_skipped: AtomicUsize,
    brand_hits_total: AtomicU64,
}

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();

    info!("reddit_funnel Stage 1 starting");
    info!("Inputs: {:?}", args.inputs);
    info!("Target LLM percent: {:.1}%", args.target_llm_percent * 100.0);

    // Load dictionaries
    let brand_dict = load_brand_dict(&args.brand_dict).await?;
    let issue_keywords = load_issue_keywords(&args.issue_keywords).await?;
    let subreddit_priors = load_subreddit_priors(&args.subreddit_priors).await?;

    info!("Loaded {} brands, {} issue keywords", brand_dict.brands.len(), issue_keywords.len());

    // Build matchers
    let processor = build_processor(&brand_dict, &issue_keywords, &subreddit_priors)?;
    let processor = Arc::new(processor);

    // Create output directory
    if !args.dry_run {
        fs::create_dir_all(&args.output_dir).await?;
    }

    // Open output files
    let candidates_path = args.output_dir.join("candidates.jsonl");
    let routed_path = args.output_dir.join("routed.jsonl");

    let candidates_file = if args.dry_run {
        None
    } else {
        Some(Arc::new(Mutex::new(BufWriter::new(
            File::create(&candidates_path).await?,
        ))))
    };
    let routed_file = if args.dry_run {
        None
    } else {
        Some(Arc::new(Mutex::new(BufWriter::new(
            File::create(&routed_path).await?,
        ))))
    };

    // Dedupe set
    let seen_hashes: Arc<Mutex<HashSet<String>>> = Arc::new(Mutex::new(HashSet::new()));
    let stats = Arc::new(Stats::default());

    let start = Instant::now();
    let max_items = args.max_items.unwrap_or(usize::MAX);

    // Process each input file
    for input in &args.inputs {
        info!("Processing: {}", input);
        process_file(
            input,
            &processor,
            &candidates_file,
            &routed_file,
            &seen_hashes,
            &stats,
            max_items,
            args.log_interval,
            args.target_llm_percent,
            args.discovery_percent_cap,
        )
        .await?;

        if stats.total_processed.load(Ordering::Relaxed) >= max_items {
            break;
        }
    }

    // Flush output files
    if let Some(f) = &candidates_file {
        f.lock().await.flush().await?;
    }
    if let Some(f) = &routed_file {
        f.lock().await.flush().await?;
    }

    let elapsed = start.elapsed();
    let total = stats.total_processed.load(Ordering::Relaxed);
    let routed_llm = stats.routed_llm_enrich.load(Ordering::Relaxed);
    let routed_discovery = stats.routed_discovery.load(Ordering::Relaxed);
    let routed_archive = stats.routed_archive.load(Ordering::Relaxed);

    info!("=== Stage 1 Complete ===");
    info!("Total processed: {}", total);
    info!("  Comments: {}", stats.total_comments.load(Ordering::Relaxed));
    info!("  Submissions: {}", stats.total_submissions.load(Ordering::Relaxed));
    info!("Routing breakdown:");
    info!("  LLM_ENRICH: {} ({:.2}%)", routed_llm, 100.0 * routed_llm as f64 / total.max(1) as f64);
    info!("  LLM_ENRICH_DISCOVERY: {} ({:.2}%)", routed_discovery, 100.0 * routed_discovery as f64 / total.max(1) as f64);
    info!("  ARCHIVE_ONLY: {} ({:.2}%)", routed_archive, 100.0 * routed_archive as f64 / total.max(1) as f64);
    info!("Dedupe skipped: {}", stats.dedupe_skipped.load(Ordering::Relaxed));
    info!("Brand hits total: {}", stats.brand_hits_total.load(Ordering::Relaxed));
    info!("Duration: {:.2}s ({:.0} items/sec)", elapsed.as_secs_f64(), total as f64 / elapsed.as_secs_f64());

    if !args.dry_run {
        info!("Output files:");
        info!("  {}", candidates_path.display());
        info!("  {}", routed_path.display());
    }

    Ok(())
}

async fn load_brand_dict(path: &PathBuf) -> Result<BrandDictionary> {
    let content = fs::read_to_string(path).await
        .with_context(|| format!("Failed to read brand dictionary: {}", path.display()))?;
    serde_json::from_str(&content)
        .with_context(|| "Failed to parse brand dictionary JSON")
}

async fn load_issue_keywords(path: &PathBuf) -> Result<HashSet<String>> {
    let content = fs::read_to_string(path).await
        .with_context(|| format!("Failed to read issue keywords: {}", path.display()))?;
    Ok(content
        .lines()
        .filter(|l| !l.trim().is_empty() && !l.starts_with('#'))
        .map(|l| l.trim().to_lowercase())
        .collect())
}

async fn load_subreddit_priors(path: &PathBuf) -> Result<SubredditPriors> {
    let content = fs::read_to_string(path).await
        .with_context(|| format!("Failed to read subreddit priors: {}", path.display()))?;
    serde_json::from_str(&content)
        .with_context(|| "Failed to parse subreddit priors JSON")
}

fn build_processor(
    brand_dict: &BrandDictionary,
    issue_keywords: &HashSet<String>,
    subreddit_priors: &SubredditPriors,
) -> Result<Processor> {
    // Build Aho-Corasick for brand aliases
    let mut aliases: Vec<String> = Vec::new();
    let mut alias_to_canonical: HashMap<usize, String> = HashMap::new();
    let mut domain_to_canonical: HashMap<String, String> = HashMap::new();

    for brand in &brand_dict.brands {
        // Skip 'reddit' - it's the source, not a brand we're tracking
        if brand.canonical == "reddit" {
            continue;
        }
        for alias in &brand.aliases {
            let idx = aliases.len();
            aliases.push(format!(" {} ", alias.to_lowercase())); // Add space padding for word boundaries
            alias_to_canonical.insert(idx, brand.canonical.clone());
        }
        for domain in &brand.domains {
            domain_to_canonical.insert(domain.to_lowercase(), brand.canonical.clone());
        }
    }

    let alias_automaton = AhoCorasickBuilder::new()
        .match_kind(MatchKind::LeftmostLongest)
        .build(&aliases)?;

    // Build Aho-Corasick for issue keywords
    let issue_vec: Vec<String> = issue_keywords.iter().cloned().collect();
    let issue_automaton = AhoCorasickBuilder::new()
        .match_kind(MatchKind::LeftmostLongest)
        .build(&issue_vec)?;

    // Build subreddit weight map
    let mut subreddit_weights: HashMap<String, i32> = HashMap::new();
    let high_weight = subreddit_priors.weights.get("high_relevance").copied().unwrap_or(3);
    let medium_weight = subreddit_priors.weights.get("medium_relevance").copied().unwrap_or(1);

    for sub in &subreddit_priors.high_relevance {
        subreddit_weights.insert(sub.to_lowercase(), high_weight);
    }
    for sub in &subreddit_priors.medium_relevance {
        subreddit_weights.insert(sub.to_lowercase(), medium_weight);
    }

    Ok(Processor {
        brand_matcher: BrandMatcher {
            alias_automaton,
            alias_to_canonical,
            domain_to_canonical,
        },
        issue_keywords: issue_keywords.clone(),
        issue_automaton,
        subreddit_weights,
        first_person_patterns: vec!["i ", "my ", "me ", "i'm ", "im ", "can't", "cannot", "won't", "doesn't", "don't"],
        question_help_patterns: vec!["anyone else", "help", "support", "fix", "workaround", "solution", "how do i", "how to"],
        error_regex: Regex::new(r"\b(404|500|502|503|exception|stack trace|traceback|error code|errno)\b")?,
        update_patterns: vec!["after update", "since update", "new version", "latest version", "recently updated"],
        domain_regex: Regex::new(r"(?:https?://)?(?:www\.)?([a-zA-Z0-9-]+\.[a-zA-Z]{2,})(?:/|$)")?,
    })
}

async fn process_file(
    input: &str,
    processor: &Arc<Processor>,
    candidates_file: &Option<Arc<Mutex<BufWriter<File>>>>,
    routed_file: &Option<Arc<Mutex<BufWriter<File>>>>,
    seen_hashes: &Arc<Mutex<HashSet<String>>>,
    stats: &Arc<Stats>,
    max_items: usize,
    log_interval: usize,
    _target_llm_percent: f64,
    _discovery_percent_cap: f64,
) -> Result<()> {
    let file = File::open(input).await?;
    let reader = BufReader::new(file);
    let decoder = ZstdDecoder::new(reader);
    let buf_decoder = BufReader::new(decoder);
    let mut lines = buf_decoder.lines();

    let mut local_count = 0usize;

    while let Some(line) = lines.next_line().await? {
        if stats.total_processed.load(Ordering::Relaxed) >= max_items {
            break;
        }

        if line.trim().is_empty() {
            continue;
        }

        let record: RedditRecord = match serde_json::from_str(&line) {
            Ok(r) => r,
            Err(_) => continue,
        };

        // Determine item type
        let is_comment = record.body.is_some() || record.parent_id.is_some();
        let item_type = if is_comment { "comment" } else { "submission" };

        // Get text content
        let title = record.title.as_deref().unwrap_or("");
        let body = if is_comment {
            record.body.as_deref().unwrap_or("")
        } else {
            record.selftext.as_deref().unwrap_or("")
        };
        let text = format!(" {} {} ", title.to_lowercase(), body.to_lowercase());

        // Compute content hash for dedupe
        let content_hash = compute_hash(&text);

        // Check dedupe
        {
            let mut seen = seen_hashes.lock().await;
            if seen.contains(&content_hash) {
                stats.dedupe_skipped.fetch_add(1, Ordering::Relaxed);
                continue;
            }
            seen.insert(content_hash.clone());
        }

        // Extract features
        let (brand_hits, domains) = extract_brands(processor, &text, record.url.as_deref());
        let issue_kw_count = count_issue_keywords(processor, &text);
        let first_person = has_pattern(&text, &processor.first_person_patterns);
        let question_help = has_pattern(&text, &processor.question_help_patterns);
        let error_artifacts = processor.error_regex.is_match(&text);
        let update_regress = has_pattern(&text, &processor.update_patterns);

        // Compute priority score
        let subreddit = record.subreddit.as_deref().unwrap_or("").to_lowercase();
        let subreddit_weight = processor.subreddit_weights.get(&subreddit).copied().unwrap_or(0);

        let brand_score = 6 * brand_hits.len() as i32 + 2 * domains.len() as i32;
        let issue_score = 2 * issue_kw_count.min(5)
            + 3 * (first_person as i32)
            + 2 * (question_help as i32)
            + 2 * (error_artifacts as i32)
            + 1 * (update_regress as i32);
        let priority = brand_score + issue_score + subreddit_weight;

        // Route decision - tighter thresholds to hit ~20% target
        // Require: brand hit + meaningful issue signals
        let base_threshold = if item_type == "submission" { 12 } else { 15 };
        let route = if brand_hits.len() >= 1 && priority >= base_threshold && issue_score >= 3 {
            "LLM_ENRICH"
        } else if brand_hits.len() >= 2 && issue_score >= 2 {
            "LLM_ENRICH"
        } else if priority >= 18 && issue_score >= 5 {
            "LLM_ENRICH_DISCOVERY"
        } else {
            "ARCHIVE_ONLY"
        };

        // Update stats
        stats.total_processed.fetch_add(1, Ordering::Relaxed);
        if is_comment {
            stats.total_comments.fetch_add(1, Ordering::Relaxed);
        } else {
            stats.total_submissions.fetch_add(1, Ordering::Relaxed);
        }
        stats.brand_hits_total.fetch_add(brand_hits.len() as u64, Ordering::Relaxed);

        match route {
            "LLM_ENRICH" => stats.routed_llm_enrich.fetch_add(1, Ordering::Relaxed),
            "LLM_ENRICH_DISCOVERY" => stats.routed_discovery.fetch_add(1, Ordering::Relaxed),
            _ => stats.routed_archive.fetch_add(1, Ordering::Relaxed),
        };

        // Build output rows
        let id = record.name.clone()
            .or_else(|| record.id.as_ref().map(|i| {
                if is_comment { format!("t1_{}", i) } else { format!("t3_{}", i) }
            }))
            .unwrap_or_default();

        let created_utc = record.created_utc.map(|t| t as i64).unwrap_or(0);

        let candidate = CandidateRow {
            id: id.clone(),
            item_type: item_type.to_string(),
            subreddit: subreddit.clone(),
            created_utc,
            brand_hits: brand_hits.clone(),
            weak_candidates: vec![],  // TODO: extract proper nouns
            domains: domains.clone(),
            issue_kw_count,
            first_person,
            question_help,
            error_artifacts,
            update_regress,
            priority,
            route: route.to_string(),
            content_hash: content_hash.clone(),
        };

        // Write candidate row
        if let Some(f) = candidates_file {
            let json_line = serde_json::to_string(&candidate)? + "\n";
            f.lock().await.write_all(json_line.as_bytes()).await?;
        }

        // Write routed row (for LLM processing)
        if route != "ARCHIVE_ONLY" {
            let routed = RoutedRow {
                id,
                item_type: item_type.to_string(),
                subreddit,
                title: title.to_string(),
                body: body.chars().take(5000).collect(),  // Limit body size
                url: record.url.unwrap_or_default(),
                created_utc,
                brand_hints: brand_hits,
                priority,
                route: route.to_string(),
            };

            if let Some(f) = routed_file {
                let json_line = serde_json::to_string(&routed)? + "\n";
                f.lock().await.write_all(json_line.as_bytes()).await?;
            }
        }

        local_count += 1;
        if local_count % log_interval == 0 {
            let total = stats.total_processed.load(Ordering::Relaxed);
            let routed = stats.routed_llm_enrich.load(Ordering::Relaxed) + stats.routed_discovery.load(Ordering::Relaxed);
            let pct = 100.0 * routed as f64 / total.max(1) as f64;
            info!("Processed {} items, routed {:.1}% to LLM", total, pct);
        }
    }

    Ok(())
}

fn extract_brands(processor: &Processor, text: &str, url: Option<&str>) -> (Vec<String>, Vec<String>) {
    let mut brand_hits: HashSet<String> = HashSet::new();
    let mut domains: Vec<String> = Vec::new();

    // Alias matching via Aho-Corasick
    for mat in processor.brand_matcher.alias_automaton.find_iter(text) {
        if let Some(canonical) = processor.brand_matcher.alias_to_canonical.get(&mat.pattern().as_usize()) {
            brand_hits.insert(canonical.clone());
        }
    }

    // Domain extraction
    let combined_text = if let Some(u) = url {
        format!("{} {}", text, u)
    } else {
        text.to_string()
    };

    for cap in processor.domain_regex.captures_iter(&combined_text) {
        if let Some(domain_match) = cap.get(1) {
            let domain = domain_match.as_str().to_lowercase();
            if let Some(canonical) = processor.brand_matcher.domain_to_canonical.get(&domain) {
                brand_hits.insert(canonical.clone());
                domains.push(domain);
            }
        }
    }

    (brand_hits.into_iter().collect(), domains)
}

fn count_issue_keywords(processor: &Processor, text: &str) -> i32 {
    processor.issue_automaton.find_iter(text).count() as i32
}

fn has_pattern(text: &str, patterns: &[&str]) -> bool {
    patterns.iter().any(|p| text.contains(p))
}

fn compute_hash(text: &str) -> String {
    let mut hasher = Sha1::new();
    // Hash first 2KB for dedupe
    let truncated: String = text.chars().take(2000).collect();
    hasher.update(truncated.as_bytes());
    format!("{:x}", hasher.finalize())
}
