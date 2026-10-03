use sha2::{Digest, Sha256};

use super::{Abi, AbiError};
use crate::storage::Storage;
use crate::transfer::TransferSource;

const DOMAIN: &[u8] = b"LayerX/programs/v2/host-state\0";
const STORAGE_DOMAIN: &[u8] = b"LayerX/programs/v2/storage-root\0";
const MAX_CANONICAL_HOST_STATE_BYTES: usize = 64 * 1024 * 1024;

fn resource_code(resource: crate::meter::ResourceKind) -> u8 {
    match resource {
        crate::meter::ResourceKind::Cpu => 0,
        crate::meter::ResourceKind::Memory => 1,
        crate::meter::ResourceKind::StorageRead => 2,
        crate::meter::ResourceKind::StorageWrite => 3,
        crate::meter::ResourceKind::StorageOccupancy => 4,
        crate::meter::ResourceKind::Output => 5,
        crate::meter::ResourceKind::OutputBytes => 6,
    }
}
fn storage_code(error: crate::storage::StorageError) -> u8 {
    match error {
        crate::storage::StorageError::InvalidProgram => 0,
        crate::storage::StorageError::InvalidPrincipal => 1,
        crate::storage::StorageError::EmptyKey => 2,
        crate::storage::StorageError::KeyTooLarge => 3,
        crate::storage::StorageError::ValueTooLarge => 4,
        crate::storage::StorageError::PrefixTooLarge => 5,
        crate::storage::StorageError::InvalidScanCursor => 6,
        crate::storage::StorageError::InvalidScanLimits => 7,
        crate::storage::StorageError::ScanCeilingExceeded => 8,
        crate::storage::StorageError::FrozenNamespace => 9,
        crate::storage::StorageError::SizeOverflow => 10,
    }
}
pub(crate) fn abi_error_bytes(error: &AbiError) -> Vec<u8> {
    let mut out = Vec::with_capacity(19);
    match error {
        AbiError::WrongVersion => out.push(0),
        AbiError::InvalidCapability => out.push(1),
        AbiError::DuplicateCapability => out.push(2),
        AbiError::CapabilityDenied => out.push(3),
        AbiError::CapabilityEscalation => out.push(4),
        AbiError::EventBounds => out.push(5),
        AbiError::CallBounds => out.push(6),
        AbiError::AmountBounds => out.push(7),
        AbiError::ReceiptMismatch => out.push(8),
        AbiError::BalanceAbsent => out.push(9),
        AbiError::BalanceEvidenceUnavailable => out.push(10),
        AbiError::InvalidEncoding => out.push(11),
        AbiError::Storage(error) => {
            out.extend_from_slice(&[12, storage_code(*error)]);
        }
        AbiError::Meter(crate::meter::MeterRefusal::BudgetExceeded {
            resource,
            limit,
            attempted,
        }) => {
            out.extend_from_slice(&[13, 0, resource_code(*resource)]);
            out.extend_from_slice(&limit.to_be_bytes());
            out.extend_from_slice(&attempted.to_be_bytes());
        }
        AbiError::Meter(crate::meter::MeterRefusal::CounterOverflow { resource }) => {
            out.extend_from_slice(&[13, 1, resource_code(*resource)]);
        }
        AbiError::Meter(crate::meter::MeterRefusal::FeeOverflow) => out.extend_from_slice(&[13, 2]),
        AbiError::AccessDeclaration => out.push(14),
        AbiError::OracleUnknownMarket => out.push(15),
        AbiError::OracleMarketHalted => out.push(16),
    }
    out
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(crate) struct HostStateCommitment {
    pub(crate) root: [u8; 32],
    pub(crate) canonical_bytes: u64,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(crate) struct HostStateIdentity {
    pub(crate) base_state: [u8; 32],
    pub(crate) receipt_oracle: [u8; 32],
    pub(crate) balance_oracle: [u8; 32],
}

fn storage_root(storage: &Storage) -> Result<[u8; 32], AbiError> {
    let mut hasher = Sha256::new();
    hasher.update(STORAGE_DOMAIN);
    let mut failed = false;
    storage.for_each_commitment_entry(|key, value| {
        let Ok(key_len) = u32::try_from(key.len()) else {
            failed = true;
            return;
        };
        let Ok(value_len) = u32::try_from(value.len()) else {
            failed = true;
            return;
        };
        hasher.update(key_len.to_be_bytes());
        hasher.update(&key);
        hasher.update(value_len.to_be_bytes());
        hasher.update(value);
    });
    if failed {
        return Err(AbiError::InvalidEncoding);
    }
    Ok(hasher.finalize().into())
}

fn field(
    write: &mut impl FnMut(&[u8]) -> Result<(), AbiError>,
    bytes: &[u8],
) -> Result<(), AbiError> {
    let len = u32::try_from(bytes.len()).map_err(|_| AbiError::InvalidEncoding)?;
    write(&len.to_be_bytes())?;
    write(bytes)?;
    Ok(())
}

pub(super) fn identity(abi: &Abi, baseline: &Storage) -> Result<HostStateIdentity, AbiError> {
    let mut receipts = Sha256::new();
    receipts.update(b"LayerX/programs/v2/receipt-oracle\0");
    for (digest, view) in &abi.receipts {
        receipts.update(digest);
        receipts.update(view.receipt_digest);
        receipts.update(view.result_code.to_be_bytes());
        receipts.update(view.asset);
        receipts.update(view.amount.to_be_bytes());
        receipts.update(view.state_root);
    }
    let mut balances = Sha256::new();
    balances.update(b"LayerX/programs/v2/balance-oracle\0");
    for ((account, asset), view) in &abi.balances {
        balances.update(account);
        balances.update(asset);
        match view {
            Ok(view) => {
                balances.update([1]);
                balances.update(view.account);
                balances.update(view.asset);
                balances.update(view.balance.to_be_bytes());
                balances.update(view.receipt_digest);
                balances.update(view.state_root);
                balances.update(view.observed_sequence.to_be_bytes());
            }
            Err(error) => {
                balances.update([0]);
                let error = abi_error_bytes(error);
                balances.update((error.len() as u64).to_be_bytes());
                balances.update(error);
            }
        }
    }
    Ok(HostStateIdentity {
        base_state: storage_root(baseline)?,
        receipt_oracle: receipts.finalize().into(),
        balance_oracle: balances.finalize().into(),
    })
}

fn write_transfers(
    abi: &Abi,
    write: &mut impl FnMut(&[u8]) -> Result<(), AbiError>,
) -> Result<(), AbiError> {
    write(
        &u32::try_from(abi.effects.transfers.len())
            .map_err(|_| AbiError::AmountBounds)?
            .to_be_bytes(),
    )?;
    for transfer in &abi.effects.transfers {
        write(&transfer.program.bytes())?;
        write(&transfer.principal.bytes())?;
        let (path, depth) = transfer.frame.canonical_bytes();
        write(&path)?;
        write(&[depth])?;
        match &transfer.source {
            TransferSource::Principal(principal) => {
                write(&[0])?;
                write(&principal.bytes())?;
            }
            TransferSource::ProgramFunding { principal, binding } => {
                write(&[1])?;
                write(&principal.bytes())?;
                write(&binding.owner_program().bytes())?;
                field(write, binding.seed())?;
                write(&binding.destination_account())?;
                write(&binding.asset())?;
            }
            TransferSource::Program(authority) => {
                write(&[2])?;
                write(&authority.owner_program().bytes())?;
                field(write, authority.seed())?;
                write(&authority.source_account())?;
                let (path, depth) = authority.staging_frame().canonical_bytes();
                write(&path)?;
                write(&[depth])?;
                write(&authority.asset())?;
                write(&authority.to())?;
                write(&authority.amount().to_be_bytes())?;
            }
        }
        write(&transfer.asset)?;
        write(&transfer.to)?;
        write(&transfer.amount.to_be_bytes())?;
    }
    Ok(())
}

fn write_state(
    abi: &Abi,
    write: &mut impl FnMut(&[u8]) -> Result<(), AbiError>,
) -> Result<(), AbiError> {
    write(DOMAIN)?;
    write(&abi.version.to_be_bytes())?;
    write(&abi.program.bytes())?;
    write(&abi.authorization.principal().bytes())?;
    let (frame, depth) = abi.authorization.frame().canonical_bytes();
    write(&frame)?;
    write(&[depth])?;
    field(
        write,
        &abi.authorization.capabilities().canonical_encoding(),
    )?;
    field(write, &abi.principal_namespace.canonical_bytes())?;
    field(write, &abi.shared_namespace.canonical_bytes())?;
    write(b"storage-overlay/v1\0")?;
    field(
        write,
        &abi.access_declaration
            .canonical_bytes()
            .map_err(|_| AbiError::InvalidEncoding)?,
    )?;
    write(&(abi.event_count_base as u64).to_be_bytes())?;
    let receipts = u32::try_from(abi.receipts.len()).map_err(|_| AbiError::InvalidEncoding)?;
    write(&receipts.to_be_bytes())?;
    for (digest, view) in &abi.receipts {
        write(digest)?;
        write(&view.receipt_digest)?;
        write(&view.result_code.to_be_bytes())?;
        write(&view.asset)?;
        write(&view.amount.to_be_bytes())?;
        write(&view.state_root)?;
    }
    let balances = u32::try_from(abi.balances.len()).map_err(|_| AbiError::InvalidEncoding)?;
    write(&balances.to_be_bytes())?;
    for ((account, asset), view) in &abi.balances {
        write(account)?;
        write(asset)?;
        match view {
            Ok(view) => {
                write(&[1])?;
                write(&view.account)?;
                write(&view.asset)?;
                write(&view.balance.to_be_bytes())?;
                write(&view.receipt_digest)?;
                write(&view.state_root)?;
                write(&view.observed_sequence.to_be_bytes())?;
            }
            Err(error) => {
                write(&[0])?;
                field(write, &abi_error_bytes(error))?;
            }
        }
    }
    write(
        &u32::try_from(abi.effects.events.len())
            .map_err(|_| AbiError::EventBounds)?
            .to_be_bytes(),
    )?;
    for event in &abi.effects.events {
        write(&event.program.bytes())?;
        write(&event.principal.bytes())?;
        let (path, depth) = event.frame.canonical_bytes();
        write(&path)?;
        write(&[depth])?;
        field(write, &event.topic)?;
        field(write, &event.data)?;
    }
    write(
        &u32::try_from(abi.effects.calls.len())
            .map_err(|_| AbiError::CallBounds)?
            .to_be_bytes(),
    )?;
    for call in &abi.effects.calls {
        write(&call.caller.bytes())?;
        write(&call.callee.bytes())?;
        write(&call.principal.bytes())?;
        let (path, depth) = call.caller_frame.canonical_bytes();
        write(&path)?;
        write(&[depth])?;
        let (path, depth) = call.callee_frame.canonical_bytes();
        write(&path)?;
        write(&[depth])?;
        field(write, &call.input)?;
        field(write, &call.capabilities.canonical_encoding())?;
    }
    write_transfers(abi, write)?;
    write(
        &u32::try_from(abi.effects.namespace_drops.len())
            .map_err(|_| AbiError::InvalidEncoding)?
            .to_be_bytes(),
    )?;
    for drop in &abi.effects.namespace_drops {
        field(write, &drop.namespace().canonical_bytes())?;
        write(&drop.reclaimed_cells().to_be_bytes())?;
        write(&drop.reclaimed_key_value_bytes().to_be_bytes())?;
        write(&drop.metered_work().to_be_bytes())?;
    }
    Ok(())
}

pub(super) fn commit(abi: &Abi, hash: bool) -> Result<HostStateCommitment, AbiError> {
    let mut canonical_bytes = 0_u64;
    write_state(abi, &mut |bytes| {
        canonical_bytes = canonical_bytes
            .checked_add(u64::try_from(bytes.len()).map_err(|_| AbiError::InvalidEncoding)?)
            .ok_or(AbiError::InvalidEncoding)?;
        if canonical_bytes > MAX_CANONICAL_HOST_STATE_BYTES as u64 {
            return Err(AbiError::InvalidEncoding);
        }
        Ok(())
    })?;
    if !hash {
        return Ok(HostStateCommitment {
            root: [0; 32],
            canonical_bytes,
        });
    }
    let mut hasher = Sha256::new();
    let mut written = 0_u64;
    write_state(abi, &mut |bytes| {
        written = written
            .checked_add(u64::try_from(bytes.len()).map_err(|_| AbiError::InvalidEncoding)?)
            .ok_or(AbiError::InvalidEncoding)?;
        if written > canonical_bytes {
            return Err(AbiError::InvalidEncoding);
        }
        hasher.update(bytes);
        Ok(())
    })?;
    if written != canonical_bytes {
        return Err(AbiError::InvalidEncoding);
    }
    Ok(HostStateCommitment {
        root: hasher.finalize().into(),
        canonical_bytes,
    })
}

impl Abi {
    pub(crate) fn replay_host_preimage(&self, maximum: usize) -> Result<Vec<u8>, crate::replay::ReplayWitnessError> {
        let maximum = maximum.min(MAX_CANONICAL_HOST_STATE_BYTES);
        let mut length = 0_usize;
        let mut over_bound = false;
        let measured = write_state(self, &mut |bytes| {
            match length.checked_add(bytes.len()) {
                Some(next) if next <= maximum => { length = next; Ok(()) }
                _ => { over_bound = true; Err(AbiError::InvalidEncoding) }
            }
        });
        if measured.is_err() {
            return Err(if over_bound { crate::replay::ReplayWitnessError::Bounds } else { crate::replay::ReplayWitnessError::StateUnavailable });
        }
        let mut out = Vec::new();
        out.try_reserve_exact(length).map_err(|_| crate::replay::ReplayWitnessError::Allocation)?;
        let mut failure = None;
        let written = write_state(self, &mut |bytes| {
            crate::replay::append(&mut out, bytes, maximum).map_err(|error| {
                failure = Some(error); AbiError::InvalidEncoding
            })
        });
        if written.is_err() { return Err(failure.unwrap_or(crate::replay::ReplayWitnessError::StateUnavailable)); }
        if out.len() != length { return Err(crate::replay::ReplayWitnessError::Encoding); }
        Ok(out)
    }
}

impl Abi {
    pub(crate) fn replay_storage_witness(&self, code_hash: [u8; 32], baseline: &Storage, maximum: usize) -> Result<crate::replay::StorageReplayWitnessV1, crate::replay::ReplayWitnessError> {
        crate::replay::StorageReplayWitnessV1::capture(code_hash, baseline, &self.storage, maximum)
    }
}

#[derive(Clone, Debug)]
pub(crate) struct CapturedAbiReplayAuthority {
    preimage_root: [u8; 32],
    authorization: super::AuthorizationContext,
    receipts: std::collections::BTreeMap<[u8; 32], super::ReceiptView>,
    balances: std::collections::BTreeMap<([u8; 32], [u8; 32]), Result<super::BalanceView, AbiError>>,
    oracle: std::sync::Arc<dyn super::CommittedOracle + Send + Sync>,
    web: std::sync::Arc<dyn super::CommittedWeb + Send + Sync>,
}
impl CapturedAbiReplayAuthority {
    pub(crate) fn payment_metadata(&self) -> Vec<u8> {
        match self.authorization.payment_account {
            None => vec![0],
            Some(account) => { let mut bytes = Vec::with_capacity(33); bytes.push(1); bytes.extend_from_slice(&account); bytes }
        }
    }
}

pub(crate) fn decode_replay_abi_error(cursor: &mut crate::replay::ReplayCursor<'_>) -> Result<AbiError, crate::replay::ReplayWitnessError> {
    use crate::replay::ReplayWitnessError as E;
    use crate::storage::StorageError;
    Ok(match cursor.u8()? {
        0 => AbiError::WrongVersion, 1 => AbiError::InvalidCapability, 2 => AbiError::DuplicateCapability,
        3 => AbiError::CapabilityDenied, 4 => AbiError::CapabilityEscalation, 5 => AbiError::EventBounds,
        6 => AbiError::CallBounds, 7 => AbiError::AmountBounds, 8 => AbiError::ReceiptMismatch,
        9 => AbiError::BalanceAbsent, 10 => AbiError::BalanceEvidenceUnavailable, 11 => AbiError::InvalidEncoding,
        12 => AbiError::Storage(match cursor.u8()? {
            0 => StorageError::InvalidProgram, 1 => StorageError::InvalidPrincipal, 2 => StorageError::EmptyKey,
            3 => StorageError::KeyTooLarge, 4 => StorageError::ValueTooLarge, 5 => StorageError::PrefixTooLarge,
            6 => StorageError::InvalidScanCursor, 7 => StorageError::InvalidScanLimits, 8 => StorageError::ScanCeilingExceeded,
            9 => StorageError::FrozenNamespace, 10 => StorageError::SizeOverflow, _ => return Err(E::Encoding),
        }),
        13 => AbiError::Meter(crate::replay::decode_meter_refusal(cursor)?),
        14 => AbiError::AccessDeclaration, 15 => AbiError::OracleUnknownMarket, 16 => AbiError::OracleMarketHalted,
        _ => return Err(E::Encoding),
    })
}
fn replay_program(cursor: &mut crate::replay::ReplayCursor<'_>) -> Result<crate::storage::ProgramId, crate::replay::ReplayWitnessError> {
    crate::storage::ProgramId::new(cursor.array()?).map_err(|_| crate::replay::ReplayWitnessError::Encoding)
}
fn replay_principal(cursor: &mut crate::replay::ReplayCursor<'_>) -> Result<crate::storage::PrincipalId, crate::replay::ReplayWitnessError> {
    crate::storage::PrincipalId::new(cursor.array()?).map_err(|_| crate::replay::ReplayWitnessError::Encoding)
}
fn replay_frame(cursor: &mut crate::replay::ReplayCursor<'_>) -> Result<super::CallFrameId, crate::replay::ReplayWitnessError> {
    super::CallFrameId::from_canonical(cursor.array()?, cursor.u8()?).map_err(|_| crate::replay::ReplayWitnessError::Encoding)
}
fn replay_capabilities(cursor: &mut crate::replay::ReplayCursor<'_>, version: u16) -> Result<super::CapabilitySet, crate::replay::ReplayWitnessError> {
    use crate::replay::ReplayWitnessError as E;
    let bytes = cursor.field()?;
    let grants = if version == 1 { super::CapabilitySet::decode_canonical(bytes) } else { super::CapabilitySet::decode_v2_canonical(bytes) }.map_err(|_| E::Encoding)?;
    let set = super::CapabilitySet::new(grants).map_err(|_| E::Encoding)?;
    if set.canonical_encoding() != bytes { return Err(E::Encoding); }
    Ok(set)
}
fn replay_push<T>(values: &mut Vec<T>, value: T) -> Result<(), crate::replay::ReplayWitnessError> {
    values.try_reserve_exact(1).map_err(|_| crate::replay::ReplayWitnessError::Allocation)?;
    values.push(value); Ok(())
}

impl Abi {
    pub(crate) fn capture_replay_authority(&self, preimage: &[u8], maximum: usize) -> Result<CapturedAbiReplayAuthority, crate::replay::ReplayWitnessError> {
        if self.replay_host_preimage(maximum)? != preimage { return Err(crate::replay::ReplayWitnessError::Binding); }
        Ok(CapturedAbiReplayAuthority { preimage_root: Sha256::digest(preimage).into(), authorization: self.authorization.clone(),
            receipts: self.receipts.clone(), balances: self.balances.clone(), oracle: std::sync::Arc::clone(&self.oracle), web: std::sync::Arc::clone(&self.web) })
    }

    pub(crate) fn restore_untrusted_host_preimage(bytes: &[u8], storage: Storage, authority: &CapturedAbiReplayAuthority) -> Result<Self, crate::replay::ReplayWitnessError> {
        use crate::replay::{ReplayCursor, ReplayWitnessError as E};
        use super::{AbiEffects, BalanceView, ReceiptView, ProgramEvent, ProgramCall, TransferRequest};
        use crate::transfer::{ProgramAuthority, ProgramFundingBinding};
        if bytes.len() > crate::MAX_ARBITRATION_HOST_STATE_BYTES { return Err(E::Bounds); }
        let root: [u8; 32] = Sha256::digest(bytes).into();
        if root != authority.preimage_root { return Err(E::Binding); }
        let mut cursor = ReplayCursor::new(bytes);
        if cursor.take(DOMAIN.len())? != DOMAIN { return Err(E::Encoding); }
        let version = cursor.u16()?;
        if super::manifest::manifest(version).is_none() { return Err(E::Encoding); }
        let program = replay_program(&mut cursor)?;
        let principal = replay_principal(&mut cursor)?;
        let frame = replay_frame(&mut cursor)?;
        let capabilities = replay_capabilities(&mut cursor, version)?;
        if principal != authority.authorization.principal() || frame != authority.authorization.frame()
            || &capabilities != authority.authorization.capabilities() { return Err(E::Binding); }
        let principal_namespace = crate::replay::replay_namespace(cursor.field()?)?;
        let shared_namespace = crate::replay::replay_namespace(cursor.field()?)?;
        if principal_namespace != crate::storage::StorageNamespace::principal(program, principal)
            || shared_namespace != crate::storage::StorageNamespace::shared(program) { return Err(E::Encoding); }
        if cursor.take(b"storage-overlay/v1\0".len())? != b"storage-overlay/v1\0" { return Err(E::Encoding); }
        let access_declaration = crate::AccessDeclaration::canonical_decode(cursor.field()?).map_err(|_| E::Encoding)?;
        let event_count_base = cursor.usize64()?;
        let mut receipts = std::collections::BTreeMap::new();
        for _ in 0..cursor.count(148)? {
            let digest = cursor.array()?;
            let view = ReceiptView { receipt_digest: cursor.array()?, result_code: cursor.i32()?, asset: cursor.array()?, amount: cursor.u128()?, state_root: cursor.array()? };
            if digest != view.receipt_digest || receipts.last_key_value().is_some_and(|(previous, _)| previous >= &digest) { return Err(E::Encoding); }
            receipts.insert(digest, view);
        }
        let mut balances = std::collections::BTreeMap::new();
        for _ in 0..cursor.count(70)? {
            let key = (cursor.array()?, cursor.array()?);
            if balances.last_key_value().is_some_and(|(previous, _)| previous >= &key) { return Err(E::Encoding); }
            let view = if cursor.boolean()? {
                let view = BalanceView { account: cursor.array()?, asset: cursor.array()?, balance: cursor.u128()?, receipt_digest: cursor.array()?, state_root: cursor.array()?, observed_sequence: cursor.u64()? };
                if view.account != key.0 || view.asset != key.1 { return Err(E::Encoding); }
                Ok(view)
            } else {
                let bytes = cursor.field()?; let mut error = ReplayCursor::new(bytes);
                let value = decode_replay_abi_error(&mut error)?;
                if !error.done() || abi_error_bytes(&value) != bytes { return Err(E::Encoding); }
                Err(value)
            };
            balances.insert(key, view);
        }
        if receipts != authority.receipts || balances != authority.balances { return Err(E::Binding); }
        let mut effects = AbiEffects::default();
        for _ in 0..cursor.count(81)? {
            let event = ProgramEvent { program: replay_program(&mut cursor)?, principal: replay_principal(&mut cursor)?, frame: replay_frame(&mut cursor)?,
                topic: cursor.owned_field(super::MAX_EVENT_TOPIC_BYTES)?, data: cursor.owned_field(super::MAX_EVENT_DATA_BYTES)? };
            replay_push(&mut effects.events, event)?;
        }
        for _ in 0..cursor.count(122)? {
            let call = ProgramCall { caller: replay_program(&mut cursor)?, callee: replay_program(&mut cursor)?, principal: replay_principal(&mut cursor)?,
                caller_frame: replay_frame(&mut cursor)?, callee_frame: replay_frame(&mut cursor)?, input: cursor.owned_field(super::MAX_CALL_INPUT_BYTES)?,
                capabilities: replay_capabilities(&mut cursor, version)? };
            replay_push(&mut effects.calls, call)?;
        }
        for _ in 0..cursor.count(186)? {
            let transfer_program = replay_program(&mut cursor)?;
            let transfer_principal = replay_principal(&mut cursor)?;
            let transfer_frame = replay_frame(&mut cursor)?;
            let source = match cursor.u8()? {
                0 => TransferSource::Principal(replay_principal(&mut cursor)?),
                1 => {
                    let principal = replay_principal(&mut cursor)?;
                    let owner = replay_program(&mut cursor)?;
                    let seed = cursor.owned_field(crate::MAX_PROGRAM_ACCOUNT_SEED_BYTES)?;
                    let account = cursor.array()?; let asset = cursor.array()?;
                    let binding = ProgramFundingBinding::issue(owner, &seed, account, asset).map_err(|_| E::Encoding)?;
                    TransferSource::ProgramFunding { principal, binding }
                }
                2 => {
                    let owner = replay_program(&mut cursor)?;
                    let seed = cursor.owned_field(crate::MAX_PROGRAM_ACCOUNT_SEED_BYTES)?;
                    let account = cursor.array()?; let frame = replay_frame(&mut cursor)?;
                    let asset = cursor.array()?; let to = cursor.array()?; let amount = cursor.u128()?;
                    TransferSource::Program(ProgramAuthority::issue(owner, &seed, account, frame, asset, to, amount).map_err(|_| E::Encoding)?)
                }
                _ => return Err(E::Encoding),
            };
            let transfer = TransferRequest { program: transfer_program, principal: transfer_principal, frame: transfer_frame, source,
                asset: cursor.array()?, to: cursor.array()?, amount: cursor.u128()? };
            replay_push(&mut effects.transfers, transfer)?;
        }
        for _ in 0..cursor.count(61)? {
            let namespace = crate::replay::replay_namespace(cursor.field()?)?;
            let drop = crate::storage::NamespaceDrop::from_untrusted_replay_fields(namespace, cursor.u64()?, cursor.u64()?, cursor.u64()?)?;
            replay_push(&mut effects.namespace_drops, drop)?;
        }
        if !cursor.done() { return Err(E::Encoding); }
        let restored = Self { version, program, authorization: authority.authorization.clone(), principal_namespace, shared_namespace, storage,
            receipts, balances, oracle: std::sync::Arc::clone(&authority.oracle), web: std::sync::Arc::clone(&authority.web), effects, event_count_base, access_declaration };
        if restored.replay_host_preimage(bytes.len())? != bytes { return Err(E::Encoding); }
        Ok(restored)
    }
}
