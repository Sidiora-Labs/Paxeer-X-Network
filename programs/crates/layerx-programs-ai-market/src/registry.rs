use crate::{
    codec::{derive_market, Reader, Writer},
    errors::*,
    policy::{
        validate_policy_records, PendingPolicy, PolicyHistoryHeader, TaskPolicyV1,
        PENDING_POLICY_BYTES, POLICY_HISTORY_HEADER_BYTES, TASK_POLICY_BYTES,
    },
    types::*,
};
use layerx_program_sdk::payments::PreparedProgramAccount;

pub const MARKET_HEADER_ABSENT_BYTES: usize = 384;
pub const MARKET_HEADER_PRESENT_BYTES: usize = 416;
pub const OPERATOR_GRANT_BYTES: usize = 50;
pub const F01_SECTION_CAP: usize = 16_384;
pub const TASK_BINDING_DESIGN_BYTES: usize = 233;
pub const COMMON_SECTION_HEADER_BYTES: usize = 8;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct MarketHeader {
    pub format_version: u16,
    pub market_id: MarketId,
    pub deployment_chain_domain: ChainDomain,
    pub program_id: ProgramId,
    pub owner_principal: PrincipalId,
    pub funding_asset: AssetId,
    pub rewards_account: AccountId,
    pub refund_recipient_account: AccountId,
    pub treasury_principal: Presence<PrincipalId>,
    pub origin_height: u64,
    pub lifecycle: u8,
    pub state_revision: u64,
    pub highest_config_version: u64,
    pub active_config_version: u64,
    pub activation_epoch: u64,
    pub activation_scheduled: bool,
    pub closure_requested_at: u64,
    pub close_phase: u8,
    pub close_cursor: u16,
    pub suspension_reason_digest: [u8; 32],
    pub metadata_digest: MetadataDigest,
    pub closing_request_digest: [u8; 32],
    pub reserved: [u8; 8],
}

/// This derives a binding only; it does not register an account or prove custody.
pub fn derive_rewards_account(program: ProgramId, asset: AssetId) -> CodecResult<AccountId> {
    let program = layerx_program_sdk::ProgramId::new(program.bytes())
        .map_err(|_| F01_ACCOUNT_BINDING_MISSING)?;
    let asset =
        layerx_program_sdk::AssetId::new(asset.bytes()).map_err(|_| F01_ACCOUNT_BINDING_MISSING)?;
    let prepared = PreparedProgramAccount::new(program, b"paxai/rewards/v1", asset)
        .map_err(|_| F01_ACCOUNT_BINDING_MISSING)?;
    AccountId::new(prepared.account().bytes()).map_err(|_| F01_ACCOUNT_BINDING_MISSING)
}

impl MarketHeader {
    pub fn validate(&self) -> CodecResult<()> {
        if self.format_version != 1 {
            return Err(BAD_VERSION);
        }
        if !(1..=5).contains(&self.lifecycle) {
            return Err(F01_WRONG_LIFECYCLE);
        }
        if self.reserved != [0; 8]
            || self.state_revision == 0
            || self.highest_config_version == 0
            || self.active_config_version == 0
            || self.active_config_version > self.highest_config_version
            || (!self.activation_scheduled && self.activation_epoch != 0)
            || self.close_phase > 5
            || (self.close_phase == 0 && (self.closure_requested_at != 0 || self.close_cursor != 0))
        {
            return Err(NON_CANONICAL);
        }
        if let Presence::Present(treasury) = self.treasury_principal {
            if treasury == self.owner_principal {
                return Err(F01_PRINCIPAL_MISMATCH);
            }
        }
        if self.market_id != derive_market(self.deployment_chain_domain, self.program_id)? {
            return Err(WRONG_MARKET);
        }
        if self.rewards_account != derive_rewards_account(self.program_id, self.funding_asset)? {
            return Err(F01_ACCOUNT_BINDING_MISSING);
        }
        Ok(())
    }
    pub const fn encoded_len(&self) -> usize {
        match self.treasury_principal {
            Presence::Absent => MARKET_HEADER_ABSENT_BYTES,
            Presence::Present(_) => MARKET_HEADER_PRESENT_BYTES,
        }
    }
    pub fn encode(&self, output: &mut [u8]) -> CodecResult<usize> {
        self.validate()?;
        if output.len() < self.encoded_len() {
            return Err(CAPACITY);
        }
        let mut w = Writer::new(output);
        w.u16(self.format_version)?;
        w.put(self.market_id.as_bytes())?;
        w.put(self.deployment_chain_domain.as_bytes())?;
        w.put(self.program_id.as_bytes())?;
        w.put(self.owner_principal.as_bytes())?;
        w.put(self.funding_asset.as_bytes())?;
        w.put(self.rewards_account.as_bytes())?;
        w.put(self.refund_recipient_account.as_bytes())?;
        w.presence(&self.treasury_principal, |w, principal| {
            w.put(principal.as_bytes())
        })?;
        w.u64(self.origin_height)?;
        w.u8(self.lifecycle)?;
        w.u64(self.state_revision)?;
        w.u64(self.highest_config_version)?;
        w.u64(self.active_config_version)?;
        w.u64(self.activation_epoch)?;
        w.boolean(self.activation_scheduled)?;
        w.u64(self.closure_requested_at)?;
        w.u8(self.close_phase)?;
        w.u16(self.close_cursor)?;
        w.put(&self.suspension_reason_digest)?;
        w.put(self.metadata_digest.as_bytes())?;
        w.put(&self.closing_request_digest)?;
        w.put(&self.reserved)?;
        Ok(w.len())
    }
    pub fn decode(input: &[u8]) -> CodecResult<Self> {
        let mut r = Reader::new(input);
        let value = Self {
            format_version: r.u16()?,
            market_id: MarketId::new(r.fixed()?)?,
            deployment_chain_domain: ChainDomain::new(r.fixed()?)?,
            program_id: ProgramId::new(r.fixed()?)?,
            owner_principal: PrincipalId::new(r.fixed()?)?,
            funding_asset: AssetId::new(r.fixed()?)?,
            rewards_account: AccountId::new(r.fixed()?)?,
            refund_recipient_account: AccountId::new(r.fixed()?)?,
            treasury_principal: r.presence(|r| PrincipalId::new(r.fixed()?))?,
            origin_height: r.u64()?,
            lifecycle: r.u8()?,
            state_revision: r.u64()?,
            highest_config_version: r.u64()?,
            active_config_version: r.u64()?,
            activation_epoch: r.u64()?,
            activation_scheduled: r.boolean()?,
            closure_requested_at: r.u64()?,
            close_phase: r.u8()?,
            close_cursor: r.u16()?,
            suspension_reason_digest: r.fixed()?,
            metadata_digest: MetadataDigest::new(r.fixed()?)?,
            closing_request_digest: r.fixed()?,
            reserved: r.fixed()?,
        };
        r.finish()?;
        value.validate()?;
        Ok(value)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct OperatorGrant {
    pub principal: PrincipalId,
    pub permissions: u8,
    pub sequence: u64,
    pub revoked: bool,
    pub reserved: [u8; 8],
}
impl OperatorGrant {
    pub fn validate(&self) -> CodecResult<()> {
        if !(1..=3).contains(&self.permissions) || self.sequence == 0 || self.reserved != [0; 8] {
            return Err(NON_CANONICAL);
        }
        Ok(())
    }
    pub fn encode(&self, output: &mut [u8]) -> CodecResult<usize> {
        self.validate()?;
        if output.len() < OPERATOR_GRANT_BYTES {
            return Err(CAPACITY);
        }
        let mut w = Writer::new(output);
        w.put(self.principal.as_bytes())?;
        w.u8(self.permissions)?;
        w.u64(self.sequence)?;
        w.boolean(self.revoked)?;
        w.put(&self.reserved)?;
        Ok(w.len())
    }
    pub fn decode(input: &[u8]) -> CodecResult<Self> {
        let mut r = Reader::new(input);
        let value = Self {
            principal: PrincipalId::new(r.fixed()?)?,
            permissions: r.u8()?,
            sequence: r.u64()?,
            revoked: r.boolean()?,
            reserved: r.fixed()?,
        };
        r.finish()?;
        value.validate()?;
        Ok(value)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct MarketClock {
    pub epoch: u64,
    pub position: u64,
    pub windows: EpochWindows,
    pub phase: EpochPhase,
}
pub fn market_clock(origin_height: u64, height: u64) -> CodecResult<MarketClock> {
    let elapsed = height.checked_sub(origin_height).ok_or(ARITHMETIC)?;
    let epoch = elapsed / 128;
    let position = elapsed % 128;
    let windows = EpochWindows::new(origin_height, epoch)?;
    Ok(MarketClock {
        epoch,
        position,
        windows,
        phase: windows.phase(height),
    })
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct F01Worksheet {
    pub non_task_bytes: usize,
    pub task_bytes: usize,
    pub section_bytes: usize,
    pub section_headroom: usize,
    pub charged_section_bytes: usize,
}
fn capacity_add(a: usize, b: usize) -> CodecResult<usize> {
    a.checked_add(b).ok_or(F01_CAPACITY_UNAVAILABLE)
}
pub fn check_f01_capacity(section_bytes: usize, total_state_bytes: usize) -> CodecResult<()> {
    let charged = capacity_add(section_bytes, COMMON_SECTION_HEADER_BYTES)?;
    if charged > F01_SECTION_CAP
        || total_state_bytes > crate::MAX_STATE_BYTES
        || total_state_bytes < charged
    {
        return Err(F01_CAPACITY_UNAVAILABLE);
    }
    Ok(())
}
fn worksheet(non_task_bytes: usize, task_count: usize) -> CodecResult<F01Worksheet> {
    if task_count > crate::MAX_TASKS {
        return Err(F01_CAPACITY_UNAVAILABLE);
    }
    let task_bytes = task_count
        .checked_mul(TASK_BINDING_DESIGN_BYTES)
        .ok_or(F01_CAPACITY_UNAVAILABLE)?;
    let section_bytes = capacity_add(non_task_bytes, task_bytes)?;
    check_f01_capacity(section_bytes, crate::MAX_STATE_BYTES)?;
    Ok(F01Worksheet {
        non_task_bytes,
        task_bytes,
        section_bytes,
        section_headroom: F01_SECTION_CAP
            .checked_sub(section_bytes)
            .ok_or(F01_CAPACITY_UNAVAILABLE)?,
        charged_section_bytes: capacity_add(section_bytes, COMMON_SECTION_HEADER_BYTES)?,
    })
}

/// Design bound only. TaskBinding serialization and whole-state admission belong to T04/core.
pub fn maximum_f01_worksheet() -> CodecResult<F01Worksheet> {
    let mut n = MARKET_HEADER_PRESENT_BYTES;
    for bytes in [
        OPERATOR_GRANT_BYTES,
        TASK_POLICY_BYTES,
        PENDING_POLICY_BYTES,
        POLICY_HISTORY_HEADER_BYTES
            .checked_mul(4)
            .ok_or(F01_CAPACITY_UNAVAILABLE)?,
        32,
        2,
        33,
        1,
        1,
        2,
        4,
    ] {
        n = capacity_add(n, bytes)?;
    }
    worksheet(n, 64)
}

/// Measures owned record encoders; adds the normative task worksheet without inventing a task codec.
pub struct F01NonTaskLayout<'a> {
    pub header: &'a MarketHeader,
    pub operator: Presence<OperatorGrant>,
    pub current: &'a TaskPolicyV1,
    pub pending: Presence<PendingPolicy>,
    pub recent: &'a [PolicyHistoryHeader],
    pub history_root: Digest32,
    pub task_count: usize,
    pub task_set_root: Presence<Digest32>,
}
impl F01NonTaskLayout<'_> {
    pub fn measured_worksheet(&self) -> CodecResult<F01Worksheet> {
        validate_policy_records(
            self.current,
            &self.pending,
            self.recent,
            self.header.highest_config_version,
        )?;
        if self.header.active_config_version != self.current.config_version {
            return Err(F01_VERSION_MISMATCH);
        }
        let mut scratch = [0; MARKET_HEADER_PRESENT_BYTES];
        let mut n = self.header.encode(&mut scratch)?;
        if let Presence::Present(operator) = self.operator {
            n = capacity_add(n, operator.encode(&mut scratch)?)?;
        }
        n = capacity_add(n, self.current.encode(&mut scratch)?)?;
        if let Presence::Present(pending) = self.pending {
            n = capacity_add(n, pending.encode(&mut scratch)?)?;
        }
        for header in self.recent {
            n = capacity_add(n, header.encode(&mut scratch)?)?;
        }
        for bytes in [32, 2, 1, 1, 1, 2, 4] {
            n = capacity_add(n, bytes)?;
        }
        if let Presence::Present(_) = self.task_set_root {
            n = capacity_add(n, 32)?;
        }
        worksheet(n, self.task_count)
    }
}
