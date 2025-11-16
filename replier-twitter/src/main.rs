use anyhow::{Context, Result};
use clap::Parser;
use cleanapp_rustlib::rabbitmq::subscriber::{Callback, Message, Subscriber};
use log::{error, info, warn};
use mysql_async::prelude::*;
use mysql_async::Pool;
use reqwest::StatusCode;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::Arc;
use std::time::Duration as StdDuration;
use tokio::sync::Mutex;

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
	#[arg(long, env = "TWITTER_USER_BEARER_TOKEN")] twitter_user_bearer_token: String,

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
		let resp = self
			.http
			.post("https://api.twitter.com/2/tweets")
			.bearer_auth(&self.token)
			.json(&req)
			.send()
			.await?;
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
		let evt: TwitterReplyEvent = msg.unmarshal_to()?;
		let this = self.clone_for_async();
		// Spawn async task per message
		tokio::spawn(async move {
			if let Err(e) = this.ensure_table().await {
				error!("ensure replier_twitter table failed: {}", e);
				return;
			}
			match this.already_replied(evt.seq).await {
				Ok(true) => {
					info!("seq {} already replied; skipping", evt.seq);
					return;
				}
				Ok(false) => {}
				Err(e) => {
					warn!("check already_replied failed: {}", e);
				}
			}
			let link = this.build_link(evt.seq, &evt.classification);
			let text = this.build_text(&link);
			match this.post_reply(&evt.tweet_id, &text).await {
				Ok(Some(reply_id)) => {
					info!("posted reply for seq {} tweet {} -> reply {}", evt.seq, evt.tweet_id, reply_id);
					if let Err(e) = this.record_attempt(evt.seq, &evt.tweet_id, &evt.classification, Some(reply_id)).await {
						warn!("record reply success failed: {}", e);
					}
				}
				Ok(None) => {
					// rate limited; record attempt without reply id
					if let Err(e) = this.record_attempt(evt.seq, &evt.tweet_id, &evt.classification, None).await {
						warn!("record attempt (429) failed: {}", e);
					}
				}
				Err(e) => {
					error!("post_reply error: {}", e);
					if let Err(e2) = this.record_attempt(evt.seq, &evt.tweet_id, &evt.classification, None).await {
						warn!("record attempt failed: {}", e2);
					}
				}
			}
		});
		Ok(())
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
		}
	}
}

#[tokio::main]
async fn main() -> Result<()> {
	env_logger::init();
	let args = Args::parse();

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
		token: args.twitter_user_bearer_token.clone(),
		base_url: args.cleanapp_base_url.clone(),
		template: args.reply_template.clone(),
		limiter: Arc::new(Mutex::new(())),
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


