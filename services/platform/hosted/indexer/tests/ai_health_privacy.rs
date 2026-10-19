use std::error::Error;
use std::sync::Arc;

use layerx_indexer::ai_health::{
    escape_display_text, observation_digest, operator_diagnostics, public_metrics, public_view,
    routing_readiness, success_ppm, summarize, Admission, AdmittedObserver, CanonicalMembership,
    CheckStatus, DiagnosticsGrant, ErrorCategory, HealthError, HealthObservation, HealthProjection,
    PublicMetrics, ReadinessLevel, Sample, SampleSummary, VerifiedObservation, HEALTH_TTL_MS,
    MAX_LATENCY_MS, MAX_PENDING_ALERTS, MAX_SIGNED_BYTES, MAX_WINDOW_SAMPLES,
};
use layerx_indexer::codec::hex;
use rustls::crypto::ring::sign::any_eddsa_type;
use rustls::pki_types::PrivatePkcs8KeyDer;
use rustls::sign::SigningKey;
use rustls::SignatureScheme;
use serde_json::{json, Value};

type TestResult = Result<(), Box<dyn Error>>;

const MARKET: [u8; 32] = [0x11; 32];
const WORKER: [u8; 32] = [0x22; 32];
const MODEL: [u8; 32] = [0x33; 32];
const DEPLOYMENT: [u8; 32] = [0x44; 32];
const OBSERVER: [u8; 32] = [0x55; 32];
const SNAPSHOT: [u8; 32] = [0x66; 32];
const OBSERVED_AT: u64 = 1_700_000_000_000;
const PKCS8_ED25519_PREFIX: [u8; 16] = [
    0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x04, 0x22, 0x04, 0x20,
];

struct Observer {
    key: Arc<dyn SigningKey>,
    admitted: AdmittedObserver,
}

fn observer(seed: u8) -> Result<Observer, Box<dyn Error>> {
    let mut der = PKCS8_ED25519_PREFIX.to_vec();
    der.extend_from_slice(&[seed; 32]);
    let key = any_eddsa_type(&PrivatePkcs8KeyDer::from(der))?;
    let public_key = {
        let spki = key
            .public_key()
            .ok_or("ed25519 key exposes no public key")?;
        let (_, raw) = spki
            .as_ref()
            .split_last_chunk::<32>()
            .ok_or("short ed25519 public key")?;
        *raw
    };
    Ok(Observer {
        key,
        admitted: AdmittedObserver {
            market_id: MARKET,
            worker_id: WORKER,
            principal: OBSERVER,
            public_key,
        },
    })
}

fn sign_body(reporter: &Observer, body: &[u8]) -> Result<Vec<u8>, Box<dyn Error>> {
    let chosen = reporter
        .key
        .choose_scheme(&[SignatureScheme::ED25519])
        .ok_or("ed25519 scheme unavailable")?;
    let mut signed = body.to_vec();
    signed.extend_from_slice(&chosen.sign(&observation_digest(body))?);
    Ok(signed)
}

fn sign(reporter: &Observer, observation: &HealthObservation) -> Result<Vec<u8>, Box<dyn Error>> {
    sign_body(reporter, &observation.body())
}

fn window(latencies: std::ops::RangeInclusive<u32>, failed: u32) -> Vec<Sample> {
    latencies
        .map(|latency_ms| Sample {
            latency_ms,
            succeeded: latency_ms != failed,
        })
        .collect()
}

fn observation(sequence: u64, observed_at_ms: u64, summary: SampleSummary) -> HealthObservation {
    HealthObservation {
        market_id: MARKET,
        worker_id: WORKER,
        model_digest: MODEL,
        deployment_digest: DEPLOYMENT,
        observer_principal: OBSERVER,
        observer_sequence: sequence,
        observed_at_ms,
        expires_at_ms: observed_at_ms + HEALTH_TTL_MS,
        advertised: CheckStatus::Passed,
        transport: CheckStatus::NotChecked,
        runner: CheckStatus::NotChecked,
        sample_count: summary.sample_count,
        success_count: summary.success_count,
        latency_p50_ms: summary.latency_p50_ms,
        latency_p95_ms: summary.latency_p95_ms,
        error_category: ErrorCategory::None,
        signature: [0; 64],
    }
}

fn runner_claimed(observation: &HealthObservation) -> HealthObservation {
    HealthObservation {
        transport: CheckStatus::Passed,
        runner: CheckStatus::Passed,
        ..observation.clone()
    }
}

fn canonical(finalized_height: u64, worker_listed: bool) -> CanonicalMembership {
    CanonicalMembership {
        snapshot_id: SNAPSHOT,
        market_id: MARKET,
        worker_id: WORKER,
        finalized_height,
        worker_listed,
    }
}

fn verified(
    reporter: &Observer,
    observation: &HealthObservation,
) -> Result<VerifiedObservation, Box<dyn Error>> {
    Ok(VerifiedObservation::verify(
        &sign(reporter, observation)?,
        &reporter.admitted,
        observation.observed_at_ms,
    )?)
}

fn decode_error(signed: &[u8]) -> Option<HealthError> {
    HealthObservation::decode(signed).err()
}

#[test]
fn a10_percentiles_and_success_ratio() -> TestResult {
    let twenty = summarize(&window(1..=20, 20))?;
    assert_eq!(
        twenty,
        SampleSummary {
            sample_count: 20,
            success_count: 19,
            latency_p50_ms: Some(10),
            latency_p95_ms: Some(19),
        }
    );
    let mut reversed = window(1..=20, 20);
    reversed.reverse();
    assert_eq!(summarize(&reversed)?, twenty);
    assert_eq!(success_ppm(19, 20)?, Some(950_000));
    assert_eq!(success_ppm(1, 3)?, Some(333_333));
    assert_eq!(success_ppm(20, 20)?, Some(1_000_000));
    assert_eq!(success_ppm(u32::MAX, u32::MAX)?, Some(1_000_000));
    assert_eq!(success_ppm(0, 0)?, None);
    assert_eq!(success_ppm(21, 20), Err(HealthError::SuccessExceedsSamples));

    let single = summarize(&[Sample {
        latency_ms: 7,
        succeeded: false,
    }])?;
    assert_eq!(
        (single.latency_p50_ms, single.latency_p95_ms),
        (Some(7), Some(7))
    );
    assert_eq!(
        summarize(&[])?,
        SampleSummary {
            sample_count: 0,
            success_count: 0,
            latency_p50_ms: None,
            latency_p95_ms: None,
        }
    );
    let hundred = summarize(&window(1..=100, 0))?;
    assert_eq!(
        (hundred.latency_p50_ms, hundred.latency_p95_ms),
        (Some(50), Some(95))
    );
    let oversized = vec![
        Sample {
            latency_ms: 1,
            succeeded: true,
        };
        MAX_WINDOW_SAMPLES + 1
    ];
    assert_eq!(summarize(&oversized), Err(HealthError::CapacityExceeded));
    let slow = [Sample {
        latency_ms: MAX_LATENCY_MS + 1,
        succeeded: true,
    }];
    assert_eq!(summarize(&slow), Err(HealthError::InconsistentLatency));
    Ok(())
}

#[test]
fn a10_publication_threshold() -> TestResult {
    let reporter = observer(7)?;
    let mut projection = HealthProjection::open_in_memory()?;
    let fresh = canonical(100, true);

    let twenty = observation(1, OBSERVED_AT, summarize(&window(1..=20, 20))?);
    let signed = sign(&reporter, &twenty)?;
    assert_eq!(
        projection.admit(&signed, &reporter.admitted, OBSERVED_AT + 1)?,
        Admission::Stored
    );
    assert_eq!(
        public_metrics(&twenty)?,
        PublicMetrics::Published {
            sample_count: 20,
            success_ppm: 950_000,
            latency_p50_ms: 10,
            latency_p95_ms: 19,
        }
    );
    let stored = projection.latest(&MARKET, &WORKER)?.ok_or("missing row")?;
    assert_eq!(stored.signed_bytes(), signed.as_slice());
    let view = public_view(&stored, Some(&fresh), 100, None, OBSERVED_AT + 1)?;
    assert_eq!(
        view["operational"]["metrics"],
        json!({"status": "published", "sample_count": 20, "success_ppm": 950_000,
               "latency_p50_ms": 10, "latency_p95_ms": 19})
    );

    let nineteen = observation(2, OBSERVED_AT + 1, summarize(&window(1..=19, 0))?);
    let zero = observation(3, OBSERVED_AT + 2, summarize(&[])?);
    for withheld in [&nineteen, &zero] {
        assert_eq!(
            public_metrics(withheld)?,
            PublicMetrics::InsufficientSamples
        );
        let signed = sign(&reporter, withheld)?;
        assert_eq!(
            projection.admit(&signed, &reporter.admitted, OBSERVED_AT + 2)?,
            Admission::Stored
        );
        let view = public_view(
            &projection.latest(&MARKET, &WORKER)?.ok_or("missing row")?,
            Some(&fresh),
            100,
            None,
            OBSERVED_AT + 2,
        )?;
        assert_eq!(
            view["operational"]["metrics"],
            json!({"status": "insufficient-samples", "sample_count": null, "success_ppm": null,
                   "latency_p50_ms": null, "latency_p95_ms": null})
        );
        assert_eq!(view["operational"]["minimum_samples"], json!(20));
    }
    assert_eq!(success_ppm(zero.success_count, zero.sample_count)?, None);

    let labels = format!(
        "market=\"{}\",worker=\"{}\",model=\"{}\",error_category=\"none\"",
        hex(&MARKET),
        hex(&WORKER),
        hex(&MODEL)
    );
    let metrics = projection.metrics_text();
    for line in [
        format!("paxai_health_success_ppm{{{labels}}} 950000"),
        format!("paxai_health_latency_p95_ms_bucket{{{labels},le=\"10\"}} 0"),
        format!("paxai_health_latency_p95_ms_bucket{{{labels},le=\"25\"}} 1"),
        format!("paxai_health_latency_p95_ms_bucket{{{labels},le=\"+Inf\"}} 1"),
        format!("paxai_health_latency_p95_ms_sum{{{labels}}} 19"),
        format!("paxai_health_latency_p95_ms_count{{{labels}}} 1"),
        format!(
            "paxai_health_withheld_observations_total{{market=\"{}\"}} 2",
            hex(&MARKET)
        ),
    ] {
        assert!(metrics.lines().any(|candidate| candidate == line), "{line}");
    }
    assert!(projection.drain_alerts().is_empty());
    Ok(())
}

fn invalid_cases(base: &HealthObservation) -> Vec<(HealthObservation, HealthError)> {
    vec![
        (
            HealthObservation {
                success_count: 21,
                ..base.clone()
            },
            HealthError::SuccessExceedsSamples,
        ),
        (
            HealthObservation {
                expires_at_ms: OBSERVED_AT + HEALTH_TTL_MS + 1,
                ..base.clone()
            },
            HealthError::InvalidExpiry,
        ),
        (
            HealthObservation {
                expires_at_ms: OBSERVED_AT,
                ..base.clone()
            },
            HealthError::InvalidExpiry,
        ),
        (
            HealthObservation {
                observed_at_ms: u64::MAX - 10,
                expires_at_ms: u64::MAX,
                ..base.clone()
            },
            HealthError::Overflow,
        ),
        (
            HealthObservation {
                latency_p50_ms: Some(20),
                ..base.clone()
            },
            HealthError::InconsistentLatency,
        ),
        (
            HealthObservation {
                latency_p95_ms: None,
                ..base.clone()
            },
            HealthError::InconsistentLatency,
        ),
        (
            HealthObservation {
                latency_p50_ms: Some(MAX_LATENCY_MS + 1),
                latency_p95_ms: Some(MAX_LATENCY_MS + 1),
                ..base.clone()
            },
            HealthError::InconsistentLatency,
        ),
        (
            HealthObservation {
                worker_id: [0; 32],
                ..base.clone()
            },
            HealthError::NonCanonical,
        ),
        (
            HealthObservation {
                observer_sequence: 0,
                ..base.clone()
            },
            HealthError::NonCanonical,
        ),
        (
            HealthObservation {
                advertised: CheckStatus::Failed,
                transport: CheckStatus::Passed,
                ..base.clone()
            },
            HealthError::NonCanonical,
        ),
    ]
}

#[test]
fn a10_invalid_observations_refuse() -> TestResult {
    let reporter = observer(7)?;
    let base = observation(1, OBSERVED_AT, summarize(&window(1..=20, 20))?);
    let valid = sign(&reporter, &base)?;
    assert_eq!(
        HealthObservation::decode(&valid)?.signing_digest(),
        base.signing_digest()
    );

    let cases = invalid_cases(&base);
    let mut projection = HealthProjection::open_in_memory()?;
    for (invalid, expected) in cases {
        assert_eq!(invalid.validate(), Err(expected.clone()));
        let signed = sign(&reporter, &invalid)?;
        assert_eq!(decode_error(&signed), Some(expected.clone()));
        assert_eq!(
            projection.admit(&signed, &reporter.admitted, OBSERVED_AT + 1),
            Err(expected)
        );
    }

    let mut unknown_category = base.body();
    if let Some(last) = unknown_category.last_mut() {
        *last = 9;
    }
    assert_eq!(
        decode_error(&sign_body(&reporter, &unknown_category)?),
        Some(HealthError::NonCanonical)
    );
    let mut next_version = base.body();
    next_version[..2].copy_from_slice(&2_u16.to_be_bytes());
    assert_eq!(
        decode_error(&sign_body(&reporter, &next_version)?),
        Some(HealthError::UnsupportedVersion)
    );
    let mut trailing = valid.clone();
    trailing.splice(trailing.len() - 64..trailing.len() - 64, [0_u8; 4]);
    assert_eq!(decode_error(&trailing), Some(HealthError::NonCanonical));
    assert_eq!(decode_error(&[0_u8; 16]), Some(HealthError::NonCanonical));
    assert_eq!(
        decode_error(&vec![0_u8; MAX_SIGNED_BYTES + 1]),
        Some(HealthError::NonCanonical)
    );

    let mut forged = valid.clone();
    if let Some(last) = forged.last_mut() {
        *last ^= 1;
    }
    let other_key = observer(8)?;
    let mismatched = AdmittedObserver {
        worker_id: [0x23; 32],
        ..reporter.admitted.clone()
    };
    for (signed, admitted, expected) in [
        (&forged, &reporter.admitted, HealthError::BadSignature),
        (&valid, &other_key.admitted, HealthError::BadSignature),
        (&valid, &mismatched, HealthError::ObserverMismatch),
    ] {
        assert_eq!(
            VerifiedObservation::verify(signed, admitted, OBSERVED_AT + 1),
            Err(expected)
        );
    }
    assert_eq!(projection.latest(&MARKET, &WORKER)?, None);
    assert_eq!(projection.drain_alerts().len(), 10);
    Ok(())
}

#[test]
fn a10_store_replay_and_restart() -> TestResult {
    let reporter = observer(7)?;
    let path =
        std::env::temp_dir().join(format!("ai_health_privacy_{}.sqlite", std::process::id()));
    if path.exists() {
        std::fs::remove_file(&path)?;
    }
    let summary = summarize(&window(1..=20, 20))?;
    let second = sign(&reporter, &observation(2, OBSERVED_AT, summary))?;
    let now = OBSERVED_AT + 10;
    {
        let mut projection = HealthProjection::open(&path)?;
        assert_eq!(
            projection.admit(&second, &reporter.admitted, now)?,
            Admission::Stored
        );
        assert_eq!(
            projection.admit(&second, &reporter.admitted, now)?,
            Admission::AlreadyApplied
        );
        let conflicting = observation(
            2,
            OBSERVED_AT,
            SampleSummary {
                success_count: 18,
                ..summary
            },
        );
        let refusals = [
            (
                sign(&reporter, &conflicting)?,
                now,
                HealthError::ReplayConflict,
            ),
            (
                sign(&reporter, &observation(1, OBSERVED_AT, summary))?,
                now,
                HealthError::SequenceConsumed,
            ),
            (
                sign(&reporter, &observation(3, OBSERVED_AT - 1, summary))?,
                now,
                HealthError::TimestampReversal,
            ),
            (
                sign(&reporter, &observation(4, OBSERVED_AT + 20, summary))?,
                now,
                HealthError::TimestampReversal,
            ),
        ];
        for (signed, at, expected) in &refusals {
            assert_eq!(
                projection.admit(signed, &reporter.admitted, *at),
                Err(expected.clone())
            );
        }
        let stored = projection.latest(&MARKET, &WORKER)?.ok_or("missing row")?;
        assert_eq!(stored.signed_bytes(), second.as_slice());
        let codes: Vec<_> = projection
            .drain_alerts()
            .iter()
            .map(|alert| alert.code)
            .collect();
        assert_eq!(
            codes,
            [
                "replay_conflict",
                "sequence_consumed",
                "timestamp_reversal",
                "timestamp_reversal"
            ]
        );
    }
    let mut reopened = HealthProjection::open(&path)?;
    let stored = reopened.latest(&MARKET, &WORKER)?.ok_or("missing row")?;
    assert_eq!(stored.signed_bytes(), second.as_slice());
    assert_eq!(stored.observation().observer_sequence, 2);
    assert_eq!(
        reopened.admit(&second, &reporter.admitted, now)?,
        Admission::AlreadyApplied
    );
    let first = sign(&reporter, &observation(1, OBSERVED_AT, summary))?;
    assert_eq!(
        reopened.admit(&first, &reporter.admitted, now),
        Err(HealthError::SequenceConsumed)
    );
    let third = sign(&reporter, &observation(3, OBSERVED_AT + 5, summary))?;
    assert_eq!(
        reopened.admit(&third, &reporter.admitted, now)?,
        Admission::Stored
    );
    drop(reopened);
    std::fs::remove_file(&path)?;
    Ok(())
}

#[test]
fn a10_expiry_at_equality() -> TestResult {
    let reporter = observer(7)?;
    let membership = canonical(100, true);
    let advertised = observation(1, OBSERVED_AT, summarize(&window(1..=20, 20))?);
    let claimed = runner_claimed(&advertised);
    let advertised_verified = verified(&reporter, &advertised)?;
    let claimed_verified = verified(&reporter, &claimed)?;
    assert_eq!(advertised.readiness(), ReadinessLevel::Advertised);

    let last_live = OBSERVED_AT + HEALTH_TTL_MS - 1;
    let expiry = OBSERVED_AT + HEALTH_TTL_MS;
    assert_eq!(
        routing_readiness(&advertised_verified, Some(&membership), 100, last_live),
        Err(HealthError::NotReady)
    );
    for observation in [&advertised_verified, &claimed_verified] {
        assert_eq!(
            routing_readiness(observation, Some(&membership), 100, expiry),
            Err(HealthError::Expired)
        );
        assert_eq!(
            routing_readiness(observation, Some(&membership), 100, OBSERVED_AT - 1),
            Err(HealthError::TimestampReversal)
        );
        let view = public_view(observation, Some(&membership), 100, None, expiry)?;
        assert_eq!(view["operational"]["readiness"], json!("expired"));
        assert_eq!(view["operational"]["age_ms"], json!("30000"));
        assert_eq!(view["canonical"]["status"], json!("finalized"));
    }
    let live_view = public_view(
        &advertised_verified,
        Some(&membership),
        100,
        None,
        last_live,
    )?;
    assert_eq!(live_view["operational"]["readiness"], json!("advertised"));
    assert_eq!(
        live_view["operational"]["expires_at_ms"],
        json!(expiry.to_string())
    );

    let claimed_signed = sign(&reporter, &claimed)?;
    assert_eq!(
        VerifiedObservation::verify(&claimed_signed, &reporter.admitted, expiry),
        Err(HealthError::Expired)
    );
    let mut projection = HealthProjection::open_in_memory()?;
    assert_eq!(
        projection.admit(&claimed_signed, &reporter.admitted, expiry),
        Err(HealthError::Expired)
    );
    assert_eq!(projection.latest(&MARKET, &WORKER)?, None);
    assert_eq!(
        projection.admit(&claimed_signed, &reporter.admitted, last_live)?,
        Admission::Stored
    );

    for category in [
        ErrorCategory::MarketPaused,
        ErrorCategory::StaleAuthority,
        ErrorCategory::StoreUnavailable,
        ErrorCategory::TlsPinMismatch,
        ErrorCategory::ModelUnavailable,
        ErrorCategory::QueueSaturated,
    ] {
        let forced = HealthObservation {
            observer_sequence: 2,
            error_category: category,
            ..claimed.clone()
        };
        assert_eq!(forced.readiness(), ReadinessLevel::NotReady);
        assert_eq!(
            routing_readiness(
                &verified(&reporter, &forced)?,
                Some(&membership),
                100,
                OBSERVED_AT
            ),
            Err(HealthError::NotReady)
        );
    }
    Ok(())
}

#[test]
fn a14_stale_authority() -> TestResult {
    let reporter = observer(7)?;
    let advertised = observation(1, OBSERVED_AT, summarize(&window(1..=20, 20))?);
    let claimed = verified(&reporter, &runner_claimed(&advertised))?;
    let advertised = verified(&reporter, &advertised)?;
    let membership = canonical(100, true);

    assert_eq!(
        routing_readiness(&claimed, Some(&membership), 109, OBSERVED_AT),
        Err(HealthError::StaleAuthority)
    );
    assert_eq!(
        routing_readiness(&advertised, Some(&membership), 108, OBSERVED_AT),
        Err(HealthError::NotReady)
    );
    let stale = public_view(&claimed, Some(&membership), 109, None, OBSERVED_AT)?;
    assert_eq!(
        stale["canonical"],
        json!({
            "status": "finalized",
            "snapshot_id": hex(&SNAPSHOT),
            "finalized_height": "100",
            "worker_listed": true,
            "authority": {"status": "stale", "lag_heights": "9", "limit_heights": "8"},
        })
    );
    assert_eq!(stale["operational"]["authoritative"], json!(false));
    assert_eq!(
        stale["operational"]["kind"],
        json!("signed-health-observation")
    );
    let fresh = public_view(&claimed, Some(&membership), 108, None, OBSERVED_AT)?;
    assert_eq!(fresh["canonical"]["authority"]["status"], json!("fresh"));
    assert_eq!(fresh["canonical"]["authority"]["lag_heights"], json!("8"));

    assert_eq!(
        routing_readiness(&claimed, None, 109, OBSERVED_AT),
        Err(HealthError::FinalityUnavailable)
    );
    let unfinalized = public_view(&claimed, None, 109, None, OBSERVED_AT)?;
    assert_eq!(
        unfinalized["canonical"],
        json!({"status": "unavailable", "snapshot_id": null, "finalized_height": null,
               "worker_listed": null, "authority": null})
    );
    assert_eq!(unfinalized["operational"]["authoritative"], json!(false));

    assert_eq!(
        routing_readiness(&claimed, Some(&membership), 99, OBSERVED_AT),
        Err(HealthError::AuthorityRegression)
    );
    assert_eq!(
        public_view(&claimed, Some(&membership), 99, None, OBSERVED_AT),
        Err(HealthError::AuthorityRegression)
    );
    assert_eq!(
        routing_readiness(&claimed, Some(&canonical(100, false)), 100, OBSERVED_AT),
        Err(HealthError::NotMember)
    );
    let elsewhere = CanonicalMembership {
        worker_id: [0x23; 32],
        ..membership
    };
    assert_eq!(
        routing_readiness(&claimed, Some(&elsewhere), 100, OBSERVED_AT),
        Err(HealthError::BindingMismatch)
    );
    assert_eq!(
        public_view(&claimed, Some(&elsewhere), 100, None, OBSERVED_AT),
        Err(HealthError::BindingMismatch)
    );
    Ok(())
}

fn label_keys(metrics: &str) -> Vec<String> {
    metrics
        .lines()
        .filter(|line| !line.starts_with('#'))
        .filter_map(|line| line.split_once('{'))
        .filter_map(|(_, rest)| rest.split_once('}'))
        .flat_map(|(labels, _)| labels.split(','))
        .filter_map(|pair| pair.split_once('='))
        .map(|(key, _)| key.to_owned())
        .collect()
}

#[test]
fn a11_private_metadata_stays_private() -> TestResult {
    let manifest: Value = json!({
        "display_name": "<img src=x onerror=alert(1)>&'\"",
        "private_locator": "private-upstream-locator-7f3a",
        "prompt": "confidential prompt body",
        "authorization": "Bearer test-credential-0001",
        "output": "decrypted model output",
    });
    let secrets = [
        "private-upstream-locator-7f3a",
        "confidential prompt body",
        "test-credential-0001",
        "decrypted model output",
    ];
    let reporter = observer(7)?;
    let base = observation(1, OBSERVED_AT, summarize(&window(1..=19, 0))?);
    let signed = sign(&reporter, &base)?;
    let mut projection = HealthProjection::open_in_memory()?;
    projection.admit(&signed, &reporter.admitted, OBSERVED_AT)?;
    let stored = projection.latest(&MARKET, &WORKER)?.ok_or("missing row")?;

    let name = manifest["display_name"].as_str().ok_or("missing name")?;
    let view = public_view(
        &stored,
        Some(&canonical(100, true)),
        100,
        Some(name),
        OBSERVED_AT,
    )?;
    assert_eq!(
        view["display_name"],
        json!("&lt;img src=x onerror=alert(1)&gt;&amp;&#39;&quot;")
    );
    let rendered = view.to_string();
    let signature_hex = hex(&stored.observation().signature);
    for forbidden in secrets.iter().copied().chain([
        "<img",
        "signature",
        "success_count",
        signature_hex.as_str(),
    ]) {
        assert!(!rendered.contains(forbidden), "{forbidden}");
    }
    assert_eq!(escape_display_text("a\u{7}b")?, "a\u{fffd}b");
    assert_eq!(
        escape_display_text(&"x".repeat(129)),
        Err(HealthError::DisplayNameTooLong)
    );
    assert_eq!(escape_display_text(&"x".repeat(128))?.len(), 128);

    let grant = DiagnosticsGrant {
        market_id: MARKET,
        generation: 3,
    };
    let diagnostics = operator_diagnostics(&stored, &grant, Some(3))?;
    assert_eq!(diagnostics["sample_count"], json!(19));
    assert_eq!(diagnostics["success_ppm"], json!(1_000_000));
    assert!(!diagnostics.to_string().contains(&signature_hex));
    let foreign = DiagnosticsGrant {
        market_id: [0x12; 32],
        generation: 3,
    };
    for (held, current) in [(&grant, None), (&grant, Some(4)), (&foreign, Some(3))] {
        assert_eq!(
            operator_diagnostics(&stored, held, current),
            Err(HealthError::AccessDenied)
        );
    }

    let mut forged = signed.clone();
    if let Some(last) = forged.last_mut() {
        *last ^= 1;
    }
    for _ in 0..MAX_PENDING_ALERTS + 6 {
        assert_eq!(
            projection.admit(&forged, &reporter.admitted, OBSERVED_AT),
            Err(HealthError::BadSignature)
        );
    }
    let alerts = projection.drain_alerts();
    assert_eq!(alerts.len(), MAX_PENDING_ALERTS);
    assert_eq!(projection.suppressed_alerts(), 6);
    let expected_alert = format!(
        "paxai_health_alert market={} worker={} category=bad_signature",
        hex(&MARKET),
        hex(&WORKER)
    );
    let metrics = projection.metrics_text();
    let mut exported: Vec<String> = alerts.iter().map(ToString::to_string).collect();
    assert!(exported.iter().all(|line| *line == expected_alert));
    exported.push(metrics.clone());
    exported.push(HealthError::BadSignature.to_string());
    for line in &exported {
        for forbidden in secrets.iter().copied().chain([signature_hex.as_str()]) {
            assert!(!line.contains(forbidden), "{forbidden}");
        }
    }
    let keys = label_keys(&metrics);
    assert!(!keys.is_empty());
    assert!(keys.iter().all(|key| {
        ["market", "worker", "model", "error_category", "le"].contains(&key.as_str())
    }));
    Ok(())
}
