use anyhow::Result;
use mysql as my;
use my::prelude::*;

use crate::models::{Brand, BrandEmail};

pub fn connect_pool(cfg: &crate::config::Config) -> Result<my::Pool> {
    let port: u16 = cfg.db_port.parse().unwrap_or(3306);
    let builder = my::OptsBuilder::new()
        .ip_or_hostname(Some(cfg.db_host.clone()))
        .tcp_port(port)
        .user(Some(cfg.db_user.clone()))
        .pass(Some(cfg.db_password.clone()))
        .db_name(Some(cfg.db_name.clone()));
    Ok(my::Pool::new(builder)?)
}

pub fn init_schema(conn: &mut my::PooledConn) -> Result<()> {
    conn.exec_drop(
        r#"
        CREATE TABLE IF NOT EXISTS brands (
            brand_name VARCHAR(255) PRIMARY KEY,
            brand_display_name VARCHAR(255) NOT NULL,
            create_timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            INDEX idx_created_at (create_timestamp)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
        "#,
        (),
    )?;

    conn.exec_drop(
        r#"
        CREATE TABLE IF NOT EXISTS brand_emails (
            email_address VARCHAR(320) PRIMARY KEY,
            brand_name VARCHAR(255) NOT NULL,
            create_timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            INDEX idx_brand_name (brand_name),
            CONSTRAINT fk_brand_emails_brand FOREIGN KEY (brand_name) REFERENCES brands(brand_name)
                ON DELETE CASCADE ON UPDATE CASCADE
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
        "#,
        (),
    )?;

    conn.exec_drop(
        r#"
        CREATE TABLE IF NOT EXISTS brand_email_notifications (
            sent_timestamp TIMESTAMP NOT NULL,
            brand_email VARCHAR(320) NOT NULL,
            brand_name VARCHAR(255) NOT NULL,
            PRIMARY KEY (sent_timestamp, brand_email),
            INDEX idx_brand_email (brand_email),
            INDEX idx_brand_name (brand_name),
            CONSTRAINT fk_notifications_brand_email FOREIGN KEY (brand_email) REFERENCES brand_emails(email_address)
                ON DELETE CASCADE ON UPDATE CASCADE,
            CONSTRAINT fk_notifications_brand FOREIGN KEY (brand_name) REFERENCES brands(brand_name)
                ON DELETE CASCADE ON UPDATE CASCADE
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
        "#,
        (),
    )?;

    Ok(())
}

pub fn upsert_brand(conn: &mut my::PooledConn, brand: &Brand) -> Result<()> {
    conn.exec_drop(
        r#"
        INSERT INTO brands (brand_name, brand_display_name) VALUES (?, ?)
        ON DUPLICATE KEY UPDATE brand_display_name = VALUES(brand_display_name)
        "#,
        (&brand.brand_name, &brand.brand_display_name),
    )?;
    Ok(())
}

pub fn upsert_brand_email(conn: &mut my::PooledConn, be: &BrandEmail) -> Result<()> {
    conn.exec_drop(
        r#"
        INSERT INTO brand_emails (email_address, brand_name) VALUES (?, ?)
        ON DUPLICATE KEY UPDATE brand_name = VALUES(brand_name)
        "#,
        (&be.email_address, &be.brand_name),
    )?;
    Ok(())
}

pub fn pick_due_notifications(
    conn: &mut my::PooledConn,
    notification_period_days: i64,
) -> Result<Vec<(String, String, String)>> {
    // Returns (email_address, brand_name, brand_display_name)
    let rows: Vec<(String, String, String)> = conn.exec(
        r#"
        SELECT be.email_address, be.brand_name, b.brand_display_name
        FROM brand_emails be
        JOIN brands b ON b.brand_name = be.brand_name
        LEFT JOIN brand_email_notifications ben
          ON ben.brand_email = be.email_address
        GROUP BY be.email_address, be.brand_name
        HAVING COALESCE(MAX(ben.sent_timestamp), TIMESTAMP('1970-01-01')) < (NOW() - INTERVAL ? DAY)
        "#,
        (notification_period_days,),
    )?;
    Ok(rows)
}

pub fn pick_due_notifications_for_brands(
    conn: &mut my::PooledConn,
    notification_period_days: i64,
    brands: &[String],
) -> Result<Vec<(String, String, String)>> {
    if brands.is_empty() { return Ok(vec![]); }
    // dynamic placeholders
    let placeholders = std::iter::repeat("?").take(brands.len()).collect::<Vec<_>>().join(",");
    let sql = format!(
        r#"
        SELECT be.email_address, be.brand_name, b.brand_display_name
        FROM brand_emails be
        JOIN brands b ON b.brand_name = be.brand_name
        LEFT JOIN brand_email_notifications ben
          ON ben.brand_email = be.email_address
        WHERE be.brand_name IN ({})
        GROUP BY be.email_address, be.brand_name
        HAVING COALESCE(MAX(ben.sent_timestamp), TIMESTAMP('1970-01-01')) < (NOW() - INTERVAL ? DAY)
        "#,
        placeholders
    );
    let mut params: Vec<my::Value> = brands.iter().map(|b| my::Value::from(b.as_str())).collect();
    params.push(my::Value::from(notification_period_days));
    let rows: Vec<(String, String, String)> = conn.exec(sql, params)?;
    Ok(rows)
}

pub fn record_notification(conn: &mut my::PooledConn, email: &str, brand: &str) -> Result<()> {
    conn.exec_drop(
        r#"INSERT INTO brand_email_notifications (sent_timestamp, brand_email, brand_name)
           VALUES (NOW(), ?, ?)"#,
        (email, brand),
    )?;
    Ok(())
}

pub fn is_email_opted_out(conn: &mut my::PooledConn, email: &str) -> Result<bool> {
    let count: Option<u64> = conn.exec_first(
        r#"SELECT COUNT(*) FROM opted_out_emails WHERE email = ?"#,
        (email,),
    )?;
    Ok(count.unwrap_or(0) > 0)
}

