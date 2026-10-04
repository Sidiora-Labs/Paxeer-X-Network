use serde::Deserialize;
use serde_json::{json, Value};

use crate::rpc::RpcCluster;
use crate::source_codec::{decode_fixed_hex, decode_hex, decode_quantity, ethereum_hex, hex, quantity};
use crate::{MigrationError, RpcQuorumConfig};

const PAXEER_CHAIN_ID: u64 = 125;
const ADDRESS_PRECOMPILE: &str = "0x0000000000000000000000000000000000001004";
const GET_EVM_ADDRESS: [u8; 4] = [0xc4, 0x13, 0xba, 0xd3];
const GET_LAYERX_DID: [u8; 4] = [0xd3, 0x60, 0x0b, 0x24];
const DID_PREFIX: &str = "did:layerx:";
const MAX_DID_BYTES: usize = DID_PREFIX.len() + 64;
const MAX_DID_RESPONSE_BYTES: usize = 64 + 32 + MAX_DID_BYTES.div_ceil(32) * 32;

#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct PaxeerBindingConfigV2 {
    pub chain_id: u64,
    pub genesis_hash: [u8; 32],
    pub rpc: RpcQuorumConfig,
}

pub struct PaxeerBindingVerifierV2 {
    config: PaxeerBindingConfigV2,
    rpc: RpcCluster,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ConfirmedPaxeerBindingV2 {
    identity: [u8; 32],
    evm_address: [u8; 20],
    block_number: u64,
    block_hash: [u8; 32],
}

impl ConfirmedPaxeerBindingV2 {
    #[must_use]
    pub const fn identity(&self) -> [u8; 32] {
        self.identity
    }

    #[must_use]
    pub const fn evm_address(&self) -> [u8; 20] {
        self.evm_address
    }

    #[must_use]
    pub const fn block_number(&self) -> u64 {
        self.block_number
    }

    #[must_use]
    pub const fn block_hash(&self) -> [u8; 32] {
        self.block_hash
    }
}

impl PaxeerBindingVerifierV2 {
    pub fn new(config: PaxeerBindingConfigV2) -> Result<Self, MigrationError> {
        if config.chain_id != PAXEER_CHAIN_ID || config.genesis_hash == [0; 32] {
            return Err(MigrationError::InvalidNetwork);
        }
        let rpc = RpcCluster::new(&config.rpc)?;
        Ok(Self { config, rpc })
    }

    pub fn verify_identity(
        &self,
        expected_did: [u8; 32],
    ) -> Result<ConfirmedPaxeerBindingV2, MigrationError> {
        if expected_did == [0; 32] {
            return Err(MigrationError::InvalidEvidence);
        }
        self.verify_network()?;
        let finalized = self.finalized_block()?;
        let mut forward = Vec::with_capacity(36);
        forward.extend_from_slice(&GET_EVM_ADDRESS);
        forward.extend_from_slice(&expected_did);
        let address = parse_address(&self.call_precompile(&forward, finalized.number, 32)?)?;

        let mut reverse = Vec::with_capacity(36);
        reverse.extend_from_slice(&GET_LAYERX_DID);
        reverse.extend_from_slice(&[0; 12]);
        reverse.extend_from_slice(&address);
        parse_identity(
            &self.call_precompile(&reverse, finalized.number, MAX_DID_RESPONSE_BYTES)?,
            expected_did,
        )?;

        let canonical = self.block_by_number(finalized.number)?;
        if canonical.hash != finalized.hash || canonical.parent_hash != finalized.parent_hash {
            return Err(MigrationError::SourceDisplaced);
        }
        let current_finalized = self.finalized_block()?;
        if current_finalized.number < finalized.number {
            return Err(MigrationError::SourcePending);
        }
        if current_finalized.number == finalized.number
            && (current_finalized.hash != finalized.hash
                || current_finalized.parent_hash != finalized.parent_hash)
        {
            return Err(MigrationError::SourceDisplaced);
        }
        Ok(ConfirmedPaxeerBindingV2 {
            identity: expected_did,
            evm_address: address,
            block_number: finalized.number,
            block_hash: finalized.hash,
        })
    }

    fn verify_network(&self) -> Result<(), MigrationError> {
        let chain_id = self.rpc.call("eth_chainId", json!([]))?;
        let chain_id = decode_quantity(
            chain_id.as_str().ok_or(MigrationError::RpcResponseMismatch)?,
        )?;
        if chain_id != self.config.chain_id {
            return Err(MigrationError::InvalidNetwork);
        }
        let genesis = self.rpc.call("eth_getBlockByNumber", json!(["earliest", false]))?;
        if parse_block(&genesis)?.hash != self.config.genesis_hash {
            return Err(MigrationError::InvalidNetwork);
        }
        Ok(())
    }

    fn finalized_block(&self) -> Result<BindingBlock, MigrationError> {
        let value = self.rpc.call("eth_getBlockByNumber", json!(["finalized", false]))?;
        let block = parse_block(&value)?;
        if block.number == 0 {
            return Err(MigrationError::SourcePending);
        }
        if block.parent_hash == [0; 32] {
            return Err(MigrationError::RpcResponseMismatch);
        }
        Ok(block)
    }

    fn block_by_number(&self, number: u64) -> Result<BindingBlock, MigrationError> {
        let value = self.rpc.call("eth_getBlockByNumber", json!([quantity(number), false]))?;
        let block = parse_block(&value)?;
        if block.number != number || (number != 0 && block.parent_hash == [0; 32]) {
            return Err(MigrationError::RpcResponseMismatch);
        }
        Ok(block)
    }

    fn call_precompile(
        &self,
        input: &[u8],
        block_number: u64,
        maximum_bytes: usize,
    ) -> Result<Vec<u8>, MigrationError> {
        let result = self.rpc.call(
            "eth_call",
            json!([{"to": ADDRESS_PRECOMPILE, "data": ethereum_hex(input)}, quantity(block_number)]),
        )?;
        let encoded = result.as_str().ok_or(MigrationError::RpcResponseMismatch)?;
        if encoded.len() > 2 + maximum_bytes * 2 {
            return Err(MigrationError::RpcResponseMismatch);
        }
        decode_hex(encoded)
    }
}

struct BindingBlock {
    number: u64,
    hash: [u8; 32],
    parent_hash: [u8; 32],
}

fn parse_block(value: &Value) -> Result<BindingBlock, MigrationError> {
    if value.is_null() {
        return Err(MigrationError::SourcePending);
    }
    let number = decode_quantity(field_string(value, "number")?)?;
    let hash = decode_fixed_hex(field_string(value, "hash")?)?;
    let parent_hash = decode_fixed_hex(field_string(value, "parentHash")?)?;
    if hash == [0; 32] {
        return Err(MigrationError::RpcResponseMismatch);
    }
    Ok(BindingBlock { number, hash, parent_hash })
}

fn field_string<'a>(value: &'a Value, key: &str) -> Result<&'a str, MigrationError> {
    value.get(key).and_then(Value::as_str).ok_or(MigrationError::RpcResponseMismatch)
}

fn parse_address(bytes: &[u8]) -> Result<[u8; 20], MigrationError> {
    if bytes.len() != 32 || bytes[..12] != [0; 12] {
        return Err(MigrationError::RpcResponseMismatch);
    }
    let address: [u8; 20] = bytes[12..].try_into().map_err(|_| MigrationError::RpcResponseMismatch)?;
    if address == [0; 20] {
        return Err(MigrationError::SourcePending);
    }
    Ok(address)
}

fn parse_identity(bytes: &[u8], expected: [u8; 32]) -> Result<(), MigrationError> {
    if bytes.len() < 96 || bytes.len() > MAX_DID_RESPONSE_BYTES {
        return Err(MigrationError::RpcResponseMismatch);
    }
    let identity: [u8; 32] = bytes[..32].try_into().map_err(|_| MigrationError::RpcResponseMismatch)?;
    if abi_length(&bytes[32..64])? != 64 {
        return Err(MigrationError::RpcResponseMismatch);
    }
    let length = abi_length(&bytes[64..96])?;
    if length > MAX_DID_BYTES {
        return Err(MigrationError::RpcResponseMismatch);
    }
    let padded_length = length.div_ceil(32) * 32;
    if bytes.len() != 96 + padded_length || bytes[96 + length..].iter().any(|byte| *byte != 0) {
        return Err(MigrationError::RpcResponseMismatch);
    }
    if identity == [0; 32] && length == 0 {
        return Err(MigrationError::SourcePending);
    }
    if identity != expected {
        return Err(MigrationError::EvidenceMismatch);
    }
    let rendered = format!("{DID_PREFIX}{}", hex(&expected));
    if bytes[96..96 + length] != *rendered.as_bytes() {
        return Err(MigrationError::EvidenceMismatch);
    }
    Ok(())
}

fn abi_length(word: &[u8]) -> Result<usize, MigrationError> {
    if word.len() != 32 || word[..24].iter().any(|byte| *byte != 0) {
        return Err(MigrationError::RpcResponseMismatch);
    }
    let value = u64::from_be_bytes(word[24..].try_into().map_err(|_| MigrationError::RpcResponseMismatch)?);
    usize::try_from(value).map_err(|_| MigrationError::RpcResponseMismatch)
}
