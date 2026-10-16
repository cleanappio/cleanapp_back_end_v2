use std::io;

/// Register before startup and retain both streams while work is unpolled.
/// Once observed, shutdown stays latched across startup and serving phases.
pub struct Shutdown {
    received: Option<&'static str>,
    #[cfg(unix)]
    interrupt: tokio::signal::unix::Signal,
    #[cfg(unix)]
    terminate: tokio::signal::unix::Signal,
}
impl Shutdown {
    pub fn new() -> io::Result<Self> {
        Ok(Self {
            received: None,
            #[cfg(unix)]
            interrupt: tokio::signal::unix::signal(tokio::signal::unix::SignalKind::interrupt())?,
            #[cfg(unix)]
            terminate: tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())?,
        })
    }
    pub async fn wait(&mut self) -> io::Result<&'static str> {
        if let Some(reason) = self.received {
            return Ok(reason);
        }
        #[cfg(unix)]
        let reason = tokio::select! {
            biased;
            _ = self.terminate.recv() => "SIGTERM",
            _ = self.interrupt.recv() => "SIGINT",
        };
        #[cfg(not(unix))]
        let reason = {
            tokio::signal::ctrl_c().await?;
            "Ctrl-C"
        };
        self.received = Some(reason);
        Ok(reason)
    }
}

#[cfg(all(test, unix))]
mod tests {
    use super::Shutdown;
    use std::{
        io::{BufRead, BufReader, Write},
        process::{Child, Command, Stdio},
        sync::mpsc,
        time::{Duration, Instant},
    };

    // The child runs no service main, database code, HTTP request or email code.
    #[test]
    fn signal_child() {
        let Ok(expected) = std::env::var("CLEANAPP_SHUTDOWN_FIXTURE") else {
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
            let reason = shutdown.wait().await.unwrap();
            assert_eq!(reason, expected.as_str());
            assert_eq!(shutdown.wait().await.unwrap(), reason);
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

    fn native_signal(signal: &str) {
        let executable = std::env::current_exe().unwrap();
        let mut child = OwnedChild(
            Command::new(executable)
                .args([
                    "--exact",
                    "shutdown::tests::signal_child",
                    "--nocapture",
                    "--test-threads=1",
                ])
                .env_clear()
                .env("PATH", "/usr/bin:/bin")
                .env("CLEANAPP_SHUTDOWN_FIXTURE", signal)
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
        // Signal only this freshly spawned native fixture, never the parent.
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
    }

    #[test]
    fn native_process_retains_sigterm_during_batch() {
        native_signal("SIGTERM");
    }

    #[test]
    fn native_process_retains_sigint_during_batch() {
        native_signal("SIGINT");
    }
}
