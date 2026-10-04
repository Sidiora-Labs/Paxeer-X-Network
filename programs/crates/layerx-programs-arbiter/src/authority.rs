use std::collections::BTreeMap;

use layerx_client::evidence::VerifiedAdmissionPrestate;
use layerx_programs_runtime::CapabilitySet;
use layerx_proof::inclusion::VerifiedBatchHeader;
use layerx_proof::merkle::{verify_path, Proof};
use layerx_proof::receipt::VerifiedReceipt;
use layerx_proof::state_range::{ModuleRangeWitness, MAX_UNIVERSAL_LEAVES};
use layerx_proof::state_witness::StateWitness;
use sha2::{Digest, Sha256};

use crate::ReplayError;

#[derive(Clone, Debug)]
pub struct VerifiedReplayAuthority {
    canonical_bytes: Vec<u8>,
    principal: [u8; 32],
    payer: [u8; 32],
    activity_binding: [u8; 32],
    capabilities: CapabilitySet,
}

impl VerifiedReplayAuthority {
    pub fn derive(
        receipt: &VerifiedReceipt,
        admission: &VerifiedAdmissionPrestate,
        header: &VerifiedBatchHeader,
        receipt_proof: &Proof,
    ) -> Result<Self, ReplayError> {
        Self::derive_call(receipt, admission, header, receipt_proof, true)
    }

    pub fn derive_program_call(
        receipt: &VerifiedReceipt,
        admission: &VerifiedAdmissionPrestate,
        header: &VerifiedBatchHeader,
        receipt_proof: &Proof,
    ) -> Result<Self, ReplayError> {
        Self::derive_call(receipt, admission, header, receipt_proof, false)
    }

    fn derive_call(
        receipt: &VerifiedReceipt,
        admission: &VerifiedAdmissionPrestate,
        header: &VerifiedBatchHeader,
        receipt_proof: &Proof,
        replay_profile: bool,
    ) -> Result<Self, ReplayError> {
        let protocol = receipt.receipt().protocol().ok_or(ReplayError::Receipt)?;
        let batch = header.header();
        let count = batch
            .last_sequence()
            .checked_sub(batch.first_sequence())
            .ok_or(ReplayError::Receipt)?;
        if count == 0
            || count > 64
            || u64::from(receipt_proof.leaf_count()) != count + 1
            || u64::from(receipt_proof.leaf_index()) >= count
            || batch
                .first_sequence()
                .checked_add(u64::from(receipt_proof.leaf_index()))
                != Some(protocol.global_sequence())
            || batch.network_id() != admission.network_id()
            || protocol.timestamp() != batch.timestamp_ms()
            || protocol.activity_id() != admission.activity_id()
            || protocol.global_sequence() != admission.execution_sequence()
            || protocol.previous_state_root() != admission.state_root()
            || receipt.evidence().receipt_digest() != Some(admission.receipt_digest())
            || protocol.protocol_version() != 3
            || protocol.module_id() != 9
            || protocol.operation() != 3
        {
            return Err(ReplayError::Receipt);
        }
        verify_path(
            receipt.canonical_bytes(),
            receipt_proof,
            &batch.receipt_merkle_root(),
        )
        .map_err(|_| ReplayError::Receipt)?;
        let activity = admission.activity();
        let bounds = activity.timestamp_bound();
        if bounds.not_after == u64::MAX
            || bounds.not_before > batch.timestamp_ms()
            || batch.timestamp_ms() > bounds.not_after
        {
            return Err(ReplayError::Authority);
        }
        let actor = did_id(activity.actor_did())?;
        let key: [u8; 32] = activity
            .authority()
            .try_into()
            .map_err(|_| ReplayError::Authority)?;
        let governance = admission.governance_records();
        let identity = governance
            .get(actor.as_slice())
            .ok_or(ReplayError::Authority)?;
        if identity.len() != 223 || &identity[..5] != b"LXGI1" || identity[5..37] != actor {
            return Err(ReplayError::Authority);
        }
        let revocation = u64_at(identity, 69)?;
        let mut owner = identity[37..69] == key;
        if identity[111..143].iter().any(|b| *b != 0)
            && batch.timestamp_ms() <= u64_at(identity, 151)?
            && identity[111..143] == key
        {
            owner = true;
        }
        let mut rotation_key = vec![10];
        rotation_key.extend_from_slice(&actor);
        if let Some(rotation) = governance.get(&rotation_key) {
            if rotation.len() != 141
                || &rotation[..5] != b"LXOR1"
                || rotation[5..37] != actor
                || rotation[69..101] != identity[37..69]
                || rotation[37..69] == rotation[69..101]
                || u64_at(rotation, 101)? == 0
                || u64_at(rotation, 101)? > revocation
                || rotation[109..141].iter().all(|b| *b == 0)
            {
                return Err(ReplayError::Authority);
            }
            if protocol.global_sequence() < u64_at(rotation, 101)? && rotation[37..69] == key {
                owner = true;
            }
        }
        let (mask, minimum, maximum) = envelope(admission, batch.epoch())?;
        let mut grant = if owner {
            Grant::owner(
                actor,
                key,
                mask,
                minimum,
                maximum,
                bounds.not_before,
                bounds
                    .not_after
                    .checked_add(1)
                    .ok_or(ReplayError::Authority)?,
                revocation,
            )
        } else {
            let mut selected = None;
            for (record_key, bytes) in governance {
                if record_key.len() != 33 || record_key[0] != 5 {
                    continue;
                }
                let candidate = Grant::decode(bytes)?;
                if candidate.key != key || candidate.grantor != actor {
                    continue;
                }
                if candidate.id != record_key[1..] || selected.is_some() {
                    return Err(ReplayError::Authority);
                }
                selected = Some(candidate);
            }
            selected.ok_or(ReplayError::Authority)?
        };
        if !owner {
            grant.overlay(governance)?;
        }
        grant.validate()?;
        if grant.grantor != actor
            || grant.grantee != actor
            || grant.authentication_only
            || grant.revoked
            || grant.revocation != revocation
            || batch.timestamp_ms() < grant.not_before
            || batch.timestamp_ms() >= grant.not_after
            || grant.scope.mask & (1 << 9) == 0
            || grant.scope.minimum > 3
            || grant.scope.maximum < 3
            || grant.scope.mask & !mask != 0
            || grant.scope.minimum < minimum
            || grant.scope.maximum > maximum
            || (grant.kind == 7 && grant.scope.approvals.len() < usize::from(grant.scope.threshold))
            || (grant.kind == 8
                && (protocol.global_sequence() < grant.scope.earliest_sequence
                    || batch.timestamp_ms() < grant.scope.earliest_timestamp))
        {
            return Err(ReplayError::Authority);
        }
        let outcome = protocol.program_outcome().ok_or(ReplayError::Receipt)?;
        let records = admission.legacy().legacy().program_records();
        let mut fee_key = b"progfee/history/v1/".to_vec();
        fee_key.extend_from_slice(&outcome.fee_schedule_version().to_be_bytes());
        let fee = records.get(&fee_key).ok_or(ReplayError::Authority)?;
        if fee.len() != 217
            || &fee[..5] != b"LXFR1"
            || u32_at(fee, 5)? != outcome.fee_schedule_version()
            || u64_at(fee, 145)? == 0
            || u64_at(fee, 145)? > batch.batch_number()
        {
            return Err(ReplayError::Authority);
        }
        let asset: [u8; 32] = fee[65..97].try_into().map_err(|_| ReplayError::Authority)?;
        let mut prices = [0; 7];
        for (i, price) in prices.iter_mut().enumerate() {
            *price = u64_at(fee, 9 + 8 * i)?;
        }
        if asset == [0; 32]
            || prices.contains(&0)
            || prices != outcome.fee_schedule_prices()
            || (outcome.occupancy_asset_id() != [0; 32] && outcome.occupancy_asset_id() != asset)
        {
            return Err(ReplayError::Authority);
        }
        let mut meter_key = b"progmet/history/v1/".to_vec();
        meter_key.extend_from_slice(&outcome.metering_schedule_version().to_be_bytes());
        let meter = records.get(&meter_key).ok_or(ReplayError::Authority)?;
        if meter.len() != 122
            || &meter[..5] != b"LXMR1"
            || u32_at(meter, 5)? != outcome.metering_schedule_version()
            || u64_at(meter, 81)? == 0
            || u64_at(meter, 81)? > batch.batch_number()
            || !matches!(meter[89], 1 | 2)
            || meter[90..122].iter().all(|b| *b == 0)
        {
            return Err(ReplayError::Authority);
        }
        let mut coefficients = [0; 9];
        for (i, coefficient) in coefficients.iter_mut().enumerate() {
            *coefficient = u64_at(meter, 9 + 8 * i)?;
        }
        if coefficients.contains(&0)
            || (outcome.metering_schedule_version() == 1
                && (coefficients != [1, 1, 1, 1, 1, 8, 8, 64, 8]
                    || u64_at(meter, 81)? != 1
                    || meter[89] != 1))
            || (outcome.metering_schedule_version() > 1
                && (u64_at(meter, 81)? <= 1 || meter[89] != 2))
        {
            return Err(ReplayError::Authority);
        }
        let mut payment = None;
        for account in admission.legacy().legacy().all_accounts().values() {
            if !matches!(account.kind, 1 | 14) || account.asset_id() != asset {
                continue;
            }
            let name = &account.name;
            let did = if name.starts_with(b"agent:") && name.ends_with(b":main") && name.len() > 11
            {
                &name[6..name.len() - 5]
            } else if name.starts_with(b"agent:")
                && name.len() > 77
                && &name[name.len() - 71..name.len() - 64] == b":asset:"
            {
                &name[6..name.len() - 71]
            } else {
                return Err(ReplayError::Authority);
            };
            if did_id(did)? != actor {
                continue;
            }
            if payment.is_some() {
                return Err(ReplayError::Authority);
            }
            payment = Some(account);
        }
        let payment = payment.ok_or(ReplayError::Authority)?;
        if payment.frozen || payment.balance() < activity.fee_limit() {
            return Err(ReplayError::Authority);
        }
        grant.check_fee(
            governance,
            asset,
            activity.fee_limit(),
            batch.timestamp_ms(),
        )?;
        let capabilities = if replay_profile {
            signed_capabilities(activity.payload())?
        } else {
            call_capabilities(activity.payload())?
        };
        let payer = if protocol.module_version() == 4 {
            payment.account_id
        } else {
            actor
        };
        let signed =
            layerx_wire::activity::encode_signed(activity).map_err(|_| ReplayError::Authority)?;
        let mut bytes = b"LXP/program-replay-authority/v1\0".to_vec();
        vector(&mut bytes, &signed)?;
        bytes.extend_from_slice(&actor);
        bytes.extend_from_slice(&grant.grantor);
        put_u64(&mut bytes, u64::from(grant.kind));
        bytes.extend_from_slice(&key);
        let mut hash_input = vec![grant.kind];
        hash_input.extend_from_slice(&grant.id);
        hash_input.extend_from_slice(&key);
        bytes.extend_from_slice(&authority_hash(&hash_input));
        bytes.extend_from_slice(&grant.id);
        put_u64(&mut bytes, 1);
        grant.scope.encode_native(&mut bytes);
        bytes.extend_from_slice(&admission.activity_id());
        bytes.extend_from_slice(&payer);
        bytes.extend_from_slice(&payment.balance().to_be_bytes());
        bytes.extend_from_slice(&activity.fee_limit().to_be_bytes());
        put_u64(&mut bytes, u64::from(outcome.fee_schedule_version()));
        put_u64(&mut bytes, u64::from(outcome.metering_schedule_version()));
        put_u64(&mut bytes, u64::from(protocol.parameter_version()));
        for value in coefficients {
            put_u64(&mut bytes, value);
        }
        for value in prices {
            put_u64(&mut bytes, value);
        }
        put_u64(&mut bytes, u64::from(protocol.protocol_version()));
        put_u64(&mut bytes, batch.batch_number());
        put_u64(&mut bytes, batch.epoch());
        if bytes.len() > 1_048_576 {
            return Err(ReplayError::Bounds);
        }
        Ok(Self {
            canonical_bytes: bytes,
            principal: actor,
            payer,
            activity_binding: admission.activity_id(),
            capabilities,
        })
    }

    pub fn verify_native(&self, bytes: &[u8]) -> Result<(), ReplayError> {
        if self.canonical_bytes != bytes {
            return Err(ReplayError::Authority);
        }
        Ok(())
    }
    pub fn canonical_bytes(&self) -> &[u8] {
        &self.canonical_bytes
    }
    pub const fn principal(&self) -> [u8; 32] {
        self.principal
    }
    pub const fn payer(&self) -> [u8; 32] {
        self.payer
    }
    pub const fn activity_binding(&self) -> [u8; 32] {
        self.activity_binding
    }
    pub fn capabilities(&self) -> &CapabilitySet {
        &self.capabilities
    }
}

#[derive(Clone, Debug, Default)]
struct Scope {
    mask: u64,
    minimum: u16,
    maximum: u16,
    asset: [u8; 32],
    per_activity: u128,
    total: u128,
    spent: u128,
    period: u64,
    per_period: u128,
    period_spent: u128,
    period_start: u64,
    purpose: [u8; 32],
    earliest_sequence: u64,
    earliest_timestamp: u64,
    threshold: u8,
    signers: Vec<[u8; 32]>,
    approvals: Vec<[u8; 32]>,
}
impl Scope {
    fn encode_native(&self, bytes: &mut Vec<u8>) {
        put_u64(bytes, self.mask);
        put_u64(bytes, u64::from(self.minimum));
        put_u64(bytes, u64::from(self.maximum));
        bytes.extend_from_slice(&self.asset);
        for value in [self.per_activity, self.total, self.spent] {
            bytes.extend_from_slice(&value.to_be_bytes());
        }
        put_u64(bytes, self.period);
        for value in [self.per_period, self.period_spent] {
            bytes.extend_from_slice(&value.to_be_bytes());
        }
        put_u64(bytes, self.period_start);
        bytes.extend_from_slice(&self.purpose);
        put_u64(bytes, self.earliest_sequence);
        put_u64(bytes, self.earliest_timestamp);
        put_u64(bytes, u64::from(self.threshold));
        put_u64(bytes, self.signers.len() as u64);
        for signer in &self.signers {
            bytes.extend_from_slice(signer);
        }
        put_u64(bytes, self.approvals.len() as u64);
        for approval in &self.approvals {
            bytes.extend_from_slice(approval);
        }
    }
}
#[derive(Clone, Debug)]
struct Grant {
    grantor: [u8; 32],
    grantee: [u8; 32],
    kind: u8,
    key: [u8; 32],
    scope: Scope,
    not_before: u64,
    not_after: u64,
    revocation: u64,
    revoked: bool,
    revoked_at: u64,
    authentication_only: bool,
    fee: Option<Scope>,
    id: [u8; 32],
}
impl Grant {
    fn owner(
        actor: [u8; 32],
        key: [u8; 32],
        mask: u64,
        minimum: u16,
        maximum: u16,
        not_before: u64,
        not_after: u64,
        revocation: u64,
    ) -> Self {
        let scope = Scope {
            mask,
            minimum,
            maximum,
            ..Scope::default()
        };
        let mut encoded = vec![0, 1, 0x20, 1, 1];
        for value in [actor, actor] {
            encoded.extend_from_slice(&32_u32.to_be_bytes());
            encoded.extend_from_slice(&value);
        }
        encoded.push(1);
        encoded.extend_from_slice(&32_u32.to_be_bytes());
        encoded.extend_from_slice(&key);
        put_u64(&mut encoded, mask);
        encoded.extend_from_slice(&minimum.to_be_bytes());
        encoded.extend_from_slice(&maximum.to_be_bytes());
        encoded.extend_from_slice(&32_u32.to_be_bytes());
        encoded.extend_from_slice(&[0; 32]);
        encoded.extend_from_slice(&[0; 48]);
        put_u64(&mut encoded, 0);
        encoded.extend_from_slice(&[0; 32]);
        put_u64(&mut encoded, 0);
        encoded.extend_from_slice(&32_u32.to_be_bytes());
        encoded.extend_from_slice(&[0; 32]);
        for value in [not_before, not_after, revocation] {
            put_u64(&mut encoded, value);
        }
        encoded.push(0);
        put_u64(&mut encoded, 0);
        encoded.extend_from_slice(&64_u32.to_be_bytes());
        encoded.extend_from_slice(&[0; 64]);
        Self {
            grantor: actor,
            grantee: actor,
            kind: 1,
            key,
            scope,
            not_before,
            not_after,
            revocation,
            revoked: false,
            revoked_at: 0,
            authentication_only: false,
            fee: None,
            id: authority_hash(&encoded),
        }
    }
    fn decode(bytes: &[u8]) -> Result<Self, ReplayError> {
        if bytes.len() > 1024 {
            return Err(ReplayError::Bounds);
        }
        let mut reader = Reader(bytes);
        if !matches!(reader.u16()?, 1 | 2 | 3) || reader.u16()? != 0x2001 {
            return Err(ReplayError::Authority);
        }
        let version = reader.u8()?;
        if !(1..=4).contains(&version) {
            return Err(ReplayError::Authority);
        }
        let grantor = reader.fixed_vector()?;
        let grantee = reader.fixed_vector()?;
        let kind = reader.u8()?;
        let key = reader.fixed_vector()?;
        let mut scope = Scope {
            mask: reader.u64()?,
            minimum: reader.u16()?,
            maximum: reader.u16()?,
            asset: reader.fixed_vector()?,
            per_activity: reader.u128()?,
            total: reader.u128()?,
            spent: reader.u128()?,
            period: reader.u64()?,
            per_period: reader.u128()?,
            period_spent: reader.u128()?,
            period_start: reader.u64()?,
            purpose: reader.fixed_vector()?,
            ..Scope::default()
        };
        let not_before = reader.u64()?;
        let not_after = reader.u64()?;
        let revocation = reader.u64()?;
        let revoked = match reader.u8()? {
            0 => false,
            1 => true,
            _ => return Err(ReplayError::Authority),
        };
        let revoked_at = reader.u64()?;
        let _: [u8; 64] = reader.fixed_vector()?;
        let fee = if version == 2 {
            Some(Scope {
                asset: reader.fixed_vector()?,
                per_activity: reader.u128()?,
                total: reader.u128()?,
                spent: reader.u128()?,
                period: reader.u64()?,
                per_period: reader.u128()?,
                period_spent: reader.u128()?,
                period_start: reader.u64()?,
                ..Scope::default()
            })
        } else {
            None
        };
        let authentication_only = version == 3;
        if authentication_only && reader.u8()? != 1 {
            return Err(ReplayError::Authority);
        }
        if version == 4 {
            scope.threshold = reader.u8()?;
            let signers = reader.u8()?;
            if signers > 8 {
                return Err(ReplayError::Bounds);
            }
            let encoded = reader.vector(256)?;
            if encoded.len() != usize::from(signers) * 32 {
                return Err(ReplayError::Authority);
            }
            scope.signers = encoded
                .chunks_exact(32)
                .map(|v| v.try_into().map_err(|_| ReplayError::Authority))
                .collect::<Result<_, _>>()?;
            let approvals = reader.u8()?;
            if approvals > 8 {
                return Err(ReplayError::Bounds);
            }
            let encoded = reader.vector(256)?;
            if encoded.len() != usize::from(approvals) * 32 {
                return Err(ReplayError::Authority);
            }
            scope.approvals = encoded
                .chunks_exact(32)
                .map(|v| v.try_into().map_err(|_| ReplayError::Authority))
                .collect::<Result<_, _>>()?;
            scope.earliest_sequence = reader.u64()?;
            scope.earliest_timestamp = reader.u64()?;
        }
        if !reader.0.is_empty()
            || matches!(kind, 7 | 8) != (version == 4)
            || (version == 3 && kind != 2)
        {
            return Err(ReplayError::Authority);
        }
        let mut canonical = bytes.to_vec();
        canonical[..2].copy_from_slice(&1_u16.to_be_bytes());
        let grant = Self {
            grantor,
            grantee,
            kind,
            key,
            scope,
            not_before,
            not_after,
            revocation,
            revoked,
            revoked_at,
            authentication_only,
            fee,
            id: authority_hash(&canonical),
        };
        grant.validate()?;
        Ok(grant)
    }
    fn validate(&self) -> Result<(), ReplayError> {
        let s = &self.scope;
        if !(1..=8).contains(&self.kind)
            || self.not_after == 0
            || self.not_after <= self.not_before
            || self.grantee == [0; 32]
            || self.key == [0; 32]
            || (!self.authentication_only && s.mask == 0)
            || s.minimum > s.maximum
        {
            return Err(ReplayError::Authority);
        }
        if self.kind == 7 {
            if s.earliest_sequence != 0
                || s.earliest_timestamp != 0
                || s.signers.is_empty()
                || s.signers.len() > 8
                || s.threshold == 0
                || usize::from(s.threshold) > s.signers.len()
                || s.approvals.len() > s.signers.len()
            {
                return Err(ReplayError::Authority);
            }
            for (i, signer) in s.signers.iter().enumerate() {
                if *signer == [0; 32] || s.signers[..i].contains(signer) {
                    return Err(ReplayError::Authority);
                }
            }
            for (i, approval) in s.approvals.iter().enumerate() {
                if !s.signers.contains(approval) || s.approvals[..i].contains(approval) {
                    return Err(ReplayError::Authority);
                }
            }
        } else {
            if s.threshold != 0 || !s.signers.is_empty() || !s.approvals.is_empty() {
                return Err(ReplayError::Authority);
            }
            if self.kind == 8 {
                if s.earliest_sequence == 0 && s.earliest_timestamp == 0 {
                    return Err(ReplayError::Authority);
                }
            } else if s.earliest_sequence != 0 || s.earliest_timestamp != 0 {
                return Err(ReplayError::Authority);
            }
        }
        if matches!(self.kind, 3 | 4)
            && (s.asset == [0; 32]
                || s.per_activity == 0
                || s.purpose == [0; 32]
                || self.revocation == 0
                || (s.total == 0 && (s.period == 0 || s.per_period == 0)))
        {
            return Err(ReplayError::Authority);
        }
        if self.authentication_only
            && (self.kind != 2
                || self.fee.is_some()
                || self.grantor == [0; 32]
                || self.revocation == 0
                || self.grantor != self.grantee
                || s.mask != 0
                || s.minimum != 0
                || s.maximum != 0
                || s.asset != [0; 32]
                || s.per_activity != 0
                || s.total != 0
                || s.spent != 0
                || s.period != 0
                || s.per_period != 0
                || s.period_spent != 0
                || s.period_start != 0
                || s.purpose != [0; 32])
        {
            return Err(ReplayError::Authority);
        }
        if let Some(fee) = &self.fee {
            if !matches!(self.kind, 2 | 3 | 4)
                || fee.asset == [0; 32]
                || fee.per_activity == 0
                || fee.total == 0
                || fee.per_activity > fee.total
                || fee.spent > fee.total
                || fee.period_spent > fee.spent
                || (fee.period == 0 && (fee.per_period != 0 || fee.period_start != 0))
                || (fee.period != 0
                    && (fee.per_period == 0
                        || fee.period_start < self.not_before
                        || (fee.period_start - self.not_before) % fee.period != 0
                        || fee.per_activity > fee.per_period
                        || fee.period_spent > fee.per_period))
            {
                return Err(ReplayError::Authority);
            }
        }
        Ok(())
    }
    fn overlay(&mut self, records: &BTreeMap<Vec<u8>, Vec<u8>>) -> Result<(), ReplayError> {
        for tag in [6, 7, 8] {
            let mut key = vec![tag];
            key.extend_from_slice(&self.id);
            if let Some(bytes) = records.get(&key) {
                if bytes.get(..32) != Some(self.id.as_slice()) {
                    return Err(ReplayError::Authority);
                }
                if tag == 6 {
                    if bytes.len() != 41 {
                        return Err(ReplayError::Authority);
                    }
                    self.revoked = true;
                    self.revoked_at = u64_at(bytes, 33)?;
                } else {
                    if bytes.len() != 72 {
                        return Err(ReplayError::Authority);
                    }
                    let scope = if tag == 7 {
                        &mut self.scope
                    } else {
                        self.fee.as_mut().ok_or(ReplayError::Authority)?
                    };
                    scope.spent = u128::from_be_bytes(
                        bytes[32..48]
                            .try_into()
                            .map_err(|_| ReplayError::Authority)?,
                    );
                    scope.period_spent = u128::from_be_bytes(
                        bytes[48..64]
                            .try_into()
                            .map_err(|_| ReplayError::Authority)?,
                    );
                    scope.period_start = u64_at(bytes, 64)?;
                }
            }
        }
        Ok(())
    }
    fn check_fee(
        &self,
        records: &BTreeMap<Vec<u8>, Vec<u8>>,
        asset: [u8; 32],
        amount: u128,
        timestamp: u64,
    ) -> Result<(), ReplayError> {
        let mut policy = [0; 32];
        policy[..28].copy_from_slice(b"native-fee-authority-version\0");
        let enforced = if let Some(value) = records.get(policy.as_slice()) {
            if value.len() != 32 || value[..31].iter().any(|b| *b != 0) || value[31] != 2 {
                return Err(ReplayError::Authority);
            }
            true
        } else {
            false
        };
        if !enforced || !matches!(self.kind, 2 | 3 | 4) || amount == 0 {
            return Ok(());
        }
        let fee = self.fee.as_ref().ok_or(ReplayError::Authority)?;
        if fee.asset != asset
            || amount > fee.per_activity
            || fee
                .spent
                .checked_add(amount)
                .ok_or(ReplayError::Authority)?
                > fee.total
        {
            return Err(ReplayError::Authority);
        }
        let period_spent = if fee.period == 0 {
            fee.period_spent
        } else {
            if timestamp < fee.period_start {
                return Err(ReplayError::Authority);
            }
            if (timestamp - fee.period_start) / fee.period != 0 {
                0
            } else {
                fee.period_spent
            }
        };
        let updated = period_spent
            .checked_add(amount)
            .ok_or(ReplayError::Authority)?;
        if fee.period != 0 && updated > fee.per_period {
            return Err(ReplayError::Authority);
        }
        Ok(())
    }
}

fn envelope(
    admission: &VerifiedAdmissionPrestate,
    epoch: u64,
) -> Result<(u64, u16, u16), ReplayError> {
    let native = admission.legacy().legacy().canonical_bytes();
    let mut r = Reader(native.get(78..).ok_or(ReplayError::Encoding)?);
    let count = r.u16()?;
    if count == 0 || count > 64 {
        return Err(ReplayError::Bounds);
    }
    let mut roots = Vec::with_capacity(usize::from(count));
    for _ in 0..count {
        roots.push(r.array::<32>()?);
    }
    let module_id = r.u16()?;
    let subtree_root = r.array()?;
    let composite_index = r.u32()?;
    let composite_count = r.u32()?;
    let depth = r.u8()?;
    if module_id != 0
        || composite_index != 0
        || composite_count != u32::from(count)
        || subtree_root != roots[0]
        || depth > 32
    {
        return Err(ReplayError::Authority);
    }
    let mut composite_siblings = Vec::with_capacity(usize::from(depth));
    for _ in 0..depth {
        composite_siblings.push(r.array()?);
    }
    let length = usize::try_from(r.u32()?).map_err(|_| ReplayError::Bounds)?;
    if length > MAX_UNIVERSAL_LEAVES {
        return Err(ReplayError::Bounds);
    }
    let mut leaves = Vec::with_capacity(length);
    for _ in 0..length {
        let encoded = r.vector(35 + 129 + 1_048_576 + 96 * 32)?;
        let witness = StateWitness::decode(encoded).map_err(|_| ReplayError::Authority)?;
        if witness.encode().map_err(|_| ReplayError::Authority)? != encoded {
            return Err(ReplayError::Authority);
        }
        leaves.push(witness);
    }
    let module = ModuleRangeWitness {
        module_id,
        subtree_root,
        composite_index,
        composite_count,
        composite_siblings,
        leaves,
    };
    let verified = module
        .verify_full_module(admission.state_root())
        .map_err(|_| ReplayError::Authority)?;
    let mut mask = 0;
    let mut minimum = u16::MAX;
    let mut maximum = 0;
    for (key, body) in verified.records() {
        if key.len() != 7 || key[0] != 3 {
            continue;
        }
        if body.len() < 18 || body[0] > 1 || body.len() != 18 + usize::from(body[1]) * 4 {
            return Err(ReplayError::Authority);
        }
        if body[0] == 0 || epoch < u64_at(body, 2)? || epoch >= u64_at(body, 10)? {
            continue;
        }
        for encoded in body[18..].chunks_exact(4) {
            let activity =
                u32::from_be_bytes(encoded.try_into().map_err(|_| ReplayError::Authority)?);
            let module = activity >> 16;
            let ordinal = activity as u16;
            if module >= 64 {
                return Err(ReplayError::Authority);
            }
            mask |= 1_u64 << module;
            minimum = minimum.min(ordinal);
            maximum = maximum.max(ordinal);
        }
    }
    if mask == 0 {
        return Err(ReplayError::Authority);
    }
    Ok((mask, minimum, maximum))
}
fn signed_capabilities(payload: &[u8]) -> Result<CapabilitySet, ReplayError> {
    let mut r = Reader(payload);
    if r.array::<34>()? != [0; 34]
        || r.take(30)? != b"LXP/program-replay-profile/v1\0"
        || r.u16()? != 1
    {
        return Err(ReplayError::Authority);
    }
    let boundaries = r.u32()?;
    let maximum = r.u32()?;
    if boundaries == 0 || boundaries > 4096 || maximum < 512 || maximum > 1_048_576 {
        return Err(ReplayError::Bounds);
    }
    let call = r.vector(1_048_576)?;
    if !r.0.is_empty() {
        return Err(ReplayError::Authority);
    }
    call_capabilities(call)
}

fn call_capabilities(call: &[u8]) -> Result<CapabilitySet, ReplayError> {
    if call.len() < 106 || call.len() > 1_048_576 {
        return Err(ReplayError::Authority);
    }
    let abi = u16::from_be_bytes(
        call[32..34]
            .try_into()
            .map_err(|_| ReplayError::Authority)?,
    );
    let entry = usize::from(u16::from_be_bytes(
        call[34..36]
            .try_into()
            .map_err(|_| ReplayError::Authority)?,
    ));
    let data = usize::try_from(u32_at(call, 36)?).map_err(|_| ReplayError::Bounds)?;
    let cap = usize::from(u16::from_be_bytes(
        call[40..42]
            .try_into()
            .map_err(|_| ReplayError::Authority)?,
    ));
    let access = usize::try_from(u32_at(call, 42)?).map_err(|_| ReplayError::Bounds)?;
    let start = 106_usize
        .checked_add(entry)
        .and_then(|v| v.checked_add(data))
        .ok_or(ReplayError::Bounds)?;
    let end = start.checked_add(cap).ok_or(ReplayError::Bounds)?;
    if end.checked_add(access) != Some(call.len()) {
        return Err(ReplayError::Authority);
    }
    let encoded = call.get(start..end).ok_or(ReplayError::Authority)?;
    let capabilities = if abi == 1 {
        CapabilitySet::decode_canonical(encoded)
    } else if matches!(abi, 2 | 3 | 4) {
        CapabilitySet::decode_v2_canonical(encoded)
    } else {
        return Err(ReplayError::Authority);
    }
    .map_err(|_| ReplayError::Authority)?;
    CapabilitySet::new(capabilities).map_err(|_| ReplayError::Authority)
}
fn did_id(did: &[u8]) -> Result<[u8; 32], ReplayError> {
    if did.is_empty() || did.len() > layerx_types::limits::MAX_DID_BYTES {
        return Err(ReplayError::Authority);
    }
    let mut hash = Sha256::new();
    hash.update(b"LXP/v1/did-id\0");
    hash.update(
        u16::try_from(did.len())
            .map_err(|_| ReplayError::Bounds)?
            .to_be_bytes(),
    );
    hash.update(did);
    Ok(hash.finalize().into())
}
fn authority_hash(bytes: &[u8]) -> [u8; 32] {
    let mut hash = Sha256::new();
    hash.update(b"LXP/v1/authority-hash\0");
    hash.update(bytes);
    hash.finalize().into()
}
fn put_u64(bytes: &mut Vec<u8>, value: u64) {
    bytes.extend_from_slice(&value.to_be_bytes());
}
fn vector(bytes: &mut Vec<u8>, value: &[u8]) -> Result<(), ReplayError> {
    bytes.extend_from_slice(
        &u32::try_from(value.len())
            .map_err(|_| ReplayError::Bounds)?
            .to_be_bytes(),
    );
    bytes.extend_from_slice(value);
    Ok(())
}
fn u64_at(bytes: &[u8], offset: usize) -> Result<u64, ReplayError> {
    Ok(u64::from_be_bytes(
        bytes
            .get(offset..offset + 8)
            .ok_or(ReplayError::Authority)?
            .try_into()
            .map_err(|_| ReplayError::Authority)?,
    ))
}
fn u32_at(bytes: &[u8], offset: usize) -> Result<u32, ReplayError> {
    Ok(u32::from_be_bytes(
        bytes
            .get(offset..offset + 4)
            .ok_or(ReplayError::Authority)?
            .try_into()
            .map_err(|_| ReplayError::Authority)?,
    ))
}
struct Reader<'a>(&'a [u8]);
impl<'a> Reader<'a> {
    fn take(&mut self, length: usize) -> Result<&'a [u8], ReplayError> {
        let bytes = self.0.get(..length).ok_or(ReplayError::Encoding)?;
        self.0 = &self.0[length..];
        Ok(bytes)
    }
    fn array<const N: usize>(&mut self) -> Result<[u8; N], ReplayError> {
        self.take(N)?.try_into().map_err(|_| ReplayError::Encoding)
    }
    fn u8(&mut self) -> Result<u8, ReplayError> {
        Ok(self.array::<1>()?[0])
    }
    fn u16(&mut self) -> Result<u16, ReplayError> {
        Ok(u16::from_be_bytes(self.array()?))
    }
    fn u32(&mut self) -> Result<u32, ReplayError> {
        Ok(u32::from_be_bytes(self.array()?))
    }
    fn u64(&mut self) -> Result<u64, ReplayError> {
        Ok(u64::from_be_bytes(self.array()?))
    }
    fn u128(&mut self) -> Result<u128, ReplayError> {
        Ok(u128::from_be_bytes(self.array()?))
    }
    fn vector(&mut self, maximum: usize) -> Result<&'a [u8], ReplayError> {
        let length = usize::try_from(self.u32()?).map_err(|_| ReplayError::Bounds)?;
        if length > maximum {
            return Err(ReplayError::Bounds);
        }
        self.take(length)
    }
    fn fixed_vector<const N: usize>(&mut self) -> Result<[u8; N], ReplayError> {
        self.vector(N)?
            .try_into()
            .map_err(|_| ReplayError::Authority)
    }
}
