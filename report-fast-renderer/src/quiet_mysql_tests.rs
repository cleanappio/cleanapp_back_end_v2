//! Opt-in disposable MySQL 8 fixture. This never accepts a production target.
use super::*;
use crate::quiet_refresh::PointSource;
use std::collections::BTreeSet;

#[test]
#[ignore = "requires the dedicated disposable loopback MySQL 8.0.44 fixture"]
fn bounded_refresh_mysql_contracts() -> Result<()> {
    let url = std::env::var("CLEANAPP_RENDERER_QUIET_MYSQL_URL")
        .map_err(|_| anyhow::anyhow!("disposable MySQL fixture environment required"))?;
    let port = url
        .strip_prefix("mysql://root@127.0.0.1:")
        .and_then(|v| v.strip_suffix("/cleanapp_renderer_quiet_test"))
        .and_then(|v| v.parse::<u16>().ok());
    anyhow::ensure!(
        port.is_some_and(|v| v > 0),
        "isolated loopback fixture required"
    );
    let mut conn = my::Conn::new(my::Opts::from_url(&url)?)?;
    assert_eq!(
        conn.query_first::<String, _>("SELECT VERSION()")?
            .as_deref(),
        Some("8.0.44")
    );
    let tables: Vec<String> = conn.query("SHOW TABLES")?;
    anyhow::ensure!(tables.is_empty(), "fixture database must be fresh");
    conn.query_drop("CREATE TABLE reports(seq INT PRIMARY KEY, latitude FLOAT NULL, longitude FLOAT NULL) ENGINE=InnoDB")?;
    conn.query_drop("CREATE TABLE report_analysis(seq INT NOT NULL, source VARCHAR(32) NOT NULL, language VARCHAR(8) NOT NULL, severity_level DOUBLE NOT NULL, is_valid BOOLEAN NOT NULL, classification VARCHAR(32) NOT NULL, PRIMARY KEY(seq,source,language), KEY idx_seq(seq), FOREIGN KEY(seq) REFERENCES reports(seq) ON DELETE CASCADE) ENGINE=InnoDB")?;
    conn.query_drop(
        "CREATE TABLE report_status(seq INT PRIMARY KEY,status VARCHAR(32) NULL) ENGINE=InnoDB",
    )?;
    conn.query_drop("CREATE TABLE reports_owners(seq INT NOT NULL,owner VARCHAR(64) NOT NULL,is_public BOOLEAN NULL,PRIMARY KEY(seq,owner)) ENGINE=InnoDB")?;
    conn.query_drop("CREATE TABLE report_side_effect_policy(report_seq INT PRIMARY KEY, FOREIGN KEY(report_seq) REFERENCES reports(seq) ON DELETE CASCADE) ENGINE=InnoDB")?;
    conn.query_drop("CREATE TABLE fixture_digit(n TINYINT PRIMARY KEY)")?;
    conn.query_drop("INSERT INTO fixture_digit VALUES(0),(1),(2),(3),(4),(5),(6),(7),(8),(9)")?;
    conn.query_drop("INSERT INTO reports SELECT 1+a.n+10*b.n+100*c.n+1000*d.n+10000*e.n+100000*f.n,37.7749,-122.4194 FROM fixture_digit a CROSS JOIN fixture_digit b CROSS JOIN fixture_digit c CROSS JOIN fixture_digit d CROSS JOIN fixture_digit e CROSS JOIN fixture_digit f WHERE 1+a.n+10*b.n+100*c.n+1000*d.n+10000*e.n+100000*f.n<=441000")?;
    conn.query_drop(
        "INSERT INTO report_analysis SELECT seq,'fixture','en',0.2,TRUE,'physical' FROM reports",
    )?;
    conn.query_drop("INSERT INTO report_status SELECT seq,'active' FROM reports")?;
    conn.query_drop("INSERT INTO reports_owners SELECT seq,'',FALSE FROM reports")?;
    conn.query_drop(
        "INSERT INTO report_side_effect_policy SELECT seq FROM reports WHERE seq<=353000",
    )?;
    conn.query_drop("DELETE FROM reports_owners WHERE seq IN(2,3,15,16)")?;
    conn.query_drop("INSERT INTO reports_owners VALUES(2,'private',FALSE),(3,'public',TRUE),(15,'private',FALSE),(15,'public',TRUE),(16,'unknown',NULL)")?;
    conn.query_drop("UPDATE report_status SET status='closed' WHERE seq=4")?;
    conn.query_drop("UPDATE report_status SET status=NULL WHERE seq=11")?;
    conn.query_drop("UPDATE report_analysis SET is_valid=FALSE WHERE seq=5")?;
    conn.query_drop("UPDATE report_analysis SET classification='digital' WHERE seq=6")?;
    conn.query_drop("UPDATE reports SET latitude=NULL WHERE seq=7")?;
    conn.query_drop("UPDATE reports SET latitude=0,longitude=0 WHERE seq=8")?;
    conn.query_drop("UPDATE reports SET latitude=91 WHERE seq=9")?;
    conn.query_drop("UPDATE reports SET longitude=181 WHERE seq=10")?;
    conn.query_drop("UPDATE report_analysis SET language='fr',severity_level=0.7 WHERE seq=13")?;
    conn.query_drop("INSERT INTO report_analysis VALUES(14,'fixture','fr',0.9,TRUE,'physical')")?;
    conn.query_drop("UPDATE reports SET latitude=-90,longitude=180 WHERE seq=18")?;
    conn.query_drop("DELETE FROM report_analysis WHERE seq=19")?;
    conn.query_drop("DELETE FROM reports WHERE seq=20")?;

    let baseline = query_report_points(&mut conn, "physical", None)?;
    assert_eq!(baseline.len(), 441000 - 11);
    let ids: Vec<i64> = (1..=1024).collect();
    let expected: BTreeSet<_> = baseline
        .iter()
        .filter(|p| p.seq <= 1024)
        .map(|p| p.seq)
        .collect();
    let rows = query_report_points(&mut conn, "physical", Some(&ids))?;
    assert_eq!(
        rows.iter().map(|p| p.seq).collect::<BTreeSet<_>>(),
        expected
    );
    assert_eq!(
        rows.iter().find(|p| p.seq == 13).unwrap().severity_level,
        0.7
    );
    assert_eq!(
        rows.iter().find(|p| p.seq == 14).unwrap().severity_level,
        0.9
    );

    // The actual parameterized production SQL must start with a bounded PK
    // range, not a scan of the 441k-row analysis table.
    let mut params: Vec<my::Value> = ids.iter().map(|id| (*id).into()).collect();
    params.push("physical".into());
    let plan: Vec<my::Row> = conn.exec(
        format!("EXPLAIN {}", point_query("physical", Some(ids.len()))),
        my::Params::Positional(params),
    )?;
    let first = plan.first().expect("EXPLAIN rows");
    assert_eq!(first.get::<String, _>("table").as_deref(), Some("r"));
    assert_eq!(first.get::<String, _>("key").as_deref(), Some("PRIMARY"));
    assert_eq!(first.get::<String, _>("type").as_deref(), Some("range"));
    assert!(first.get::<u64, _>("rows").unwrap() <= ids.len() as u64);
    for row in plan.iter().skip(1) {
        assert_ne!(row.get::<String, _>("type").as_deref(), Some("ALL"));
    }

    let mut source = PhysicalRefreshDb(conn);
    assert_eq!(source.policy_ids_after(352998, 4)?, vec![352999, 353000]);
    assert_eq!(source.recent_policy_ids(3)?, vec![353000, 352999, 352998]);
    let selected = source.points(&[1, 2, 3, 4, 13, 14, 20])?;
    assert_eq!(
        selected.iter().map(|p| p.seq).collect::<BTreeSet<_>>(),
        BTreeSet::from([1, 3, 13, 14])
    );

    let began = Instant::now();
    let mut checked = 0;
    let spread: Vec<i64> = (0..13312).map(|id| 1 + id * 31).collect();
    for chunk in spread.chunks(crate::quiet_refresh::QUERY_CHUNK) {
        source.points(chunk)?;
        checked += chunk.len();
    }
    let elapsed = began.elapsed();
    eprintln!("renderer fixture: population=441000 candidates={checked} lookup_ms={} chunks={} (synthetic CI evidence, not production cost)", elapsed.as_millis(), checked.div_ceil(crate::quiet_refresh::QUERY_CHUNK));
    anyhow::ensure!(
        elapsed < Duration::from_secs(20),
        "synthetic refresh exceeds cycle budget"
    );
    Ok(())
}
