//! Proposed authenticated AI market views: `GET /v1/ai/markets/{market}/snapshot`,
//! `/participants` and `/epochs` over the indexer's verified finalized projection.
//!
//! Every request re-authenticates the presented gateway key against the durable key store, so
//! a revoked or disabled key loses access on its next request even while it holds a cursor.
//! Rate admission happens before any projection read. Responses carry the exact snapshot
//! binding, its achieved evidence rank and observed age, and typed component statuses.

use layerx_indexer::ai_market::{
    parse_epoch_query, parse_snapshot_query, Availability, CursorKeyring, EpochStatus, Freshness,
    PageRequest, ParticipantKind, ParticipantRow, Presence, ProjectionState, ProjectionStore,
    QueryLimiter, ScoreStatus, ViewError, Viewer,
};
use layerx_indexer::codec::hex0x;
use serde_json::{json, Value};

use crate::http::{split_target, OutgoingResponse};
use crate::store::RedisStore;
use crate::{authenticate_gateway_key, AccessError};

const PREFIX: &str = "/v1/ai/markets/";

/// Indexer projection, cursor keys and per-principal limiter served by the gateway.
pub struct AiViews {
    pub store: ProjectionStore,
    pub cursor_keys: CursorKeyring,
    pub limiter: QueryLimiter,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum View {
    Snapshot,
    Participants,
    Epochs,
}

fn route(path: &str) -> Option<(&str, View)> {
    let (market, view) = path.strip_prefix(PREFIX)?.split_once('/')?;
    let view = match view {
        "snapshot" => View::Snapshot,
        "participants" => View::Participants,
        "epochs" => View::Epochs,
        _ => return None,
    };
    Some((market, view))
}

/// Answers an AI market view request, or `None` when `target` is not an AI view path.
/// `verified_authority_height` is the independently verified F02 authority height used to
/// label view freshness; `None` labels it unknown.
#[must_use]
pub fn respond(
    views: &AiViews,
    keys: &RedisStore,
    method: &str,
    target: &str,
    authorization: Option<&str>,
    verified_authority_height: Option<u64>,
    now_ms: u64,
) -> Option<OutgoingResponse> {
    let Ok((path, query)) = split_target(target) else {
        return route(target).map(|_| error(&ViewError::InvalidEncoding));
    };
    let (market, view) = route(path)?;
    if method != "GET" {
        return Some(response(
            405,
            &json!({"ok": false, "error": {"code": "method_not_allowed"}}),
        ));
    }
    Some(
        serve(
            views,
            keys,
            (view, market, query),
            authorization,
            verified_authority_height,
            now_ms,
        )
        .unwrap_or_else(|failure| failure),
    )
}

fn serve(
    views: &AiViews,
    keys: &RedisStore,
    (view, market, query): (View, &str, Option<&str>),
    authorization: Option<&str>,
    verified_authority_height: Option<u64>,
    now_ms: u64,
) -> Result<OutgoingResponse, OutgoingResponse> {
    let record =
        authenticate_gateway_key(keys, authorization.unwrap_or_default()).map_err(|failure| {
            match failure {
                AccessError::Unauthenticated => response(
                    401,
                    &json!({"ok": false, "error": {"code": "api_key_required"}}),
                ),
                AccessError::PersistenceUnavailable => response(
                    503,
                    &json!({"ok": false, "error": {"code": "persistence_unavailable"}}),
                ),
            }
        })?;
    let viewer = Viewer::for_principal(&record.principal_digest).map_err(|e| error(&e))?;
    let _permit = views
        .limiter
        .admit(&viewer, now_ms)
        .map_err(|e| error(&e))?;
    let result = match view {
        View::Snapshot => parse_snapshot_query(market, query).and_then(|(market, selector)| {
            views
                .store
                .market_view(
                    &viewer,
                    &market,
                    selector.as_ref(),
                    verified_authority_height,
                )
                .map(|v| {
                    json!({
                        "snapshot_id": hex0x(v.snapshot_id.as_bytes()),
                        "projection": projection(v.projection),
                        "binding": binding(&v.binding),
                        "components": components(&v.availability),
                        "source_activity": hex0x(v.source_activity.as_bytes()),
                        "freshness": freshness(v.freshness),
                    })
                })
        }),
        View::Participants => PageRequest::parse(market, query).and_then(|request| {
            views
                .store
                .participants(&viewer, &views.cursor_keys, &request, now_ms)
                .map(|page| {
                    json!({
                        "snapshot_id": hex0x(page.snapshot_id.as_bytes()),
                        "binding": binding(&page.binding),
                        "components": components(&page.availability),
                        "rows": page.rows.iter().map(row).collect::<Vec<_>>(),
                        "cursor": page.cursor,
                    })
                })
        }),
        View::Epochs => parse_epoch_query(market, query).and_then(|(market, from, limit)| {
            views.store.epochs(&market, from, limit).map(|page| {
                json!({
                    "component": availability(page.component),
                    "entries": page.entries.iter().map(|e| json!({
                        "epoch": e.epoch.to_string(),
                        "status": epoch_status(e.status),
                        "snapshot_id": e.snapshot_id.map(|id| hex0x(id.as_bytes())),
                    })).collect::<Vec<_>>(),
                })
            })
        }),
    };
    result
        .map(|value| response(200, &json!({"ok": true, "result": value})))
        .map_err(|e| error(&e))
}

fn response(status: u16, value: &Value) -> OutgoingResponse {
    OutgoingResponse {
        content_type: "application/json".to_owned(),
        headers: Vec::new(),
        status,
        body: value.to_string().into_bytes(),
        retry_after: None,
    }
}

/// HTTP status and body for a view failure: the stable category, plus the oldest retained
/// position for a pruned snapshot and the lag for stale authority. Nothing else is revealed.
fn error(failure: &ViewError) -> OutgoingResponse {
    let status = match failure {
        ViewError::InvalidEncoding | ViewError::WrongDomain | ViewError::CursorMismatch => 400,
        ViewError::AccessRefused => 404,
        ViewError::FinalityUnavailable | ViewError::RollbackRefused => 409,
        ViewError::SnapshotPruned { .. } | ViewError::CursorExpired => 410,
        ViewError::ResponseTooLarge => 413,
        ViewError::UnsupportedVersion => 422,
        ViewError::RateLimited => 429,
        ViewError::IntegrityFailure | ViewError::BindingMismatch | ViewError::SnapshotConflict => {
            500
        }
        ViewError::ProjectionUnavailable
        | ViewError::CapacityExceeded
        | ViewError::EndpointDisabled
        | ViewError::Quarantined
        | ViewError::AuthorityStale { .. }
        | ViewError::Store(_) => 503,
    };
    let mut detail = json!({ "code": failure.category() });
    match failure {
        ViewError::SnapshotPruned { oldest } => {
            detail["oldest"] = oldest.map_or(Value::Null, |(id, sequence)| {
                json!({"snapshot_id": hex0x(id.as_bytes()), "observed_sequence": sequence.to_string()})
            });
        }
        ViewError::AuthorityStale { lag } => detail["lag"] = json!(lag.to_string()),
        _ => {}
    }
    let mut out = response(status, &json!({"ok": false, "error": detail}));
    if *failure == ViewError::RateLimited {
        out.retry_after = Some(1);
    }
    out
}

fn present<T>(value: Presence<T>, render: impl FnOnce(T) -> Value) -> Value {
    match value {
        Presence::Present(v) => render(v),
        Presence::Absent => Value::Null,
    }
}

fn binding(b: &layerx_indexer::ai_market::SnapshotBinding) -> Value {
    json!({
        "chain": hex0x(b.chain.as_bytes()),
        "program": hex0x(b.program.as_bytes()),
        "market": hex0x(b.market.as_bytes()),
        "observed_sequence": b.observed_sequence.to_string(),
        "execution_height": b.execution_height.to_string(),
        "batch_id": hex0x(b.batch_id.as_bytes()),
        "native_state_root": hex0x(b.native_state_root.as_bytes()),
        "revision": b.revision.to_string(),
        "state_digest": hex0x(b.state_digest.as_bytes()),
        "epoch": present(b.epoch, |e| json!(e.to_string())),
        "config": b.config.get().to_string(),
        "policy": hex0x(b.policy.as_bytes()),
        "roster": present(b.roster, |r| json!(hex0x(r.as_bytes()))),
        "checkpoint": hex0x(b.checkpoint.as_bytes()),
        "settlement": present(b.settlement, |s| json!(hex0x(s.as_bytes()))),
        "rank": b.rank,
        "publication_time_ms": b.publication_time_ms.to_string(),
    })
}

fn row(r: &ParticipantRow) -> Value {
    json!({
        "kind": match r.kind {
            ParticipantKind::Worker => "worker",
            ParticipantKind::Evaluator => "evaluator",
        },
        "id": hex0x(&r.id),
        "owner": hex0x(r.owner.as_bytes()),
        "generation": r.generation.to_string(),
        "identity_state": r.identity_state,
        "frozen_member": r.frozen_member,
        "frozen_generation": present(r.frozen_generation, |g| json!(g.to_string())),
        "eligibility": r.eligibility,
        "metadata": present(r.metadata, |m| json!(hex0x(m.as_bytes()))),
        "metadata_revision": r.metadata_revision.to_string(),
        "score": {
            "status": score_status(r.score.status()),
            "epoch": present(r.score.epoch(), |e| json!(e.to_string())),
            "ppm": present(r.score.ppm(), |p| json!(p)),
        },
        "reward": {
            "status": availability(r.reward.status()),
            "asset": present(r.reward.asset(), |a| json!(hex0x(a.as_bytes()))),
            "earned": present(r.reward.earned(), |v| json!(v.to_string())),
            "claimed": present(r.reward.claimed(), |v| json!(v.to_string())),
        },
        "history": {
            "status": availability(r.history_status),
            "digest": present(r.history, |h| json!(hex0x(h.as_bytes()))),
        },
    })
}

const FEATURE_NAMES: [&str; 10] = [
    "F01", "F02", "F03", "F04", "F05", "F06", "F07", "F08", "F09", "F10",
];

fn components(statuses: &[Availability; 10]) -> Value {
    Value::Object(
        FEATURE_NAMES
            .iter()
            .zip(statuses)
            .map(|(name, status)| ((*name).to_owned(), json!(availability(*status))))
            .collect(),
    )
}

const fn availability(status: Availability) -> &'static str {
    match status {
        Availability::Available => "available",
        Availability::NotEnabled => "not-enabled",
        Availability::NotYetProduced => "not-yet-produced",
        Availability::ContentUnavailable => "content-unavailable",
        Availability::UnsupportedVersion => "unsupported-version",
    }
}

const fn score_status(status: ScoreStatus) -> &'static str {
    match status {
        ScoreStatus::Present => "present",
        ScoreStatus::NoAdmissibleScore => "no-admissible-score",
        ScoreStatus::InsufficientCoverage => "insufficient-coverage",
        ScoreStatus::NotProduced => "not-produced",
        ScoreStatus::Unavailable => "unavailable",
        ScoreStatus::Unsupported => "unsupported",
    }
}

const fn projection(state: ProjectionState) -> &'static str {
    match state {
        ProjectionState::ObservedUnverified => "observed-unverified",
        ProjectionState::EvidenceVerifiedUnfinalized => "evidence-verified-unfinalized",
        ProjectionState::FinalizedPublishable => "finalized-publishable",
        ProjectionState::Archived => "archived",
        ProjectionState::Quarantined => "quarantined",
    }
}

const fn epoch_status(status: EpochStatus) -> &'static str {
    match status {
        EpochStatus::Retained => "retained",
        EpochStatus::NeverOpened => "never-opened",
        EpochStatus::RetainedTerminal => "retained-terminal",
        EpochStatus::ArchiveRequired => "archive-required",
        EpochStatus::ArchiveUnavailable => "archive-unavailable",
        EpochStatus::UnsupportedVersion => "unsupported-version",
    }
}

fn freshness(value: Freshness) -> Value {
    match value {
        Freshness::Current => json!({"label": "current"}),
        Freshness::Stale { lag } => json!({"label": "stale", "lag": lag.to_string()}),
        Freshness::Unknown => json!({"label": "unknown"}),
    }
}
