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

#[derive(Clone, Copy, Debug, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
struct NativeEffectActivity {
    module: u16,
    ordinal: u16,
}

impl NativeEffectActivity {
    fn value(self) -> Result<ActivityType, NativePolicyError> {
        let module = ModuleId::from_u16(self.module).map_err(|_| NativePolicyError::Activity)?;
        if module == ModuleId::Programs { return Err(NativePolicyError::Activity); }
        ActivityType::new(module, self.ordinal).map_err(|_| NativePolicyError::Activity)
    }
}

#[derive(Clone, Copy, Debug, Serialize, Deserialize, Eq, PartialEq)]
enum NativeAuthorizationKind {
    SpendingLimit,
    SupplyCap,
    PerDrawMaximum,
    GrantAllowance,
}

impl NativeAuthorizationKind {
    fn value(self) -> crate::capability::AuthorizationKind {
        match self {
            Self::SpendingLimit => crate::capability::AuthorizationKind::SpendingLimit,
            Self::SupplyCap => crate::capability::AuthorizationKind::SupplyCap,
            Self::PerDrawMaximum => crate::capability::AuthorizationKind::PerDrawMaximum,
            Self::GrantAllowance => crate::capability::AuthorizationKind::GrantAllowance,
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize, Eq, PartialEq)]
#[serde(tag = "kind", deny_unknown_fields)]
enum NativeEffectScope {
    Operation,
    Transfer { from: [u8; 32], to: [u8; 32], asset: [u8; 32], maximum_amount: String },
    Issuance { account: [u8; 32], asset: [u8; 32], maximum_amount: String },
    Destruction { account: [u8; 32], asset: [u8; 32], maximum_amount: String },
    Authorization { authorization: NativeAuthorizationKind, account: [u8; 32], asset: Option<[u8; 32]>, maximum_amount: String },
    Fee { source_account: [u8; 32], asset: [u8; 32], maximum_amount: String },
}

impl NativeEffectScope {
    fn validate(&self) -> Result<(), NativePolicyError> {
        let nonzero = |value: &[u8; 32]| if *value == [0; 32] { Err(NativePolicyError::Source) } else { Ok(()) };
        match self {
            Self::Operation => {},
            Self::Transfer { from, to, asset, maximum_amount } => {
                nonzero(from)?; nonzero(to)?; nonzero(asset)?; canonical_amount(maximum_amount)?;
            },
            Self::Issuance { account, asset, maximum_amount }
            | Self::Destruction { account, asset, maximum_amount } => {
                nonzero(account)?; nonzero(asset)?; canonical_amount(maximum_amount)?;
            },
            Self::Authorization { account, asset, maximum_amount, .. } => {
                nonzero(account)?;
                if let Some(asset) = asset { nonzero(asset)?; }
                canonical_amount(maximum_amount)?;
            },
            Self::Fee { source_account, asset, maximum_amount } => {
                nonzero(source_account)?; nonzero(asset)?; canonical_amount(maximum_amount)?;
            },
        }
        Ok(())
    }

    fn matching_amount(&self, actual: &crate::capability::Effect) -> Option<u128> {
        use crate::capability::Effect as Actual;
        match (self, actual) {
            (Self::Transfer { from, to, asset, .. }, Actual::Transfer { from: actual_from, to: actual_to, asset: actual_asset, amount })
                if from == actual_from && to == actual_to && asset == actual_asset => Some(*amount),
            (Self::Issuance { account, asset, .. }, Actual::Issuance { account: actual_account, asset: actual_asset, amount })
                if account == actual_account && asset == actual_asset => Some(*amount),
            (Self::Destruction { account, asset, .. }, Actual::Destruction { account: actual_account, asset: actual_asset, amount })
                if account == actual_account && asset == actual_asset => Some(*amount),
            (Self::Authorization { authorization, account, asset, .. }, Actual::Authorization { kind, account: actual_account, asset: actual_asset, amount })
                if authorization.value() == *kind && account == actual_account && asset == actual_asset => Some(*amount),
            _ => None,
        }
    }

    fn maximum(&self) -> Result<Option<u128>, NativePolicyError> {
        match self {
            Self::Operation => Ok(None),
            Self::Transfer { maximum_amount, .. } | Self::Issuance { maximum_amount, .. }
            | Self::Destruction { maximum_amount, .. } | Self::Authorization { maximum_amount, .. }
            | Self::Fee { maximum_amount, .. } => canonical_amount(maximum_amount).map(Some),
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
struct NativeEffectRule {
    id: String,
    effect: Effect,
    activity: NativeEffectActivity,
    scope: NativeEffectScope,
    required_approval: bool,
    purpose_commitment: Option<[u8; 32]>,
}

#[derive(Clone, Debug, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
struct NativeEffectSource {
    schema: String,
    version: String,
    network_id: u32,
    step_limit: u64,
    rules: Vec<NativeEffectRule>,
}

#[derive(Clone)]
pub(crate) struct NativeEffectPolicy {
    source: Arc<[u8]>,
    parsed: NativeEffectSource,
}

pub(crate) struct NativeEffectIntent {
    activity: ActivityType,
    network_id: u32,
    canonical_digest: [u8; 32],
    effects: Vec<crate::capability::Effect>,
    fee: Option<crate::capability::binding::NativeEffectFeeV1>,
    purpose_commitment: [u8; 32],
}

impl NativeEffectIntent {
    pub(crate) fn from_prepared(
        prepared: &Prepared,
        fee_policy: &layerx_client::payments::CommittedSnapshot<layerx_client::payments::NativeFeePolicy>,
        purpose_commitment: [u8; 32],
    ) -> Result<Self, NativePolicyError> {
        crate::prepare::verify_disclosure_binding(prepared).map_err(|_| NativePolicyError::Disclosure)?;
        if prepared.envelope.activity_type().module() == ModuleId::Programs {
            return Err(NativePolicyError::Activity);
        }
        if crate::capability::binding::purpose_commitment(&prepared.disclosure)
            .is_some_and(|value| value != purpose_commitment)
        {
            return Err(NativePolicyError::Disclosure);
        }
        let semantic = crate::capability::derive_native_effects(&prepared.disclosure, &VerifiedInputs::default())
            .map_err(|_| NativePolicyError::Effects)?;
        if !semantic.program_spend_bounds().is_empty() { return Err(NativePolicyError::Effects); }
        let fee = crate::capability::binding::native_effect_fee(prepared, fee_policy, prepared.observed_head_sequence)
            .map_err(|_| NativePolicyError::Disclosure)?;
        Ok(Self {
            activity: prepared.envelope.activity_type(), network_id: prepared.envelope.network_id(),
            canonical_digest: Sha256::digest(&prepared.canonical_bytes).into(),
            effects: semantic.effects().to_vec(), fee, purpose_commitment,
        })
    }
}

impl NativeEffectPolicy {
    pub(crate) const SCHEMA: &'static str = "layerx.native-effect-policy.v1";

    pub(crate) fn load(source: &[u8]) -> Result<Self, NativePolicyError> {
        if source.is_empty() || source.len() > MAX_SOURCE { return Err(NativePolicyError::Bound); }
        let parsed: NativeEffectSource = serde_json::from_slice(source).map_err(|_| NativePolicyError::Source)?;
        if parsed.schema != Self::SCHEMA || parsed.version.is_empty() || parsed.version.len() > 255
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
                || !ids.insert(rule.id.clone()) || (rule.effect == Effect::Deny && rule.required_approval)
            {
                return Err(NativePolicyError::Source);
            }
            rule.activity.value()?;
            rule.scope.validate()?;
        }
        Ok(Self { source: Arc::from(source), parsed })
    }

    pub(crate) fn source(&self) -> &[u8] { &self.source }

    pub(crate) fn evaluate_local(&self, intent: &NativeEffectIntent) -> Result<NativeLocalDecision, NativePolicyError> {
        if intent.network_id != self.parsed.network_id { return Err(NativePolicyError::Network); }
        let width = 1_usize.checked_add(intent.effects.len())
            .and_then(|width| width.checked_add(usize::from(intent.fee.is_some()))).ok_or(NativePolicyError::Bound)?;
        if u64::try_from(width).ok().and_then(|width| width.checked_mul(self.parsed.rules.len() as u64))
            .is_none_or(|steps| steps > self.parsed.step_limit)
        {
            return Err(NativePolicyError::Bound);
        }
        let mut covered = vec![false; width];
        let mut approval = false;
        let mut matched = BTreeSet::new();
        let mut ordered: Vec<_> = self.parsed.rules.iter().collect();
        ordered.sort_by(|left, right| left.id.cmp(&right.id));
        for rule in ordered {
            if rule.activity.value()? != intent.activity
                || rule.purpose_commitment.is_some_and(|purpose| purpose != intent.purpose_commitment)
            { continue; }
            let mut hits = Vec::new();
            match &rule.scope {
                NativeEffectScope::Operation => hits.push(0),
                NativeEffectScope::Fee { source_account, asset, maximum_amount } => {
                    let maximum = canonical_amount(maximum_amount)?;
                    if intent.fee.as_ref().is_some_and(|fee| fee.source_account == *source_account
                        && fee.asset == *asset && fee.maximum_amount <= maximum)
                    { hits.push(width - 1); }
                },
                scope => {
                    let maximum = scope.maximum()?.ok_or(NativePolicyError::Source)?;
                    let mut total = 0_u128;
                    for (index, actual) in intent.effects.iter().enumerate() {
                        if let Some(amount) = scope.matching_amount(actual) {
                            total = total.checked_add(amount).ok_or(NativePolicyError::Bound)?;
                            hits.push(index + 1);
                        }
                    }
                    if total > maximum { hits.clear(); }
                },
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
            policy_version: self.parsed.version.clone(), policy_source_digest: Sha256::digest(self.source.as_ref()).into(),
            canonical_digest: intent.canonical_digest, activity: intent.activity,
            matched_rules: matched.into_iter().collect(),
        })
    }
}

#[cfg(test)]
mod native_effect_policy_tests {
    use super::*;

    fn source() -> serde_json::Value {
        serde_json::json!({
            "schema": NativeEffectPolicy::SCHEMA, "version": "effect-v1", "network_id": 17,
            "step_limit": 100, "rules": [{ "id": "review", "effect": "Permit",
                "activity": { "module": 1, "ordinal": 5 }, "scope": { "kind": "Operation" },
                "required_approval": true, "purpose_commitment": null }],
        })
    }

    fn bytes(value: &serde_json::Value) -> Vec<u8> {
        serde_json::to_vec(value).expect("real configured JSON source")
    }

    #[test]
    fn native_effect_policy_profile_is_closed_and_separate_from_programs() {
        let encoded = bytes(&source());
        let policy = NativeEffectPolicy::load(&encoded).expect("non-Programs source");
        assert_eq!(policy.source(), encoded);
        assert!(policy.parsed.rules[0].required_approval);
        assert!(NativeProgramPolicy::load(&encoded).is_err());
        let mut changed = source(); changed["schema"] = serde_json::json!(SOURCE_VERSION);
        assert!(NativeEffectPolicy::load(&bytes(&changed)).is_err());
        let mut changed = source(); changed["rules"][0]["activity"]["module"] = serde_json::json!(9);
        assert!(NativeEffectPolicy::load(&bytes(&changed)).is_err());
        let mut changed = source(); changed["rules"][0]["effect"] = serde_json::json!("Deny");
        assert!(NativeEffectPolicy::load(&bytes(&changed)).is_err());
        let mut changed = source(); changed["unknown_authority"] = serde_json::json!(true);
        assert!(NativeEffectPolicy::load(&bytes(&changed)).is_err());
        let mut changed = source(); changed["rules"][0]["scope"] = serde_json::json!({ "kind": "Transfer", "maximum_amount": "10" });
        assert!(NativeEffectPolicy::load(&bytes(&changed)).is_err());
    }

    #[test]
    fn native_effect_policy_scope_preserves_exact_account_refs_and_canonical_amount() {
        let mut value = source();
        value["rules"][0]["scope"] = serde_json::json!({ "kind": "Transfer",
            "from": [1; 32].to_vec(), "to": [2; 32].to_vec(), "asset": [3; 32].to_vec(),
            "maximum_amount": "40" });
        let policy = NativeEffectPolicy::load(&bytes(&value)).expect("complete scope");
        let scope = &policy.parsed.rules[0].scope;
        assert_eq!(scope.matching_amount(&crate::capability::Effect::Transfer {
            from: [1; 32], to: [2; 32], asset: [3; 32], amount: 40 }), Some(40));
        assert_eq!(scope.matching_amount(&crate::capability::Effect::Transfer {
            from: [4; 32], to: [2; 32], asset: [3; 32], amount: 40 }), None);
        value["rules"][0]["scope"]["maximum_amount"] = serde_json::json!("040");
        assert!(NativeEffectPolicy::load(&bytes(&value)).is_err());
        value["rules"][0]["scope"]["maximum_amount"] = serde_json::json!("40");
        value["rules"][0]["scope"]["from"] = serde_json::json!([0; 32].to_vec());
        assert!(NativeEffectPolicy::load(&bytes(&value)).is_err());
    }
}
