use clap::Parser;
use anyhow::Result;
use tokio::time::{sleep, Duration};
use tonic::transport::{Channel, Endpoint, ClientTlsConfig};
use tonic::Request;
use hex::FromHex;
use sha3::{Digest, Keccak256};
use url::Url;
use mysql_async::prelude::Queryable;

pub mod proto { tonic::include_proto!("stxn.io"); }

use proto::{request_registrator_service_client::RequestRegistratorServiceClient, PushRequestProto, UserEventProto, UserObjectiveProto, AdditionalDataProto, CallObjectProto, AppChainResultStatus};

#[derive(Parser, Debug, Clone)]
#[command(name = "reports-pusher")] 
struct Args {
    /// MySQL connection string, e.g. mysql://user:pass@host:port/db
    #[arg(long)]
    mysql_url: String,

    /// Request registrator gRPC endpoint, e.g. https://stxn-cleanapp-dev.stxn.io:443
    #[arg(long)]
    request_registrator_url: String,

    /// App id (32 bytes hex) for CleanApp rewards
    #[arg(long)]
    app_id_hex: String,

    /// Chain id for objectives (e.g., 84532 for Base Sepolia)
    #[arg(long)] 
    chain_id: u64,

    /// Poll interval secs
    #[arg(long, default_value = "5")]
    poll_secs: u64,
}

fn keccak256(data: &[u8]) -> [u8; 32] {
    let mut hasher = Keccak256::new();
    hasher.update(data);
    let out = hasher.finalize();
    let mut arr = [0u8; 32];
    arr.copy_from_slice(&out);
    arr
}

async fn connect_rr(url: &str) -> Result<RequestRegistratorServiceClient<Channel>> {
    let parsed = Url::parse(url)?;
    let scheme = parsed.scheme();
    let mut endpoint = Endpoint::from_shared(url.to_string())?
        .http2_keep_alive_interval(Duration::from_secs(30))
        .keep_alive_timeout(Duration::from_secs(10))
        .keep_alive_while_idle(true);
    // Explicit TLS config with SNI/ALPN if https
    if scheme == "https" {
        if let Some(host) = parsed.host_str() {
            let tls = ClientTlsConfig::new()
                .domain_name(host.to_string())
                .with_enabled_roots();
            endpoint = endpoint.tls_config(tls)?;
        }
    }
    let channel = endpoint.connect().await?;
    if scheme == "https" {
        log::info!("using TLS to connect to {}", parsed.host_str().unwrap_or(""));
    }
    Ok(RequestRegistratorServiceClient::new(channel))
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse();
    stderrlog::new()
        .verbosity(log::Level::Info)
        .timestamp(stderrlog::Timestamp::Millisecond)
        .init()
        .unwrap();

    log::info!("reports-pusher starting: RR={}, chain_id={}, poll={}s", args
        .request_registrator_url, args.chain_id, args.poll_secs);

    let mut rr = connect_rr(&args.request_registrator_url).await?;

    // Parse app id
    let app_id = <[u8; 32]>::from_hex(args.app_id_hex.trim_start_matches("0x")).expect("APP_ID_HEX must be 32-byte hex");

    // Connect to MySQL
    let opts = mysql_async::Opts::from_url(&args.mysql_url)?;
    let pool = mysql_async::Pool::new(opts);

    loop {
        if let Err(e) = run_once(&mut rr, &pool, &app_id, args.chain_id).await {
            log::error!("run_once error: {:#}", e);
        }
        sleep(Duration::from_secs(args.poll_secs)).await;
    }
}

async fn run_once(rr: &mut RequestRegistratorServiceClient<Channel>, pool: &mysql_async::Pool, app_id: &[u8; 32], chain_id: u64) -> Result<()> {
    let mut conn = pool.get_conn().await?;

    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS reports_pushed (
            report_seq BIGINT PRIMARY KEY
        )
    "#).await?;

    let rows: Vec<(i64, String, f64, f64)> = conn.exec_map(
        r#"
        SELECT r.seq, r.id, r.latitude, r.longitude
        FROM reports r
        LEFT JOIN reports_pushed p ON p.report_seq = r.seq
        WHERE p.report_seq IS NULL
        ORDER BY r.seq ASC
        LIMIT 50
        "#,
        (),
        |(seq, id, lat, lon)| (seq, id, lat, lon)
    ).await?;

    if rows.is_empty() { return Ok(()); }

    for (seq, user_id, lat, lon) in rows {
        let user_objective = UserObjectiveProto {
            app_id: app_id.to_vec(),
            nonse: seq as u64,
            chain_id,
            call_objects: vec![CallObjectProto {
                id: 0,
                chain_id,
                salt: vec![0; 32],
                amount: vec![0; 32],
                gas: vec![0; 32],
                address: vec![],
                skippable: true,
                verifiable: false,
                callvalue: vec![],
                returnvalue: vec![],
            }],
        };

        let additional_data = vec![
            AdditionalDataProto { key: keccak256(b"user_id").to_vec(), value: user_id.as_bytes().to_vec() },
            AdditionalDataProto { key: keccak256(b"latitude").to_vec(), value: lat.to_le_bytes().to_vec() },
            AdditionalDataProto { key: keccak256(b"longitude").to_vec(), value: lon.to_le_bytes().to_vec() },
            AdditionalDataProto { key: keccak256(b"report_seq").to_vec(), value: seq.to_le_bytes().to_vec() },
        ];

        let intent_id = {
            let mut buf = Vec::with_capacity(32 + 8);
            buf.extend_from_slice(app_id);
            buf.extend_from_slice(&(seq as u64).to_be_bytes());
            keccak256(&buf).to_vec()
        };

        let event = UserEventProto {
            intent_id,
            app_id: app_id.to_vec(),
            chain_id,
            block_number: 0,
            user_objective: Some(user_objective),
            additional_data,
        };

        let req = PushRequestProto { event: Some(event) };
        let resp = rr.push(Request::new(req)).await?.into_inner();
        if let Some(res) = resp.result { 
            let status = res.status();
            if status != AppChainResultStatus::Ok { 
                log::warn!("Push failed for report {}: {:?}", seq, res.message);
                continue; 
            }
        }
        conn.exec_drop("INSERT INTO reports_pushed (report_seq) VALUES (?)", (seq,)).await?;
        log::info!("Pushed report seq={} as sequence_id={}", seq, resp.sequence_id);
    }

    Ok(())
} 