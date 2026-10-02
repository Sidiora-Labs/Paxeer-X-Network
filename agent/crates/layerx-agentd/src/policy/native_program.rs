use std::collections::BTreeSet;
use std::sync::Arc;

use layerx_types::payload::{ActivityType, ModuleId};
use serde::{Deserialize, Serialize};
use sha2::{Digest as _, Sha256};

use crate::capability::{ProgramSpendBound, ProgramValueSource, VerifiedInputs};
use crate::prepare::Prepared;

const SOURCE_VERSION: &str = "layerx.native-program-policy.v1";
const MAX_SOURCE: usize = 1024 * 1024;
const MAX_RULES: usize = 1024;
const MAX_STEPS: u64 = 1_000_000;

#[derive(Clone, Debug, Eq, PartialEq)]
pub(crate) enum NativePolicyError {
    Source,
    Bound,
    Activity,
    InvalidProgramAccount,
    Disclosure,
    Effects,
    Network,
    NoCoverage,
    ExplicitDeny,
}

#[derive(Clone, Copy, Debug, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
struct NativeActivity {
    module: u16,
    ordinal: u16,
}

impl NativeActivity {
    fn value(self) -> Result<ActivityType, NativePolicyError> {
        let module = ModuleId::from_u16(self.module).map_err(|_| NativePolicyError::Activity)?;
        if module != ModuleId::Programs { return Err(NativePolicyError::Activity); }
        ActivityType::new(module, self.ordinal).map_err(|_| NativePolicyError::Activity)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize, Eq, PartialEq)]
#[serde(tag = "kind", deny_unknown_fields)]
enum NativeSource {
    Principal,
    Program { owner_program: [u8; 32], seed: Vec<u8>, source_account: [u8; 32] },
}

impl NativeSource {
    fn validate(&self) -> Result<(), NativePolicyError> {
        if let Self::Program { owner_program, seed, source_account } = self {
            let program = layerx_programs_runtime::ProgramId::new(*owner_program)
                .map_err(|_| NativePolicyError::InvalidProgramAccount)?;
            if !layerx_programs_runtime::accounts::derive_program_account(program, seed)
                .is_ok_and(|account| account.matches(source_account))
            {
                return Err(NativePolicyError::InvalidProgramAccount);
            }
        }
        Ok(())
    }

    fn matches(&self, actual: &ProgramValueSource) -> bool {
        match (self, actual) {
            (Self::Principal, ProgramValueSource::Principal) => true,
            (Self::Program { owner_program, seed, source_account },
             ProgramValueSource::Program { owner_program: owner, seed: actual_seed, source_account: source }) => {
                owner_program == owner && seed == actual_seed && source_account == source
            }
            _ => false,
        }
    }
}

#[derive(Clone, Copy, Debug, Serialize, Deserialize, Eq, PartialEq)]
enum Effect {
    Permit,
    Deny,
}

#[derive(Clone, Debug, Serialize, Deserialize, Eq, PartialEq)]
#[serde(tag = "kind", deny_unknown_fields)]
enum Scope {
    Operation,
    Spend {
        source: NativeSource,
        asset: [u8; 32],
        destination: [u8; 32],
        maximum_amount: String,
    },
    ExitRoute {
        owner_program: [u8; 32],
        seed: Vec<u8>,
        source_account: [u8; 32],
        asset: [u8; 32],
        destination: [u8; 32],
    },
}

#[derive(Clone, Debug, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
struct Rule {
    id: String,
    effect: Effect,
    activity: NativeActivity,
    scope: Scope,
    required_approval: bool,
    purpose_commitment: Option<[u8; 32]>,
}

#[derive(Clone, Debug, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
struct Source {
    schema: String,
    version: String,
    network_id: u32,
    step_limit: u64,
    rules: Vec<Rule>,
}

#[derive(Clone)]
pub(crate) struct NativeProgramPolicy {
    source: Arc<[u8]>,
    parsed: Source,
}

#[derive(Clone, Copy, Debug, Serialize, Deserialize, Eq, PartialEq)]
pub(crate) enum NativeLocalOutcome {
    Permitted,
    ApprovalRequired,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub(crate) struct NativeLocalDecision {
    pub outcome: NativeLocalOutcome,
    pub policy_version: String,
    pub policy_source_digest: [u8; 32],
    pub canonical_digest: [u8; 32],
    pub activity: ActivityType,
    pub matched_rules: Vec<String>,
}

pub(crate) struct NativeProgramIntent {
    activity: ActivityType,
    network_id: u32,
    canonical_digest: [u8; 32],
    spend: Vec<ProgramSpendBound>,
    route: Option<Route>,
    purpose_commitment: Option<[u8; 32]>,
}

struct Route {
    owner_program: [u8; 32],
    seed: Vec<u8>,
    source_account: [u8; 32],
    asset: [u8; 32],
    destination: [u8; 32],
}

impl NativeProgramIntent {
    pub(crate) fn from_prepared(prepared: &Prepared) -> Result<Self, NativePolicyError> {
        use layerx_crypto::disclosure::{DisclosedNativeOperation, DisclosedProgramWindDownOperation};
        crate::prepare::verify_disclosure_binding(prepared).map_err(|_| NativePolicyError::Disclosure)?;
        let activity = prepared.envelope.activity_type();
        if activity.module() != ModuleId::Programs { return Err(NativePolicyError::Activity); }
        let semantic = crate::capability::derive_effects(&prepared.disclosure, &VerifiedInputs {
            revoke_balance: None,
        }).map_err(|_| NativePolicyError::Effects)?;
        let route = match &prepared.disclosure.native_operation {
            Some(DisclosedNativeOperation::ProgramWindDown(value)) => match &value.operation {
                DisclosedProgramWindDownOperation::Route { account, asset, destination, seed } => Some(Route {
                    owner_program: value.program_id.bytes(), seed: seed.clone(),
                    source_account: *account, asset: *asset, destination: *destination,
                }),
                _ => None,
            },
            _ => None,
        };
        Ok(Self {
            activity,
            network_id: prepared.envelope.network_id(),
            canonical_digest: Sha256::digest(&prepared.canonical_bytes).into(),
            spend: semantic.program_spend_bounds().to_vec(),
            route,
            purpose_commitment: crate::capability::binding::purpose_commitment(&prepared.disclosure),
        })
    }
}

fn canonical_amount(text: &str) -> Result<u128, NativePolicyError> {
    let amount = text.parse::<u128>().map_err(|_| NativePolicyError::Source)?;
    if amount.to_string() != text { return Err(NativePolicyError::Source); }
    Ok(amount)
}

impl NativeProgramPolicy {
    pub(crate) fn load(source: &[u8]) -> Result<Self, NativePolicyError> {
        if source.is_empty() || source.len() > MAX_SOURCE { return Err(NativePolicyError::Bound); }
        let parsed: Source = serde_json::from_slice(source).map_err(|_| NativePolicyError::Source)?;
        if parsed.schema != SOURCE_VERSION || parsed.version.is_empty() || parsed.version.len() > 255
            || parsed.version.as_bytes().contains(&0) || parsed.network_id == 0
            || parsed.rules.is_empty() || parsed.rules.len() > MAX_RULES
            || parsed.step_limit == 0 || parsed.step_limit > MAX_STEPS
            || parsed.step_limit < parsed.rules.len() as u64
        {
            return Err(NativePolicyError::Bound);
        }
        let mut ids = BTreeSet::new();
        for rule in &parsed.rules {
            if rule.id.is_empty() || rule.id.len() > 255 || rule.id.as_bytes().contains(&0)
                || !ids.insert(rule.id.clone())
                || (rule.effect == Effect::Deny && rule.required_approval)
            {
                return Err(NativePolicyError::Source);
            }
            rule.activity.value()?;
            match &rule.scope {
                Scope::Operation => {}
                Scope::Spend { source, maximum_amount, .. } => {
                    source.validate()?;
                    canonical_amount(maximum_amount)?;
                }
                Scope::ExitRoute { owner_program, seed, source_account, .. } => {
                    NativeSource::Program {
                        owner_program: *owner_program, seed: seed.clone(), source_account: *source_account,
                    }.validate()?;
                }
            }
        }
        Ok(Self { source: Arc::from(source), parsed })
    }

    pub(crate) fn source(&self) -> &[u8] { &self.source }

    pub(crate) fn evaluate_local(
        &self,
        intent: &NativeProgramIntent,
    ) -> Result<NativeLocalDecision, NativePolicyError> {
        if intent.network_id != self.parsed.network_id { return Err(NativePolicyError::Network); }
        let width = 1_usize.checked_add(intent.spend.len())
            .and_then(|width| width.checked_add(usize::from(intent.route.is_some())))
            .ok_or(NativePolicyError::Bound)?;
        if u64::try_from(width).ok().and_then(|width| width.checked_mul(self.parsed.rules.len() as u64))
            .is_none_or(|steps| steps > self.parsed.step_limit)
        {
            return Err(NativePolicyError::Bound);
        }
        let mut covered = vec![false; width];
        let mut approval = false;
        let mut matched = BTreeSet::new();
        for rule in &self.parsed.rules {
            if rule.activity.value()? != intent.activity
                || rule.purpose_commitment.is_some_and(|purpose| intent.purpose_commitment != Some(purpose))
            {
                continue;
            }
            let mut hits = Vec::new();
            match &rule.scope {
                Scope::Operation => hits.push(0),
                Scope::Spend { source, asset, destination, maximum_amount } => {
                    let maximum = canonical_amount(maximum_amount)?;
                    let mut total = 0_u128;
                    for (index, exposure) in intent.spend.iter().enumerate() {
                        if source.matches(&exposure.source) && asset == &exposure.asset
                            && destination == &exposure.destination
                        {
                            total = total.checked_add(exposure.maximum_amount).ok_or(NativePolicyError::Bound)?;
                            hits.push(index + 1);
                        }
                    }
                    if total > maximum { hits.clear(); }
                }
                Scope::ExitRoute { owner_program, seed, source_account, asset, destination } => {
                    if intent.route.as_ref().is_some_and(|route| {
                        owner_program == &route.owner_program && seed == &route.seed
                            && source_account == &route.source_account && asset == &route.asset
                            && destination == &route.destination
                    }) {
                        hits.push(width - 1);
                    }
                }
            }
            if hits.is_empty() { continue; }
            if rule.effect == Effect::Deny { return Err(NativePolicyError::ExplicitDeny); }
            matched.insert(rule.id.clone());
            approval |= rule.required_approval;
            for index in hits { covered[index] = true; }
        }
        if covered.iter().any(|covered| !covered) { return Err(NativePolicyError::NoCoverage); }
        Ok(NativeLocalDecision {
            outcome: if approval { NativeLocalOutcome::ApprovalRequired } else { NativeLocalOutcome::Permitted },
            policy_version: self.parsed.version.clone(),
            policy_source_digest: Sha256::digest(self.source.as_ref()).into(),
            canonical_digest: intent.canonical_digest,
            activity: intent.activity,
            matched_rules: matched.into_iter().collect(),
        })
    }
}
