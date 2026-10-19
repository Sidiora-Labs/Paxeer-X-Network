//! Health listener of the sidecar: `/healthz`, `/readyz` within three
//! cadences of the last accepted submission, and `/metrics` in the
//! Prometheus text format.

use std::collections::BTreeMap;
use std::fmt::Write as _;
use std::io::{Read, Write};
use std::net::TcpListener;
use std::sync::{Arc, Mutex, MutexGuard, PoisonError};
use std::thread;
use std::time::Duration;

use layerx_oracle_feeder::{health_status, ready};

use crate::now_ms;
use crate::round::Outcome;

/// Counters and gauges of the sidecar, shared by the round and the listener.
#[derive(Debug, Default)]
pub struct Metrics {
    rounds_total: u64,
    submissions: BTreeMap<&'static str, u64>,
    fetch_errors: BTreeMap<(String, &'static str), u64>,
    observed_at_ms: BTreeMap<String, u64>,
    last_accepted_height: u64,
    last_accepted_ms: u64,
    refused: bool,
}

/// Locks the shared metrics; a panicked holder leaves plain counters behind.
pub fn lock(metrics: &Mutex<Metrics>) -> MutexGuard<'_, Metrics> {
    metrics.lock().unwrap_or_else(PoisonError::into_inner)
}

impl Metrics {
    pub fn round(&mut self) {
        self.rounds_total += 1;
    }

    pub fn fetch_error(&mut self, market: &str, kind: &'static str) {
        *self
            .fetch_errors
            .entry((market.to_owned(), kind))
            .or_default() += 1;
    }

    pub fn observed(&mut self, market: &str, observed_at_ms: u64) {
        self.observed_at_ms
            .insert(market.to_owned(), observed_at_ms);
    }

    /// Counts one submission attempt. An accepted one at `target_height`
    /// restarts the readiness window; a refused feed key or validator turns
    /// the sidecar not ready until the next accepted one.
    pub fn submission(&mut self, outcome: &Outcome, target_height: u64, now_ms: u64) {
        *self.submissions.entry(outcome.label()).or_default() += 1;
        match outcome {
            Outcome::Accepted { .. } => {
                self.last_accepted_height = target_height;
                self.last_accepted_ms = now_ms;
                self.refused = false;
            }
            Outcome::NotValidator | Outcome::UnregisteredFeeder => self.refused = true,
            _ => {}
        }
    }

    #[must_use]
    pub fn is_ready(&self, now_ms: u64, cadence_ms: u64) -> bool {
        !self.refused && ready(self.last_accepted_ms, now_ms, cadence_ms)
    }

    /// The `/metrics` body; market ages are taken at `now_ms`.
    #[must_use]
    pub fn metrics_text(&self, now_ms: u64) -> String {
        let mut out = String::new();
        let _ = writeln!(out, "# TYPE price_sidecar_rounds_total counter");
        let _ = writeln!(out, "price_sidecar_rounds_total {}", self.rounds_total);
        let _ = writeln!(out, "# TYPE price_sidecar_submissions_total counter");
        for (result, count) in &self.submissions {
            let _ = writeln!(
                out,
                "price_sidecar_submissions_total{{result=\"{result}\"}} {count}"
            );
        }
        let _ = writeln!(out, "# TYPE price_sidecar_fetch_errors_total counter");
        for ((market, kind), count) in &self.fetch_errors {
            let _ = writeln!(
                out,
                "price_sidecar_fetch_errors_total{{market=\"{market}\",kind=\"{kind}\"}} {count}"
            );
        }
        let _ = writeln!(out, "# TYPE price_sidecar_market_age_ms gauge");
        for (market, observed_at) in &self.observed_at_ms {
            let _ = writeln!(
                out,
                "price_sidecar_market_age_ms{{market=\"{market}\"}} {}",
                now_ms.saturating_sub(*observed_at)
            );
        }
        let _ = writeln!(out, "# TYPE price_sidecar_last_accepted_height gauge");
        let _ = writeln!(
            out,
            "price_sidecar_last_accepted_height {}",
            self.last_accepted_height
        );
        out
    }
}

/// The full HTTP response to `request`.
#[must_use]
pub fn respond(request: &[u8], metrics: &Metrics, now_ms: u64, cadence_ms: u64) -> String {
    if request.starts_with(b"GET /metrics ") {
        let body = metrics.metrics_text(now_ms);
        format!(
            "HTTP/1.1 200 OK\r\nContent-Type: text/plain; version=0.0.4\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
            body.len()
        )
    } else {
        let status = health_status(request, metrics.is_ready(now_ms, cadence_ms));
        format!("HTTP/1.1 {status}\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
    }
}

/// Serves the health routes on `address` from a background thread.
///
/// # Errors
/// Refuses an address that cannot be bound.
pub fn serve(address: &str, metrics: Arc<Mutex<Metrics>>, cadence_ms: u64) -> Result<(), String> {
    let listener = TcpListener::bind(address).map_err(|e| format!("health bind {address}: {e}"))?;
    thread::spawn(move || {
        for mut stream in listener.incoming().flatten() {
            let mut request = [0u8; 1024];
            let _ = stream.set_read_timeout(Some(Duration::from_secs(2)));
            let read = stream.read(&mut request).unwrap_or(0);
            let response = respond(&request[..read], &lock(&metrics), now_ms(), cadence_ms);
            let _ = stream.write_all(response.as_bytes());
        }
    });
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use layerx_oracle_feeder::chainflow::FetchError;

    const ACCEPTED: u64 = 1_700_000_000_000;

    fn accepted() -> Outcome {
        Outcome::Accepted {
            hash: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08".to_owned(),
        }
    }

    #[test]
    fn readiness_window() {
        let mut metrics = Metrics::default();
        let readyz = b"GET /readyz HTTP/1.1\r\nHost: sidecar\r\n\r\n";
        let healthz = b"GET /healthz HTTP/1.1\r\nHost: sidecar\r\n\r\n";
        assert!(respond(readyz, &metrics, ACCEPTED, 1000)
            .starts_with("HTTP/1.1 503 Service Unavailable\r\n"));
        metrics.submission(&accepted(), 812, ACCEPTED);
        for (now, ready) in [
            (ACCEPTED + 3000, "HTTP/1.1 200 OK\r\n"),
            (ACCEPTED + 3001, "HTTP/1.1 503 Service Unavailable\r\n"),
        ] {
            assert_eq!(
                respond(readyz, &metrics, now, 1000),
                format!("{ready}Content-Length: 0\r\nConnection: close\r\n\r\n")
            );
            assert_eq!(
                respond(healthz, &metrics, now, 1000),
                "HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
            );
        }
        assert!(
            respond(b"GET /other HTTP/1.1\r\n\r\n", &metrics, ACCEPTED, 1000)
                .starts_with("HTTP/1.1 404 Not Found\r\n")
        );
        metrics.submission(&Outcome::UnregisteredFeeder, 813, ACCEPTED + 100);
        assert!(!metrics.is_ready(ACCEPTED + 200, 1000));
        metrics.submission(&accepted(), 814, ACCEPTED + 300);
        assert!(metrics.is_ready(ACCEPTED + 400, 1000));
    }

    #[test]
    fn metrics_text() {
        let mut metrics = Metrics::default();
        metrics.round();
        metrics.fetch_error("BTC", FetchError::Stale.kind());
        metrics.observed("ETH", ACCEPTED - 250);
        metrics.submission(&accepted(), 812, ACCEPTED);
        assert_eq!(
            metrics.metrics_text(ACCEPTED),
            "# TYPE price_sidecar_rounds_total counter\n\
             price_sidecar_rounds_total 1\n\
             # TYPE price_sidecar_submissions_total counter\n\
             price_sidecar_submissions_total{result=\"accepted\"} 1\n\
             # TYPE price_sidecar_fetch_errors_total counter\n\
             price_sidecar_fetch_errors_total{market=\"BTC\",kind=\"stale\"} 1\n\
             # TYPE price_sidecar_market_age_ms gauge\n\
             price_sidecar_market_age_ms{market=\"ETH\"} 250\n\
             # TYPE price_sidecar_last_accepted_height gauge\n\
             price_sidecar_last_accepted_height 812\n"
        );
        let response = respond(b"GET /metrics HTTP/1.1\r\n\r\n", &metrics, ACCEPTED, 1000);
        let body = metrics.metrics_text(ACCEPTED);
        assert_eq!(
            response,
            format!(
                "HTTP/1.1 200 OK\r\nContent-Type: text/plain; version=0.0.4\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            )
        );
    }
}
