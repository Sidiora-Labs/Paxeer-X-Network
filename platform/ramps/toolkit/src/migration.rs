use std::collections::BTreeMap;

use layerx_interop_gateway::trace::TraceId;
use layerx_migrate::ethereum::{EthereumConfig, EthereumVerifier};
use layerx_migrate::solana::{SolanaConfig, SolanaVerifier};
use layerx_migrate::{SourceChain, SourceEvidence, SourceVerifier};
use layerx_types::account::AccountId;
use serde::{Deserialize, Serialize};
use sha2::{Digest as _, Sha256};

use crate::{QuoteTerms, RampDirection, RampError, RampOrder};
pub use layerx_migrate::MigrationError as SourceVerificationError;

pub const SOURCE_SETTLEMENT_VERSION: &str = "layerx-migration-source-v2";
const SOURCE_CLAIM_DOMAIN: &[u8] = b"LXP/market-maker-ramp/source-claim/v2\0";

#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum SourceChainKind {
    Ethereum,
    Solana,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct SourceQuoteBinding {
    pub quote_id: String,
    pub chain: SourceChainKind,
    pub source_asset: [u8; 32],
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct SourceSettlementConfig {
    pub ethereum: Option<EthereumConfig>,
    pub solana: Option<SolanaConfig>,
    pub quotes: Vec<SourceQuoteBinding>,
}

#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct SourceSettlementRequest {
    pub version: String,
    pub order_digest: [u8; 32],
    pub chain: SourceChainKind,
    pub source_evidence: String,
}

#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum SourceSettlementState {
    SourcePending,
    SourceSettled,
    LayerxPending,
    LayerxRefused,
    Done,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct SourceSettlementResult {
    pub version: String,
    pub order_digest: [u8; 32],
    pub state: SourceSettlementState,
    pub source_evidence_digest: [u8; 32],
    pub source_claim_id: Option<[u8; 32]>,
}

#[derive(Clone, Deserialize, Eq, PartialEq, Serialize)]
#[serde(deny_unknown_fields)]
pub struct SourceSettlementRecordV2 {
    pub(crate) version: String,
    pub(crate) order_digest: [u8; 32],
    pub(crate) source_claim_id: [u8; 32],
    pub(crate) source_evidence_digest: [u8; 32],
    pub(crate) chain: String,
    pub(crate) network: Vec<u8>,
    pub(crate) transaction: Vec<u8>,
    pub(crate) custody: Vec<u8>,
    pub(crate) custody_reference: [u8; 32],
    pub(crate) source: Vec<u8>,
    pub(crate) source_asset: [u8; 32],
    pub(crate) source_amount: u128,
    pub(crate) layerx_asset: [u8; 32],
    pub(crate) layerx_amount: u128,
    pub(crate) destination: [u8; 32],
    pub(crate) finality_height: u64,
    pub(crate) canonical_source_evidence: Vec<u8>,
}

impl std::fmt::Debug for SourceSettlementRequest {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter
            .debug_struct("SourceSettlementRequest")
            .field("version", &self.version)
            .field("order_digest", &self.order_digest)
            .field("chain", &self.chain)
            .finish_non_exhaustive()
    }
}

impl std::fmt::Debug for SourceSettlementRecordV2 {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter
            .debug_struct("SourceSettlementRecordV2")
            .field("order_digest", &self.order_digest)
            .field("source_claim_id", &self.source_claim_id)
            .field("source_evidence_digest", &self.source_evidence_digest)
            .field("finality_height", &self.finality_height)
            .finish_non_exhaustive()
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedSourceSettlement(SourceSettlementRecordV2);

impl VerifiedSourceSettlement {
    pub const fn record(&self) -> &SourceSettlementRecordV2 {
        &self.0
    }
    pub const fn source_claim_id(&self) -> [u8; 32] {
        self.0.source_claim_id
    }
    pub const fn evidence_digest(&self) -> [u8; 32] {
        self.0.source_evidence_digest
    }
    pub(crate) fn into_record(self) -> SourceSettlementRecordV2 {
        self.0
    }
}

impl SourceSettlementRecordV2 {
    pub const fn order_digest(&self) -> [u8; 32] {
        self.order_digest
    }
    pub fn chain_name(&self) -> &str {
        &self.chain
    }
    pub const fn source_claim_id(&self) -> [u8; 32] {
        self.source_claim_id
    }
    pub const fn evidence_digest(&self) -> [u8; 32] {
        self.source_evidence_digest
    }
    pub fn validate_order(&self, order: &RampOrder) -> Result<(), RampError> {
        order.validate_bound()?;
        let evidence = SourceEvidence::new(self.canonical_source_evidence.clone())
            .map_err(RampError::SourceSettlement)?;
        let lengths = match self.chain.as_str() {
            "ethereum" => (8, 32, 20, 20),
            "solana" => (32, 64, 32, 32),
            _ => return Err(RampError::OrderBinding),
        };
        if self.version != SOURCE_SETTLEMENT_VERSION
            || order.direction() != RampDirection::OnRamp
            || self.order_digest != order.order_digest
            || self.layerx_asset != order.quote.layerx_asset
            || self.layerx_amount != order.quote.layerx_amount
            || self.source_amount != order.quote.external_amount_minor
            || self.destination != customer_destination(order)?
            || self.source_asset == [0; 32]
            || self.custody_reference == [0; 32]
            || self.finality_height == 0
            || self.source_evidence_digest != evidence.digest()
            || (
                self.network.len(),
                self.transaction.len(),
                self.custody.len(),
                self.source.len(),
            ) != lengths
            || [
                &self.network,
                &self.transaction,
                &self.custody,
                &self.source,
            ]
            .iter()
            .any(|bytes| bytes.iter().all(|byte| *byte == 0))
            || self.source_claim_id
                != claim_id(
                    &self.chain,
                    &self.network,
                    &self.transaction,
                    &self.custody,
                    self.custody_reference,
                )?
        {
            return Err(RampError::OrderBinding);
        }
        Ok(())
    }
}

pub fn source_trace(header: Option<&str>, order_digest: [u8; 32]) -> TraceId {
    let mut entropy = [0; 16];
    entropy.copy_from_slice(&order_digest[..16]);
    TraceId::from_inbound(header, entropy)
}

pub const fn source_error_status(error: SourceVerificationError) -> u16 {
    match error {
        SourceVerificationError::SourcePending => 202,
        SourceVerificationError::RpcUnavailable
        | SourceVerificationError::RpcRateLimited { .. }
        | SourceVerificationError::RpcDivergence
        | SourceVerificationError::StorageRefused
        | SourceVerificationError::Configuration => 503,
        SourceVerificationError::CheckpointConflict => 409,
        _ => 400,
    }
}

pub struct SourceSettlementService {
    config: SourceSettlementConfig,
    ethereum: Option<EthereumVerifier>,
    solana: Option<SolanaVerifier>,
    quotes: BTreeMap<String, SourceQuoteBinding>,
}

impl SourceSettlementService {
    pub fn new(config: SourceSettlementConfig) -> Result<Self, RampError> {
        if config.ethereum.is_none() && config.solana.is_none() || config.quotes.is_empty() {
            return Err(RampError::Configuration);
        }
        let mut quotes = BTreeMap::new();
        for binding in &config.quotes {
            if binding.quote_id.is_empty()
                || binding.quote_id.len() > 128
                || binding.source_asset == [0; 32]
                || matches!(binding.chain, SourceChainKind::Ethereum) && config.ethereum.is_none()
                || matches!(binding.chain, SourceChainKind::Solana) && config.solana.is_none()
                || quotes
                    .insert(binding.quote_id.clone(), binding.clone())
                    .is_some()
            {
                return Err(RampError::Configuration);
            }
        }
        let ethereum = config
            .ethereum
            .clone()
            .map(EthereumVerifier::new)
            .transpose()
            .map_err(RampError::SourceSettlement)?;
        let solana = config
            .solana
            .clone()
            .map(SolanaVerifier::new)
            .transpose()
            .map_err(RampError::SourceSettlement)?;
        Ok(Self {
            config,
            ethereum,
            solana,
            quotes,
        })
    }

    pub fn validate_catalogue(
        &self,
        catalogue: &BTreeMap<String, QuoteTerms>,
    ) -> Result<(), RampError> {
        for quote_id in self.quotes.keys() {
            if !catalogue
                .get(quote_id)
                .is_some_and(|quote| quote.direction == RampDirection::OnRamp)
            {
                return Err(RampError::Configuration);
            }
        }
        Ok(())
    }

    pub fn evidence(request: &SourceSettlementRequest) -> Result<SourceEvidence, RampError> {
        if request.version != SOURCE_SETTLEMENT_VERSION
            || request.order_digest == [0; 32]
            || request.source_evidence.len() > 1_398_104
        {
            return Err(RampError::InvalidOrder);
        }
        let canonical = crate::clients::base64_decode(&request.source_evidence)
            .map_err(|_| RampError::InvalidOrder)?;
        SourceEvidence::new(canonical).map_err(RampError::SourceSettlement)
    }

    pub fn verify(
        &self,
        order: &RampOrder,
        request: &SourceSettlementRequest,
        trace: &TraceId,
    ) -> Result<VerifiedSourceSettlement, RampError> {
        order.validate_bound()?;
        if order.direction() != RampDirection::OnRamp || order.order_digest != request.order_digest
        {
            return Err(RampError::OrderBinding);
        }
        let binding = self
            .quotes
            .get(&order.quote.quote_id)
            .ok_or(RampError::OrderBinding)?;
        if binding.chain != request.chain {
            return Err(RampError::OrderBinding);
        }
        let evidence = Self::evidence(request)?;
        let (finality, chain, network, custody) = match request.chain {
            SourceChainKind::Ethereum => {
                let config = self
                    .config
                    .ethereum
                    .as_ref()
                    .ok_or(RampError::Configuration)?;
                let finality = self
                    .ethereum
                    .as_ref()
                    .ok_or(RampError::Configuration)?
                    .verify_asset_finality(&evidence, trace)
                    .map_err(RampError::SourceSettlement)?;
                if finality.chain()
                    != (SourceChain::Ethereum {
                        chain_id: config.chain_id,
                    })
                {
                    return Err(RampError::OrderBinding);
                }
                (
                    finality,
                    "ethereum",
                    config.chain_id.to_be_bytes().to_vec(),
                    config.custody_contract.to_vec(),
                )
            }
            SourceChainKind::Solana => {
                let config = self
                    .config
                    .solana
                    .as_ref()
                    .ok_or(RampError::Configuration)?;
                let finality = self
                    .solana
                    .as_ref()
                    .ok_or(RampError::Configuration)?
                    .verify_asset_finality(&evidence, trace)
                    .map_err(RampError::SourceSettlement)?;
                if finality.chain()
                    != (SourceChain::Solana {
                        genesis_hash: config.genesis_hash,
                    })
                {
                    return Err(RampError::OrderBinding);
                }
                (
                    finality,
                    "solana",
                    config.genesis_hash.to_vec(),
                    config.custody_account.to_vec(),
                )
            }
        };
        if finality.source_asset() != binding.source_asset {
            return Err(RampError::OrderBinding);
        }
        let source = match finality.source() {
            layerx_migrate::ExternalAddress::Ethereum(bytes) => bytes.to_vec(),
            layerx_migrate::ExternalAddress::Solana(bytes) => bytes.to_vec(),
        };
        let transaction = finality.transaction().bytes().to_vec();
        let record = SourceSettlementRecordV2 {
            version: SOURCE_SETTLEMENT_VERSION.to_owned(),
            order_digest: order.order_digest,
            source_claim_id: claim_id(
                chain,
                &network,
                &transaction,
                &custody,
                finality.custody_reference(),
            )?,
            source_evidence_digest: evidence.digest(),
            chain: chain.to_owned(),
            network,
            transaction,
            custody,
            custody_reference: finality.custody_reference(),
            source,
            source_asset: finality.source_asset(),
            source_amount: finality.source_amount(),
            layerx_asset: finality.layerx_asset(),
            layerx_amount: finality.layerx_amount(),
            destination: finality.destination(),
            finality_height: finality.finality_height(),
            canonical_source_evidence: evidence.canonical().to_vec(),
        };
        record.validate_order(order)?;
        Ok(VerifiedSourceSettlement(record))
    }

    pub fn verify_retained(
        &self,
        order: &RampOrder,
        record: &SourceSettlementRecordV2,
        trace: &TraceId,
    ) -> Result<(), RampError> {
        record.validate_order(order)?;
        let request = SourceSettlementRequest {
            version: SOURCE_SETTLEMENT_VERSION.to_owned(),
            order_digest: order.order_digest,
            chain: match record.chain.as_str() {
                "ethereum" => SourceChainKind::Ethereum,
                "solana" => SourceChainKind::Solana,
                _ => return Err(RampError::OrderBinding),
            },
            source_evidence: crate::clients::base64_encode(&record.canonical_source_evidence),
        };
        let current = self.verify(order, &request, trace)?.into_record();
        if current.source_claim_id != record.source_claim_id
            || current.source_evidence_digest != record.source_evidence_digest
            || current.finality_height < record.finality_height
        {
            return Err(RampError::OrderBinding);
        }
        Ok(())
    }
}

fn customer_destination(order: &RampOrder) -> Result<[u8; 32], RampError> {
    let account =
        AccountId::parse(&order.customer.account).map_err(|_| RampError::InvalidPrincipal)?;
    layerx_wire::hash::account_id(&account).map_err(|_| RampError::OrderBinding)
}

fn claim_id(
    chain: &str,
    network: &[u8],
    transaction: &[u8],
    custody: &[u8],
    reference: [u8; 32],
) -> Result<[u8; 32], RampError> {
    let mut hash = Sha256::new();
    hash.update(SOURCE_CLAIM_DOMAIN);
    hash.update([match chain {
        "ethereum" => 1,
        "solana" => 2,
        _ => return Err(RampError::OrderBinding),
    }]);
    for bytes in [network, transaction, custody] {
        hash.update((bytes.len() as u64).to_be_bytes());
        hash.update(bytes);
    }
    hash.update(reference);
    Ok(hash.finalize().into())
}
