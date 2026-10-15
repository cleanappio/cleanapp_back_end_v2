use anyhow::Result;
use chrono::{DateTime, NaiveDateTime, Utc};
use my::prelude::*;
use mysql as my;
use std::collections::BTreeMap;
use std::time::{Duration, Instant};

use crate::{
    config::Config,
    model::{BrandSummaryItem, Report, ReportAnalysis, ReportPoint, ReportWithAnalysis},
};

pub fn connect_pool() -> Result<my::Pool> {
    let cfg: &Config = crate::config::get_config();
    let port: u16 = cfg.db_port.parse().unwrap_or(3306);

    // If MySQL is still booting (common in CI compose), avoid failing hard on first attempt.
    // In prod, this also makes cold starts more resilient (e.g., DB restart).
    let mut max_wait_sec: u64 = 300;
    if std::env::var("CI").ok().as_deref() == Some("true") {
        max_wait_sec = 30;
    }
    if let Ok(v) = std::env::var("DB_CONNECT_MAX_WAIT_SEC") {
        if let Ok(n) = v.parse::<u64>() {
            if n > 0 {
                max_wait_sec = n;
            }
        }
    }

    let deadline = Instant::now().checked_add(Duration::from_secs(max_wait_sec));
    let mut wait = Duration::from_millis(250);
    let mut attempt: u64 = 0;

    loop {
        attempt += 1;
        let builder = my::OptsBuilder::new()
            .ip_or_hostname(Some(cfg.db_host.clone()))
            .tcp_port(port)
            .user(Some(cfg.db_user.clone()))
            .pass(Some(cfg.db_password.clone()))
            .db_name(Some(cfg.db_name.clone()));

        match my::Pool::new(builder) {
            Ok(pool) => return Ok(pool),
            Err(err) => {
                if deadline.is_some_and(|d| Instant::now() >= d) {
                    return Err(err.into());
                }
                tracing::warn!(
                    "mysql: connect failed (attempt={}) host={} port={} db={} retry_in_ms={} err={}",
                    attempt,
                    cfg.db_host,
                    port,
                    cfg.db_name,
                    wait.as_millis(),
                    err
                );
                std::thread::sleep(wait);
                wait = std::cmp::min(wait.saturating_mul(2), Duration::from_secs(5));
            }
        }
    }
}

pub fn fetch_brand_summaries(
    pool: &my::Pool,
    classification: &str,
    lang: &str,
) -> Result<Vec<BrandSummaryItem>> {
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
    Ok(merge_brand_summary_groups(rows))
}

// The memory map is keyed by brand_name, while SQL also groups display-name
// variants. Merge those subgroups before insertion so none overwrite a count.
// Use the largest subgroup's display name, with lexical ties, independently of
// SQL row order. Keep the original brand keys distinct; this is not aliasing.
fn merge_brand_summary_groups(rows: Vec<(String, String, u64)>) -> Vec<BrandSummaryItem> {
    let mut brands: BTreeMap<String, (BrandSummaryItem, u64)> = BTreeMap::new();
    for (brand_name, brand_display_name, total) in rows {
        let (summary, largest_subgroup) = brands.entry(brand_name.clone()).or_insert_with(|| {
            (
                BrandSummaryItem {
                    brand_name,
                    brand_display_name: brand_display_name.clone(),
                    total: 0,
                },
                0,
            )
        });
        summary.total += total;
        if total > *largest_subgroup
            || (total == *largest_subgroup && brand_display_name < summary.brand_display_name)
        {
            summary.brand_display_name = brand_display_name;
            *largest_subgroup = total;
        }
    }
    brands.into_values().map(|(summary, _)| summary).collect()
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
          AND r.latitude BETWEEN -90 AND 90 AND r.longitude BETWEEN -180 AND 180
          AND (r.latitude <> 0 OR r.longitude <> 0)
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
        out.push(ReportPoint {
            seq,
            severity_level,
            latitude,
            longitude,
        });
    }
    Ok(out)
}

pub fn fetch_report_with_analysis(pool: &my::Pool, seq: i64) -> Result<ReportWithAnalysis> {
    let mut conn = pool.get_conn()?;

    let report_row: Option<my::Row> = conn.exec_first(
        r#"
        SELECT seq, ts, id, team, latitude, longitude, x, y, image, action_id, description
        FROM reports
        WHERE seq = ?
        "#,
        (seq,),
    )?;

    let mut report_row =
        report_row.ok_or_else(|| anyhow::anyhow!("report seq={} not found", seq))?;

    let report = Report {
        seq: report_row.take::<i64, _>(0).unwrap_or(0),
        timestamp: parse_mysql_datetime_value(
            report_row
                .take::<my::Value, _>(1)
                .ok_or_else(|| anyhow::anyhow!("missing timestamp for seq={}", seq))?,
            "timestamp",
            seq,
        )?,
        id: report_row.take::<String, _>(2).unwrap_or_default(),
        team: report_row.take::<i32, _>(3).unwrap_or(0),
        latitude: report_row
            .take::<Option<f64>, _>(4)
            .unwrap_or(None)
            .unwrap_or(0.0),
        longitude: report_row
            .take::<Option<f64>, _>(5)
            .unwrap_or(None)
            .unwrap_or(0.0),
        x: report_row
            .take::<Option<f64>, _>(6)
            .unwrap_or(None)
            .unwrap_or(0.0),
        y: report_row
            .take::<Option<f64>, _>(7)
            .unwrap_or(None)
            .unwrap_or(0.0),
        image: report_row.take::<Option<Vec<u8>>, _>(8).unwrap_or(None),
        action_id: report_row.take::<String, _>(9).unwrap_or_default(),
        description: report_row.take::<String, _>(10).unwrap_or_default(),
    };

    let analysis_rows: Vec<my::Row> = conn.exec(
        r#"
        SELECT
            seq,
            COALESCE(source, '') AS source,
            COALESCE(analysis_text, '') AS analysis_text,
            analysis_image,
            COALESCE(title, '') AS title,
            COALESCE(description, '') AS description,
            COALESCE(brand_name, '') AS brand_name,
            COALESCE(brand_display_name, '') AS brand_display_name,
            COALESCE(litter_probability, 0.0) AS litter_probability,
            COALESCE(hazard_probability, 0.0) AS hazard_probability,
            COALESCE(digital_bug_probability, 0.0) AS digital_bug_probability,
            COALESCE(severity_level, 0.0) AS severity_level,
            COALESCE(summary, '') AS summary,
            COALESCE(language, '') AS language,
            COALESCE(classification, '') AS classification,
            is_valid,
            COALESCE(inferred_contact_emails, '') AS inferred_contact_emails,
            created_at,
            updated_at
        FROM report_analysis
        WHERE seq = ?
        ORDER BY (language = 'en') DESC, updated_at DESC, created_at DESC
        "#,
        (seq,),
    )?;

    if analysis_rows.is_empty() {
        return Err(anyhow::anyhow!("no analysis rows found for seq={}", seq));
    }

    let mut analysis = Vec::with_capacity(analysis_rows.len());
    for mut row in analysis_rows {
        analysis.push(ReportAnalysis {
            seq: row.take::<i64, _>(0).unwrap_or(0),
            source: row.take::<String, _>(1).unwrap_or_default(),
            analysis_text: row.take::<String, _>(2).unwrap_or_default(),
            analysis_image: row.take::<Option<Vec<u8>>, _>(3).unwrap_or(None),
            title: row.take::<String, _>(4).unwrap_or_default(),
            description: row.take::<String, _>(5).unwrap_or_default(),
            brand_name: row.take::<String, _>(6).unwrap_or_default(),
            brand_display_name: row.take::<String, _>(7).unwrap_or_default(),
            litter_probability: row.take::<Option<f64>, _>(8).unwrap_or(None).unwrap_or(0.0),
            hazard_probability: row.take::<Option<f64>, _>(9).unwrap_or(None).unwrap_or(0.0),
            digital_bug_probability: row
                .take::<Option<f64>, _>(10)
                .unwrap_or(None)
                .unwrap_or(0.0),
            severity_level: row
                .take::<Option<f64>, _>(11)
                .unwrap_or(None)
                .unwrap_or(0.0),
            summary: row.take::<String, _>(12).unwrap_or_default(),
            language: row.take::<String, _>(13).unwrap_or_default(),
            classification: row.take::<String, _>(14).unwrap_or_default(),
            is_valid: row.take::<bool, _>(15).unwrap_or(false),
            inferred_contact_emails: row.take::<String, _>(16).unwrap_or_default(),
            created_at: parse_mysql_datetime_value(
                row.take::<my::Value, _>(17)
                    .ok_or_else(|| anyhow::anyhow!("missing created_at for seq={}", seq))?,
                "created_at",
                seq,
            )?,
            updated_at: parse_mysql_datetime_value(
                row.take::<my::Value, _>(18)
                    .ok_or_else(|| anyhow::anyhow!("missing updated_at for seq={}", seq))?,
                "updated_at",
                seq,
            )?,
        });
    }

    Ok(ReportWithAnalysis { report, analysis })
}

fn parse_mysql_datetime_value(value: my::Value, field: &str, seq: i64) -> Result<DateTime<Utc>> {
    match value {
        my::Value::Date(year, month, day, hour, minute, second, micros) => {
            let naive = NaiveDateTime::new(
                chrono::NaiveDate::from_ymd_opt(year.into(), month.into(), day.into()).ok_or_else(
                    || {
                        anyhow::anyhow!(
                            "invalid mysql date {} for seq={} ({:04}-{:02}-{:02})",
                            field,
                            seq,
                            year,
                            month,
                            day
                        )
                    },
                )?,
                chrono::NaiveTime::from_hms_micro_opt(
                    hour.into(),
                    minute.into(),
                    second.into(),
                    micros,
                )
                .ok_or_else(|| {
                    anyhow::anyhow!(
                        "invalid mysql time {} for seq={} ({:02}:{:02}:{:02}.{:06})",
                        field,
                        seq,
                        hour,
                        minute,
                        second,
                        micros
                    )
                })?,
            );
            Ok(DateTime::<Utc>::from_naive_utc_and_offset(naive, Utc))
        }
        my::Value::Bytes(bytes) => {
            let raw = String::from_utf8(bytes).map_err(|e| {
                anyhow::anyhow!("parse {} for seq={} invalid utf8: {}", field, seq, e)
            })?;
            let naive = NaiveDateTime::parse_from_str(&raw, "%Y-%m-%d %H:%M:%S")
                .or_else(|_| NaiveDateTime::parse_from_str(&raw, "%Y-%m-%d %H:%M:%S%.f"));
            let naive = naive.map_err(|e| {
                anyhow::anyhow!("parse {} for seq={} value='{}': {}", field, seq, raw, e)
            })?;
            Ok(DateTime::<Utc>::from_naive_utc_and_offset(naive, Utc))
        }
        other => Err(anyhow::anyhow!(
            "unexpected mysql datetime type for {} seq={}: {:?}",
            field,
            seq,
            other
        )),
    }
}

#[cfg(test)]
mod tests {
    use super::merge_brand_summary_groups;

    fn row(brand: &str, display: &str, total: u64) -> (String, String, u64) {
        (brand.to_string(), display.to_string(), total)
    }

    #[test]
    fn brand_display_variants_sum_without_losing_the_dominant_name() {
        let rows = vec![
            row("grok", "Grok", 299),
            row("grok", "Grok (xAI)", 2),
            row("grok", "Grok / xAI", 1),
            row("grok", "Grok / xAI / Tesla", 1),
        ];
        for input in [rows.clone(), rows.into_iter().rev().collect()] {
            let summaries = merge_brand_summary_groups(input);
            assert_eq!(summaries.len(), 1);
            assert_eq!(summaries[0].brand_name, "grok");
            assert_eq!(summaries[0].brand_display_name, "Grok");
            assert_eq!(summaries[0].total, 303);
        }
    }

    #[test]
    fn multiple_brand_keys_stay_distinct_and_ordered() {
        let summaries = merge_brand_summary_groups(vec![
            row("xai", "xAI", 7),
            row("grok", "Grok / xAI", 1),
            row("cleanapp", "CleanApp", 165),
            row("grok", "Grok", 299),
            row("cleanapp", "Clean App", 3),
        ]);
        let got: Vec<_> = summaries
            .iter()
            .map(|item| {
                (
                    item.brand_name.as_str(),
                    item.brand_display_name.as_str(),
                    item.total,
                )
            })
            .collect();
        assert_eq!(
            got,
            vec![
                ("cleanapp", "CleanApp", 168),
                ("grok", "Grok", 300),
                ("xai", "xAI", 7),
            ]
        );
    }

    #[test]
    fn empty_brand_groups_produce_no_summaries() {
        assert!(merge_brand_summary_groups(vec![]).is_empty());
    }

    #[test]
    fn equally_large_display_groups_choose_the_same_name_in_any_order() {
        let rows = vec![
            row("example", "Zulu", 4),
            row("example", "Alpha", 4),
            row("example", "Smaller subgroup", 3),
        ];
        for offset in 0..rows.len() {
            let mut input = rows.clone();
            input.rotate_left(offset);
            for candidate in [input.clone(), input.into_iter().rev().collect()] {
                let summaries = merge_brand_summary_groups(candidate);
                assert_eq!(summaries[0].brand_display_name, "Alpha");
                assert_eq!(summaries[0].total, 11);
            }
        }
    }
}
