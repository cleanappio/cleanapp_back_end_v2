use anyhow::Result;
use mysql as my;
use my::prelude::*;

use crate::{config::Config, model::{BrandSummaryItem, ReportPoint}};

pub fn connect_pool() -> Result<my::Pool> {
    let cfg: &Config = crate::config::get_config();
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


