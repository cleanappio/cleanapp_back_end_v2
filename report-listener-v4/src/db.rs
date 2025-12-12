use anyhow::Result;
use mysql as my;
use my::prelude::*;

use crate::{cfg::Config, models::{BrandSummaryItem, Report, ReportAnalysis, ReportBatch, ReportWithAnalysis, ReportPoint}};

pub fn connect_pool(cfg: &Config) -> Result<my::Pool> {
    let port: u16 = cfg.db_port.parse().unwrap_or(3306);
    let builder = my::OptsBuilder::new()
        .ip_or_hostname(Some(cfg.db_host.clone()))
        .tcp_port(port)
        .user(Some(cfg.db_user.clone()))
        .pass(Some(cfg.db_password.clone()))
        .db_name(Some(cfg.db_name.clone()));
    Ok(my::Pool::new(builder)?)
}

pub fn fetch_brand_summaries(pool: &my::Pool, classification: &str, lang: &str) -> Result<Vec<BrandSummaryItem>> {
    let mut conn = pool.get_conn()?;
    let rows: Vec<(String, String, u64)> = conn.exec(
        r#"
        SELECT ra.brand_name, ra.brand_display_name, COUNT(*) AS total
        FROM report_analysis ra
        WHERE ra.language = ? AND ra.classification = ? AND ra.is_valid = TRUE AND ra.brand_name <> ''
        GROUP BY ra.brand_name, ra.brand_display_name
        ORDER BY ra.brand_name, ra.brand_display_name
        "#,
        (lang, classification),
    )?;
    Ok(rows.into_iter().map(|(brand_name, brand_display_name, total)| BrandSummaryItem { brand_name, brand_display_name, total }).collect())
}

pub fn fetch_reports_by_brand(pool: &my::Pool, brand_name: &str, limit: usize) -> Result<ReportBatch> {
    let mut conn = pool.get_conn()?;

    // Reports query similar to Go version, with filters on status/ownership
    let report_rows: Vec<my::Row> = conn.exec(
        r#"
        SELECT DISTINCT r.seq,
               DATE_FORMAT(r.ts, '%Y-%m-%d %H:%i:%s') AS ts,
               r.id,
               r.latitude,
               r.longitude,
               COALESCE(r.image, '') AS image,
               (SELECT DATE_FORMAT(MAX(created_at), '%Y-%m-%d %H:%i:%s') FROM sent_reports_emails WHERE seq = r.seq) as last_email_sent_at,
               DATE_FORMAT(ei.source_timestamp, '%Y-%m-%d %H:%i:%s') as source_timestamp
        FROM reports r
        INNER JOIN report_analysis ra ON r.seq = ra.seq
        LEFT JOIN report_status rs ON r.seq = rs.seq
        LEFT JOIN reports_owners ro ON r.seq = ro.seq
        LEFT JOIN external_ingest_index ei ON r.seq = ei.seq
        WHERE ra.brand_name = ?
          AND (rs.status IS NULL OR rs.status = 'active')
          AND ra.is_valid = TRUE
          AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)
        ORDER BY ts DESC
        LIMIT ?
        "#,
        (brand_name, limit as u64),
    )?;

    if report_rows.is_empty() {
        return Ok(ReportBatch { reports: vec![], count: 0, from_seq: 0, to_seq: 0 });
    }

    let mut reports: Vec<Report> = Vec::with_capacity(report_rows.len());
    let mut seqs: Vec<i64> = Vec::with_capacity(report_rows.len());
    for mut row in report_rows {
        let seq: i64 = row.take::<i64, _>(0).unwrap_or(0);
        let ts: String = row.take::<Option<String>, _>(1).unwrap_or(None).unwrap_or_default();
        let id: String = row.take::<Option<String>, _>(2).unwrap_or(None).unwrap_or_default();
        let lat: f64 = row.take::<Option<f64>, _>(3).unwrap_or(None).unwrap_or(0.0);
        let lon: f64 = row.take::<Option<f64>, _>(4).unwrap_or(None).unwrap_or(0.0);
        let image: Vec<u8> = row.take::<Option<Vec<u8>>, _>(5).unwrap_or(None).unwrap_or_default();
        let last_email_sent_at: Option<String> = row.take::<Option<String>, _>(6).unwrap_or(None);
        let source_timestamp: Option<String> = row.take::<Option<String>, _>(7).unwrap_or(None);
        reports.push(Report { seq, timestamp: ts, id, latitude: lat, longitude: lon, image, last_email_sent_at, source_timestamp });
        seqs.push(seq);
    }

    // Build IN placeholders
    let placeholders = std::iter::repeat("?").take(seqs.len()).collect::<Vec<_>>().join(",");
    let sql = format!(
        r#"
        SELECT 
            ra.seq, ra.source, ra.analysis_text, ra.analysis_image,
            ra.title, ra.description, ra.brand_name, ra.brand_display_name,
            ra.litter_probability, ra.hazard_probability, ra.digital_bug_probability,
            ra.severity_level, ra.summary, ra.language, ra.classification
        FROM report_analysis ra
        WHERE ra.seq IN ({})
        ORDER BY ra.seq DESC, ra.language ASC
        "#,
        placeholders
    );
    let params: Vec<my::Value> = seqs.iter().map(|s| my::Value::from(*s)).collect();
    let rows: Vec<my::Row> = conn.exec(sql, params)?;

    use std::collections::BTreeMap;
    let mut analyses_by_seq: BTreeMap<i64, Vec<ReportAnalysis>> = BTreeMap::new();
    for mut row in rows {
        let seq: i64 = row.take::<i64, _>(0).unwrap_or(0);
        let source: String = row.take::<String, _>(1).unwrap_or_default();
        let analysis_text: String = row.take::<Option<String>, _>(2).unwrap_or(None).unwrap_or_default();
        let analysis_image: Vec<u8> = row.take::<Option<Vec<u8>>, _>(3).unwrap_or(None).unwrap_or_default();
        let title: String = row.take::<Option<String>, _>(4).unwrap_or(None).unwrap_or_default();
        let description: String = row.take::<Option<String>, _>(5).unwrap_or(None).unwrap_or_default();
        let brand_name: String = row.take::<Option<String>, _>(6).unwrap_or(None).unwrap_or_default();
        let brand_display_name: String = row.take::<Option<String>, _>(7).unwrap_or(None).unwrap_or_default();
        let litter_probability: f64 = row.take::<Option<f64>, _>(8).unwrap_or(None).unwrap_or(0.0);
        let hazard_probability: f64 = row.take::<Option<f64>, _>(9).unwrap_or(None).unwrap_or(0.0);
        let digital_bug_probability: f64 = row.take::<Option<f64>, _>(10).unwrap_or(None).unwrap_or(0.0);
        let severity_level: f64 = row.take::<Option<f64>, _>(11).unwrap_or(None).unwrap_or(0.0);
        let summary: String = row.take::<Option<String>, _>(12).unwrap_or(None).unwrap_or_default();
        let language: String = row.take::<Option<String>, _>(13).unwrap_or(None).unwrap_or_else(|| "en".to_string());
        let classification: String = row.take::<Option<String>, _>(14).unwrap_or(None).unwrap_or_else(|| "physical".to_string());

        let rec = ReportAnalysis {
            seq,
            source,
            analysis_text,
            analysis_image,
            title,
            description,
            brand_name,
            brand_display_name,
            litter_probability,
            hazard_probability,
            digital_bug_probability,
            severity_level,
            summary,
            language,
            classification,
            created_at: String::new(),
        };
        analyses_by_seq.entry(seq).or_default().push(rec);
    }

    let mut with_analysis: Vec<ReportWithAnalysis> = Vec::with_capacity(reports.len());
    for r in reports {
        if let Some(analysis) = analyses_by_seq.get(&r.seq) {
            with_analysis.push(ReportWithAnalysis { report: r, analysis: analysis.clone() });
        }
    }

    let count = with_analysis.len();
    let from_seq = with_analysis.first().map(|x| x.report.seq).unwrap_or(0);
    let to_seq = with_analysis.last().map(|x| x.report.seq).unwrap_or(0);
    Ok(ReportBatch { reports: with_analysis, count, from_seq, to_seq })
}

pub fn fetch_report_points(pool: &my::Pool, classification: &str) -> Result<Vec<ReportPoint>> {
    let mut conn = pool.get_conn()?;
    let base = r#"
        SELECT r.seq,
               COALESCE(MAX(ra.severity_level), 0.0) AS severity_level,
               r.latitude,
               r.longitude
        FROM reports r
        INNER JOIN report_analysis ra ON r.seq = ra.seq
        LEFT JOIN report_status rs ON r.seq = rs.seq
        LEFT JOIN reports_owners ro ON r.seq = ro.seq
        WHERE ra.is_valid = TRUE
          AND (rs.status IS NULL OR rs.status = 'active')
          AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)
          AND r.latitude IS NOT NULL AND r.longitude IS NOT NULL
    "#;
    let sql = if classification.eq_ignore_ascii_case("all") {
        format!(
            "{} GROUP BY r.seq, r.latitude, r.longitude ORDER BY r.seq DESC",
            base
        )
    } else {
        format!(
            "{} AND ra.classification = ? GROUP BY r.seq, r.latitude, r.longitude ORDER BY r.seq DESC",
            base
        )
    };

    let rows: Vec<my::Row> = if classification.eq_ignore_ascii_case("all") {
        conn.exec(sql, ())?
    } else {
        conn.exec(sql, (classification,))?
    };

    let mut out: Vec<ReportPoint> = Vec::with_capacity(rows.len());
    for mut row in rows {
        let seq: i64 = row.take::<i64, _>(0).unwrap_or(0);
        let severity_level: f64 = row.take::<Option<f64>, _>(1).unwrap_or(None).unwrap_or(0.0);
        let latitude: f64 = row.take::<Option<f64>, _>(2).unwrap_or(None).unwrap_or(0.0);
        let longitude: f64 = row.take::<Option<f64>, _>(3).unwrap_or(None).unwrap_or(0.0);
        out.push(ReportPoint { seq, severity_level, latitude, longitude });
    }
    Ok(out)
}

pub fn fetch_report_by_seq(pool: &my::Pool, seq: i64) -> Result<ReportWithAnalysis> {
    let mut conn = pool.get_conn()?;
    // fetch report
    let report_row: Option<(i64, String, String, f64, f64, Vec<u8>, Option<String>, Option<String>)> = conn.exec_first(
        r#"
        SELECT r.seq,
               DATE_FORMAT(r.ts, '%Y-%m-%d %H:%i:%s') AS ts,
               r.id,
               r.latitude,
               r.longitude,
               COALESCE(r.image, '') AS image,
               (SELECT DATE_FORMAT(MAX(created_at), '%Y-%m-%d %H:%i:%s') FROM sent_reports_emails WHERE seq = r.seq) as last_email_sent_at,
               DATE_FORMAT(ei.source_timestamp, '%Y-%m-%d %H:%i:%s') as source_timestamp
        FROM reports r
        LEFT JOIN report_status rs ON r.seq = rs.seq
        LEFT JOIN reports_owners ro ON r.seq = ro.seq
        LEFT JOIN external_ingest_index ei ON r.seq = ei.seq
        WHERE r.seq = ?
          AND (rs.status IS NULL OR rs.status = 'active')
          AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)
        LIMIT 1
        "#,
        (seq,))?;

    let (seq_v, ts, id, lat, lon, image, last_email_sent_at, source_timestamp) = report_row.ok_or_else(|| anyhow::anyhow!("report not found or unavailable"))?;
    let report = Report { seq: seq_v, timestamp: ts, id, latitude: lat, longitude: lon, image, last_email_sent_at, source_timestamp };

    // fetch analyses
    let rows: Vec<my::Row> = conn.exec(
        r#"
        SELECT 
            ra.seq, ra.source, ra.analysis_text, ra.analysis_image,
            ra.title, ra.description, ra.brand_name, ra.brand_display_name,
            ra.litter_probability, ra.hazard_probability, ra.digital_bug_probability,
            ra.severity_level, ra.summary, ra.language, ra.classification,
            DATE_FORMAT(ra.created_at, '%Y-%m-%d %H:%i:%s') AS created_at
        FROM report_analysis ra
        WHERE ra.seq = ?
        ORDER BY ra.language ASC
        "#,
        (seq_v,))?;

    let mut analyses: Vec<ReportAnalysis> = Vec::with_capacity(rows.len());
    for mut row in rows {
        let seq: i64 = row.take::<i64, _>(0).unwrap_or(0);
        let source: String = row.take::<String, _>(1).unwrap_or_default();
        let analysis_text: String = row.take::<Option<String>, _>(2).unwrap_or(None).unwrap_or_default();
        let analysis_image: Vec<u8> = row.take::<Option<Vec<u8>>, _>(3).unwrap_or(None).unwrap_or_default();
        let title: String = row.take::<Option<String>, _>(4).unwrap_or(None).unwrap_or_default();
        let description: String = row.take::<Option<String>, _>(5).unwrap_or(None).unwrap_or_default();
        let brand_name: String = row.take::<Option<String>, _>(6).unwrap_or(None).unwrap_or_default();
        let brand_display_name: String = row.take::<Option<String>, _>(7).unwrap_or(None).unwrap_or_default();
        let litter_probability: f64 = row.take::<Option<f64>, _>(8).unwrap_or(None).unwrap_or(0.0);
        let hazard_probability: f64 = row.take::<Option<f64>, _>(9).unwrap_or(None).unwrap_or(0.0);
        let digital_bug_probability: f64 = row.take::<Option<f64>, _>(10).unwrap_or(None).unwrap_or(0.0);
        let severity_level: f64 = row.take::<Option<f64>, _>(11).unwrap_or(None).unwrap_or(0.0);
        let summary: String = row.take::<Option<String>, _>(12).unwrap_or(None).unwrap_or_default();
        let language: String = row.take::<Option<String>, _>(13).unwrap_or(None).unwrap_or_else(|| "en".to_string());
        let classification: String = row.take::<Option<String>, _>(14).unwrap_or(None).unwrap_or_else(|| "physical".to_string());
        let created_at: String = row.take::<Option<String>, _>(15).unwrap_or(None).unwrap_or_default();
        analyses.push(ReportAnalysis { seq, source, analysis_text, analysis_image, title, description, brand_name, brand_display_name, litter_probability, hazard_probability, digital_bug_probability, severity_level, summary, language, classification, created_at });
    }

    Ok(ReportWithAnalysis { report, analysis: analyses })
}


