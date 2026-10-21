use std::collections::{BTreeMap, BTreeSet};
use std::fmt::Debug;
use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::Path;

use layerx_human_service::journeys::{
    DepositJourney, ExitJourney, JourneyEngine, WithdrawalJourney,
};
use layerx_human_service::store::{
    PrincipalId, PrincipalScope, PrincipalStore, RetentionPeriod, RetentionPolicy, TenancyDigest,
};
use serde_json::Value;

const SERVICE_ARTIFACT: &str = env!("CARGO_BIN_EXE_layerx-human-service");

fn checked<T, E: Debug>(value: Result<T, E>) -> T {
    value.unwrap_or_else(|_| panic!("genuine durable journey owner refused"))
}

fn text<'a>(value: &'a Value, field: &str) -> &'a str {
    value[field]
        .as_str()
        .expect("genuine fixture field required")
}

fn protected(path: &Path) -> Value {
    let metadata = checked(fs::symlink_metadata(path));
    assert!(metadata.is_file() && metadata.len() > 0 && metadata.len() <= 8 * 1024 * 1024);
    assert_eq!(metadata.permissions().mode() & 0o077, 0);
    checked(serde_json::from_slice(&checked(fs::read(path))))
}

fn id(value: &str) -> [u8; 32] {
    assert_eq!(value.len(), 64);
    assert!(value
        .bytes()
        .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte)));
    let mut bytes = [0; 32];
    for (index, byte) in bytes.iter_mut().enumerate() {
        *byte = checked(u8::from_str_radix(&value[index * 2..index * 2 + 2], 16));
    }
    bytes
}

fn store(fixture: &Value) -> PrincipalStore {
    let root = Path::new(text(fixture, "human_store"));
    assert!(root.is_absolute() && root.is_dir());
    let period = RetentionPeriod::new(
        fixture["retention_seconds"]
            .as_u64()
            .expect("actual retention"),
    );
    checked(PrincipalStore::open(
        root,
        RetentionPolicy {
            journeys: period,
            notifications: period,
            audit: period,
            telemetry: period,
            cache: period,
        },
        TenancyDigest::new(id(text(fixture, "tenancy_digest"))),
    ))
}

#[derive(Debug, Eq, PartialEq)]
struct Actual {
    family: &'static str,
    started_at: u64,
    updated_at: u64,
    inner_ids: Vec<String>,
}

fn inventory(scope: &PrincipalScope<'_>) -> BTreeMap<String, Actual> {
    let mut result = BTreeMap::new();
    let mut insert = |identity: String, actual: Actual| {
        assert!(
            result.insert(identity, actual).is_none(),
            "duplicate durable outer ID"
        );
    };
    for journey in checked(JourneyEngine::list(scope)) {
        let status = checked(journey.status());
        insert(
            status.journey_id().as_str().to_owned(),
            Actual {
                family: "native",
                started_at: journey.started_at(),
                updated_at: journey.updated_at(),
                inner_ids: vec![],
            },
        );
    }
    for journey in checked(DepositJourney::list_readonly(scope)) {
        let status = checked(journey.status());
        insert(
            status.journey_id().as_str().to_owned(),
            Actual {
                family: "deposit",
                started_at: journey.started_at(),
                updated_at: journey.updated_at(),
                inner_ids: journey
                    .inner_journey_id()
                    .into_iter()
                    .map(|value| value.as_str().to_owned())
                    .collect(),
            },
        );
    }
    for journey in checked(WithdrawalJourney::list_readonly(scope)) {
        let status = checked(journey.status());
        insert(
            status.journey_id().as_str().to_owned(),
            Actual {
                family: "withdrawal",
                started_at: journey.started_at(),
                updated_at: journey.updated_at(),
                inner_ids: journey
                    .inner_journey_id()
                    .into_iter()
                    .map(|value| value.as_str().to_owned())
                    .collect(),
            },
        );
    }
    for journey in checked(ExitJourney::list_readonly(scope)) {
        let status = checked(journey.status());
        insert(
            status.journey_id().as_str().to_owned(),
            Actual {
                family: "exit",
                started_at: journey.started_at(),
                updated_at: journey.updated_at(),
                inner_ids: journey
                    .inner_journey_id()
                    .into_iter()
                    .map(|value| value.as_str().to_owned())
                    .collect(),
            },
        );
    }
    result
}

#[test]
fn genuine_journey_family_owners_survive_reopen() {
    assert!(Path::new(SERVICE_ARTIFACT).is_absolute());
    let observed_path = std::env::var("PAXEER_X_HUMAN_JOURNEY_OBSERVATIONS")
        .expect("actual HTTP production journey observations required");
    let observed = protected(Path::new(&observed_path));
    let fixture = protected(Path::new(text(&observed, "fixture")));
    assert_eq!(
        text(&fixture, "schema"),
        "layerx-human-journey-projections.v1"
    );
    assert_eq!(fixture["disposable_real_authority"], true);
    let principal = checked(PrincipalId::new(text(&fixture, "principal")));
    let foreign = checked(PrincipalId::new(text(&fixture, "other_principal")));
    assert_ne!(principal, foreign);
    let rows = observed["journeys"]
        .as_array()
        .expect("actual served family inventory");
    assert!(rows.len() >= 7);
    let before = {
        let mut owner = store(&fixture);
        inventory(&checked(owner.principal(&principal)))
    };
    let families = rows
        .iter()
        .map(|row| text(row, "family"))
        .collect::<BTreeSet<_>>();
    assert_eq!(
        families,
        BTreeSet::from(["native", "deposit", "withdrawal", "exit"])
    );
    let mut seen = BTreeSet::new();
    for row in rows {
        let identity = text(row, "journey_id");
        assert!(seen.insert(identity));
        let actual = before
            .get(identity)
            .expect("returned HTTP parent must exist in actual family owner");
        assert_eq!(actual.family, text(row, "family"));
        assert_eq!(
            actual.started_at,
            row["started_at_seconds"]
                .as_u64()
                .expect("actual creation seconds")
        );
        assert_eq!(
            actual.updated_at,
            row["updated_at_seconds"]
                .as_u64()
                .expect("actual update seconds")
        );
        let expected_inner = row["inner_ids"]
            .as_array()
            .expect("actual inner ownership IDs")
            .iter()
            .map(|value| value.as_str().expect("owned inner ID").to_owned())
            .collect::<Vec<_>>();
        assert_eq!(actual.inner_ids, expected_inner);
    }
    let after = {
        let mut owner = store(&fixture);
        inventory(&checked(owner.principal(&principal)))
    };
    assert_eq!(
        before, after,
        "reopen preserves exact durable family identity and metadata"
    );
    let unrelated = {
        let mut owner = store(&fixture);
        inventory(&checked(owner.principal(&foreign)))
    };
    assert!(
        seen.iter()
            .all(|identity| !unrelated.contains_key(*identity)),
        "foreign principal cannot restore original outer owners"
    );
}
