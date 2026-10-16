use std::{io, time::Duration};

/// Keep signal listeners alive during a batch, so shutdown cannot be lost while
/// database/network work is draining. A pending shutdown wins over another tick.
pub struct Shutdown {
    #[cfg(unix)]
    interrupt: tokio::signal::unix::Signal,
    #[cfg(unix)]
    terminate: tokio::signal::unix::Signal,
}

impl Shutdown {
    pub fn new() -> io::Result<Self> {
        Ok(Self {
            #[cfg(unix)]
            interrupt: tokio::signal::unix::signal(tokio::signal::unix::SignalKind::interrupt())?,
            #[cfg(unix)]
            terminate: tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())?,
        })
    }

    async fn received(&mut self) -> io::Result<&'static str> {
        #[cfg(unix)]
        {
            tokio::select! {
                _ = self.interrupt.recv() => Ok("SIGINT"),
                _ = self.terminate.recv() => Ok("SIGTERM"),
            }
        }
        #[cfg(not(unix))]
        {
            tokio::signal::ctrl_c().await?;
            Ok("Ctrl-C")
        }
    }

    pub async fn wait_before_batch(&mut self, delay: Duration) -> io::Result<Option<&'static str>> {
        tokio::select! {
            biased;
            signal = self.received() => signal.map(Some),
            _ = tokio::time::sleep(delay) => Ok(None),
        }
    }
}

#[cfg(all(test, unix))]
mod tests {
    use super::Shutdown;
    use std::{
        io::{BufRead, BufReader, Write},
        process::{Child, Command, Stdio},
        sync::mpsc,
        time::{Duration, Instant, SystemTime, UNIX_EPOCH},
    };

    // The child runs no service main, database code, HTTP request or email code.
    #[test]
    fn signal_child() {
        let Ok(expected) = std::env::var("FETCHER_SIGNAL_FIXTURE") else {
            return;
        };
        let runtime = tokio::runtime::Builder::new_multi_thread()
            .worker_threads(2)
            .enable_all()
            .build()
            .unwrap();
        runtime.block_on(async {
            let mut shutdown = Shutdown::new().unwrap();
            println!("READY:{}", std::process::id());
            std::io::stdout().flush().unwrap();
            // Model a batch finishing after the signal was delivered. The
            // listeners are registered but no receive future is being polled.
            let mut finish = String::new();
            assert!(std::io::stdin().read_line(&mut finish).unwrap() > 0);
            tokio::time::sleep(Duration::from_millis(20)).await;
            let reason = shutdown.wait_before_batch(Duration::ZERO).await.unwrap();
            assert_eq!(reason, Some(expected.as_str()));
        });
    }

    struct OwnedChild(Child);
    impl Drop for OwnedChild {
        fn drop(&mut self) {
            if self.0.try_wait().ok().flatten().is_none() {
                // Failure cleanup targets only this freshly spawned test child.
                let _ = self.0.kill();
                let _ = self.0.wait();
            }
        }
    }

    fn entrypoint_signal(signal: &str) {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        let dir = std::env::temp_dir().join(format!(
            "fetcher-signal-{}-{signal}-{nonce}",
            std::process::id()
        ));
        std::fs::create_dir(&dir).unwrap();
        let executable = std::env::current_exe().unwrap();
        let quoted = format!(
            "'{}'",
            executable.display().to_string().replace('\'', "'\\''")
        );
        let entrypoint = include_str!("../entrypoint.sh").replace(
            "/email-fetcher",
            &format!("{quoted} --exact shutdown::tests::signal_child --nocapture --test-threads=1"),
        );
        let script = dir.join("entrypoint.sh");
        std::fs::write(&script, entrypoint).unwrap();
        let mut child = OwnedChild(
            Command::new("/bin/sh")
                .arg(&script)
                .env_clear()
                .env("PATH", "/usr/bin:/bin")
                .env("FETCHER_SIGNAL_FIXTURE", signal)
                .stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::null())
                .spawn()
                .unwrap(),
        );
        let output = child.0.stdout.take().unwrap();
        let (tx, rx) = mpsc::channel();
        let reader = std::thread::spawn(move || {
            for line in BufReader::new(output).lines() {
                let line = line.unwrap();
                if let Some(pid) = line.split("READY:").nth(1) {
                    let _ = tx.send(pid.parse::<u32>().unwrap());
                }
            }
        });
        let pid = rx.recv_timeout(Duration::from_secs(5)).unwrap();
        // This runs the actual shell entrypoint with only the binary path
        // substituted: exec must preserve the process we are about to signal.
        assert_eq!(pid, child.0.id());
        assert!(Command::new("/bin/kill")
            .arg(if signal == "SIGTERM" { "-TERM" } else { "-INT" })
            .arg(pid.to_string())
            .status()
            .unwrap()
            .success());
        child
            .0
            .stdin
            .take()
            .unwrap()
            .write_all(b"batch finished\n")
            .unwrap();
        let deadline = Instant::now() + Duration::from_secs(5);
        loop {
            if let Some(status) = child.0.try_wait().unwrap() {
                assert!(status.success());
                break;
            }
            assert!(Instant::now() < deadline, "owned test child did not drain");
            std::thread::sleep(Duration::from_millis(10));
        }
        reader.join().unwrap();
        std::fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn entrypoint_exec_receives_sigterm_during_batch() {
        entrypoint_signal("SIGTERM");
    }

    #[test]
    fn entrypoint_exec_receives_sigint_during_batch() {
        entrypoint_signal("SIGINT");
    }
}
