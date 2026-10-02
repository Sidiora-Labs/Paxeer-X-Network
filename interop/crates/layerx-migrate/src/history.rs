use layerx_interop_gateway::principal::PrincipalId;
use layerx_interop_gateway::trace::TraceId;
use sha2::{Digest as _, Sha256};

use crate::journal::Journal;
use crate::source_codec::{decode_hex, ethereum_hex, Reader, Writer};
use crate::{
    ExternalAddress, ExternalHistoryKind, ExternalHistoryRecord, ExternalHistorySink,
    ExternalProvenance, JournalConfig, MigrationError, SourceChain, SourceTransaction,
    VerifiedHistoryPage, HISTORY_PAGE_LIMIT,
};

const RECORD_DOMAIN: &[u8] = b"LXP/ExternalHistory/v1\0";
const KEY_DOMAIN: &[u8] = b"LXP/ExternalHistory/key/v1\0";
const MAX_RECORD_HEX_BYTES: usize = 512;

pub struct DurableExternalHistory {
    journal: Journal,
}

impl DurableExternalHistory {
    pub fn new(config: &JournalConfig) -> Result<Self, MigrationError> {
        Ok(Self {
            journal: Journal::new(config)?,
        })
    }

    pub fn read(
        &self,
        principal: &PrincipalId,
        after: Option<[u8; 32]>,
        limit: usize,
    ) -> Result<ExternalHistoryPage, MigrationError> {
        if limit == 0 || limit > HISTORY_PAGE_LIMIT {
            return Err(MigrationError::InvalidHistory);
        }
        self.journal.external_history(principal, after, limit)
    }
}

impl ExternalHistorySink for DurableExternalHistory {
    fn store_external(
        &mut self,
        principal: &PrincipalId,
        page: &VerifiedHistoryPage,
        _trace: &TraceId,
    ) -> Result<(), MigrationError> {
        if page.records().len() > HISTORY_PAGE_LIMIT || page.evidence_digest() == [0; 32] {
            return Err(MigrationError::InvalidHistory);
        }
        let rows = page.records().iter().map(encode).collect::<Result<Vec<_>, _>>()?;
        self.journal.store_external_history(principal, page.evidence_digest(), rows)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExternalHistoryPage {
    pub(crate) records: Vec<ExternalHistoryRecord>,
    pub(crate) next_cursor: Option<[u8; 32]>,
}

impl ExternalHistoryPage {
    #[must_use]
    pub fn records(&self) -> &[ExternalHistoryRecord] {
        &self.records
    }

    #[must_use]
    pub const fn next_cursor(&self) -> Option<[u8; 32]> {
        self.next_cursor
    }
}

fn kind_byte(kind: ExternalHistoryKind) -> u8 {
    match kind {
        ExternalHistoryKind::Incoming => 1,
        ExternalHistoryKind::Outgoing => 2,
        ExternalHistoryKind::Contract => 3,
    }
}

pub(crate) fn record_key(record: &ExternalHistoryRecord) -> [u8; 32] {
    let mut hash = Sha256::new();
    hash.update(KEY_DOMAIN);
    record.chain().commit(&mut hash);
    record.transaction().commit(&mut hash);
    record.address().commit(&mut hash);
    hash.update(record.source_asset());
    hash.update([kind_byte(record.kind())]);
    hash.finalize().into()
}

fn encode(record: &ExternalHistoryRecord) -> Result<String, MigrationError> {
    record.validate()?;
    let mut writer = Writer::new(RECORD_DOMAIN);
    match (record.chain(), record.transaction(), record.address(), record.provenance()) {
        (SourceChain::Ethereum { chain_id }, SourceTransaction::Ethereum(transaction),
         ExternalAddress::Ethereum(address), ExternalProvenance::Ethereum) => {
            writer.u8(1);
            writer.u64(chain_id);
            writer.fixed(&transaction);
            writer.fixed(&address);
        }
        (SourceChain::Solana { genesis_hash }, SourceTransaction::Solana(transaction),
         ExternalAddress::Solana(address), ExternalProvenance::Solana) => {
            writer.u8(2);
            writer.fixed(&genesis_hash);
            writer.fixed(&transaction);
            writer.fixed(&address);
        }
        _ => return Err(MigrationError::InvalidHistory),
    }
    writer.u8(kind_byte(record.kind()));
    writer.u64(record.timestamp());
    writer.fixed(&record.source_asset());
    writer.u128(record.source_amount());
    Ok(ethereum_hex(&writer.finish()))
}

pub(crate) fn decode(encoded: &str) -> Result<ExternalHistoryRecord, MigrationError> {
    if encoded.len() > MAX_RECORD_HEX_BYTES {
        return Err(MigrationError::CheckpointIntegrity);
    }
    decode_inner(encoded).map_err(|_| MigrationError::CheckpointIntegrity)
}

fn decode_inner(encoded: &str) -> Result<ExternalHistoryRecord, MigrationError> {
    let bytes = decode_hex(encoded)?;
    let mut reader = Reader::new(&bytes, RECORD_DOMAIN)?;
    let (chain, transaction, address, provenance) = match reader.u8()? {
        1 => (
            SourceChain::Ethereum { chain_id: reader.u64()? },
            SourceTransaction::ethereum(reader.array()?)?,
            ExternalAddress::Ethereum(reader.array()?),
            ExternalProvenance::Ethereum,
        ),
        2 => (
            SourceChain::Solana { genesis_hash: reader.array()? },
            SourceTransaction::solana(reader.array()?)?,
            ExternalAddress::Solana(reader.array()?),
            ExternalProvenance::Solana,
        ),
        _ => return Err(MigrationError::InvalidHistory),
    };
    let kind = match reader.u8()? {
        1 => ExternalHistoryKind::Incoming,
        2 => ExternalHistoryKind::Outgoing,
        3 => ExternalHistoryKind::Contract,
        _ => return Err(MigrationError::InvalidHistory),
    };
    let record = ExternalHistoryRecord {
        chain, transaction, address, kind,
        timestamp: reader.u64()?,
        source_asset: reader.array()?,
        source_amount: reader.u128()?,
        provenance,
    };
    reader.finish()?;
    record.validate()?;
    if encode(&record)? != encoded {
        return Err(MigrationError::CheckpointIntegrity);
    }
    Ok(record)
}
