use layerx_types::intent::{ProgramCall, ProgramId, PROGRAM_CALL_PAYLOAD_DOMAIN};
use layerx_types::program_call::{NativeProgramCall, Resources};
use layerx_types::program_lifecycle::{
    NativeProgramDeploy, NativeProgramUpgrade, NativeProgramWindDown, ProgramUpgradePolicy,
    ProgramWindDownOperation,
};
use sha2::{Digest, Sha256};

use super::{Activity, DisclosureError, DisclosureFields, DisclosedNativeOperation, Encoder, Expiry};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DisclosedProgramDeploy {
    pub program_id: ProgramId,
    pub guest_abi: u16,
    pub policy: ProgramUpgradePolicy,
    pub new_hash: [u8; 32],
    pub interface: Option<Vec<u8>>,
    pub wasm: Vec<u8>,
}

impl DisclosedProgramDeploy {
    #[must_use]
    pub fn native(&self) -> NativeProgramDeploy<'_> {
        NativeProgramDeploy {
            program_id: self.program_id, guest_abi: self.guest_abi, policy: self.policy,
            new_hash: self.new_hash, interface: self.interface.as_deref(), wasm: &self.wasm,
        }
    }

    pub(super) fn encode_audit(&self, out: &mut Encoder) -> Result<(), DisclosureError> {
        self.native().encode().map_err(|_| DisclosureError::MalformedPayload)?;
        out.u8(9)?;
        out.fixed(&self.program_id.bytes())?;
        out.u16(self.guest_abi)?;
        match self.policy {
            ProgramUpgradePolicy::Immutable => out.u8(0)?,
            ProgramUpgradePolicy::Authority(authority) => {
                out.u8(1)?;
                out.fixed(&authority)?;
            }
        }
        out.fixed(&self.new_hash)?;
        option_commitment(out, self.interface.as_deref())?;
        commitment(out, &self.wasm)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DisclosedProgramUpgrade {
    pub program_id: ProgramId,
    pub guest_abi: u16,
    pub old_hash: [u8; 32],
    pub new_hash: [u8; 32],
    pub migration_hook: Vec<u8>,
    pub clear_interface: bool,
    pub interface: Option<Vec<u8>>,
    pub wasm: Vec<u8>,
}

impl DisclosedProgramUpgrade {
    #[must_use]
    pub fn native(&self) -> NativeProgramUpgrade<'_> {
        NativeProgramUpgrade {
            program_id: self.program_id, guest_abi: self.guest_abi,
            old_hash: self.old_hash, new_hash: self.new_hash,
            migration_hook: &self.migration_hook, clear_interface: self.clear_interface,
            interface: self.interface.as_deref(), wasm: &self.wasm,
        }
    }

    pub(super) fn encode_audit(&self, out: &mut Encoder) -> Result<(), DisclosureError> {
        self.native().encode().map_err(|_| DisclosureError::MalformedPayload)?;
        out.u8(10)?;
        out.fixed(&self.program_id.bytes())?;
        out.u16(self.guest_abi)?;
        out.fixed(&self.old_hash)?;
        out.fixed(&self.new_hash)?;
        out.bytes(&self.migration_hook, 128)?;
        out.u8(u8::from(self.clear_interface))?;
        option_commitment(out, self.interface.as_deref())?;
        commitment(out, &self.wasm)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DisclosedProgramCall {
    pub program_id: ProgramId,
    pub guest_abi: u16,
    pub entrypoint: Vec<u8>,
    pub calldata: Vec<u8>,
    pub capabilities: Vec<u8>,
    pub access_declaration: Vec<u8>,
    pub response_capacity: u32,
    pub resources: Resources,
}

impl DisclosedProgramCall {
    #[must_use]
    pub fn native(&self) -> NativeProgramCall<'_> {
        NativeProgramCall {
            program_id: self.program_id, guest_abi: self.guest_abi,
            entrypoint: &self.entrypoint, calldata: &self.calldata,
            capabilities: &self.capabilities, access_declaration: &self.access_declaration,
            response_capacity: self.response_capacity, resources: self.resources,
        }
    }

    pub(super) fn encode_audit(&self, out: &mut Encoder) -> Result<(), DisclosureError> {
        self.native().encode().map_err(|_| DisclosureError::MalformedPayload)?;
        out.u8(11)?;
        out.fixed(&self.program_id.bytes())?;
        out.u16(self.guest_abi)?;
        out.bytes(&self.entrypoint, 128)?;
        out.u32(self.response_capacity)?;
        for ceiling in self.resources.0 { out.u64(ceiling)?; }
        commitment(out, &self.calldata)?;
        commitment(out, &self.capabilities)?;
        commitment(out, &self.access_declaration)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum DisclosedProgramWindDownOperation {
    Route { account: [u8; 32], asset: [u8; 32], destination: [u8; 32], seed: Vec<u8> },
    Deprecate { exit_program: [u8; 32], deadline_batch: u64 },
    Tombstone,
    Exit { account: [u8; 32] },
    BoundedExit { account: [u8; 32], maximum_exit_amount: u128 },
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DisclosedProgramWindDown {
    pub program_id: ProgramId,
    pub operation: DisclosedProgramWindDownOperation,
}

impl DisclosedProgramWindDown {
    #[must_use]
    pub fn native(&self) -> NativeProgramWindDown<'_> {
        let operation = match &self.operation {
            DisclosedProgramWindDownOperation::Route { account, asset, destination, seed } =>
                ProgramWindDownOperation::Route {
                    account: *account, asset: *asset, destination: *destination, seed,
                },
            DisclosedProgramWindDownOperation::Deprecate { exit_program, deadline_batch } =>
                ProgramWindDownOperation::Deprecate {
                    exit_program: *exit_program, deadline_batch: *deadline_batch,
                },
            DisclosedProgramWindDownOperation::Tombstone => ProgramWindDownOperation::Tombstone,
            DisclosedProgramWindDownOperation::Exit { account } =>
                ProgramWindDownOperation::Exit { account: *account },
            DisclosedProgramWindDownOperation::BoundedExit { account, maximum_exit_amount } =>
                ProgramWindDownOperation::BoundedExit {
                    account: *account, maximum_exit_amount: *maximum_exit_amount,
                },
        };
        NativeProgramWindDown { program_id: self.program_id, operation }
    }

    pub(super) fn encode_audit(&self, out: &mut Encoder) -> Result<(), DisclosureError> {
        let encoded = self.native().encode().map_err(|_| DisclosureError::MalformedPayload)?;
        out.u8(12)?;
        out.bytes(&encoded, 512)?;
        Ok(())
    }
}

pub(super) fn encode_legacy(call: &ProgramCall, out: &mut Encoder) -> Result<(), DisclosureError> {
    out.u8(13)?;
    out.fixed(&call.callee().bytes())?;
    out.u64(call.budget().fuel())?;
    out.fixed(&call.budget().fee_limit().to_be_bytes())?;
    out.sequence_length(call.capabilities().as_slice().len(), 5)?;
    for capability in call.capabilities().as_slice() { out.u8(capability.tag())?; }
    commitment(out, call.calldata().as_bytes())
}

fn commitment(out: &mut Encoder, value: &[u8]) -> Result<(), DisclosureError> {
    out.u32(u32::try_from(value.len()).map_err(|_| DisclosureError::MalformedPayload)?)?;
    let digest: [u8; 32] = Sha256::digest(value).into();
    out.fixed(&digest)?;
    Ok(())
}

fn option_commitment(out: &mut Encoder, value: Option<&[u8]>) -> Result<(), DisclosureError> {
    match value {
        Some(value) => { out.u8(1)?; commitment(out, value) }
        None => { out.u8(0)?; Ok(()) }
    }
}

pub(super) fn fields(activity: &Activity) -> Result<DisclosureFields, DisclosureError> {
    let malformed = || DisclosureError::MalformedPayload;
    let payload = activity.payload();
    let ordinal = activity.activity_type().ordinal();
    let legacy = ordinal == 3 && payload.starts_with(PROGRAM_CALL_PAYLOAD_DOMAIN);
    if !legacy && (activity.protocol_version() != 3 || activity.network_id() == 0) {
        return Err(malformed());
    }
    let operation = match ordinal {
        1 => {
            let value = NativeProgramDeploy::decode(payload).map_err(|_| malformed())?;
            if value.encode().map_err(|_| malformed())? != payload { return Err(malformed()); }
            DisclosedNativeOperation::ProgramDeploy(Box::new(DisclosedProgramDeploy {
                program_id: value.program_id, guest_abi: value.guest_abi, policy: value.policy,
                new_hash: value.new_hash, interface: value.interface.map(<[u8]>::to_vec),
                wasm: value.wasm.to_vec(),
            }))
        }
        2 => {
            let value = NativeProgramUpgrade::decode(payload).map_err(|_| malformed())?;
            if value.encode().map_err(|_| malformed())? != payload { return Err(malformed()); }
            DisclosedNativeOperation::ProgramUpgrade(Box::new(DisclosedProgramUpgrade {
                program_id: value.program_id, guest_abi: value.guest_abi,
                old_hash: value.old_hash, new_hash: value.new_hash,
                migration_hook: value.migration_hook.to_vec(), clear_interface: value.clear_interface,
                interface: value.interface.map(<[u8]>::to_vec), wasm: value.wasm.to_vec(),
            }))
        }
        3 if legacy => DisclosedNativeOperation::LegacyProgramCall(Box::new(
            ProgramCall::from_canonical_payload(payload).map_err(|_| malformed())?,
        )),
        3 => {
            let value = NativeProgramCall::decode(payload).map_err(|_| malformed())?;
            if value.encode().map_err(|_| malformed())? != payload { return Err(malformed()); }
            DisclosedNativeOperation::ProgramCall(Box::new(DisclosedProgramCall {
                program_id: value.program_id, guest_abi: value.guest_abi,
                entrypoint: value.entrypoint.to_vec(), calldata: value.calldata.to_vec(),
                capabilities: value.capabilities.to_vec(), access_declaration: value.access_declaration.to_vec(),
                response_capacity: value.response_capacity, resources: value.resources,
            }))
        }
        7 => {
            let value = NativeProgramWindDown::decode(payload).map_err(|_| malformed())?;
            if value.encode().map_err(|_| malformed())? != payload { return Err(malformed()); }
            let operation = match value.operation {
                ProgramWindDownOperation::Route { account, asset, destination, seed } =>
                    DisclosedProgramWindDownOperation::Route { account, asset, destination, seed: seed.to_vec() },
                ProgramWindDownOperation::Deprecate { exit_program, deadline_batch } =>
                    DisclosedProgramWindDownOperation::Deprecate { exit_program, deadline_batch },
                ProgramWindDownOperation::Tombstone => DisclosedProgramWindDownOperation::Tombstone,
                ProgramWindDownOperation::Exit { account } => DisclosedProgramWindDownOperation::Exit { account },
                ProgramWindDownOperation::BoundedExit { account, maximum_exit_amount } =>
                    DisclosedProgramWindDownOperation::BoundedExit { account, maximum_exit_amount },
            };
            DisclosedNativeOperation::ProgramWindDown(Box::new(DisclosedProgramWindDown {
                program_id: value.program_id, operation,
            }))
        }
        _ => return Err(DisclosureError::UnsupportedActivity(activity.activity_type().value())),
    };
    let bound = activity.timestamp_bound();
    Ok(DisclosureFields {
        activity_type: activity.activity_type(), actor: activity.actor_did().to_vec(),
        authority: activity.authority().to_vec(), counterparties: Vec::new(), amounts: Vec::new(),
        asset: [0; 32], fee_limit: activity.fee_limit(),
        expiry: Expiry { not_before: bound.not_before, not_after: bound.not_after, payload_expires_at: bound.not_after },
        idempotency_key: activity.idempotency_key(), evm_payout_binding: None, withdrawal: None,
        payment: None, authority_grant: None, session_grant: None, onboarding: None,
        native_operation: Some(operation),
    })
}
