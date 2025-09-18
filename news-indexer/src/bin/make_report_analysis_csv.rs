use anyhow::{Context, Result};
use clap::Parser;
use csv::{ReaderBuilder, WriterBuilder};
use serde::{Deserialize, Serialize};
use std::fs::File;
use std::io::{BufReader, BufWriter};

#[derive(Parser, Debug)]
struct Args {
    #[arg(long)]
    input: String,
    #[arg(long)]
    output: String,
    #[arg(long, default_value_t = 0)]
    seq_gap: i64,
}

#[derive(Deserialize)]
struct ReportRow {
    seq: i64,
    ts: String,
    id: String,
    team: i32,
    latitude: f64,
    longitude: f64,
    x: f64,
    y: f64,
    image: String,
    action_id: String,
    description: String,
}

#[derive(Serialize)]
struct AnalysisRow {
    seq: i64,
    source: String,
    analysis_text: String,
    analysis_image: String,
    title: String,
    description: String,
    litter_probability: f32,
    hazard_probability: f32,
    severity_level: f32,
    summary: String,
    created_at: String,
    updated_at: String,
    language: String,
    brand_name: String,
    brand_display_name: String,
    is_valid: i32,
    classification: String,
    digital_bug_probability: f32,
    inferred_contact_emails: String,
}

fn normalize_brand_name(brand_name: &str) -> String {
    if brand_name.is_empty() { return "".to_string(); }
    let mut s = brand_name.to_lowercase();
    for ch in ["-", "_", ".", ",", "&", "'"] { s = s.replace(ch, ""); }
    s.split_whitespace().collect::<String>()
}

fn parse_description(desc: &str) -> Option<(String, String, String, String)> {
    // Expect: Dig:AppStore:<appname>:<link>:<title>:<desc256>
    // But appname/title/desc can contain ':'; identify link by first http(s)
    let prefix = "Dig:AppStore:";
    let rest = desc.strip_prefix(prefix)?;

    // Find first occurrence of http:// or https://
    let http_pos = rest.find("http://");
    let https_pos = rest.find("https://");

    let link_start = match (http_pos, https_pos) {
        (Some(h), Some(hs)) => Some(h.min(hs)) ,
        (Some(h), None) => Some(h),
        (None, Some(hs)) => Some(hs),
        (None, None) => None,
    }?;

    // app name is everything before the link, trim trailing ':' if present
    let appname = rest[..link_start].trim_end_matches(':').trim().to_string();

    // Determine where URL ends: find first ':' AFTER the scheme separator '://'
    let scheme_sep_rel = rest[link_start..].find("://")?; // position relative to link_start
    let after_scheme = link_start + scheme_sep_rel + 3; // position right after '://'

    let link_end_rel = rest[after_scheme..].find(':').map(|p| after_scheme + p);
    let (link, after_link) = if let Some(end) = link_end_rel {
        (rest[link_start..end].to_string(), rest[end + 1..].to_string())
    } else {
        // No delimiter after link; assume no title/desc present
        (rest[link_start..].to_string(), String::new())
    };

    // Now split remaining into title and desc by first ':' (title may contain ':',
    // desc gets the remainder with colons preserved)
    let mut split = after_link.splitn(2, ':');
    let title = split.next().unwrap_or("").to_string();
    let desc_tail = split.next().unwrap_or("").to_string();

    Some((appname, link, title, desc_tail))
}

fn build_summary(title: &str, desc_tail: &str, link: &str) -> String {
    format!("{} : {} : {}", title.trim(), desc_tail.trim(), link.trim())
}

fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();

    let rdr_file = File::open(&args.input).with_context(|| format!("open input {}", &args.input))?;
    let mut rdr = ReaderBuilder::new().has_headers(true).from_reader(BufReader::new(rdr_file));

    let wtr_file = File::create(&args.output).with_context(|| format!("create output {}", &args.output))?;
    let mut wtr = WriterBuilder::new().has_headers(true).from_writer(BufWriter::new(wtr_file));

    for result in rdr.deserialize::<ReportRow>() {
        let row = result?;
        if let Some((appname, link, title, desc_tail)) = parse_description(&row.description) {
            let seq = row.seq + args.seq_gap;
            let brand_display_name = appname.clone();
            let brand_name = normalize_brand_name(&appname);
            let description = desc_tail.trim().to_string();
            let summary = build_summary(&title, &desc_tail, &link);

            let out = AnalysisRow {
                seq,
                source: "CleanAppBot".to_string(),
                analysis_text: "''".to_string(),
                analysis_image: String::new(),
                title,
                description,
                litter_probability: 0.0,
                hazard_probability: 0.0,
                severity_level: 0.7,
                summary,
                created_at: row.ts.clone(),
                updated_at: chrono::Utc::now().format("%Y-%m-%d %H:%M:%S").to_string(),
                language: "en".to_string(),
                brand_name,
                brand_display_name,
                is_valid: 1,
                classification: "digital".to_string(),
                digital_bug_probability: 1.0,
                inferred_contact_emails: "''".to_string(),
            };
            wtr.serialize(out)?;
        }
    }

    wtr.flush()?;
    Ok(())
}
