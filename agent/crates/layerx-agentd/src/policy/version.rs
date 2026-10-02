//! Policy validation, immutable request snapshots and retained version history.

use std::collections::{BTreeMap, BTreeSet};
use std::str;
use std::sync::Arc;

use super::{Decision, PolicySet, Rule, RuleConstraints, RuleEffect};

pub const MAX_POLICY_SOURCE_BYTES: usize = 1_048_576;
const MAX_POLICY_RULES: usize = 4_096;
const MAX_POLICY_LINE_BYTES: usize = 4_096;

/// Policy validation refusal taxonomy.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum PolicyValidationError {
    EmptyVersion,
    EmptyRuleId,
    DuplicateRuleId(String),
    EmptyPurpose,
    InvalidSequenceWindow,
    ZeroStepLimit,
    StepLimitTooSmall,
    VersionAlreadyRetained(String),
}

/// Bounded source-loader failures.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum PolicySourceError {
    TooLarge,
    InvalidUtf8,
    LineTooLarge,
    TooManyRules,
    InvalidDeclaration,
    InvalidInteger,
    InvalidEffect,
    Validation(PolicyValidationError),
}

/// Successful activation evidence.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Activation {
    pub previous_version: String,
    pub active_version: String,
    pub generation: u64,
}

/// Immutable policy selection captured when a request is received.
#[derive(Clone, Debug)]
pub struct PolicySnapshot {
    retained: Arc<RetainedPolicy>,
    generation: u64,
}

#[derive(Debug)]
struct RetainedPolicy {
    policy: PolicySet,
    source: Option<Arc<[u8]>>,
}

impl PolicySnapshot {
    #[must_use]
    pub fn policy(&self) -> &PolicySet {
        &self.retained.policy
    }

    #[must_use]
    pub fn version(&self) -> &str {
        &self.retained.policy.version
    }

    #[must_use]
    pub fn source(&self) -> Option<&[u8]> {
        self.retained.source.as_deref()
    }

    #[must_use]
    pub const fn generation(&self) -> u64 {
        self.generation
    }
}

/// Retrievable decision record retained without lossy projection.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PolicyAuditEntry {
    pub request_id: [u8; 32],
    pub decision: Decision,
}

/// Active policy plus all versions and decisions retained for reconstruction.
pub struct PolicyRegistry {
    active_version: String,
    generation: u64,
    versions: BTreeMap<String, Arc<RetainedPolicy>>,
    audit: BTreeMap<[u8; 32], PolicyAuditEntry>,
}

impl PolicyRegistry {
    /// Seeds the registry with a validated initial policy at generation 1.
    ///
    /// # Errors
    ///
    /// Propagates `validate_policy`: `EmptyVersion`, `ZeroStepLimit`, `StepLimitTooSmall`,
    /// `EmptyRuleId`, `DuplicateRuleId`, `EmptyPurpose` or `InvalidSequenceWindow`.
    pub fn new(initial: PolicySet) -> Result<Self, PolicyValidationError> {
        validate_policy(&initial)?;
        Ok(Self::initial(initial, None))
    }

    pub fn from_source(source: &[u8]) -> Result<Self, PolicySourceError> {
        let policy = load_policy_source(source)?;
        Ok(Self::initial(policy, Some(Arc::from(source))))
    }

    fn initial(initial: PolicySet, source: Option<Arc<[u8]>>) -> Self {
        let active_version = initial.version.clone();
        let versions = BTreeMap::from([(
            active_version.clone(),
            Arc::new(RetainedPolicy { policy: initial, source }),
        )]);
        Self {
            active_version,
            generation: 1,
            versions,
            audit: BTreeMap::new(),
        }
    }

    /// Captures the policy that applies before request processing begins.
    #[must_use]
    pub fn begin_request(&self) -> PolicySnapshot {
        let retained = self
            .versions
            .get(&self.active_version)
            .cloned()
            .unwrap_or_else(|| unreachable!("active policy is retained"));
        PolicySnapshot {
            retained,
            generation: self.generation,
        }
    }

    #[must_use]
    pub fn active_version(&self) -> &str {
        &self.active_version
    }

    #[must_use]
    pub fn retained(&self, version: &str) -> Option<&PolicySet> {
        self.versions.get(version).map(|retained| &retained.policy)
    }

    pub fn record_decision(&mut self, request_id: [u8; 32], decision: Decision) {
        self.audit.insert(
            request_id,
            PolicyAuditEntry {
                request_id,
                decision,
            },
        );
    }

    #[must_use]
    pub fn audit_entry(&self, request_id: [u8; 32]) -> Option<&PolicyAuditEntry> {
        self.audit.get(&request_id)
    }
}

pub(crate) fn validate_policy(policy: &PolicySet) -> Result<(), PolicyValidationError> {
    if policy.version.is_empty() {
        return Err(PolicyValidationError::EmptyVersion);
    }
    if policy.evaluation_step_limit == 0 {
        return Err(PolicyValidationError::ZeroStepLimit);
    }
    let rule_count = u64::try_from(policy.rules.len()).unwrap_or(u64::MAX);
    if rule_count > policy.evaluation_step_limit {
        return Err(PolicyValidationError::StepLimitTooSmall);
    }
    let mut ids = BTreeSet::new();
    for rule in &policy.rules {
        if rule.id.is_empty() {
            return Err(PolicyValidationError::EmptyRuleId);
        }
        if !ids.insert(rule.id.clone()) {
            return Err(PolicyValidationError::DuplicateRuleId(rule.id.clone()));
        }
        if rule.constraints.purposes.iter().any(String::is_empty) {
            return Err(PolicyValidationError::EmptyPurpose);
        }
        if rule
            .constraints
            .sequence_window
            .is_some_and(|window| window.first > window.last)
        {
            return Err(PolicyValidationError::InvalidSequenceWindow);
        }
    }
    Ok(())
}

pub(crate) fn activate_policy(
    registry: &mut PolicyRegistry,
    policy: PolicySet,
) -> Result<Activation, PolicyValidationError> {
    activate_retained(registry, policy, None)
}

pub(crate) fn activate_policy_source(
    registry: &mut PolicyRegistry,
    source: &[u8],
) -> Result<Activation, PolicySourceError> {
    let policy = load_policy_source(source)?;
    activate_retained(registry, policy, Some(Arc::from(source)))
        .map_err(PolicySourceError::Validation)
}

fn activate_retained(
    registry: &mut PolicyRegistry,
    policy: PolicySet,
    source: Option<Arc<[u8]>>,
) -> Result<Activation, PolicyValidationError> {
    validate_policy(&policy)?;
    if registry.versions.contains_key(&policy.version) {
        return Err(PolicyValidationError::VersionAlreadyRetained(
            policy.version,
        ));
    }
    let previous_version = registry.active_version.clone();
    let active_version = policy.version.clone();
    let generation = registry.generation.saturating_add(1);
    registry
        .versions
        .insert(active_version.clone(), Arc::new(RetainedPolicy { policy, source }));
    registry.active_version.clone_from(&active_version);
    registry.generation = generation;
    Ok(Activation {
        previous_version,
        active_version,
        generation,
    })
}

/// Loads a bounded line-oriented policy source without ambient state.
///
/// # Errors
///
/// Returns `TooLarge`, `InvalidUtf8`, `LineTooLarge` or `TooManyRules` on the bounds,
/// `InvalidDeclaration` for an unknown, repeated or absent `version=`/`steps=` or a
/// comma-less `rule=`, `InvalidInteger`, `InvalidEffect`, or `Validation` on the set.
pub fn load_policy_source(source: &[u8]) -> Result<PolicySet, PolicySourceError> {
    if source.len() > MAX_POLICY_SOURCE_BYTES {
        return Err(PolicySourceError::TooLarge);
    }
    let text = str::from_utf8(source).map_err(|_| PolicySourceError::InvalidUtf8)?;
    let mut version = None;
    let mut evaluation_step_limit = None;
    let mut rules = Vec::new();
    for line in text.lines() {
        if line.len() > MAX_POLICY_LINE_BYTES {
            return Err(PolicySourceError::LineTooLarge);
        }
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        if let Some(value) = line.strip_prefix("version=") {
            if version.replace(value.to_owned()).is_some() {
                return Err(PolicySourceError::InvalidDeclaration);
            }
        } else if let Some(value) = line.strip_prefix("steps=") {
            let parsed = value
                .parse::<u64>()
                .map_err(|_| PolicySourceError::InvalidInteger)?;
            if evaluation_step_limit.replace(parsed).is_some() {
                return Err(PolicySourceError::InvalidDeclaration);
            }
        } else if let Some(value) = line.strip_prefix("rule=") {
            if rules.len() >= MAX_POLICY_RULES {
                return Err(PolicySourceError::TooManyRules);
            }
            let (id, effect) = value
                .split_once(',')
                .ok_or(PolicySourceError::InvalidDeclaration)?;
            let effect = match effect {
                "permit" => RuleEffect::Permit,
                "deny" => RuleEffect::Deny,
                _ => return Err(PolicySourceError::InvalidEffect),
            };
            rules.push(Rule {
                id: id.to_owned(),
                effect,
                constraints: RuleConstraints::default(),
            });
        } else {
            return Err(PolicySourceError::InvalidDeclaration);
        }
    }
    let policy = PolicySet {
        version: version.ok_or(PolicySourceError::InvalidDeclaration)?,
        rules,
        evaluation_step_limit: evaluation_step_limit
            .ok_or(PolicySourceError::InvalidDeclaration)?,
    };
    validate_policy(&policy).map_err(PolicySourceError::Validation)?;
    Ok(policy)
}
