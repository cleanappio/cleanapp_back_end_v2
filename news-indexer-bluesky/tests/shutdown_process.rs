#![cfg(unix)]

use news_indexer_bluesky::shutdown::{Shutdown, Wait};
use std::{
    io::{BufRead, BufReader, Write},
    process::{Child, Command, Stdio},
    sync::{
        atomic::{AtomicUsize, Ordering},
        mpsc, Arc,
    },
    time::{Duration, Instant},
};

fn checkpoint(name: &str) {
    println!("CHECKPOINT:{name}");
    std::io::stdout().flush().unwrap();
}

fn complete_work() {
    let mut line = String::new();
    assert!(std::io::stdin().read_line(&mut line).unwrap() > 0);
}

// An owned native child: no service startup, database, sender, or remote call.
#[test]
fn process_fixture() {
    let Ok(mode) = std::env::var("NEWS_SHUTDOWN_FIXTURE") else {
        return;
    };
    let expected = std::env::var("NEWS_SHUTDOWN_SIGNAL").unwrap();
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .worker_threads(2)
        .enable_all()
        .build()
        .unwrap();
    runtime.block_on(async {
        let mut shutdown = Shutdown::new().unwrap();
        println!("PID:{}", std::process::id());
        std::io::stdout().flush().unwrap();
        let reason = match mode.as_str() {
            "startup" => {
                checkpoint("STARTUP");
                complete_work();
                tokio::time::sleep(Duration::from_millis(20)).await;
                shutdown
                    .run_batches(Duration::ZERO, || async {
                        panic!("a shutdown pending during startup started a batch");
                    })
                    .await
                    .unwrap()
            }
            "batch" => {
                let count = Arc::new(AtomicUsize::new(0));
                let batches = count.clone();
                let signal = shutdown
                    .run_batches(Duration::ZERO, || {
                        let batches = batches.clone();
                        async move {
                            assert_eq!(
                                batches.fetch_add(1, Ordering::SeqCst),
                                0,
                                "pending shutdown started another batch"
                            );
                            checkpoint("WORK_STARTED");
                            complete_work();
                            tokio::time::sleep(Duration::from_millis(20)).await;
                            checkpoint("NETWORK_RECEIPT");
                            complete_work();
                            checkpoint("DATABASE_RECEIPT");
                        }
                    })
                    .await
                    .unwrap();
                assert_eq!(count.load(Ordering::SeqCst), 1);
                signal
            }
            "stream" => {
                assert!(matches!(
                    shutdown.wait_for(std::future::ready(1)).await.unwrap(),
                    Wait::Ready(1)
                ));
                checkpoint("CURSOR_RECEIPT");
                // This models the active message's cursor-to-store section.
                // It is deliberately outside the cancellable next-message wait.
                complete_work();
                tokio::time::sleep(Duration::from_millis(20)).await;
                checkpoint("STORE_RECEIPT");
                match shutdown.wait_for(std::future::ready(2)).await.unwrap() {
                    Wait::Stopped(reason) => reason,
                    Wait::Ready(_) => panic!("pending shutdown admitted a second message"),
                }
            }
            "idle" => {
                checkpoint("IDLE_WAIT");
                match shutdown
                    .wait_for(std::future::pending::<()>())
                    .await
                    .unwrap()
                {
                    Wait::Stopped(reason) => reason,
                    Wait::Ready(_) => unreachable!(),
                }
            }
            _ => panic!("unknown fixture"),
        };
        assert_eq!(reason, expected);
        // Once observed, shutdown remains latched across another ready wait.
        assert!(matches!(
            shutdown.wait_for(std::future::ready(())).await.unwrap(),
            Wait::Stopped(_)
        ));
        checkpoint("DISCONNECTED");
    });
}

struct OwnedChild(Child);
impl Drop for OwnedChild {
    fn drop(&mut self) {
        if self.0.try_wait().ok().flatten().is_none() {
            let _ = self.0.kill(); // Failure cleanup only for our local fixture.
            let _ = self.0.wait();
        }
    }
}

fn expect(rx: &mpsc::Receiver<String>, expected: &str) {
    let deadline = Instant::now() + Duration::from_secs(8);
    loop {
        let line = rx
            .recv_timeout(deadline.saturating_duration_since(Instant::now()))
            .unwrap();
        if line.ends_with(expected) {
            return;
        }
    }
}

fn signal_during(mode: &str, signal: &str) {
    let mut child = OwnedChild(
        Command::new(std::env::current_exe().unwrap())
            .args([
                "--exact",
                "process_fixture",
                "--nocapture",
                "--test-threads=1",
            ])
            .env_clear()
            .env("PATH", "/usr/bin:/bin")
            .env("NEWS_SHUTDOWN_FIXTURE", mode)
            .env("NEWS_SHUTDOWN_SIGNAL", signal)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap(),
    );
    let output = child.0.stdout.take().unwrap();
    let (tx, rx) = mpsc::channel();
    let reader = std::thread::spawn(move || {
        for line in BufReader::new(output).lines() {
            let _ = tx.send(line.unwrap());
        }
    });
    expect(&rx, &format!("PID:{}", child.0.id()));
    expect(
        &rx,
        match mode {
            "startup" => "CHECKPOINT:STARTUP",
            "batch" => "CHECKPOINT:WORK_STARTED",
            "stream" => "CHECKPOINT:CURSOR_RECEIPT",
            _ => "CHECKPOINT:IDLE_WAIT",
        },
    );
    assert!(Command::new("/bin/kill")
        .arg(if signal == "SIGTERM" { "-TERM" } else { "-INT" })
        .arg(child.0.id().to_string())
        .status()
        .unwrap()
        .success());
    if mode != "idle" {
        assert!(
            child.0.try_wait().unwrap().is_none(),
            "signal cancelled active work"
        );
        child
            .0
            .stdin
            .as_mut()
            .unwrap()
            .write_all(b"work completed\n")
            .unwrap();
    }
    if mode == "batch" {
        expect(&rx, "CHECKPOINT:NETWORK_RECEIPT");
        assert!(
            child.0.try_wait().unwrap().is_none(),
            "exited before database receipt"
        );
        child
            .0
            .stdin
            .as_mut()
            .unwrap()
            .write_all(b"database committed\n")
            .unwrap();
        expect(&rx, "CHECKPOINT:DATABASE_RECEIPT");
    } else if mode == "stream" {
        expect(&rx, "CHECKPOINT:STORE_RECEIPT");
    }
    expect(&rx, "CHECKPOINT:DISCONNECTED");
    let deadline = Instant::now() + Duration::from_secs(8);
    loop {
        if let Some(status) = child.0.try_wait().unwrap() {
            assert!(status.success(), "fixture did not finish cleanly");
            break;
        }
        assert!(Instant::now() < deadline, "owned fixture failed to drain");
        std::thread::sleep(Duration::from_millis(10));
    }
    reader.join().unwrap();
}

#[test]
fn term_during_startup_skips_first_batch() {
    signal_during("startup", "SIGTERM")
}
#[test]
fn int_during_startup_skips_first_batch() {
    signal_during("startup", "SIGINT")
}
#[test]
fn term_during_batch_waits_for_both_receipts() {
    signal_during("batch", "SIGTERM")
}
#[test]
fn int_during_batch_waits_for_both_receipts() {
    signal_during("batch", "SIGINT")
}
#[test]
fn term_during_message_finishes_store_before_next_message() {
    signal_during("stream", "SIGTERM")
}
#[test]
fn int_during_message_finishes_store_before_next_message() {
    signal_during("stream", "SIGINT")
}
#[test]
fn term_interrupts_idle_wait() {
    signal_during("idle", "SIGTERM")
}
#[test]
fn int_interrupts_idle_wait() {
    signal_during("idle", "SIGINT")
}
