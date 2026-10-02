use std::collections::BTreeMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex, Weak};
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};

use crate::events::{Fact, Record};
use crate::secret::{sha256_hex, valid_hex, valid_identifier, valid_principal};
use crate::tls::Upstream;

pub const MAX_PENDING: usize = 1024;
pub const MAX_OBSERVATION_BYTES: usize = 16 * 1024;
const UNAVAILABLE_AFTER: Duration = Duration::from_secs(30);
const REQUIRED_UPSTREAM_SUFFIXES: [&str; 5] = [
    "UPSTREAM_URL",
    "UPSTREAM_CA_DER",
    "UPSTREAM_TOKEN_FILE",
    "UPSTREAM_CLIENT_IDENTITY_PKCS12",
    "UPSTREAM_CLIENT_IDENTITY_PASSWORD_FILE",
];
const UPSTREAM_SUFFIXES: [&str; 6] = [
    "UPSTREAM_URL",
    "UPSTREAM_CA_DER",
    "UPSTREAM_TOKEN_FILE",
    "UPSTREAM_CLIENT_IDENTITY_PKCS12",
    "UPSTREAM_CLIENT_IDENTITY_PASSWORD_FILE",
    "UPSTREAM_COOKIE_FILE",
];

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct Observation {
    pub kind: String,
    pub id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub principal: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub principal_digest: Option<String>,
    pub resource: String,
    pub sequence: u64,
    pub source_sequence: u64,
    pub occurred_at: u64,
    pub facts: Vec<Fact>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub activity_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub amount: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub asset: Option<String>,
}

#[must_use]
pub fn event_id(kind: &str, resource: &str, sequence: u64) -> String {
    let mut bytes = Vec::new();
    for part in [
        kind.as_bytes(),
        resource.as_bytes(),
        &sequence.to_be_bytes(),
    ] {
        bytes.extend_from_slice(&(part.len() as u64).to_be_bytes());
        bytes.extend_from_slice(part);
    }
    sha256_hex(&bytes)
}

impl Observation {
    /// # Errors
    /// Refuses invalid identities, ambiguous principals and oversized facts.
    pub fn validate(&self) -> Result<(), String> {
        let principal_valid = match (&self.principal, &self.principal_digest) {
            (Some(principal), None) => valid_principal(principal),
            (None, Some(digest)) => {
                matches!(self.kind.as_str(), "payment" | "program") && valid_hex(digest, 32)
            }
            _ => false,
        };
        if !matches!(
            self.kind.as_str(),
            "payment" | "program" | "journey" | "approval"
        ) || !principal_valid
            || !valid_identifier(&self.resource, 128)
            || self.sequence == 0
            || self.occurred_at == 0
            || self.id != event_id(&self.kind, &self.resource, self.sequence)
            || self.facts.is_empty()
            || self.facts.len() >= 32
            || self.facts.iter().any(|fact| {
                fact.name == "source_sequence"
                    || !valid_identifier(&fact.name, 128)
                    || fact.value.len() > 512
                    || fact.value.contains(['\0', '\n', '\r'])
            })
            || self
                .activity_id
                .as_ref()
                .is_some_and(|id| !valid_hex(id, 32))
        {
            return Err("invalid producer observation".to_owned());
        }
        if self.kind == "payment"
            && (self.activity_id.as_deref() != Some(self.resource.as_str())
                || self.amount.as_ref().is_none_or(|amount| {
                    amount.is_empty() || amount.len() > 39 || amount.parse::<u128>().is_err()
                })
                || self
                    .asset
                    .as_ref()
                    .is_none_or(|asset| !valid_hex(asset, 32)))
        {
            return Err("invalid payment observation".to_owned());
        }
        Ok(())
    }

    #[must_use]
    pub fn record(&self, principal: String) -> Record {
        let mut facts = self.facts.clone();
        facts.push(Fact {
            name: "source_sequence".to_owned(),
            value: self.source_sequence.to_string(),
        });
        Record {
            id: self.id.clone(),
            principal,
            subject: self.resource.clone(),
            subject_sequence: self.sequence,
            occurred_at: self.occurred_at,
            facts,
            activity_id: self.activity_id.clone(),
            amount: self.amount.clone(),
            asset: self.asset.clone(),
        }
    }

    /// # Errors
    /// Refuses invalid or oversized observations.
    pub fn encode(&self) -> Result<Vec<u8>, String> {
        self.validate()?;
        let bytes = serde_json::to_vec(self).map_err(|error| error.to_string())?;
        if bytes.len() > MAX_OBSERVATION_BYTES {
            return Err("producer observation exceeds bound".to_owned());
        }
        Ok(bytes)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct Pending {
    pub observation: Observation,
    pub body: Vec<u8>,
    pub observed: bool,
}

impl Pending {
    /// # Errors
    /// Refuses invalid or oversized observations.
    pub fn new(observation: Observation) -> Result<Self, String> {
        let body = observation.encode()?;
        Ok(Self {
            observation,
            body,
            observed: false,
        })
    }

    /// # Errors
    /// Refuses a queued body that differs from its complete observation.
    pub fn validate(&self) -> Result<(), String> {
        if self.body != self.observation.encode()? {
            return Err("producer queue body mismatch".to_owned());
        }
        Ok(())
    }
}

pub trait Outbox: Send + Sync + 'static {
    /// # Errors
    /// Refuses unreadable or corrupt durable queue state.
    fn pending(&self) -> Result<Option<Pending>, String>;
    /// # Errors
    /// Returns a durable acknowledgement failure.
    fn acknowledge(&self, id: &str, observed: bool) -> Result<(), String>;
    /// # Errors
    /// Refuses unavailable, corrupt or out-of-order durable delivery state.
    fn select(&self) -> Result<Option<Pending>, String> { self.pending() }
    /// # Errors
    /// Refuses unavailable, corrupt or out-of-order durable delivery state.
    fn scheduling(&self, _id: &str) -> Result<Option<DeliveryState>, String> { Ok(None) }
    /// # Errors
    /// Refuses unavailable, corrupt or out-of-order durable delivery state.
    fn failed_delivery(&self, _id: &str, _generation: Option<u64>, _reason: DeliveryFailure) -> Result<(), String> {
        Err("durable delivery scheduling unavailable".to_owned())
    }
    /// # Errors
    /// Refuses unavailable, corrupt or out-of-order durable delivery state.
    fn resume_delivery(&self, _id: &str, _generation: u64) -> Result<(), String> {
        Err("durable delivery recovery unavailable".to_owned())
    }
}

pub const MAX_DELIVERY_ATTEMPTS: u16 = 5;
pub const MAX_DELIVERY_BACKOFF: u64 = 5;
pub const MAX_SCHEDULED_PRINCIPALS: usize = 10_000;

#[derive(Clone, Copy, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum DeliveryFailure {
    ObservationUnavailable,
    ObservationRefused,
    AcknowledgementInvalid,
    NotificationUnavailable,
    NotificationRefused,
    PersistenceUnavailable,
    RecoveryRequired,
}

impl DeliveryFailure {
    fn from_error(error: &str) -> Self {
        if error == "event observation unavailable" { Self::ObservationUnavailable }
        else if error.starts_with("event observation refused:") { Self::ObservationRefused }
        else if error.contains("acknowledgement") { Self::AcknowledgementInvalid }
        else if error == "event notification unavailable" { Self::NotificationUnavailable }
        else if error.starts_with("event notification refused:") { Self::NotificationRefused }
        else { Self::PersistenceUnavailable }
    }
}

#[derive(Clone, Default, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct DeliveryState {
    pub selected_turn: u64,
    pub attempts: u16,
    pub next_attempt_at: u64,
    pub first_refused_at: Option<u64>,
    pub last_refused_at: Option<u64>,
    pub last_refusal: Option<DeliveryFailure>,
    pub last_progress_at: Option<u64>,
    pub last_delivered_id: Option<String>,
    pub enrollment_generation: Option<u64>,
    pub last_recovery_generation: Option<u64>,
    pub redelivery_required: bool,
}

impl DeliveryState {
    #[must_use]
    pub fn eligible(&self, now: u64) -> bool {
        now >= self.next_attempt_at || self.next_attempt_at.saturating_sub(now) > MAX_DELIVERY_BACKOFF
    }
}


#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct QueueState {
    counters: BTreeMap<String, u64>,
    entries: Vec<Pending>,
    retained: BTreeMap<String, Observation>,
    #[serde(default)]
    delivery: DeliveryState,
}

impl QueueState {
    /// # Errors
    /// Refuses corrupt entries, identities and non-contiguous counters.
    pub fn validate(&self) -> Result<(), String> {
        if self.delivery.attempts > MAX_DELIVERY_ATTEMPTS
            || self.delivery.redelivery_required != (self.delivery.attempts == MAX_DELIVERY_ATTEMPTS)
            || self.delivery.last_delivered_id.as_ref().is_some_and(|id| !valid_hex(id, 32))
            || self.delivery.first_refused_at > self.delivery.last_refused_at
            || (self.delivery.attempts > 0 && (self.delivery.first_refused_at.is_none()
                || self.delivery.last_refused_at.is_none() || self.delivery.last_refusal.is_none()))
        {
            return Err("invalid producer scheduling state".to_owned());
        }
        if self.entries.len() > MAX_PENDING {
            return Err("producer queue exceeds bound".to_owned());
        }
        let mut sequences = BTreeMap::<String, Vec<u64>>::new();
        for (transition, observation) in &self.retained {
            observation.validate()?;
            if transition.is_empty() {
                return Err("producer transition missing".to_owned());
            }
            sequences
                .entry(format!("{}:{}", observation.kind, observation.resource))
                .or_default()
                .push(observation.sequence);
        }
        if sequences.len() != self.counters.len() {
            return Err("producer counter mismatch".to_owned());
        }
        for (resource, mut values) in sequences {
            values.sort_unstable();
            if values
                .iter()
                .enumerate()
                .any(|(index, value)| *value != index as u64 + 1)
                || self.counters.get(&resource) != values.last()
            {
                return Err("producer sequence mismatch".to_owned());
            }
        }
        let mut ids = std::collections::BTreeSet::new();
        let mut pending_sequences = BTreeMap::new();
        for pending in &self.entries {
            let stream = (&pending.observation.kind, &pending.observation.resource);
            if pending_sequences.insert(stream, pending.observation.sequence)
                .is_some_and(|earlier| earlier >= pending.observation.sequence)
            {
                return Err("producer pending stream reordered".to_owned());
            }
            pending.validate()?;
            if !ids.insert(&pending.observation.id)
                || !self
                    .retained
                    .values()
                    .any(|entry| *entry == pending.observation)
            {
                return Err("producer pending identity mismatch".to_owned());
            }
        }
        Ok(())
    }

    /// # Errors
    /// Refuses changed retries, full queues and exhausted resource sequences.
    pub fn enqueue(
        &mut self,
        transition: &str,
        mut observation: Observation,
    ) -> Result<Observation, String> {
        if let Some(previous) = self.retained.get(transition) {
            observation.sequence = previous.sequence;
            observation.id.clone_from(&previous.id);
            return if *previous == observation {
                Ok(previous.clone())
            } else {
                Err("producer transition conflicts".to_owned())
            };
        }
        if self.entries.len() >= MAX_PENDING {
            return Err("producer queue full".to_owned());
        }
        let resource = format!("{}:{}", observation.kind, observation.resource);
        observation.sequence = self
            .counters
            .get(&resource)
            .copied()
            .unwrap_or(0)
            .checked_add(1)
            .ok_or_else(|| "producer sequence exhausted".to_owned())?;
        observation.id = event_id(
            &observation.kind,
            &observation.resource,
            observation.sequence,
        );
        let pending = Pending::new(observation.clone())?;
        self.counters.insert(resource, observation.sequence);
        self.retained
            .insert(transition.to_owned(), observation.clone());
        self.entries.push(pending);
        Ok(observation)
    }

    #[must_use]
    pub fn pending(&self) -> Option<Pending> {
        self.entries.first().cloned()
    }

    #[must_use]
    pub fn retained(&self, transition: &str) -> Option<&Observation> {
        self.retained.get(transition)
    }

    #[must_use]
    pub fn full(&self) -> bool {
        self.entries.len() >= MAX_PENDING
    }

    #[must_use]
    pub const fn delivery(&self) -> &DeliveryState { &self.delivery }

    #[must_use]
    pub fn pending_count(&self) -> usize { self.entries.len() }

    /// # Errors
    /// Refuses unavailable, corrupt or out-of-order durable delivery state.
    pub fn operator_redelivery(&mut self) -> Result<(), String> {
        if self.entries.is_empty() { return Err("producer queue empty".to_owned()); }
        self.delivery.attempts = 0;
        self.delivery.next_attempt_at = 0;
        self.delivery.redelivery_required = false;
        Ok(())
    }

    /// # Errors
    /// Refuses unavailable, corrupt or out-of-order durable delivery state.
    pub fn selected(&mut self, turn: u64) -> Result<(), String> {
        if self.entries.is_empty() || turn <= self.delivery.selected_turn {
            return Err("invalid producer selection turn".to_owned());
        }
        self.delivery.selected_turn = turn;
        Ok(())
    }

    /// # Errors
    /// Refuses unavailable, corrupt or out-of-order durable delivery state.
    pub fn failed_delivery(&mut self, id: &str, generation: Option<u64>, now: u64, reason: DeliveryFailure) -> Result<(), String> {
        if self.entries.first().is_none_or(|pending| pending.observation.id != id) {
            return Err("delivery refusal has no pending head".to_owned());
        }
        self.delivery.attempts = self.delivery.attempts.saturating_add(1).min(MAX_DELIVERY_ATTEMPTS);
        self.delivery.first_refused_at.get_or_insert(now);
        self.delivery.last_refused_at = Some(self.delivery.last_refused_at.unwrap_or(now).max(now));
        if !self.delivery.redelivery_required || reason != DeliveryFailure::RecoveryRequired {
            self.delivery.last_refusal = Some(reason);
        }
        if !self.delivery.redelivery_required {
            self.delivery.enrollment_generation = generation.or(self.delivery.enrollment_generation);
        }
        self.delivery.redelivery_required = self.delivery.attempts == MAX_DELIVERY_ATTEMPTS;
        let delay = (1_u64 << self.delivery.attempts).min(MAX_DELIVERY_BACKOFF);
        self.delivery.next_attempt_at = now.checked_add(delay)
            .ok_or_else(|| "delivery retry deadline exhausted".to_owned())?;
        Ok(())
    }

    /// # Errors
    /// Refuses unavailable, corrupt or out-of-order durable delivery state.
    pub fn resume_delivery(&mut self, id: &str, generation: u64) -> Result<(), String> {
        if self.entries.first().is_none_or(|pending| pending.observation.id != id)
            || self.delivery.enrollment_generation.is_some_and(|previous| generation <= previous)
        {
            return Err("delivery recovery needs a newer authenticated generation".to_owned());
        }
        self.delivery.attempts = 0;
        self.delivery.next_attempt_at = 0;
        self.delivery.enrollment_generation = Some(generation);
        self.delivery.last_recovery_generation = Some(generation);
        self.delivery.redelivery_required = false;
        Ok(())
    }

    /// # Errors
    /// Refuses acknowledgements outside the durable queue order.
    pub fn acknowledge(&mut self, id: &str, observed: bool) -> Result<(), String> {
        let first = self
            .entries
            .first_mut()
            .ok_or_else(|| "producer queue empty".to_owned())?;
        if first.observation.id != id || first.observed == observed {
            return Err("producer acknowledgement out of order".to_owned());
        }
        let now = crate::secret::unix_seconds()?;
        if observed {
            first.observed = true;
        } else {
            self.delivery.last_delivered_id = Some(id.to_owned());
            self.delivery.enrollment_generation = None;
            self.entries.remove(0);
        }
        self.delivery.attempts = 0;
        self.delivery.next_attempt_at = 0;
        self.delivery.redelivery_required = false;
        self.delivery.last_progress_at = Some(now);
        Ok(())
    }
}

#[derive(Default)]
pub struct Health {
    failed_since: Mutex<Option<Instant>>,
    failures: AtomicU64,
    overflow: AtomicU64,
}

impl Health {
    pub fn overflow(&self) {
        self.overflow.fetch_add(1, Ordering::Relaxed);
        self.failed();
        eprintln!("event_producer_queue_overflow");
    }

    fn failed(&self) {
        self.failures.fetch_add(1, Ordering::Relaxed);
        if let Ok(mut since) = self.failed_since.lock() {
            since.get_or_insert_with(Instant::now);
        }
    }

    fn recovered(&self) {
        if let Ok(mut since) = self.failed_since.lock() {
            *since = None;
        }
    }

    #[must_use]
    pub fn ready(&self) -> bool {
        self.failed_since
            .lock()
            .is_ok_and(|since| since.is_none_or(|since| since.elapsed() < UNAVAILABLE_AFTER))
    }

    #[must_use]
    pub fn metrics(&self) -> (u64, u64) {
        (
            self.failures.load(Ordering::Relaxed),
            self.overflow.load(Ordering::Relaxed),
        )
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct NotificationAcknowledgement {
    position: u64,
    #[serde(rename = "duplicate")]
    _duplicate: bool,
    queued: Vec<String>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct EnrollmentStatus {
    principal: String,
    generation: u64,
    bound: bool,
}

pub struct Client {
    sources: BTreeMap<String, Upstream>,
    webhooks: Upstream,
}

impl Client {
    /// # Errors
    /// Requires mutually authenticated source and webhook clients.
    pub fn new(sources: BTreeMap<String, Upstream>, webhooks: Upstream) -> Result<Self, String> {
        if sources.is_empty()
            || !webhooks.authenticated_producer()
            || sources.iter().any(|(kind, source)| {
                !matches!(
                    kind.as_str(),
                    "payment" | "program" | "journey" | "approval"
                ) || !source.authenticated_producer()
            })
        {
            return Err("producer requires mTLS and bearer credentials".to_owned());
        }
        Ok(Self { sources, webhooks })
    }

    /// # Errors
    /// Refuses missing per-kind TLS clients or webhook configuration.
    pub fn from_environment(kinds: &[&str]) -> Result<Self, String> {
        let mut sources = BTreeMap::new();
        for kind in kinds {
            let prefix = format!("LAYERX_EVENTS_{}", kind.to_ascii_uppercase());
            sources.insert((*kind).to_owned(), Upstream::from_environment(&prefix)?);
        }
        Self::new(
            sources,
            Upstream::from_environment("LAYERX_EVENTS_WEBHOOKS")?,
        )
    }

    /// # Errors
    /// Refuses a producer variable set without any upstream URL, and names the first
    /// required producer variable missing once an upstream URL is set.
    pub fn configured(kinds: &[&str], present: impl Fn(&str) -> bool) -> Result<bool, String> {
        let prefixes = kinds
            .iter()
            .map(|kind| format!("LAYERX_EVENTS_{}", kind.to_ascii_uppercase()))
            .chain(std::iter::once("LAYERX_EVENTS_WEBHOOKS".to_owned()))
            .collect::<Vec<_>>();
        let urls = prefixes
            .iter()
            .map(|prefix| format!("{prefix}_UPSTREAM_URL"))
            .collect::<Vec<_>>();
        if !urls.iter().any(|url| present(url)) {
            for prefix in &prefixes {
                for suffix in UPSTREAM_SUFFIXES {
                    let variable = format!("{prefix}_{suffix}");
                    if present(&variable) {
                        return Err(format!("{variable} is set without {}", urls[0]));
                    }
                }
            }
            return Ok(false);
        }
        for prefix in &prefixes {
            for suffix in REQUIRED_UPSTREAM_SUFFIXES {
                let variable = format!("{prefix}_{suffix}");
                if !present(&variable) {
                    return Err(format!("{variable} is required"));
                }
            }
        }
        Ok(true)
    }

    /// # Errors
    /// Refuses a partially configured producer; returns no client when no producer
    /// variable is set.
    pub fn from_environment_if_configured(kinds: &[&str]) -> Result<Option<Self>, String> {
        if Self::configured(kinds, |variable| std::env::var_os(variable).is_some())? {
            Self::from_environment(kinds).map(Some)
        } else {
            Ok(None)
        }
    }

    /// # Errors
    /// Returns observation or notification failures without discarding the queue entry.
    pub fn deliver(&self, pending: &Pending) -> Result<bool, String> {
        pending.validate()?;
        let observation = &pending.observation;
        if !pending.observed {
            let source = self
                .sources
                .get(&observation.kind)
                .ok_or_else(|| "producer source missing".to_owned())?;
            let response = source
                .post("/internal/v1/observe", &pending.body)
                .map_err(|_| "event observation unavailable".to_owned())?;
            if response.status != 200 || !response.content_type.starts_with("application/json") {
                return Err(format!("event observation refused: {}", response.status));
            }
            let record: Record = serde_json::from_slice(&response.body)
                .map_err(|_| "event acknowledgement malformed".to_owned())?;
            let principal = observation
                .principal
                .clone()
                .unwrap_or_else(|| record.principal.clone());
            if observation
                .principal_digest
                .as_ref()
                .is_some_and(|digest| sha256_hex(principal.as_bytes()) != *digest)
                || record != observation.record(principal)
            {
                return Err("event acknowledgement mismatch".to_owned());
            }
            return Ok(true);
        }
        let path = format!(
            "/internal/v1/events/{}/{}",
            observation.kind, observation.id
        );
        let response = self
            .webhooks
            .post(&path, b"{}")
            .map_err(|_| "event notification unavailable".to_owned())?;
        if response.status != 202 || !response.content_type.starts_with("application/json") {
            return Err(format!("event notification refused: {}", response.status));
        }
        let acknowledgement: NotificationAcknowledgement = serde_json::from_slice(&response.body)
            .map_err(|_| "event notification acknowledgement malformed".to_owned())?;
        if acknowledgement.position == 0 || acknowledgement.queued.len() > MAX_PENDING
            || acknowledgement.queued.iter().any(|id| !valid_identifier(id, 128))
            || acknowledgement.queued.iter().collect::<std::collections::BTreeSet<_>>().len()
                != acknowledgement.queued.len()
        {
            return Err("event notification acknowledgement mismatch".to_owned());
        }
        Ok(false)
    }

    fn enrollment(&self, pending: &Pending) -> Result<(u64, bool), String> {
        let principal = pending.observation.principal.as_deref()
            .filter(|principal| valid_principal(principal))
            .ok_or_else(|| "enrollment recovery requires a principal".to_owned())?;
        let source = self.sources.get(&pending.observation.kind)
            .ok_or_else(|| "producer source missing".to_owned())?;
        let response = source.get(&format!("/internal/v1/principals/{principal}/enrollment"))
            .map_err(|_| "enrollment recovery unavailable".to_owned())?;
        if response.status != 200 || !response.content_type.starts_with("application/json") {
            return Err("enrollment recovery refused".to_owned());
        }
        let enrollment: EnrollmentStatus = serde_json::from_slice(&response.body)
            .map_err(|_| "enrollment recovery malformed".to_owned())?;
        if enrollment.principal != principal {
            return Err("enrollment recovery principal mismatch".to_owned());
        }
        Ok((enrollment.generation, enrollment.bound))
    }

    /// # Errors
    /// Refuses unavailable, corrupt or out-of-order durable delivery state.
    pub fn step<S: Outbox>(&self, store: &S, health: &Health) -> Result<(), String> {
        let result = store.select().and_then(|pending| {
            let Some(pending) = pending else {
                for source in self.sources.values() {
                    if !source.get("/livez").is_ok_and(|response| response.status == 200) {
                        return Err("event source unreachable".to_owned());
                    }
                }
                return Ok(());
            };
            let schedule = store.scheduling(&pending.observation.id)?;
            let enrollment = schedule.as_ref().map(|_| self.enrollment(&pending));
            if let Some(schedule) = schedule.as_ref().filter(|state| state.redelivery_required) {
                match enrollment.as_ref().and_then(|result| result.as_ref().ok()) {
                    Some((generation, true)) if schedule.enrollment_generation.is_none_or(|old| *generation > old) => {
                        store.resume_delivery(&pending.observation.id, *generation)?;
                    }
                    _ => {
                        store.failed_delivery(&pending.observation.id, None, DeliveryFailure::RecoveryRequired)?;
                        return Err("event redelivery requires authenticated recovery".to_owned());
                    }
                }
            }
            let attempt = self.deliver(&pending).and_then(|observed| {
                store.acknowledge(&pending.observation.id, observed)
            });
            if let Err(error) = &attempt {
                if schedule.is_some() {
                    let generation = enrollment.and_then(Result::ok).map(|(generation, _)| generation);
                    store.failed_delivery(&pending.observation.id, generation, DeliveryFailure::from_error(error))?;
                }
            }
            attempt
        });
        match &result {
            Ok(()) => health.recovered(),
            Err(error) => {
                health.failed();
                eprintln!("event_producer_delivery_failure: {error}");
            }
        }
        result
    }

    /// # Errors
    /// Returns failure to start the delivery worker.
    pub fn spawn<S: Outbox>(self, store: Weak<S>, health: Arc<Health>) -> Result<(), String> {
        std::thread::Builder::new()
            .name("event-producer".to_owned())
            .spawn(move || {
                while let Some(store) = store.upgrade() {
                    let _ = self.step(store.as_ref(), &health);
                    drop(store);
                    std::thread::sleep(Duration::from_secs(1));
                }
            })
            .map(|_| ())
            .map_err(|error| error.to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    pub(super) fn observation(sequence: u64) -> Observation {
        Observation {
            kind: "journey".to_owned(),
            id: event_id("journey", "journey-one", sequence),
            principal: Some("principal-one".to_owned()),
            principal_digest: None,
            resource: "journey-one".to_owned(),
            sequence,
            source_sequence: 17,
            occurred_at: 123,
            facts: vec![Fact {
                name: "state".to_owned(),
                value: "processing".to_owned(),
            }],
            activity_id: None,
            amount: None,
            asset: None,
        }
    }

    #[test]
    fn durable_payload_cannot_be_substituted_or_reidentified() {
        let first = observation(1);
        assert_ne!(first.id, observation(2).id);
        assert_ne!(first.id, event_id("approval", "journey-one", 1));
        assert_ne!(first.id, event_id("journey", "journey-two", 1));
        let mut pending = Pending::new(first.clone()).unwrap_or_else(|error| panic!("{error}"));
        assert!(pending.validate().is_ok());
        pending.observed = true;
        assert!(pending.validate().is_ok());
        pending.observation.facts[0].value = "refused".to_owned();
        assert!(pending.validate().is_err());
        let mut changed = first;
        changed.sequence = 2;
        assert!(changed.validate().is_err());
        changed = observation(1);
        changed.principal_digest = Some("a".repeat(64));
        assert!(changed.validate().is_err());
        changed.principal = None;
        assert!(changed.validate().is_err());
        changed = observation(0);
        assert!(changed.validate().is_err());
        changed = observation(1);
        changed.facts[0].value = "x".repeat(513);
        assert!(changed.validate().is_err());
    }

    #[test]
    fn degraded_producer_is_absent_only_when_every_producer_variable_is_unset() {
        let names = ["PAYMENT", "WEBHOOKS"]
            .into_iter()
            .flat_map(|kind| {
                UPSTREAM_SUFFIXES
                    .into_iter()
                    .map(move |suffix| format!("LAYERX_EVENTS_{kind}_{suffix}"))
            })
            .collect::<Vec<_>>();
        let required = names
            .iter()
            .filter(|name| !name.ends_with("_UPSTREAM_COOKIE_FILE"))
            .cloned()
            .collect::<Vec<_>>();
        let configured = |set: &[String]| {
            Client::configured(&["payment"], |variable| {
                set.iter().any(|name| name == variable)
            })
        };
        assert_eq!(configured(&[]), Ok(false));
        assert_eq!(configured(&required), Ok(true));
        for name in names.iter().filter(|name| !name.ends_with("_UPSTREAM_URL")) {
            assert_eq!(
                configured(std::slice::from_ref(name)),
                Err(format!(
                    "{name} is set without LAYERX_EVENTS_PAYMENT_UPSTREAM_URL"
                ))
            );
        }
        for missing in &required {
            let set = required
                .iter()
                .filter(|name| *name != missing)
                .cloned()
                .collect::<Vec<_>>();
            assert_eq!(configured(&set), Err(format!("{missing} is required")));
        }
        assert_eq!(
            Client::configured(&["payment", "program"], |variable| {
                required.iter().any(|name| name == variable)
            }),
            Err("LAYERX_EVENTS_PROGRAM_UPSTREAM_URL is required".to_owned())
        );
    }

    #[test]
    fn unreachable_threshold_refuses_readiness_and_tracks_overflow() {
        let health = Health::default();
        assert!(health.ready());
        health.failed();
        assert!(health.ready());
        *health
            .failed_since
            .lock()
            .unwrap_or_else(|error| panic!("{error}")) = Some(
            Instant::now()
                .checked_sub(UNAVAILABLE_AFTER)
                .unwrap_or_else(|| panic!("readiness clock underflow")),
        );
        assert!(!health.ready());
        health.overflow();
        assert_eq!(health.metrics(), (2, 1));
        health.recovered();
        assert!(health.ready());
    }
}

#[cfg(test)]
mod queue_tests {
    use super::*;

    #[test]
    fn counters_are_per_resource_and_retries_preserve_complete_entries() {
        let mut queue = QueueState::default();
        let mut one = super::tests::observation(1);
        one.source_sequence = 57;
        let first = queue
            .enqueue("journal-57", one.clone())
            .unwrap_or_else(|error| panic!("{error}"));
        let mut two = one.clone();
        two.source_sequence = 83;
        let second = queue
            .enqueue("journal-83", two)
            .unwrap_or_else(|error| panic!("{error}"));
        let mut other = one.clone();
        other.resource = "journey-two".to_owned();
        other.source_sequence = 84;
        let other = queue
            .enqueue("journal-84", other)
            .unwrap_or_else(|error| panic!("{error}"));
        assert_eq!((first.sequence, second.sequence, other.sequence), (1, 2, 1));
        assert_eq!((first.source_sequence, second.source_sequence), (57, 83));
        assert_eq!(queue.enqueue("journal-57", one.clone()), Ok(first.clone()));
        let encoded = serde_json::to_vec(&queue).unwrap_or_else(|error| panic!("{error}"));
        let mut queue: QueueState =
            serde_json::from_slice(&encoded).unwrap_or_else(|error| panic!("{error}"));
        assert!(queue.validate().is_ok());
        assert!(queue.acknowledge(&second.id, true).is_err());
        assert!(queue.acknowledge(&first.id, false).is_err());
        assert!(queue.acknowledge(&first.id, true).is_ok());
        assert!(queue.acknowledge(&first.id, false).is_ok());
        assert_eq!(queue.enqueue("journal-57", one.clone()), Ok(first));
        assert_eq!(queue.pending().map(|entry| entry.observation), Some(second));
        one.facts[0].value = "changed".to_owned();
        assert!(queue.enqueue("journal-57", one).is_err());
    }

    #[test]
    fn overflow_does_not_consume_a_resource_sequence() {
        let mut queue = QueueState::default();
        for sequence in 1..=MAX_PENDING {
            let mut observation = super::tests::observation(1);
            observation.source_sequence = sequence as u64;
            assert!(queue
                .enqueue(&format!("transition-{sequence}"), observation)
                .is_ok());
        }
        let candidate = super::tests::observation(1);
        assert!(queue.enqueue("after-full", candidate.clone()).is_err());
        let head = queue.pending().unwrap_or_else(|| panic!("queue empty"));
        assert!(queue.acknowledge(&head.observation.id, true).is_ok());
        assert!(queue.acknowledge(&head.observation.id, false).is_ok());
        let next = queue
            .enqueue("after-full", candidate)
            .unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(next.sequence, MAX_PENDING as u64 + 1);
        assert!(queue.validate().is_ok());
    }
}

#[cfg(test)]
mod scheduling_tests {
    use super::*;

    #[test]
    fn durable_retry_state_is_bounded_and_legacy_entries_keep_their_bytes() {
        let mut queue = QueueState::default();
        let observation = queue.enqueue("first", super::tests::observation(1))
            .unwrap_or_else(|error| panic!("{error}"));
        let original = queue.pending().unwrap_or_else(|| panic!("pending missing"));
        for _ in 0..MAX_DELIVERY_ATTEMPTS + 2 {
            queue.failed_delivery(&observation.id, Some(7), 100, DeliveryFailure::ObservationRefused)
                .unwrap_or_else(|error| panic!("{error}"));
        }
        assert_eq!(queue.delivery().attempts, MAX_DELIVERY_ATTEMPTS);
        assert_eq!(queue.delivery().next_attempt_at, 100 + MAX_DELIVERY_BACKOFF);
        assert!(queue.delivery().redelivery_required);
        assert!(!queue.delivery().eligible(100));
        assert!(queue.delivery().eligible(100 + MAX_DELIVERY_BACKOFF));
        assert_eq!(queue.pending(), Some(original.clone()));
        assert!(queue.resume_delivery(&observation.id, 7).is_err());
        let mut encoded = serde_json::to_value(&queue).unwrap_or_else(|error| panic!("{error}"));
        encoded["delivery"]["redelivery_required"] = serde_json::json!(false);
        let invalid: QueueState = serde_json::from_value(encoded).unwrap_or_else(|error| panic!("{error}"));
        assert!(invalid.validate().is_err());
        let mut legacy = serde_json::to_value(&queue).unwrap_or_else(|error| panic!("{error}"));
        legacy.as_object_mut().unwrap_or_else(|| panic!("queue object missing")).remove("delivery");
        let legacy: QueueState = serde_json::from_value(legacy).unwrap_or_else(|error| panic!("{error}"));
        assert!(legacy.validate().is_ok());
        assert_eq!(legacy.pending(), Some(original));
        queue.resume_delivery(&observation.id, 8).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(queue.delivery().first_refused_at, Some(100));
        assert_eq!(queue.delivery().last_recovery_generation, Some(8));
        assert!(queue.delivery().eligible(100));
    }

    #[test]
    fn replay_rejects_a_reordered_subject_stream() {
        let mut queue = QueueState::default();
        queue.enqueue("first", super::tests::observation(1)).unwrap_or_else(|error| panic!("{error}"));
        queue.enqueue("second", super::tests::observation(2)).unwrap_or_else(|error| panic!("{error}"));
        queue.entries.swap(0, 1);
        assert_eq!(queue.validate(), Err("producer pending stream reordered".to_owned()));
    }
}
