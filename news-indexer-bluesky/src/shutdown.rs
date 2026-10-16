use std::{future::Future, io, time::Duration};

/// Signal listeners stay alive while startup and the current work unit finish.
/// Only idle waits may be cancelled; callers must await active work separately.
pub struct Shutdown {
    #[cfg(unix)]
    interrupt: tokio::signal::unix::Signal,
    #[cfg(unix)]
    terminate: tokio::signal::unix::Signal,
    reason: Option<&'static str>,
}

pub enum Wait<T> {
    Ready(T),
    Stopped(&'static str),
}

impl Shutdown {
    pub fn new() -> io::Result<Self> {
        Ok(Self {
            #[cfg(unix)]
            interrupt: tokio::signal::unix::signal(tokio::signal::unix::SignalKind::interrupt())?,
            #[cfg(unix)]
            terminate: tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())?,
            reason: None,
        })
    }

    async fn received(&mut self) -> io::Result<&'static str> {
        #[cfg(unix)]
        {
            let (value, reason) = tokio::select! {
                value = self.interrupt.recv() => (value, "SIGINT"),
                value = self.terminate.recv() => (value, "SIGTERM"),
            };
            value.ok_or_else(|| io::Error::other("shutdown signal stream closed"))?;
            Ok(reason)
        }
        #[cfg(not(unix))]
        {
            tokio::signal::ctrl_c().await?;
            Ok("Ctrl-C")
        }
    }

    /// Use only for cancellation-safe idle/connect/read waits, never a batch or
    /// a message whose network/database receipts are still being completed.
    pub async fn wait_for<F: Future>(&mut self, idle: F) -> io::Result<Wait<F::Output>> {
        if let Some(reason) = self.reason {
            return Ok(Wait::Stopped(reason));
        }
        let result = tokio::select! {
            biased;
            reason = self.received() => Wait::Stopped(reason?),
            value = idle => Wait::Ready(value),
        };
        if let Wait::Stopped(reason) = result {
            self.reason = Some(reason);
        }
        Ok(result)
    }

    /// Preserve the immediate first batch and the delay after each completed
    /// batch. A pending signal wins over even a zero-delay next iteration.
    pub async fn run_batches<F, Work>(
        &mut self,
        interval: Duration,
        mut batch: F,
    ) -> io::Result<&'static str>
    where
        F: FnMut() -> Work,
        Work: Future<Output = ()>,
    {
        let mut delay = Duration::ZERO;
        loop {
            match self.wait_for(tokio::time::sleep(delay)).await? {
                Wait::Stopped(reason) => return Ok(reason),
                Wait::Ready(()) => {}
            }
            // Do not race this future against shutdown: it owns the current
            // network work and its corresponding database receipts.
            batch().await;
            delay = interval;
        }
    }
}
