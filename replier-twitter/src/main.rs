use anyhow::{Context, Result};
use clap::Parser;
use cleanapp_rustlib::rabbitmq::subscriber::{permanent, Callback, Message, Subscriber};
use log::{error, info, warn};
use mysql_async::prelude::*;
use mysql_async::Pool;
use reqwest::StatusCode;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::future::Future;
use std::sync::Arc;
use std::time::Duration as StdDuration;
use tokio::sync::Mutex;
use base64::engine::general_purpose::STANDARD;
use base64::Engine as _;

#[derive(Parser, Debug, Clone)]
struct Args {
	#[arg(long, default_value = "config.toml")] config_path: String,
	#[arg(long, env = "DB_URL")] db_url: Option<String>,

	// Rabbit
	#[arg(long, env = "AMQP_HOST", default_value = "localhost")] amqp_host: String,
	#[arg(long, env = "AMQP_PORT", default_value_t = 5672)] amqp_port: u16,
	#[arg(long, env = "AMQP_USER", default_value = "guest")] amqp_user: String,
	#[arg(long, env = "AMQP_PASSWORD", default_value = "guest")] amqp_password: String,
	#[arg(long, env = "RABBITMQ_EXCHANGE", default_value = "cleanapp")] exchange: String,
	#[arg(long, env = "RABBITMQ_TWITTER_REPLY_QUEUE", default_value = "twitter-reply")] queue: String,
	#[arg(long, env = "RABBITMQ_TWITTER_REPLY_ROUTING_KEY", default_value = "twitter.reply")] routing_key: String,

	// Twitter API
	#[arg(long, env = "TWITTER_USER_BEARER_TOKEN")] twitter_user_bearer_token: Option<String>,
	// OAuth 1.0a user-context (preferred env names)
	#[arg(long, env = "TWITTER_API_KEY")] twitter_api_key: Option<String>,
	#[arg(long, env = "TWITTER_API_SECRET")] twitter_api_secret: Option<String>,
	#[arg(long, env = "TWITTER_OAUTH1_ACCESS_TOKEN")] twitter_oauth1_access_token: Option<String>,
	#[arg(long, env = "TWITTER_OAUTH1_ACCESS_SECRET")] twitter_oauth1_access_secret: Option<String>,

	// CleanApp URL and reply text
	#[arg(long, env = "CLEANAPP_BASE_URL", default_value = "https://cleanapp.io")] cleanapp_base_url: String,
	#[arg(long, env = "TWITTER_REPLY_TEMPLATE", default_value = "The relevant cleanapp report was created by your mention: {link} #cleanapped")]
	reply_template: String,
}

#[derive(Deserialize, Debug, Clone)]
struct TwitterReplyEvent {
	seq: i32,
	tweet_id: String,
	classification: String, // "physical" | "digital"
}

#[derive(Serialize)]
struct CreateTweetRequest<'a> {
	text: &'a str,
	reply: CreateTweetReply<'a>,
}

#[derive(Serialize)]
struct CreateTweetReply<'a> {
	in_reply_to_tweet_id: &'a str,
}

struct ReplyCallback {
	pool: Pool,
	http: reqwest::Client,
	token: String,
	base_url: String,
	template: String,
	// throttle to avoid bursts if needed
	limiter: Arc<Mutex<()>>,
	oauth1: Option<OAuth1Creds>,
}

#[derive(Clone)]
struct OAuth1Creds {
	consumer_key: String,
	consumer_secret: String,
	access_token: String,
	access_secret: String,
}

fn oauth_percent_encode(s: &str) -> String {
	// RFC3986 percent-encoding
	urlencoding::encode(s).into_owned()
}

fn oauth1_auth_header(creds: &OAuth1Creds, method: &str, url: &str) -> String {
	use hmac::{Hmac, Mac};
	use sha1::Sha1;
	type HmacSha1 = Hmac<Sha1>;

	let nonce = format!("{:x}", rand::random::<u64>());
	let timestamp = format!("{}", std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap_or_default().as_secs());
	let mut params: Vec<(String, String)> = vec![
		("oauth_consumer_key".to_string(), creds.consumer_key.clone()),
		("oauth_nonce".to_string(), nonce.clone()),
		("oauth_signature_method".to_string(), "HMAC-SHA1".to_string()),
		("oauth_timestamp".to_string(), timestamp.clone()),
		("oauth_token".to_string(), creds.access_token.clone()),
		("oauth_version".to_string(), "1.0".to_string()),
	];
	// Sort by key, then value
	params.sort_by(|a, b| a.0.cmp(&b.0).then(a.1.cmp(&b.1)));
	let param_str = params
		.iter()
		.map(|(k, v)| format!("{}={}", oauth_percent_encode(k), oauth_percent_encode(v)))
		.collect::<Vec<_>>()
		.join("&");
	let base_str = format!(
		"{}&{}&{}",
		method.to_uppercase(),
		oauth_percent_encode(url),
		oauth_percent_encode(&param_str)
	);
	let signing_key = format!(
		"{}&{}",
		oauth_percent_encode(&creds.consumer_secret),
		oauth_percent_encode(&creds.access_secret)
	);
	let mut mac = HmacSha1::new_from_slice(signing_key.as_bytes()).unwrap();
	mac.update(base_str.as_bytes());
	let signature_bytes = mac.finalize().into_bytes();
	let signature = STANDARD.encode(signature_bytes);
	let header = format!(
		"OAuth oauth_consumer_key=\"{}\", oauth_nonce=\"{}\", oauth_signature=\"{}\", oauth_signature_method=\"HMAC-SHA1\", oauth_timestamp=\"{}\", oauth_token=\"{}\", oauth_version=\"1.0\"",
		oauth_percent_encode(&creds.consumer_key),
		oauth_percent_encode(&nonce),
		oauth_percent_encode(&signature),
		oauth_percent_encode(&timestamp),
		oauth_percent_encode(&creds.access_token),
	);
	header
}

impl ReplyCallback {
	fn build_link(&self, seq: i32, classification: &str) -> String {
		let tab = if classification.eq_ignore_ascii_case("physical") {
			"physical"
		} else {
			"digital"
		};
		format!("{}/?tab={}&seq={}", self.base_url.trim_end_matches('/'), tab, seq)
	}

	fn build_text(&self, link: &str) -> String {
		self.template.replace("{link}", link)
	}

	async fn ensure_table(&self) -> Result<()> {
		let mut c = self.pool.get_conn().await?;
		c.query_drop(
			r#"
			CREATE TABLE IF NOT EXISTS replier_twitter (
				seq INT NOT NULL PRIMARY KEY,
				tweet_id BIGINT NOT NULL,
				classification ENUM('physical','digital') NOT NULL,
				reply_tweet_id BIGINT NULL,
				replied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				attempts INT DEFAULT 0,
				UNIQUE KEY uniq_tweet (tweet_id),
				CONSTRAINT fk_replier_twitter_seq FOREIGN KEY (seq) REFERENCES reports(seq)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
			"#,
		)
		.await?;
		Ok(())
	}

	async fn already_replied(&self, seq: i32) -> Result<bool> {
		let mut c = self.pool.get_conn().await?;
		let exists: Option<i32> = c
			.exec_first("SELECT seq FROM replier_twitter WHERE seq = ?", (seq,))
			.await?;
		Ok(exists.is_some())
	}

	async fn record_attempt(&self, seq: i32, tweet_id: &str, classification: &str, reply_tweet_id: Option<i64>) -> Result<()> {
		let mut c = self.pool.get_conn().await?;
		if let Some(rid) = reply_tweet_id {
			c.exec_drop(
				r#"INSERT INTO replier_twitter (seq, tweet_id, classification, reply_tweet_id)
				   VALUES (?, ?, ?, ?)
				   ON DUPLICATE KEY UPDATE reply_tweet_id=VALUES(reply_tweet_id), replied_at=NOW()"#,
				(seq, tweet_id, classification, rid),
			)
			.await?;
		} else {
			c.exec_drop(
				r#"INSERT INTO replier_twitter (seq, tweet_id, classification, attempts)
				   VALUES (?, ?, ?, 1)
				   ON DUPLICATE KEY UPDATE attempts=attempts+1, replied_at=NOW()"#,
				(seq, tweet_id, classification),
			)
			.await?;
		}
		Ok(())
	}

	async fn post_reply(&self, in_reply_to_tweet_id: &str, text: &str) -> Result<Option<i64>> {
		// best-effort throttle
		let _g = self.limiter.lock().await;

		let req = CreateTweetRequest {
			text,
			reply: CreateTweetReply {
				in_reply_to_tweet_id,
			},
		};
		let url = "https://api.twitter.com/2/tweets";
		let request_builder = self.http.post(url).json(&req);
		let request_builder = if let Some(ref creds) = self.oauth1 {
			let auth = oauth1_auth_header(creds, "POST", url);
			request_builder.header("Authorization", auth)
		} else if !self.token.is_empty() {
			request_builder.bearer_auth(&self.token)
		} else {
			anyhow::bail!("twitter auth not configured: provide OAuth1 creds or TWITTER_USER_BEARER_TOKEN");
		};
		let resp = request_builder.send().await?;
		if resp.status() == StatusCode::TOO_MANY_REQUESTS {
			warn!("twitter 429 when creating reply; backing off");
			return Ok(None);
		}
		if !resp.status().is_success() {
			let st = resp.status();
			let body = resp.text().await.unwrap_or_default();
			anyhow::bail!("twitter create tweet error {}: {}", st, body);
		}
		let v: serde_json::Value = resp.json().await.unwrap_or(serde_json::json!({}));
		let id_opt = v
			.get("data")
			.and_then(|d| d.get("id"))
			.and_then(|x| x.as_str())
			.and_then(|s| s.parse::<i64>().ok());
		Ok(id_opt)
	}
}

impl Callback for ReplyCallback {
	fn on_message(&self, msg: &Message) -> Result<(), Box<dyn std::error::Error>> {
		let evt: TwitterReplyEvent = msg.unmarshal_to().map_err(|e| permanent(e))?;
		let this = self.clone_for_async();

		enum ProcessingError {
			Transient(anyhow::Error),
			Permanent(anyhow::Error),
		}

		fn block_on<F: Future>(fut: F) -> F::Output {
			tokio::task::block_in_place(|| tokio::runtime::Handle::current().block_on(fut))
		}

		let res: std::result::Result<(), ProcessingError> = block_on(async move {
			this.ensure_table()
				.await
				.map_err(ProcessingError::Transient)?;

			// Misconfiguration should not retry forever.
			if this.oauth1.is_none() && this.token.is_empty() {
				return Err(ProcessingError::Permanent(anyhow::anyhow!(
					"twitter auth not configured (need OAuth1 creds or TWITTER_USER_BEARER_TOKEN)"
				)));
			}

			match this.already_replied(evt.seq).await {
				Ok(true) => {
					info!("seq {} already replied; skipping", evt.seq);
					return Ok(());
				}
				Ok(false) => {}
				Err(e) => {
					// DB check failure is retryable; do not post a duplicate reply.
					return Err(ProcessingError::Transient(e));
				}
			}

			let link = this.build_link(evt.seq, &evt.classification);
			let text = this.build_text(&link);

			match this.post_reply(&evt.tweet_id, &text).await {
				Ok(Some(reply_id)) => {
					info!(
						"posted reply for seq {} tweet {} -> reply {}",
						evt.seq, evt.tweet_id, reply_id
					);
					// If we posted the tweet but can't record it, treat as permanent to avoid duplicates.
					this.record_attempt(
						evt.seq,
						&evt.tweet_id,
						&evt.classification,
						Some(reply_id),
					)
					.await
					.map_err(ProcessingError::Permanent)?;
					Ok(())
				}
				Ok(None) => {
					// 429: record attempt, then retry later via requeue.
					this.record_attempt(evt.seq, &evt.tweet_id, &evt.classification, None)
						.await
						.map_err(ProcessingError::Transient)?;
					Err(ProcessingError::Transient(anyhow::anyhow!(
						"twitter rate limited (429)"
					)))
				}
				Err(e) => {
					error!("post_reply error: {}", e);
					// Best-effort record attempt; if it fails, still requeue.
					if let Err(e2) =
						this.record_attempt(evt.seq, &evt.tweet_id, &evt.classification, None)
							.await
					{
						warn!("record attempt failed: {}", e2);
					}
					Err(ProcessingError::Transient(e))
				}
			}
		});

			match res {
				Ok(()) => Ok(()),
				Err(ProcessingError::Transient(e)) => Err(Box::new(std::io::Error::new(
					std::io::ErrorKind::Other,
					e.to_string(),
				))),
				Err(ProcessingError::Permanent(e)) => Err(permanent(std::io::Error::new(
					std::io::ErrorKind::Other,
					e.to_string(),
				))),
			}
		}
	}

impl ReplyCallback {
	fn clone_for_async(&self) -> Self {
		Self {
			pool: self.pool.clone(),
			http: self.http.clone(),
			token: self.token.clone(),
			base_url: self.base_url.clone(),
			template: self.template.clone(),
			limiter: self.limiter.clone(),
			oauth1: self.oauth1.clone(),
		}
	}
}

#[tokio::main]
async fn main() -> Result<()> {
	env_logger::init();
	let args = Args::parse();

	// Allow disabling via env for prod by default
	let service_disabled = std::env::var("REPLIER_TWITTER_SERVICE_DISABLED")
		.unwrap_or_else(|_| "false".to_string())
		.to_lowercase();
	if service_disabled == "true" || service_disabled == "1" || service_disabled == "yes" {
		info!("replier_twitter disabled by REPLIER_TWITTER_SERVICE_DISABLED=true; exiting");
		return Ok(());
	}

	let db_url = args
		.db_url
		.clone()
		.context("db_url must be provided via --db-url or DB_URL")?;

	info!(
		"replier_twitter start exchange={} queue={} routing_key={}",
		args.exchange, args.queue, args.routing_key
	);

	let pool = Pool::new(mysql_async::Opts::from_url(&db_url)?);
	let http = reqwest::Client::builder()
		.timeout(StdDuration::from_secs(30))
		.build()?;

	let callback = Arc::new(ReplyCallback {
		pool: pool.clone(),
		http,
		token: args.twitter_user_bearer_token.clone().unwrap_or_default(),
		base_url: args.cleanapp_base_url.clone(),
		template: args.reply_template.clone(),
		limiter: Arc::new(Mutex::new(())),
		oauth1: match (&args.twitter_api_key, &args.twitter_api_secret, &args.twitter_oauth1_access_token, &args.twitter_oauth1_access_secret) {
			(Some(ck), Some(cs), Some(at), Some(as_)) if !ck.is_empty() && !cs.is_empty() && !at.is_empty() && !as_.is_empty() => {
				Some(OAuth1Creds {
					consumer_key: ck.clone(),
					consumer_secret: cs.clone(),
					access_token: at.clone(),
					access_secret: as_.clone(),
				})
			}
			_ => None
		},
	});

	let amqp_url = format!(
		"amqp://{}:{}@{}:{}",
		args.amqp_user, args.amqp_password, args.amqp_host, args.amqp_port
	);

	let mut subscriber = Subscriber::new(&amqp_url, &args.exchange, &args.queue).await?;
	let mut routing_map: HashMap<String, Arc<dyn Callback + Send + Sync + 'static>> = HashMap::new();
	routing_map.insert(args.routing_key.clone(), callback);
	subscriber.start(routing_map).await?;

	// Block forever
	tokio::signal::ctrl_c().await.ok();
	Ok(())
}
