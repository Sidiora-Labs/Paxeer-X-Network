use crate::{
    codec::{domain_hash, Reader, Writer},
    errors::{
        CodecResult, CAPACITY, F01_CAPACITY_UNAVAILABLE, F01_INVALID_POLICY, F01_POLICY_MISMATCH,
        F01_VERSION_MISMATCH, NON_CANONICAL,
    },
    types::{Digest32, PolicyDigest, Presence, PrincipalId, RubricDigest, Version},
};

pub const TASK_POLICY_BYTES: usize = 307;
pub const PENDING_POLICY_BYTES: usize = 379;
pub const POLICY_HISTORY_HEADER_BYTES: usize = 49;
pub const MAX_RECENT_POLICY_HEADERS: usize = 4;
pub const OBJECTIVE: u8 = 1;
pub const SUBJECTIVE: u8 = 2;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PolicyCommitments {
    pub model_artifact: Digest32,
    pub dataset_artifact: [u8; 32],
    pub benchmark_suite: Digest32,
    pub rubric: RubricDigest,
    pub task_schema: Digest32,
    pub result_schema: Digest32,
    pub service_terms: Digest32,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TaskPolicyV1 {
    pub config_version: u64,
    pub task_kind: u8,
    pub assessment_mode: u8,
    pub commitments: PolicyCommitments,
    pub max_workers: u8,
    pub max_evaluators: u8,
    pub max_tasks_per_epoch: u16,
    pub max_input_bytes: u32,
    pub max_output_bytes: u32,
    pub task_timeout_heights: u16,
    pub score_min: u32,
    pub score_max: u32,
    pub epoch_budget_cap: u128,
    pub minimum_epoch_funding: u128,
    pub minimum_worker_count: u8,
    pub minimum_evaluator_count: u8,
    pub owner_affiliation_policy: u8,
    pub reserved: [u8; 16],
}

impl TaskPolicyV1 {
    /// Commitments do not establish the external rubric's dataset declaration or truth.
    ///
    /// # Errors
    /// Returns `F01_INVALID_POLICY` when the supplied fields fail `validate`.
    pub fn bounded_default(
        config_version: u64,
        task_kind: u8,
        commitments: PolicyCommitments,
        epoch_budget_cap: u128,
        minimum_epoch_funding: u128,
    ) -> CodecResult<Self> {
        let value = Self {
            config_version,
            task_kind,
            assessment_mode: OBJECTIVE,
            commitments,
            max_workers: 32,
            max_evaluators: 8,
            max_tasks_per_epoch: 64,
            max_input_bytes: 1_048_576,
            max_output_bytes: 1_048_576,
            task_timeout_heights: 32,
            score_min: 0,
            score_max: 1_000_000,
            epoch_budget_cap,
            minimum_epoch_funding,
            minimum_worker_count: 1,
            minimum_evaluator_count: 3,
            owner_affiliation_policy: 1,
            reserved: [0; 16],
        };
        value.validate()?;
        Ok(value)
    }

    /// Checks every policy field against its bound.
    ///
    /// # Errors
    /// Returns `F01_INVALID_POLICY` when any field is out of bounds or reserved bytes are set.
    pub fn validate(&self) -> CodecResult<()> {
        if self.config_version == 0
            || !(1..=3).contains(&self.task_kind)
            || !(OBJECTIVE..=SUBJECTIVE).contains(&self.assessment_mode)
            || (self.task_kind != 1 && self.commitments.dataset_artifact == [0; 32])
            || !(1..=32).contains(&self.max_workers)
            || !(1..=8).contains(&self.max_evaluators)
            || !(1..=64).contains(&self.max_tasks_per_epoch)
            || !(1..=16_777_216).contains(&self.max_input_bytes)
            || !(1..=16_777_216).contains(&self.max_output_bytes)
            || !(1..=64).contains(&self.task_timeout_heights)
            || !(1..=self.max_workers).contains(&self.minimum_worker_count)
            || !(3..=self.max_evaluators).contains(&self.minimum_evaluator_count)
            || self.score_min != 0
            || self.score_max != 1_000_000
            || self.epoch_budget_cap == 0
            || self.minimum_epoch_funding == 0
            || self.minimum_epoch_funding > self.epoch_budget_cap
            || self.owner_affiliation_policy != 1
            || self.reserved != [0; 16]
        {
            return Err(F01_INVALID_POLICY);
        }
        Ok(())
    }

    /// Writes the canonical policy bytes into `output`.
    ///
    /// # Errors
    /// Returns `F01_INVALID_POLICY` when the policy is invalid; `CAPACITY` when `output` is shorter than `TASK_POLICY_BYTES`.
    pub fn encode(&self, output: &mut [u8]) -> CodecResult<usize> {
        self.validate()?;
        if output.len() < TASK_POLICY_BYTES {
            return Err(CAPACITY);
        }
        let mut w = Writer::new(output);
        w.u64(self.config_version)?;
        w.u8(self.task_kind)?;
        w.u8(self.assessment_mode)?;
        w.put(self.commitments.model_artifact.as_bytes())?;
        w.put(&self.commitments.dataset_artifact)?;
        w.put(self.commitments.benchmark_suite.as_bytes())?;
        w.put(self.commitments.rubric.as_bytes())?;
        w.put(self.commitments.task_schema.as_bytes())?;
        w.put(self.commitments.result_schema.as_bytes())?;
        w.put(self.commitments.service_terms.as_bytes())?;
        w.u8(self.max_workers)?;
        w.u8(self.max_evaluators)?;
        w.u16(self.max_tasks_per_epoch)?;
        w.u32(self.max_input_bytes)?;
        w.u32(self.max_output_bytes)?;
        w.u16(self.task_timeout_heights)?;
        w.u32(self.score_min)?;
        w.u32(self.score_max)?;
        w.u128(self.epoch_budget_cap)?;
        w.u128(self.minimum_epoch_funding)?;
        w.u8(self.minimum_worker_count)?;
        w.u8(self.minimum_evaluator_count)?;
        w.u8(self.owner_affiliation_policy)?;
        w.put(&self.reserved)?;
        Ok(w.len())
    }

    /// Reads a policy from its exact canonical bytes.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the input is short or has trailing bytes; `F01_INVALID_POLICY` when a required digest is zero or the policy is invalid.
    pub fn decode(input: &[u8]) -> CodecResult<Self> {
        let mut r = Reader::new(input);
        let value = Self {
            config_version: r.u64()?,
            task_kind: r.u8()?,
            assessment_mode: r.u8()?,
            commitments: PolicyCommitments {
                model_artifact: Digest32::new(r.fixed()?).map_err(|_| F01_INVALID_POLICY)?,
                dataset_artifact: r.fixed()?,
                benchmark_suite: Digest32::new(r.fixed()?).map_err(|_| F01_INVALID_POLICY)?,
                rubric: RubricDigest::new(r.fixed()?).map_err(|_| F01_INVALID_POLICY)?,
                task_schema: Digest32::new(r.fixed()?).map_err(|_| F01_INVALID_POLICY)?,
                result_schema: Digest32::new(r.fixed()?).map_err(|_| F01_INVALID_POLICY)?,
                service_terms: Digest32::new(r.fixed()?).map_err(|_| F01_INVALID_POLICY)?,
            },
            max_workers: r.u8()?,
            max_evaluators: r.u8()?,
            max_tasks_per_epoch: r.u16()?,
            max_input_bytes: r.u32()?,
            max_output_bytes: r.u32()?,
            task_timeout_heights: r.u16()?,
            score_min: r.u32()?,
            score_max: r.u32()?,
            epoch_budget_cap: r.u128()?,
            minimum_epoch_funding: r.u128()?,
            minimum_worker_count: r.u8()?,
            minimum_evaluator_count: r.u8()?,
            owner_affiliation_policy: r.u8()?,
            reserved: r.fixed()?,
        };
        r.finish()?;
        value.validate()?;
        Ok(value)
    }

    /// Hashes the canonical policy bytes under the policy domain.
    ///
    /// # Errors
    /// Returns `F01_INVALID_POLICY` when the policy is invalid; `NON_CANONICAL` when the hash is all zero.
    pub fn digest(&self) -> CodecResult<PolicyDigest> {
        let mut bytes = [0; TASK_POLICY_BYTES];
        self.encode(&mut bytes)?;
        PolicyDigest::new(domain_hash("PAXAI/policy/v1", &bytes)?.bytes())
    }
}

/// Returns the config version after `highest`.
///
/// # Errors
/// Returns `NON_CANONICAL` when `highest` is zero; `ARITHMETIC` when it is `u64::MAX`.
pub fn next_config_version(highest: u64) -> CodecResult<u64> {
    Ok(Version::new(highest)?.next()?.get())
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PendingPolicy {
    pub policy: TaskPolicyV1,
    pub digest: PolicyDigest,
    pub effective_epoch: u64,
    pub proposer: PrincipalId,
}
impl PendingPolicy {
    /// Checks that the stored digest matches the staged policy.
    ///
    /// # Errors
    /// Returns `F01_POLICY_MISMATCH` when the digest differs; propagates `TaskPolicyV1::digest` refusals.
    pub fn validate(&self) -> CodecResult<()> {
        if self.policy.digest()? != self.digest {
            return Err(F01_POLICY_MISMATCH);
        }
        Ok(())
    }
    /// Writes the canonical pending-policy bytes into `output`.
    ///
    /// # Errors
    /// Propagates `validate` refusals; returns `CAPACITY` when `output` is shorter than `PENDING_POLICY_BYTES`.
    pub fn encode(&self, output: &mut [u8]) -> CodecResult<usize> {
        self.validate()?;
        if output.len() < PENDING_POLICY_BYTES {
            return Err(CAPACITY);
        }
        self.policy.encode(&mut output[..TASK_POLICY_BYTES])?;
        let mut w = Writer::new(&mut output[TASK_POLICY_BYTES..]);
        w.put(self.digest.as_bytes())?;
        w.u64(self.effective_epoch)?;
        w.put(self.proposer.as_bytes())?;
        Ok(TASK_POLICY_BYTES + w.len())
    }
    /// Reads a pending policy from its exact canonical bytes.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the input is short, has trailing bytes, or holds a zero digest or proposer; propagates `TaskPolicyV1::decode` and `validate` refusals.
    pub fn decode(input: &[u8]) -> CodecResult<Self> {
        let mut r = Reader::new(input);
        let value = Self {
            policy: TaskPolicyV1::decode(r.take(TASK_POLICY_BYTES)?)?,
            digest: PolicyDigest::new(r.fixed()?)?,
            effective_epoch: r.u64()?,
            proposer: PrincipalId::new(r.fixed()?)?,
        };
        r.finish()?;
        value.validate()?;
        Ok(value)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PolicyHistoryHeader {
    pub config_version: u64,
    pub digest: PolicyDigest,
    pub effective_epoch: u64,
    pub disposition: u8,
}
impl PolicyHistoryHeader {
    /// Checks the header's version and disposition.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the version is zero or the disposition is not 1 or 2.
    pub fn validate(&self) -> CodecResult<()> {
        if self.config_version == 0 || !(1..=2).contains(&self.disposition) {
            return Err(NON_CANONICAL);
        }
        Ok(())
    }
    /// Writes the canonical header bytes into `output`.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the header is invalid; `CAPACITY` when `output` is shorter than `POLICY_HISTORY_HEADER_BYTES`.
    pub fn encode(&self, output: &mut [u8]) -> CodecResult<usize> {
        self.validate()?;
        if output.len() < POLICY_HISTORY_HEADER_BYTES {
            return Err(CAPACITY);
        }
        let mut w = Writer::new(output);
        w.u64(self.config_version)?;
        w.put(self.digest.as_bytes())?;
        w.u64(self.effective_epoch)?;
        w.u8(self.disposition)?;
        Ok(w.len())
    }
    /// Reads a header from its exact canonical bytes.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the input is short, has trailing bytes, holds a zero digest, or fails `validate`.
    pub fn decode(input: &[u8]) -> CodecResult<Self> {
        let mut r = Reader::new(input);
        let value = Self {
            config_version: r.u64()?,
            digest: PolicyDigest::new(r.fixed()?)?,
            effective_epoch: r.u64()?,
            disposition: r.u8()?,
        };
        r.finish()?;
        value.validate()?;
        Ok(value)
    }
}

/// Returns the empty policy-history root.
///
/// # Errors
/// Returns `NON_CANONICAL` when the hash is all zero.
pub fn initial_policy_history_root() -> CodecResult<Digest32> {
    domain_hash("PAXAI/policy-history/v1", &[])
}

/// Pure fold of completed headers, with the prior completed version supplied by the state owner.
///
/// # Errors
/// Returns `NON_CANONICAL` when a header is invalid; `F01_VERSION_MISMATCH` when versions do not strictly increase past `previous_version`.
pub fn fold_policy_history(
    previous_root: Digest32,
    previous_version: u64,
    completed: &[PolicyHistoryHeader],
) -> CodecResult<Digest32> {
    let mut root = previous_root;
    let mut last = previous_version;
    for header in completed {
        header.validate()?;
        if header.config_version <= last {
            return Err(F01_VERSION_MISMATCH);
        }
        let mut preimage = [0; 81];
        let mut w = Writer::new(&mut preimage);
        w.put(root.as_bytes())?;
        w.u8(header.disposition)?;
        w.u64(header.config_version)?;
        w.put(header.digest.as_bytes())?;
        w.u64(header.effective_epoch)?;
        root = domain_hash("PAXAI/policy-history/v1", &preimage)?;
        last = header.config_version;
    }
    Ok(root)
}

/// Validates restored policy records without staging, activation, cancellation, or compaction.
///
/// # Errors
/// Returns `F01_INVALID_POLICY` when `current` is invalid; `F01_CAPACITY_UNAVAILABLE` when more than `MAX_RECENT_POLICY_HEADERS` headers are given; `F01_VERSION_MISMATCH` when versions are out of order or inconsistent with `highest_config_version`; `F01_POLICY_MISMATCH` when the current version's header disagrees with `current`; `NON_CANONICAL` when a header is invalid; propagates `PendingPolicy::validate` refusals.
pub fn validate_policy_records(
    current: &TaskPolicyV1,
    pending: &Presence<PendingPolicy>,
    recent: &[PolicyHistoryHeader],
    highest_config_version: u64,
) -> CodecResult<()> {
    current.validate()?;
    if recent.len() > MAX_RECENT_POLICY_HEADERS {
        return Err(F01_CAPACITY_UNAVAILABLE);
    }
    if highest_config_version < current.config_version {
        return Err(F01_VERSION_MISMATCH);
    }
    let current_digest = current.digest()?;
    let mut last = 0;
    for header in recent {
        header.validate()?;
        if header.config_version <= last || header.config_version > highest_config_version {
            return Err(F01_VERSION_MISMATCH);
        }
        if header.config_version == current.config_version
            && (header.digest != current_digest || header.disposition != 1)
        {
            return Err(F01_POLICY_MISMATCH);
        }
        if header.disposition == 1 && header.config_version > current.config_version {
            return Err(F01_VERSION_MISMATCH);
        }
        last = header.config_version;
    }
    if let Presence::Present(value) = pending {
        value.validate()?;
        if value.policy.config_version <= current.config_version
            || value.policy.config_version <= last
            || value.policy.config_version != highest_config_version
        {
            return Err(F01_VERSION_MISMATCH);
        }
    } else if highest_config_version > current.config_version && last != highest_config_version {
        return Err(F01_VERSION_MISMATCH);
    }
    Ok(())
}
