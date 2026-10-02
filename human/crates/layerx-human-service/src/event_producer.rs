use std::sync::{Arc, Mutex, OnceLock};
use std::io::{Read, Write};
use std::os::unix::fs::{FileTypeExt, MetadataExt, PermissionsExt};
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::Path;
use std::time::Duration;

use layerx_platform_internal::producer::{DeliveryFailure, DeliveryState, Health, Observation, Outbox, Pending, QueueState,
    MAX_SCHEDULED_PRINCIPALS};

use crate::store::{PrincipalScope, PrincipalStore, RowKey, StoreError, Table};

static HEALTH: OnceLock<Arc<Health>> = OnceLock::new();

pub(crate) fn health() -> Arc<Health> {
    Arc::clone(HEALTH.get_or_init(|| Arc::new(Health::default())))
}

fn key() -> Result<RowKey, StoreError> {
    RowKey::new("event-producer")
}

fn state(scope: &PrincipalScope<'_>) -> Result<QueueState, StoreError> {
    let state = scope
        .get(Table::EventOutbox, &key()?)
        .map(|row| {
            serde_json::from_slice::<QueueState>(row.bytes())
                .map_err(|_| StoreError::Corrupt("invalid event outbox"))
        })
        .transpose()?
        .unwrap_or_default();
    state
        .validate()
        .map_err(|_| StoreError::Corrupt("invalid event outbox identity"))?;
    Ok(state)
}

pub(crate) fn enqueue_row(
    scope: &PrincipalScope<'_>,
    transition: &str,
    mut observation: Observation,
) -> Result<(Table, RowKey, Vec<u8>), StoreError> {
    let mut state = state(scope)?;
    if let Some(previous) = state.retained(transition) {
        observation.source_sequence = previous.source_sequence;
        observation.occurred_at = previous.occurred_at;
    }
    let result = state.enqueue(transition, observation);
    if result.is_err() && state.full() {
        health().overflow();
    }
    result.map_err(|_| StoreError::Corrupt("event enqueue refused"))?;
    let bytes =
        serde_json::to_vec(&state).map_err(|_| StoreError::Corrupt("event outbox encoding"))?;
    Ok((Table::EventOutbox, key()?, bytes))
}

pub(crate) struct HumanOutbox {
    store: Arc<Mutex<PrincipalStore>>,
    health: Arc<Health>,
}

impl HumanOutbox {
    pub(crate) fn ready(&self) -> bool {
        self.health.ready() && self.scheduling_ready().unwrap_or(false)
    }

    fn scheduling_ready(&self) -> Result<bool, String> {
        let now = layerx_platform_internal::secret::unix_seconds()?;
        let mut store = self.store.lock().map_err(|_| "Human store unavailable")?;
        let principals = store.known_principals().map_err(|error| error.to_string())?;
        if principals.len() > MAX_SCHEDULED_PRINCIPALS {
            return Err("Human principal scheduling bound exceeded".to_owned());
        }
        for principal in principals {
            let scope = store.principal(&principal).map_err(|error| error.to_string())?;
            let state = state(&scope).map_err(|error| error.to_string())?;
            if state.delivery().attempts > 0 && state.delivery().first_refused_at
                .is_some_and(|first| now.saturating_sub(first) >= 30)
            {
                return Ok(false);
            }
        }
        Ok(true)
    }

    fn scheduled(&self, select: bool) -> Result<Option<Pending>, String> {
        self.scheduled_at(select, layerx_platform_internal::secret::unix_seconds()?)
    }

    fn scheduled_at(&self, select: bool, now: u64) -> Result<Option<Pending>, String> {
        let mut store = self.store.lock().map_err(|_| "Human store unavailable")?;
        let principals = store.known_principals().map_err(|error| error.to_string())?;
        if principals.len() > MAX_SCHEDULED_PRINCIPALS {
            return Err("Human principal scheduling bound exceeded".to_owned());
        }
        let mut maximum = 0;
        let mut selected: Option<(crate::store::PrincipalId, u64, Pending)> = None;
        for principal in principals {
            let scope = store.principal(&principal).map_err(|error| error.to_string())?;
            let state = state(&scope).map_err(|error| error.to_string())?;
            maximum = maximum.max(state.delivery().selected_turn);
            if let Some(pending) = state.pending() {
                let turn = state.delivery().selected_turn;
                if (!select || state.delivery().eligible(now))
                    && selected.as_ref().is_none_or(|(_, earlier, _)| turn < *earlier)
                {
                    selected = Some((principal, turn, pending));
                }
            }
        }
        let Some((principal, _, pending)) = selected else { return Ok(None); };
        if select {
            let mut scope = store.principal(&principal).map_err(|error| error.to_string())?;
            let mut state = state(&scope).map_err(|error| error.to_string())?;
            state.selected(maximum.checked_add(1).ok_or("Human scheduling turn exhausted")?)?;
            persist_state(&mut scope, &state, now)?;
        }
        Ok(Some(pending))
    }

    fn update_head<T>(&self, id: &str, update: impl FnOnce(&mut QueueState) -> Result<T, String>) -> Result<T, String> {
        let mut store = self.store.lock().map_err(|_| "Human store unavailable")?;
        for principal in store.known_principals().map_err(|error| error.to_string())? {
            let mut scope = store.principal(&principal).map_err(|error| error.to_string())?;
            let mut state = state(&scope).map_err(|error| error.to_string())?;
            if state.pending().is_some_and(|entry| entry.observation.id == id) {
                let result = update(&mut state)?;
                persist_state(&mut scope, &state, layerx_platform_internal::secret::unix_seconds()?)?;
                return Ok(result);
            }
        }
        Err("Human delivery has no pending head".to_owned())
    }

    fn status(&self, principal: &crate::store::PrincipalId, redeliver: bool) -> Result<serde_json::Value, String> {
        let mut store = self.store.lock().map_err(|_| "Human store unavailable")?;
        if !store.known_principals().map_err(|error| error.to_string())?.contains(principal) {
            return Err("unknown Human event principal".to_owned());
        }
        let mut scope = store.principal(principal).map_err(|error| error.to_string())?;
        let mut state = state(&scope).map_err(|error| error.to_string())?;
        if redeliver {
            state.operator_redelivery()?;
            persist_state(&mut scope, &state, layerx_platform_internal::secret::unix_seconds()?)?;
        }
        Ok(serde_json::json!({"principal":principal.as_str(), "delivery":state.delivery(),
            "pending":state.pending(), "pending_count":state.pending_count()}))
    }
    pub(crate) fn start(store: Arc<Mutex<PrincipalStore>>) -> Result<Arc<Self>, String> {
        let outbox = Arc::new(Self {
            store,
            health: health(),
        });
        start_status_listener(&outbox)?;
        layerx_platform_internal::producer::Client::from_environment(&["journey", "approval"])?
            .spawn(Arc::downgrade(&outbox), health())?;
        Ok(outbox)
    }
}

impl Outbox for HumanOutbox {
    fn pending(&self) -> Result<Option<Pending>, String> { self.scheduled(false) }

    fn select(&self) -> Result<Option<Pending>, String> { self.scheduled(true) }

    fn scheduling(&self, id: &str) -> Result<Option<DeliveryState>, String> {
        self.update_head(id, |state| Ok(Some(state.delivery().clone())))
    }

    fn failed_delivery(&self, id: &str, generation: Option<u64>, reason: DeliveryFailure) -> Result<(), String> {
        let now = layerx_platform_internal::secret::unix_seconds()?;
        self.update_head(id, |state| state.failed_delivery(id, generation, now, reason))
    }

    fn resume_delivery(&self, id: &str, generation: u64) -> Result<(), String> {
        self.update_head(id, |state| state.resume_delivery(id, generation))
    }

    fn acknowledge(&self, id: &str, observed: bool) -> Result<(), String> {
        let mut store = self.store.lock().map_err(|_| "Human store unavailable")?;
        for principal in store
            .known_principals()
            .map_err(|error| error.to_string())?
        {
            let mut scope = store
                .principal(&principal)
                .map_err(|error| error.to_string())?;
            let mut state = state(&scope).map_err(|error| error.to_string())?;
            if let Some(pending) = state.pending().filter(|entry| entry.observation.id == id) {
                state.acknowledge(id, observed)?;
                let bytes = serde_json::to_vec(&state).map_err(|error| error.to_string())?;
                return scope
                    .put(
                        Table::EventOutbox,
                        key().map_err(|error| error.to_string())?,
                        pending.observation.occurred_at,
                        bytes,
                    )
                    .map_err(|error| error.to_string());
            }
        }
        Err("Human acknowledgement has no pending fact".to_owned())
    }
}

fn persist_state(scope: &mut PrincipalScope<'_>, state: &QueueState, now: u64) -> Result<(), String> {
    scope.put(Table::EventOutbox, key().map_err(|error| error.to_string())?, now,
        serde_json::to_vec(state).map_err(|error| error.to_string())?)
        .map_err(|error| error.to_string())
}

fn start_status_listener(outbox: &Arc<HumanOutbox>) -> Result<(), String> {
    let Some(path) = std::env::var_os("LAYERX_HUMAN_EVENT_STATUS_SOCKET") else { return Ok(()); };
    let path = Path::new(&path);
    let parent = path.parent().ok_or("Human event status parent missing")?;
    let metadata = std::fs::symlink_metadata(parent).map_err(|error| error.to_string())?;
    let uid = rustix::process::geteuid().as_raw();
    if !path.is_absolute() || std::fs::canonicalize(parent).map_err(|error| error.to_string())? != parent
        || !metadata.is_dir() || metadata.uid() != uid || metadata.mode() & 0o077 != 0
    {
        return Err("Human event status directory must be owner-only".to_owned());
    }
    match std::fs::symlink_metadata(path) {
        Ok(metadata) => {
            if !metadata.file_type().is_socket() || metadata.uid() != uid || metadata.mode() & 0o077 != 0
                || UnixStream::connect(path).is_ok()
            {
                return Err("Human event status socket is occupied or unprotected".to_owned());
            }
            std::fs::remove_file(path).map_err(|error| error.to_string())?;
        }
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => {}
        Err(error) => return Err(error.to_string()),
    }
    let listener = UnixListener::bind(path).map_err(|error| error.to_string())?;
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600)).map_err(|error| error.to_string())?;
    listener.set_nonblocking(true).map_err(|error| error.to_string())?;
    let owner = Arc::downgrade(outbox);
    std::thread::Builder::new().name("human-event-status".to_owned()).spawn(move || {
        while let Some(outbox) = owner.upgrade() {
            match listener.accept() {
                Ok((mut stream, _)) => {
                    if !rustix::net::sockopt::socket_peercred(&stream).is_ok_and(|peer| peer.uid.as_raw() == uid) {
                        continue;
                    }
                    let result = status_request(&outbox, &mut stream);
                    let response = result.unwrap_or_else(|_| serde_json::json!({"error":"event_status_refused"}));
                    if let Ok(bytes) = serde_json::to_vec(&response) { let _ = stream.write_all(&bytes); }
                }
                Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                    drop(outbox);
                    std::thread::sleep(Duration::from_millis(100));
                }
                Err(_) => break,
            }
        }
    }).map(|_| ()).map_err(|error| error.to_string())
}

#[derive(serde::Deserialize)]
#[serde(deny_unknown_fields)]
struct StatusRequest { operation: String, principal: String }

fn status_request(outbox: &HumanOutbox, stream: &mut UnixStream) -> Result<serde_json::Value, String> {
    stream.set_read_timeout(Some(Duration::from_secs(3))).map_err(|error| error.to_string())?;
    stream.set_write_timeout(Some(Duration::from_secs(3))).map_err(|error| error.to_string())?;
    let mut bytes = Vec::new();
    stream.take(4097).read_to_end(&mut bytes).map_err(|error| error.to_string())?;
    if bytes.len() > 4096 { return Err("event status request exceeds bound".to_owned()); }
    let request: StatusRequest = serde_json::from_slice(&bytes).map_err(|error| error.to_string())?;
    if !matches!(request.operation.as_str(), "status" | "redeliver") {
        return Err("unknown event status operation".to_owned());
    }
    outbox.status(&crate::store::PrincipalId::new(request.principal).map_err(|error| error.to_string())?,
        request.operation == "redeliver")
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::store::{AgentTenantId, PrincipalId, RetentionPeriod, RetentionPolicy, TenancyMap};

    #[test]
    fn stream_and_producer_entries_persist_together_and_outlive_delivery_restarts() {
        let directory =
            std::env::temp_dir().join(format!("human-event-outbox-{}", std::process::id()));
        std::fs::create_dir_all(&directory).unwrap_or_else(|error| panic!("{error}"));
        let principal = PrincipalId::new("alice").unwrap_or_else(|error| panic!("{error}"));
        let other = PrincipalId::new("bob").unwrap_or_else(|error| panic!("{error}"));
        let tenant = AgentTenantId::new("tenant-alice").unwrap_or_else(|error| panic!("{error}"));
        let map = TenancyMap::new([(principal.clone(), tenant.clone()), (other.clone(), tenant)])
            .unwrap_or_else(|error| panic!("{error}"));
        let digest = map
            .install(&directory)
            .unwrap_or_else(|error| panic!("{error}"));
        let period = RetentionPeriod::new(10);
        let retention = RetentionPolicy {
            journeys: period,
            notifications: period,
            audit: period,
            telemetry: period,
            cache: period,
        };
        let store = Arc::new(Mutex::new(
            PrincipalStore::open(&directory, retention, digest)
                .unwrap_or_else(|error| panic!("{error}")),
        ));
        let outbox = HumanOutbox {
            store: Arc::clone(&store),
            health: health(),
        };
        {
            let mut store = store.lock().unwrap_or_else(|error| panic!("{error}"));
            let mut scope = store
                .principal(&principal)
                .unwrap_or_else(|error| panic!("{error}"));
            crate::server::stream_journal::StreamJournal::append(
                &mut scope,
                "notification:one",
                "notification",
                100,
                serde_json::json!({"notification":{}}),
            )
            .unwrap_or_else(|_| panic!("notification append"));
            let body = serde_json::json!({"approval":{"approval_id":"approval-one","agent_id":"agent-one","state":"pending","created_at":100}});
            crate::server::stream_journal::StreamJournal::append(
                &mut scope,
                "approval:one:pending",
                "approval-created",
                101,
                body.clone(),
            )
            .unwrap_or_else(|_| panic!("approval append"));
            crate::server::stream_journal::StreamJournal::append(
                &mut scope,
                "approval:one:pending",
                "approval-created",
                101,
                body,
            )
            .unwrap_or_else(|_| panic!("approval retry"));
            assert_eq!(
                state(&scope)
                    .unwrap_or_else(|error| panic!("{error}"))
                    .pending()
                    .map(|entry| (
                        entry.observation.sequence,
                        entry.observation.source_sequence
                    )),
                Some((1, 2))
            );
            let foreign = store
                .principal(&other)
                .unwrap_or_else(|error| panic!("{error}"));
            assert!(state(&foreign)
                .unwrap_or_else(|error| panic!("{error}"))
                .pending()
                .is_none());
        }
        assert_restart(directory, store, outbox, retention, digest, &principal);
    }

    fn assert_restart(
        directory: std::path::PathBuf,
        store: Arc<Mutex<PrincipalStore>>,
        outbox: HumanOutbox,
        retention: RetentionPolicy,
        digest: crate::store::TenancyDigest,
        principal: &PrincipalId,
    ) {
        let pending = outbox
            .pending()
            .unwrap_or_else(|error| panic!("{error}"))
            .unwrap_or_else(|| panic!("pending missing"));
        assert!(outbox.acknowledge(&pending.observation.id, false).is_err());
        assert!(outbox.acknowledge(&pending.observation.id, true).is_ok());
        drop(outbox);
        drop(store);
        let store = Arc::new(Mutex::new(
            PrincipalStore::open(&directory, retention, digest)
                .unwrap_or_else(|error| panic!("{error}")),
        ));
        let outbox = HumanOutbox {
            store: Arc::clone(&store),
            health: health(),
        };
        let mut observed = pending.clone();
        observed.observed = true;
        assert_eq!(
            outbox.pending().unwrap_or_else(|error| panic!("{error}")),
            Some(observed)
        );
        {
            let mut store = store.lock().unwrap_or_else(|error| panic!("{error}"));
            let mut scope = store
                .principal(principal)
                .unwrap_or_else(|error| panic!("{error}"));
            scope.expire(200).unwrap_or_else(|error| panic!("{error}"));
            assert!(state(&scope)
                .unwrap_or_else(|error| panic!("{error}"))
                .pending()
                .is_some());
        }
        assert!(outbox.acknowledge(&pending.observation.id, false).is_ok());
        assert!(outbox
            .pending()
            .unwrap_or_else(|error| panic!("{error}"))
            .is_none());
        drop(outbox);
        drop(store);
        std::fs::remove_dir_all(directory).unwrap_or_else(|error| panic!("{error}"));
    }
}

#[cfg(test)]
#[path = "event_producer_tls.rs"]
mod tls_tests;

#[cfg(test)]
mod fairness_tests {
    use super::*;
    use crate::store::{AgentTenantId, PrincipalId, RetentionPeriod, RetentionPolicy, TenancyMap};

    fn required<T, E: std::fmt::Debug>(result: Result<T, E>) -> T {
        result.unwrap_or_else(|error| panic!("{error:?}"))
    }

    #[test]
    fn failed_first_principal_cannot_starve_a_healthy_outbox_and_survives_restart() {
        let directory = std::env::temp_dir().join(format!("human-fair-events-{}", std::process::id()));
        required(std::fs::create_dir_all(&directory));
        let alice = required(PrincipalId::new("alice"));
        let bob = required(PrincipalId::new("bob"));
        let tenant = required(AgentTenantId::new("tenant"));
        let tenancy = required(TenancyMap::new([(alice.clone(), tenant.clone()), (bob.clone(), tenant)]));
        let digest = required(tenancy.install(&directory));
        let period = RetentionPeriod::new(10);
        let retention = RetentionPolicy { journeys: period, notifications: period,
            audit: period, telemetry: period, cache: period };
        let store = Arc::new(Mutex::new(required(PrincipalStore::open(&directory, retention, digest))));
        {
            let mut store = required(store.lock());
            for principal in [&alice, &bob] {
                let mut scope = required(store.principal(principal));
                for (sequence, status) in [(1, "pending"), (2, "approved")] {
                    required(crate::server::stream_journal::StreamJournal::append(&mut scope,
                        &format!("approval:{}:{sequence}", principal.as_str()),
                        if sequence == 1 { "approval-created" } else { "approval-approved" },
                        100 + sequence,
                        serde_json::json!({"approval":{"approval_id":format!("approval-{}", principal.as_str()),
                            "agent_id":"agent-one", "state":status, "created_at":100}})));
                }
            }
        }
        let outbox = HumanOutbox { store: Arc::clone(&store), health: Arc::new(Health::default()) };
        let now = 1_000;
        let first = required(outbox.pending()).unwrap_or_else(|| panic!("first pending"));
        assert_eq!(first.observation.principal.as_deref(), Some("alice"));
        assert_eq!(required(outbox.pending()), Some(first.clone()));
        assert_eq!(required(outbox.scheduled_at(true, now)), Some(first.clone()));
        required(outbox.update_head(&first.observation.id, |state| state.failed_delivery(
            &first.observation.id, Some(1), now, DeliveryFailure::ObservationRefused)));
        let failure = required(outbox.status(&alice, false));
        for sequence in [1, 2] {
            let healthy = required(outbox.scheduled_at(true, now)).unwrap_or_else(|| panic!("healthy principal starved"));
            assert_eq!(healthy.observation.principal.as_deref(), Some("bob"));
            assert_eq!(healthy.observation.sequence, sequence);
            assert!(outbox.acknowledge(&healthy.observation.id, false).is_err());
            required(outbox.acknowledge(&healthy.observation.id, true));
            let observed = required(outbox.scheduled_at(true, now)).unwrap_or_else(|| panic!("notification missing"));
            assert!(observed.observed);
            assert_eq!(observed.body, healthy.body);
            required(outbox.acknowledge(&healthy.observation.id, false));
        }
        let remaining = required(outbox.status(&alice, false));
        assert_eq!(remaining["pending"], serde_json::to_value(&first).unwrap_or_else(|error| panic!("{error}")));
        assert_eq!(remaining["delivery"]["first_refused_at"], failure["delivery"]["first_refused_at"]);
        for _ in 1..layerx_platform_internal::producer::MAX_DELIVERY_ATTEMPTS {
            required(outbox.update_head(&first.observation.id, |state| state.failed_delivery(
            &first.observation.id, Some(1), now, DeliveryFailure::ObservationRefused)));
        }
        let terminal = required(outbox.status(&alice, false));
        assert_eq!(terminal["pending_count"], 2);
        assert_eq!(terminal["delivery"]["redelivery_required"], true);
        drop(outbox);
        drop(store);
        let outbox = HumanOutbox {
            store: Arc::new(Mutex::new(required(PrincipalStore::open(&directory, retention, digest)))),
            health: Arc::new(Health::default()),
        };
        assert_eq!(required(outbox.status(&alice, false)), terminal);
        assert_eq!(required(outbox.status(&bob, false))["pending_count"], 0);
        assert!(outbox.resume_delivery(&first.observation.id, 1).is_err());
        required(outbox.resume_delivery(&first.observation.id, 2));
        assert_eq!(required(outbox.scheduled_at(true, now)), Some(first.clone()));
        let second_id = layerx_platform_internal::producer::event_id("approval", "approval-alice", 2);
        assert!(outbox.acknowledge(&second_id, true).is_err());
        required(outbox.acknowledge(&first.observation.id, true));
        required(outbox.acknowledge(&first.observation.id, false));
        let second = required(outbox.scheduled_at(true, now)).unwrap_or_else(|| panic!("second event missing"));
        assert_eq!(second.observation.id, second_id);
        required(outbox.update_head(&second_id, |state| state.failed_delivery(
            &second_id, Some(2), now, DeliveryFailure::NotificationUnavailable)));
        required(outbox.status(&alice, true));
        assert_eq!(required(outbox.scheduled_at(true, now)), Some(second));
        drop(outbox);
        required(std::fs::remove_dir_all(directory));
    }
}
