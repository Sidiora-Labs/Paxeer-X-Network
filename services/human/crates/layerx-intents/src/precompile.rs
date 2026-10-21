//! Paxeer precompile event decoding and routing into `LayerX` intents.

use layerx_types::payload::{PerpsPayload, PerpsTimeInForce, TradeSide};

pub use crate::keccak::keccak256;
use crate::{
    BridgeWithdrawRequest, Intent, IntentError, IntentErrorReason, IntentField, IntentKind,
    NativeCustodyCredit,
};

const fn precompile_address(low: u16) -> [u8; 20] {
    let mut address = [0_u8; 20];
    let bytes = low.to_be_bytes();
    address[18] = bytes[0];
    address[19] = bytes[1];
    address
}

/// The `LayerXExchange` precompile address.
pub const EXCHANGE_PRECOMPILE: [u8; 20] = precompile_address(0x1015);
/// The `LayerXBridge` precompile address.
pub const BRIDGE_PRECOMPILE: [u8; 20] = precompile_address(0x1016);
/// The `Launchpad` precompile address.
pub const LAUNCHPAD_PRECOMPILE: [u8; 20] = precompile_address(0x1017);

/// Every event declared by the exchange, bridge and launchpad precompile ABIs.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub enum PrecompileEventKind {
    MarginDeposited,
    MarginWithdrawalRequested,
    OrderCancelRequested,
    OrderPlaced,
    SettlementRequested,
    BridgeIn,
    BridgeOut,
    ConfigUpdated,
    FeeDistributed,
    FeeFlow,
    FeeStrategyChanged,
    MarketCreated,
    NftMint,
    NftTransfer,
    PoolRegistered,
    PoolStateUpdated,
    RouterTrade,
    Swap,
    TokenDeployed,
}

impl PrecompileEventKind {
    pub const ALL: [Self; 19] = [
        Self::MarginDeposited,
        Self::MarginWithdrawalRequested,
        Self::OrderCancelRequested,
        Self::OrderPlaced,
        Self::SettlementRequested,
        Self::BridgeIn,
        Self::BridgeOut,
        Self::ConfigUpdated,
        Self::FeeDistributed,
        Self::FeeFlow,
        Self::FeeStrategyChanged,
        Self::MarketCreated,
        Self::NftMint,
        Self::NftTransfer,
        Self::PoolRegistered,
        Self::PoolStateUpdated,
        Self::RouterTrade,
        Self::Swap,
        Self::TokenDeployed,
    ];

    /// Returns the precompile that emits this event.
    #[must_use]
    pub const fn precompile(self) -> [u8; 20] {
        match self {
            Self::MarginDeposited
            | Self::MarginWithdrawalRequested
            | Self::OrderCancelRequested
            | Self::OrderPlaced
            | Self::SettlementRequested => EXCHANGE_PRECOMPILE,
            Self::BridgeIn | Self::BridgeOut => BRIDGE_PRECOMPILE,
            _ => LAUNCHPAD_PRECOMPILE,
        }
    }

    /// Returns the canonical ABI event signature.
    #[must_use]
    pub const fn signature(self) -> &'static str {
        match self {
            Self::MarginDeposited => {
                "MarginDeposited(bytes32,bytes32,address,bytes32,uint256,bytes32,uint64)"
            }
            Self::MarginWithdrawalRequested => {
                "MarginWithdrawalRequested(bytes32,bytes32,address,bytes32,uint256,uint64)"
            }
            Self::OrderCancelRequested => "OrderCancelRequested(bytes32,bytes32,address,uint64)",
            Self::OrderPlaced => {
                "OrderPlaced(bytes32,bytes32,address,uint8,uint256,uint256,uint8,uint64)"
            }
            Self::SettlementRequested => "SettlementRequested(bytes32,bytes32,address,uint64)",
            Self::BridgeIn => "BridgeIn(uint64,bytes32,address,uint64,address,uint256,string)",
            Self::BridgeOut => "BridgeOut(uint64,address,uint256,address,uint64)",
            Self::ConfigUpdated => "ConfigUpdated(bytes32,uint256,uint256,uint256,uint256)",
            Self::FeeDistributed => {
                "FeeDistributed(bytes32,uint256,uint8,uint256,address,uint256,uint256)"
            }
            Self::FeeFlow => {
                "FeeFlow(uint8,address,address,uint256,uint256,uint256,uint256,uint256,uint256)"
            }
            Self::FeeStrategyChanged => {
                "FeeStrategyChanged(bytes32,uint256,uint8,uint8,uint256,uint256)"
            }
            Self::MarketCreated => {
                "MarketCreated(bytes32,address,address,address,address,uint256,uint256)"
            }
            Self::NftMint => "NftMint(uint256,address,address,uint8,uint256,uint256)",
            Self::NftTransfer => "NftTransfer(address,address,address,uint256,uint256,uint256)",
            Self::PoolRegistered => {
                "PoolRegistered(address,address,address,address,uint256,uint256,uint256)"
            }
            Self::PoolStateUpdated => {
                "PoolStateUpdated(bytes32,uint256,uint256,uint256,uint256,uint256,uint256)"
            }
            Self::RouterTrade => {
                "RouterTrade(uint8,address,address,address,address,uint256,uint256,uint256,uint256,uint256)"
            }
            Self::Swap => {
                "Swap(bytes32,address,bool,uint256,uint256,uint256,uint256,uint256,uint256)"
            }
            Self::TokenDeployed => {
                "TokenDeployed(address,address,address,bytes32,string,string,uint8,uint256,uint256,uint256)"
            }
        }
    }

    /// Returns the event's `topic0`, the Keccak-256 of its signature.
    #[must_use]
    pub fn topic0(self) -> [u8; 32] {
        keccak256(self.signature().as_bytes())
    }

    /// Identifies the event a precompile log carries.
    #[must_use]
    pub fn identify(address: [u8; 20], topic0: [u8; 32]) -> Option<Self> {
        Self::ALL
            .into_iter()
            .find(|kind| kind.precompile() == address && kind.topic0() == topic0)
    }
}

/// One EVM log as returned by a receipt or `eth_getLogs`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EvmLog<'a> {
    pub address: [u8; 20],
    pub topics: &'a [[u8; 32]],
    pub data: &'a [u8],
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MarginDeposited {
    pub intent_id: [u8; 32],
    pub account: [u8; 32],
    pub owner: [u8; 20],
    pub asset_id: [u8; 32],
    pub amount: [u8; 32],
    pub deposit_id: [u8; 32],
    pub nonce: u64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MarginWithdrawalRequested {
    pub intent_id: [u8; 32],
    pub account: [u8; 32],
    pub owner: [u8; 20],
    pub asset_id: [u8; 32],
    pub amount: [u8; 32],
    pub nonce: u64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct OrderCancelRequested {
    pub intent_id: [u8; 32],
    pub order_id: [u8; 32],
    pub owner: [u8; 20],
    pub nonce: u64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct OrderPlaced {
    pub intent_id: [u8; 32],
    pub market_id: [u8; 32],
    pub owner: [u8; 20],
    pub side: u8,
    pub price: [u8; 32],
    pub quantity: [u8; 32],
    pub time_in_force: u8,
    pub nonce: u64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SettlementRequested {
    pub intent_id: [u8; 32],
    pub position_id: [u8; 32],
    pub owner: [u8; 20],
    pub nonce: u64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BridgeIn {
    pub chain: u64,
    pub tx_hash: [u8; 32],
    pub recipient: [u8; 20],
    pub log_index: u64,
    pub asset: [u8; 20],
    pub amount: [u8; 32],
    pub denom: String,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BridgeOut {
    pub chain: u64,
    pub asset: [u8; 20],
    pub amount: [u8; 32],
    pub recipient: [u8; 20],
    pub nonce: u64,
}

/// `ConfigUpdated` of the `EventEmitter` family at the launchpad precompile.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ConfigUpdated {
    pub key: [u8; 32],
    pub old_value: [u8; 32],
    pub new_value: [u8; 32],
    pub timestamp: [u8; 32],
    pub block_number: [u8; 32],
}

/// `FeeDistributed` of the `EventEmitter` family at the launchpad precompile.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct FeeDistributed {
    pub pool_id: [u8; 32],
    pub nft_id: [u8; 32],
    pub strategy: u8,
    pub amount: [u8; 32],
    pub recipient: [u8; 20],
    pub timestamp: [u8; 32],
    pub block_number: [u8; 32],
}

/// `FeeFlow` of the `EventEmitter` family at the launchpad precompile.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct FeeFlow {
    pub kind: u8,
    pub pool: [u8; 20],
    pub party: [u8; 20],
    pub amount: [u8; 32],
    pub protocol_cut: [u8; 32],
    pub pool_cut: [u8; 32],
    pub epoch: [u8; 32],
    pub timestamp: [u8; 32],
    pub block_number: [u8; 32],
}

/// `FeeStrategyChanged` of the `EventEmitter` family at the launchpad precompile.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct FeeStrategyChanged {
    pub pool_id: [u8; 32],
    pub nft_id: [u8; 32],
    pub old_strategy: u8,
    pub new_strategy: u8,
    pub timestamp: [u8; 32],
    pub block_number: [u8; 32],
}

/// `MarketCreated` of the `EventEmitter` family at the launchpad precompile.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MarketCreated {
    pub pool_id: [u8; 32],
    pub token: [u8; 20],
    pub creator: [u8; 20],
    pub pool: [u8; 20],
    pub optical: [u8; 20],
    pub timestamp: [u8; 32],
    pub block_number: [u8; 32],
}

/// `NftMint` of the `EventEmitter` family at the launchpad precompile.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NftMint {
    pub token_id: [u8; 32],
    pub creator: [u8; 20],
    pub pool: [u8; 20],
    pub strategy: u8,
    pub timestamp: [u8; 32],
    pub block_number: [u8; 32],
}

/// `NftTransfer` of the `EventEmitter` family at the launchpad precompile.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NftTransfer {
    pub nft: [u8; 20],
    pub from: [u8; 20],
    pub to: [u8; 20],
    pub token_id: [u8; 32],
    pub timestamp: [u8; 32],
    pub block_number: [u8; 32],
}

/// `PoolRegistered` of the `EventEmitter` family at the launchpad precompile.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PoolRegistered {
    pub pool: [u8; 20],
    pub token: [u8; 20],
    pub creator: [u8; 20],
    pub optical: [u8; 20],
    pub nft_id: [u8; 32],
    pub timestamp: [u8; 32],
    pub block_number: [u8; 32],
}

/// `PoolStateUpdated` of the `EventEmitter` family at the launchpad precompile.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PoolStateUpdated {
    pub pool_id: [u8; 32],
    pub virtual_reserve: [u8; 32],
    pub real_reserve: [u8; 32],
    pub token_reserve: [u8; 32],
    pub price: [u8; 32],
    pub timestamp: [u8; 32],
    pub block_number: [u8; 32],
}

/// `RouterTrade` of the `EventEmitter` family at the launchpad precompile.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RouterTrade {
    pub kind: u8,
    pub pool: [u8; 20],
    pub sender: [u8; 20],
    pub token_in: [u8; 20],
    pub token_out: [u8; 20],
    pub amount_in: [u8; 32],
    pub amount_out: [u8; 32],
    pub intermediate_usdl: [u8; 32],
    pub timestamp: [u8; 32],
    pub block_number: [u8; 32],
}

/// `Swap` of the `EventEmitter` family at the launchpad precompile.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Swap {
    pub pool_id: [u8; 32],
    pub trader: [u8; 20],
    pub is_buy: bool,
    pub amount_in: [u8; 32],
    pub amount_out: [u8; 32],
    pub fee: [u8; 32],
    pub price: [u8; 32],
    pub timestamp: [u8; 32],
    pub block_number: [u8; 32],
}

/// `TokenDeployed` of the `EventEmitter` family at the launchpad precompile.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TokenDeployed {
    pub token: [u8; 20],
    pub pool: [u8; 20],
    pub creator: [u8; 20],
    pub salt: [u8; 32],
    pub name: String,
    pub symbol: String,
    pub decimals: u8,
    pub total_supply: [u8; 32],
    pub timestamp: [u8; 32],
    pub block_number: [u8; 32],
}

/// A decoded precompile event.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum PrecompileEvent {
    MarginDeposited(MarginDeposited),
    MarginWithdrawalRequested(MarginWithdrawalRequested),
    OrderCancelRequested(OrderCancelRequested),
    OrderPlaced(OrderPlaced),
    SettlementRequested(SettlementRequested),
    BridgeIn(BridgeIn),
    BridgeOut(BridgeOut),
    ConfigUpdated(ConfigUpdated),
    FeeDistributed(FeeDistributed),
    FeeFlow(FeeFlow),
    FeeStrategyChanged(FeeStrategyChanged),
    MarketCreated(MarketCreated),
    NftMint(NftMint),
    NftTransfer(NftTransfer),
    PoolRegistered(PoolRegistered),
    PoolStateUpdated(PoolStateUpdated),
    RouterTrade(RouterTrade),
    Swap(Swap),
    TokenDeployed(TokenDeployed),
}

/// Refusal to decode a log as a precompile event.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EventDecodeError {
    /// The log names no declared event of a known precompile.
    UnknownEvent,
    /// The log's topic count does not match the event's indexed inputs.
    TopicCount(usize),
    /// The log data does not match the event's non-indexed inputs.
    DataLength(usize),
    /// A value word is not the canonical ABI encoding of its type.
    NonCanonicalWord,
    /// A dynamic string is out of bounds or not UTF-8.
    InvalidString,
}

struct Abi<'a> {
    topics: &'a [[u8; 32]],
    data: &'a [u8],
}

impl Abi<'_> {
    fn shape(&self, indexed: usize, head: usize, dynamic: bool) -> Result<(), EventDecodeError> {
        if self.topics.len() != indexed + 1 {
            return Err(EventDecodeError::TopicCount(self.topics.len()));
        }
        let length = self.data.len();
        if !length.is_multiple_of(32) || length < head * 32 || (!dynamic && length != head * 32) {
            return Err(EventDecodeError::DataLength(length));
        }
        Ok(())
    }

    fn topic(&self, index: usize) -> Result<[u8; 32], EventDecodeError> {
        self.topics
            .get(index)
            .copied()
            .ok_or(EventDecodeError::TopicCount(self.topics.len()))
    }

    fn word(&self, index: usize) -> Result<[u8; 32], EventDecodeError> {
        word_at(self.data, index * 32)
    }

    fn string(&self, index: usize) -> Result<String, EventDecodeError> {
        let invalid = |_| EventDecodeError::InvalidString;
        let offset = usize::try_from(uint64(self.word(index)?)?).map_err(invalid)?;
        let length = usize::try_from(uint64(word_at(self.data, offset)?)?).map_err(invalid)?;
        let start = offset
            .checked_add(32)
            .ok_or(EventDecodeError::InvalidString)?;
        let end = start
            .checked_add(length)
            .ok_or(EventDecodeError::InvalidString)?;
        let bytes = self
            .data
            .get(start..end)
            .ok_or(EventDecodeError::InvalidString)?;
        String::from_utf8(bytes.to_vec()).map_err(|_| EventDecodeError::InvalidString)
    }
}

fn word_at(data: &[u8], offset: usize) -> Result<[u8; 32], EventDecodeError> {
    data.get(offset..)
        .and_then(<[u8]>::first_chunk::<32>)
        .copied()
        .ok_or(EventDecodeError::DataLength(data.len()))
}

fn left_zero(word: &[u8; 32], width: usize) -> Result<(), EventDecodeError> {
    if word[..32 - width].iter().all(|byte| *byte == 0) {
        Ok(())
    } else {
        Err(EventDecodeError::NonCanonicalWord)
    }
}

fn address(word: [u8; 32]) -> Result<[u8; 20], EventDecodeError> {
    left_zero(&word, 20)?;
    let mut out = [0_u8; 20];
    out.copy_from_slice(&word[12..]);
    Ok(out)
}

fn uint64(word: [u8; 32]) -> Result<u64, EventDecodeError> {
    left_zero(&word, 8)?;
    let mut out = [0_u8; 8];
    out.copy_from_slice(&word[24..]);
    Ok(u64::from_be_bytes(out))
}

fn uint8(word: [u8; 32]) -> Result<u8, EventDecodeError> {
    left_zero(&word, 1)?;
    Ok(word[31])
}

fn boolean(word: [u8; 32]) -> Result<bool, EventDecodeError> {
    match uint8(word)? {
        0 => Ok(false),
        1 => Ok(true),
        _ => Err(EventDecodeError::NonCanonicalWord),
    }
}

/// Returns a `uint256` word as `u128` when it fits the `LayerX` amount width.
#[must_use]
pub fn uint256_to_u128(word: &[u8; 32]) -> Option<u128> {
    left_zero(word, 16).ok()?;
    let mut out = [0_u8; 16];
    out.copy_from_slice(&word[16..]);
    Some(u128::from_be_bytes(out))
}

impl PrecompileEvent {
    /// Decodes a log emitted by the exchange, bridge or launchpad precompile.
    ///
    /// # Errors
    ///
    /// Refuses a log from another address, an undeclared `topic0`, a wrong
    /// topic or data shape, or a non-canonical ABI word.
    pub fn decode(log: &EvmLog<'_>) -> Result<Self, EventDecodeError> {
        let first = log.topics.first().ok_or(EventDecodeError::TopicCount(0))?;
        let kind = PrecompileEventKind::identify(log.address, *first)
            .ok_or(EventDecodeError::UnknownEvent)?;
        let abi = Abi {
            topics: log.topics,
            data: log.data,
        };
        match kind {
            PrecompileEventKind::MarginDeposited
            | PrecompileEventKind::MarginWithdrawalRequested
            | PrecompileEventKind::OrderCancelRequested
            | PrecompileEventKind::OrderPlaced
            | PrecompileEventKind::SettlementRequested => decode_exchange(kind, &abi),
            PrecompileEventKind::BridgeIn | PrecompileEventKind::BridgeOut => {
                decode_bridge(kind, &abi)
            }
            _ => decode_launchpad(kind, &abi),
        }
    }

    /// Returns the declared event kind.
    #[must_use]
    pub const fn kind(&self) -> PrecompileEventKind {
        match self {
            Self::MarginDeposited(_) => PrecompileEventKind::MarginDeposited,
            Self::MarginWithdrawalRequested(_) => PrecompileEventKind::MarginWithdrawalRequested,
            Self::OrderCancelRequested(_) => PrecompileEventKind::OrderCancelRequested,
            Self::OrderPlaced(_) => PrecompileEventKind::OrderPlaced,
            Self::SettlementRequested(_) => PrecompileEventKind::SettlementRequested,
            Self::BridgeIn(_) => PrecompileEventKind::BridgeIn,
            Self::BridgeOut(_) => PrecompileEventKind::BridgeOut,
            Self::ConfigUpdated(_) => PrecompileEventKind::ConfigUpdated,
            Self::FeeDistributed(_) => PrecompileEventKind::FeeDistributed,
            Self::FeeFlow(_) => PrecompileEventKind::FeeFlow,
            Self::FeeStrategyChanged(_) => PrecompileEventKind::FeeStrategyChanged,
            Self::MarketCreated(_) => PrecompileEventKind::MarketCreated,
            Self::NftMint(_) => PrecompileEventKind::NftMint,
            Self::NftTransfer(_) => PrecompileEventKind::NftTransfer,
            Self::PoolRegistered(_) => PrecompileEventKind::PoolRegistered,
            Self::PoolStateUpdated(_) => PrecompileEventKind::PoolStateUpdated,
            Self::RouterTrade(_) => PrecompileEventKind::RouterTrade,
            Self::Swap(_) => PrecompileEventKind::Swap,
            Self::TokenDeployed(_) => PrecompileEventKind::TokenDeployed,
        }
    }
}

fn decode_exchange(
    kind: PrecompileEventKind,
    abi: &Abi<'_>,
) -> Result<PrecompileEvent, EventDecodeError> {
    Ok(match kind {
        PrecompileEventKind::MarginDeposited => {
            abi.shape(3, 4, false)?;
            PrecompileEvent::MarginDeposited(MarginDeposited {
                intent_id: abi.topic(1)?,
                account: abi.topic(2)?,
                owner: address(abi.topic(3)?)?,
                asset_id: abi.word(0)?,
                amount: abi.word(1)?,
                deposit_id: abi.word(2)?,
                nonce: uint64(abi.word(3)?)?,
            })
        }
        PrecompileEventKind::MarginWithdrawalRequested => {
            abi.shape(3, 3, false)?;
            PrecompileEvent::MarginWithdrawalRequested(MarginWithdrawalRequested {
                intent_id: abi.topic(1)?,
                account: abi.topic(2)?,
                owner: address(abi.topic(3)?)?,
                asset_id: abi.word(0)?,
                amount: abi.word(1)?,
                nonce: uint64(abi.word(2)?)?,
            })
        }
        PrecompileEventKind::OrderCancelRequested => {
            abi.shape(3, 1, false)?;
            PrecompileEvent::OrderCancelRequested(OrderCancelRequested {
                intent_id: abi.topic(1)?,
                order_id: abi.topic(2)?,
                owner: address(abi.topic(3)?)?,
                nonce: uint64(abi.word(0)?)?,
            })
        }
        PrecompileEventKind::OrderPlaced => {
            abi.shape(3, 5, false)?;
            PrecompileEvent::OrderPlaced(OrderPlaced {
                intent_id: abi.topic(1)?,
                market_id: abi.topic(2)?,
                owner: address(abi.topic(3)?)?,
                side: uint8(abi.word(0)?)?,
                price: abi.word(1)?,
                quantity: abi.word(2)?,
                time_in_force: uint8(abi.word(3)?)?,
                nonce: uint64(abi.word(4)?)?,
            })
        }
        _ => {
            abi.shape(3, 1, false)?;
            PrecompileEvent::SettlementRequested(SettlementRequested {
                intent_id: abi.topic(1)?,
                position_id: abi.topic(2)?,
                owner: address(abi.topic(3)?)?,
                nonce: uint64(abi.word(0)?)?,
            })
        }
    })
}

fn decode_bridge(
    kind: PrecompileEventKind,
    abi: &Abi<'_>,
) -> Result<PrecompileEvent, EventDecodeError> {
    if kind == PrecompileEventKind::BridgeIn {
        abi.shape(3, 4, true)?;
        return Ok(PrecompileEvent::BridgeIn(BridgeIn {
            chain: uint64(abi.topic(1)?)?,
            tx_hash: abi.topic(2)?,
            recipient: address(abi.topic(3)?)?,
            log_index: uint64(abi.word(0)?)?,
            asset: address(abi.word(1)?)?,
            amount: abi.word(2)?,
            denom: abi.string(3)?,
        }));
    }
    abi.shape(3, 2, false)?;
    Ok(PrecompileEvent::BridgeOut(BridgeOut {
        chain: uint64(abi.topic(1)?)?,
        asset: address(abi.topic(2)?)?,
        amount: abi.word(0)?,
        recipient: address(abi.word(1)?)?,
        nonce: uint64(abi.topic(3)?)?,
    }))
}

fn decode_launchpad(
    kind: PrecompileEventKind,
    abi: &Abi<'_>,
) -> Result<PrecompileEvent, EventDecodeError> {
    Ok(match kind {
        PrecompileEventKind::ConfigUpdated => {
            abi.shape(1, 4, false)?;
            PrecompileEvent::ConfigUpdated(ConfigUpdated {
                key: abi.topic(1)?,
                old_value: abi.word(0)?,
                new_value: abi.word(1)?,
                timestamp: abi.word(2)?,
                block_number: abi.word(3)?,
            })
        }
        PrecompileEventKind::FeeDistributed => {
            abi.shape(1, 6, false)?;
            PrecompileEvent::FeeDistributed(FeeDistributed {
                pool_id: abi.topic(1)?,
                nft_id: abi.word(0)?,
                strategy: uint8(abi.word(1)?)?,
                amount: abi.word(2)?,
                recipient: address(abi.word(3)?)?,
                timestamp: abi.word(4)?,
                block_number: abi.word(5)?,
            })
        }
        PrecompileEventKind::FeeFlow => {
            abi.shape(3, 6, false)?;
            PrecompileEvent::FeeFlow(FeeFlow {
                kind: uint8(abi.topic(1)?)?,
                pool: address(abi.topic(2)?)?,
                party: address(abi.topic(3)?)?,
                amount: abi.word(0)?,
                protocol_cut: abi.word(1)?,
                pool_cut: abi.word(2)?,
                epoch: abi.word(3)?,
                timestamp: abi.word(4)?,
                block_number: abi.word(5)?,
            })
        }
        PrecompileEventKind::FeeStrategyChanged => {
            abi.shape(1, 5, false)?;
            PrecompileEvent::FeeStrategyChanged(FeeStrategyChanged {
                pool_id: abi.topic(1)?,
                nft_id: abi.word(0)?,
                old_strategy: uint8(abi.word(1)?)?,
                new_strategy: uint8(abi.word(2)?)?,
                timestamp: abi.word(3)?,
                block_number: abi.word(4)?,
            })
        }
        PrecompileEventKind::NftMint => {
            abi.shape(3, 3, false)?;
            PrecompileEvent::NftMint(NftMint {
                token_id: abi.topic(1)?,
                creator: address(abi.topic(2)?)?,
                pool: address(abi.topic(3)?)?,
                strategy: uint8(abi.word(0)?)?,
                timestamp: abi.word(1)?,
                block_number: abi.word(2)?,
            })
        }
        PrecompileEventKind::NftTransfer => {
            abi.shape(3, 3, false)?;
            PrecompileEvent::NftTransfer(NftTransfer {
                nft: address(abi.topic(1)?)?,
                from: address(abi.topic(2)?)?,
                to: address(abi.topic(3)?)?,
                token_id: abi.word(0)?,
                timestamp: abi.word(1)?,
                block_number: abi.word(2)?,
            })
        }
        _ => return decode_launchpad_market(kind, abi),
    })
}

fn decode_launchpad_market(
    kind: PrecompileEventKind,
    abi: &Abi<'_>,
) -> Result<PrecompileEvent, EventDecodeError> {
    Ok(match kind {
        PrecompileEventKind::MarketCreated => {
            abi.shape(3, 4, false)?;
            PrecompileEvent::MarketCreated(MarketCreated {
                pool_id: abi.topic(1)?,
                token: address(abi.topic(2)?)?,
                creator: address(abi.topic(3)?)?,
                pool: address(abi.word(0)?)?,
                optical: address(abi.word(1)?)?,
                timestamp: abi.word(2)?,
                block_number: abi.word(3)?,
            })
        }
        PrecompileEventKind::PoolRegistered => {
            abi.shape(3, 4, false)?;
            PrecompileEvent::PoolRegistered(PoolRegistered {
                pool: address(abi.topic(1)?)?,
                token: address(abi.topic(2)?)?,
                creator: address(abi.topic(3)?)?,
                optical: address(abi.word(0)?)?,
                nft_id: abi.word(1)?,
                timestamp: abi.word(2)?,
                block_number: abi.word(3)?,
            })
        }
        PrecompileEventKind::PoolStateUpdated => {
            abi.shape(1, 6, false)?;
            PrecompileEvent::PoolStateUpdated(PoolStateUpdated {
                pool_id: abi.topic(1)?,
                virtual_reserve: abi.word(0)?,
                real_reserve: abi.word(1)?,
                token_reserve: abi.word(2)?,
                price: abi.word(3)?,
                timestamp: abi.word(4)?,
                block_number: abi.word(5)?,
            })
        }
        PrecompileEventKind::RouterTrade => {
            abi.shape(3, 7, false)?;
            PrecompileEvent::RouterTrade(RouterTrade {
                kind: uint8(abi.topic(1)?)?,
                pool: address(abi.topic(2)?)?,
                sender: address(abi.topic(3)?)?,
                token_in: address(abi.word(0)?)?,
                token_out: address(abi.word(1)?)?,
                amount_in: abi.word(2)?,
                amount_out: abi.word(3)?,
                intermediate_usdl: abi.word(4)?,
                timestamp: abi.word(5)?,
                block_number: abi.word(6)?,
            })
        }
        PrecompileEventKind::Swap => {
            abi.shape(2, 7, false)?;
            PrecompileEvent::Swap(Swap {
                pool_id: abi.topic(1)?,
                trader: address(abi.topic(2)?)?,
                is_buy: boolean(abi.word(0)?)?,
                amount_in: abi.word(1)?,
                amount_out: abi.word(2)?,
                fee: abi.word(3)?,
                price: abi.word(4)?,
                timestamp: abi.word(5)?,
                block_number: abi.word(6)?,
            })
        }
        _ => {
            abi.shape(3, 7, true)?;
            PrecompileEvent::TokenDeployed(TokenDeployed {
                token: address(abi.topic(1)?)?,
                pool: address(abi.topic(2)?)?,
                creator: address(abi.topic(3)?)?,
                salt: abi.word(0)?,
                name: abi.string(1)?,
                symbol: abi.string(2)?,
                decimals: uint8(abi.word(3)?)?,
                total_supply: abi.word(4)?,
                timestamp: abi.word(5)?,
                block_number: abi.word(6)?,
            })
        }
    })
}

fn amount(word: &[u8; 32]) -> Result<u128, IntentError> {
    match uint256_to_u128(word) {
        Some(0) => Err(IntentError {
            field: IntentField::Amount,
            reason: IntentErrorReason::Zero,
        }),
        Some(value) => Ok(value),
        None => Err(IntentError {
            field: IntentField::Amount,
            reason: IntentErrorReason::InvalidRange,
        }),
    }
}

fn nonzero(id: &[u8; 32], field: IntentField) -> Result<(), IntentError> {
    if id.iter().all(|byte| *byte == 0) {
        Err(IntentError {
            field,
            reason: IntentErrorReason::Zero,
        })
    } else {
        Ok(())
    }
}

fn mismatch(field: IntentField, matches: bool) -> Result<(), IntentError> {
    if matches {
        Ok(())
    } else {
        Err(IntentError {
            field,
            reason: IntentErrorReason::EventMismatch,
        })
    }
}

fn perps(payload: PerpsPayload) -> Result<PerpsPayload, IntentError> {
    payload.encode().map_err(|_| IntentError {
        field: IntentField::PrecompileEvent,
        reason: IntentErrorReason::InvalidCanonicalEncoding,
    })?;
    Ok(payload)
}

/// An exchange `OrderPlaced` event routed to a perps `ORDER_PLACE` activity.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExchangeOrder {
    pub(crate) event: OrderPlaced,
    pub(crate) payload: PerpsPayload,
}

impl ExchangeOrder {
    /// Binds the order to the `LayerX` margin account that owns it. The perps
    /// order identifier is the exchange intent identifier.
    ///
    /// # Errors
    ///
    /// Refuses a side outside buy/sell, a time in force outside
    /// GTC/IOC/FOK/post-only, zero or over-wide values.
    pub fn new(event: OrderPlaced, owner_account_id: [u8; 32]) -> Result<Self, IntentError> {
        Self::new_for_abi(event, owner_account_id, 1)
    }

    /// Binds the order under the given perps ABI version.
    ///
    /// # Errors
    ///
    /// Refuses a perps ABI version outside 1 to 3, a non-zero time in force
    /// before version 3, and every refusal of [`Self::new`].
    pub fn new_for_abi(
        event: OrderPlaced,
        owner_account_id: [u8; 32],
        perps_abi_version: u32,
    ) -> Result<Self, IntentError> {
        if !(1..=3).contains(&perps_abi_version)
            || (event.time_in_force != 0 && perps_abi_version != 3)
        {
            return Err(IntentError {
                field: IntentField::TimeInForce,
                reason: IntentErrorReason::InvalidRange,
            });
        }
        let side = match event.side {
            1 => TradeSide::Buy,
            2 => TradeSide::Sell,
            _ => {
                return Err(IntentError {
                    field: IntentField::Side,
                    reason: IntentErrorReason::InvalidRange,
                })
            }
        };
        let Ok(time_in_force) = PerpsTimeInForce::from_byte(event.time_in_force) else {
            return Err(IntentError {
                field: IntentField::TimeInForce,
                reason: IntentErrorReason::InvalidRange,
            });
        };
        nonzero(&event.market_id, IntentField::Market)?;
        nonzero(&event.intent_id, IntentField::Order)?;
        nonzero(&owner_account_id, IntentField::Account)?;
        let payload = perps(PerpsPayload::OrderPlace {
            market_id: event.market_id,
            order_id: event.intent_id,
            owner_account_id,
            side,
            price: amount(&event.price)?,
            quantity: amount(&event.quantity)?,
            time_in_force,
        })?;
        Ok(Self { event, payload })
    }

    #[must_use]
    pub const fn event(&self) -> &OrderPlaced {
        &self.event
    }

    #[must_use]
    pub const fn payload(&self) -> &PerpsPayload {
        &self.payload
    }
}

/// An exchange `OrderCancelRequested` event routed to a perps `ORDER_CANCEL`.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExchangeCancel {
    pub(crate) event: OrderCancelRequested,
    pub(crate) payload: PerpsPayload,
}

impl ExchangeCancel {
    /// Binds the cancellation to the market that holds the order.
    ///
    /// # Errors
    ///
    /// Refuses a zero market or order identifier.
    pub fn new(event: OrderCancelRequested, market_id: [u8; 32]) -> Result<Self, IntentError> {
        nonzero(&market_id, IntentField::Market)?;
        nonzero(&event.order_id, IntentField::Order)?;
        let payload = perps(PerpsPayload::OrderCancel {
            market_id,
            order_id: event.order_id,
        })?;
        Ok(Self { event, payload })
    }

    #[must_use]
    pub const fn event(&self) -> &OrderCancelRequested {
        &self.event
    }

    #[must_use]
    pub const fn payload(&self) -> &PerpsPayload {
        &self.payload
    }
}

/// An exchange `SettlementRequested` event routed to a perps `POSITION_CLOSE`.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExchangeSettle {
    pub(crate) event: SettlementRequested,
    pub(crate) payload: PerpsPayload,
}

impl ExchangeSettle {
    /// Binds the settlement to the market that holds the position.
    ///
    /// # Errors
    ///
    /// Refuses a zero market or position identifier.
    pub fn new(event: SettlementRequested, market_id: [u8; 32]) -> Result<Self, IntentError> {
        nonzero(&market_id, IntentField::Market)?;
        nonzero(&event.position_id, IntentField::Position)?;
        let payload = perps(PerpsPayload::PositionClose {
            market_id,
            position_id: event.position_id,
        })?;
        Ok(Self { event, payload })
    }

    #[must_use]
    pub const fn event(&self) -> &SettlementRequested {
        &self.event
    }

    #[must_use]
    pub const fn payload(&self) -> &PerpsPayload {
        &self.payload
    }
}

/// An exchange `MarginDeposited` event routed to the custody credit that
/// carries its proven deposit into the margin account.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExchangeMarginDeposit {
    pub(crate) event: MarginDeposited,
    pub(crate) credit: NativeCustodyCredit,
}

impl ExchangeMarginDeposit {
    /// # Errors
    ///
    /// Refuses a credit whose deposit, asset, amount or beneficiary differs
    /// from the event.
    pub fn new(event: MarginDeposited, credit: NativeCustodyCredit) -> Result<Self, IntentError> {
        mismatch(
            IntentField::DepositProof,
            credit.deposit_id() == event.deposit_id,
        )?;
        mismatch(IntentField::Asset, credit.asset().bytes() == event.asset_id)?;
        mismatch(
            IntentField::Amount,
            credit.amount().value() == amount(&event.amount)?,
        )?;
        mismatch(
            IntentField::Account,
            credit.payload().get(107..139) == Some(&event.account[..]),
        )?;
        Ok(Self { event, credit })
    }

    #[must_use]
    pub const fn event(&self) -> &MarginDeposited {
        &self.event
    }

    #[must_use]
    pub const fn credit(&self) -> &NativeCustodyCredit {
        &self.credit
    }
}

/// An exchange `MarginWithdrawalRequested` event routed to the asset-module
/// withdrawal that pays the margin out to the requesting owner.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExchangeMarginWithdraw {
    pub(crate) event: MarginWithdrawalRequested,
    pub(crate) request: BridgeWithdrawRequest,
}

impl ExchangeMarginWithdraw {
    /// # Errors
    ///
    /// Refuses a request whose asset, amount or payout address differs from
    /// the event.
    pub fn new(
        event: MarginWithdrawalRequested,
        request: BridgeWithdrawRequest,
    ) -> Result<Self, IntentError> {
        mismatch(IntentField::Asset, request.asset.bytes() == event.asset_id)?;
        mismatch(
            IntentField::Amount,
            request.amount.value() == amount(&event.amount)?,
        )?;
        mismatch(
            IntentField::PayoutAddress,
            request.payout_address.bytes() == event.owner,
        )?;
        Ok(Self { event, request })
    }

    #[must_use]
    pub const fn event(&self) -> &MarginWithdrawalRequested {
        &self.event
    }

    #[must_use]
    pub const fn request(&self) -> &BridgeWithdrawRequest {
        &self.request
    }
}

/// The `LayerX` context an exchange event needs before it becomes an intent.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum RouteBinding {
    /// Paxeer-settled bridge and launchpad events need no binding.
    Unbound,
    /// Orders, cancels and settlements name their market and owning account.
    Market {
        market_id: [u8; 32],
        owner_account_id: [u8; 32],
    },
    MarketVersioned {
        market_id: [u8; 32],
        owner_account_id: [u8; 32],
        perps_abi_version: u32,
    },
    /// Margin deposits carry the custody credit of their proven deposit.
    Custody(NativeCustodyCredit),
    /// Margin withdrawals carry the owner's withdrawal request.
    Withdrawal(BridgeWithdrawRequest),
}

/// Refusal to route a precompile log into an intent.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RouteError {
    Decode(EventDecodeError),
    Binding(PrecompileEventKind),
    Intent(IntentError),
}

/// Decodes a precompile log and routes it into its versioned intent.
///
/// # Errors
///
/// Refuses an undecodable log, a binding of the wrong shape for the event, or
/// an event the binding contradicts.
pub fn route(log: &EvmLog<'_>, binding: RouteBinding) -> Result<Intent, RouteError> {
    let event = PrecompileEvent::decode(log).map_err(RouteError::Decode)?;
    route_event(event, binding)
}

/// Routes a decoded precompile event into its versioned intent.
///
/// # Errors
///
/// Refuses a binding of the wrong shape for the event, or an event the
/// binding contradicts.
pub fn route_event(event: PrecompileEvent, binding: RouteBinding) -> Result<Intent, RouteError> {
    let kind = event.kind();
    let wrong = || RouteError::Binding(kind);
    let intent =
        |value: Result<IntentKind, IntentError>| value.map(Intent::v1).map_err(RouteError::Intent);
    match (event, binding) {
        (
            PrecompileEvent::OrderPlaced(value),
            RouteBinding::Market {
                market_id,
                owner_account_id,
            },
        ) => {
            if value.market_id != market_id {
                return Err(RouteError::Intent(IntentError {
                    field: IntentField::Market,
                    reason: IntentErrorReason::EventMismatch,
                }));
            }
            intent(ExchangeOrder::new(value, owner_account_id).map(IntentKind::ExchangeOrder))
        }
        (
            PrecompileEvent::OrderPlaced(value),
            RouteBinding::MarketVersioned {
                market_id,
                owner_account_id,
                perps_abi_version,
            },
        ) => {
            if value.market_id != market_id {
                return Err(RouteError::Intent(IntentError {
                    field: IntentField::Market,
                    reason: IntentErrorReason::EventMismatch,
                }));
            }
            intent(
                ExchangeOrder::new_for_abi(value, owner_account_id, perps_abi_version)
                    .map(IntentKind::ExchangeOrder),
            )
        }
        (PrecompileEvent::OrderCancelRequested(value), RouteBinding::Market { market_id, .. }) => {
            intent(ExchangeCancel::new(value, market_id).map(IntentKind::ExchangeCancel))
        }
        (PrecompileEvent::SettlementRequested(value), RouteBinding::Market { market_id, .. }) => {
            intent(ExchangeSettle::new(value, market_id).map(IntentKind::ExchangeSettle))
        }
        (PrecompileEvent::MarginDeposited(value), RouteBinding::Custody(credit)) => {
            intent(ExchangeMarginDeposit::new(value, credit).map(IntentKind::ExchangeMarginDeposit))
        }
        (PrecompileEvent::MarginWithdrawalRequested(value), RouteBinding::Withdrawal(request)) => {
            ExchangeMarginWithdraw::new(value, request)
                .map(|value| Intent::v2(IntentKind::ExchangeMarginWithdraw(value)))
                .map_err(RouteError::Intent)
        }
        (event, RouteBinding::Unbound) => settled(event).map(Intent::v1),
        _ => Err(wrong()),
    }
}

fn settled(event: PrecompileEvent) -> Result<IntentKind, RouteError> {
    Ok(match event {
        PrecompileEvent::BridgeIn(value) => IntentKind::BridgeIn(value),
        PrecompileEvent::BridgeOut(value) => IntentKind::BridgeOut(value),
        PrecompileEvent::FeeDistributed(value) => distributed(value).map_err(RouteError::Intent)?,
        PrecompileEvent::FeeStrategyChanged(value) => IntentKind::LaunchpadFeeStrategy(value),
        PrecompileEvent::MarketCreated(value) => IntentKind::LaunchpadCreate(value),
        PrecompileEvent::Swap(value) => IntentKind::LaunchpadSwap(value),
        event => return Err(RouteError::Binding(event.kind())),
    })
}

fn distributed(event: FeeDistributed) -> Result<IntentKind, IntentError> {
    Ok(match event.strategy {
        0 => IntentKind::LaunchpadFeesClaim(event),
        1 => IntentKind::LaunchpadFeesBurn(event),
        2 => IntentKind::LaunchpadAirdropExecute(event),
        3 => IntentKind::LaunchpadLpRewards(event),
        _ => {
            return Err(IntentError {
                field: IntentField::PrecompileEvent,
                reason: IntentErrorReason::InvalidRange,
            })
        }
    })
}
