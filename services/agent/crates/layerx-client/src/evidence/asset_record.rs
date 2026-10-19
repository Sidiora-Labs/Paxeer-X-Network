use layerx_proof::state_range::VerifiedModuleInventory;
use layerx_types::verify::VerificationLevel;

use crate::read::Freshness;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum AssetEvidenceError {
    Inventory,
    UnknownAsset,
    Encoding,
    Identity,
    Issuer,
    Paused,
    Supply,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum AssetSourceKind {
    Mutable,
    Initial,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AssetRecordMetadata {
    pub asset_id: [u8; 32],
    pub symbol: Vec<u8>,
    pub name: Vec<u8>,
    pub decimals: u8,
    pub custody_kind: u8,
    pub custody_reference: Vec<u8>,
    pub paused: bool,
    pub supply_cap: u128,
    pub issuer_did: [u8; 32],
    pub issuer_kind: u8,
    pub total_units: u128,
    pub salt: [u8; 32],
}

#[derive(Clone, Debug)]
pub struct VerifiedEffectiveAsset {
    metadata: AssetRecordMetadata,
    canonical_bytes: Vec<u8>,
    source_kind: AssetSourceKind,
    state_root: [u8; 32],
    level: VerificationLevel,
    freshness: Freshness,
}

impl VerifiedEffectiveAsset {
    pub const fn asset_id(&self) -> [u8; 32] {
        self.metadata.asset_id
    }
    pub const fn metadata(&self) -> &AssetRecordMetadata {
        &self.metadata
    }
    pub const fn registered(&self) -> bool {
        true
    }
    pub const fn paused(&self) -> bool {
        self.metadata.paused
    }
    pub const fn state_root(&self) -> [u8; 32] {
        self.state_root
    }
    pub const fn level(&self) -> VerificationLevel {
        self.level
    }
    pub const fn freshness(&self) -> Freshness {
        self.freshness
    }
    pub const fn source_kind(&self) -> AssetSourceKind {
        self.source_kind
    }
    pub fn canonical_bytes(&self) -> &[u8] {
        &self.canonical_bytes
    }
}

/// Issuer authority over one registered asset, as the kernel admits Asset/10 Mint: the actor is
/// the recorded issuer, the asset is unpaused and the units leave its issuance account.
#[derive(Clone, Debug)]
pub struct VerifiedIssuance {
    asset: VerifiedEffectiveAsset,
    account: [u8; 32],
    headroom: u128,
}

impl VerifiedIssuance {
    pub const fn asset(&self) -> &VerifiedEffectiveAsset {
        &self.asset
    }
    pub const fn account(&self) -> [u8; 32] {
        self.account
    }
    pub const fn headroom(&self) -> u128 {
        self.headroom
    }
}

pub(super) fn issuance(
    asset: VerifiedEffectiveAsset,
    issuer: [u8; 32],
) -> Result<VerifiedIssuance, AssetEvidenceError> {
    let metadata = asset.metadata();
    if metadata.issuer_did != issuer {
        return Err(AssetEvidenceError::Issuer);
    }
    if metadata.paused {
        return Err(AssetEvidenceError::Paused);
    }
    let ceiling = if metadata.supply_cap == 0 {
        u128::MAX
    } else {
        metadata.supply_cap
    };
    let headroom = ceiling
        .checked_sub(metadata.total_units)
        .ok_or(AssetEvidenceError::Supply)?;
    let account = layerx_wire::hash::asset_issuance_account_id(&metadata.asset_id)
        .map_err(|_| AssetEvidenceError::Encoding)?;
    Ok(VerifiedIssuance {
        asset,
        account,
        headroom,
    })
}

pub(super) fn resolve(
    inventory: &VerifiedModuleInventory,
    asset_id: [u8; 32],
    state_root: [u8; 32],
    level: VerificationLevel,
    freshness: Freshness,
) -> Result<VerifiedEffectiveAsset, AssetEvidenceError> {
    if inventory.module_id() != 1 || inventory.state_root() != state_root {
        return Err(AssetEvidenceError::Inventory);
    }
    let mut mutable_key = [0_u8; 38];
    mutable_key[..6].copy_from_slice(b"asset:");
    mutable_key[6..].copy_from_slice(&asset_id);
    let records = inventory.records();
    let find = |key: &[u8]| {
        records
            .binary_search_by(|(candidate, _)| candidate.as_slice().cmp(key))
            .ok()
            .map(|index| records[index].1.as_slice())
    };
    let (canonical_bytes, source_kind) = if let Some(bytes) = find(&mutable_key) {
        (bytes, AssetSourceKind::Mutable)
    } else if let Some(bytes) = find(&asset_id) {
        (bytes, AssetSourceKind::Initial)
    } else {
        return Err(AssetEvidenceError::UnknownAsset);
    };
    let metadata = decode_record(canonical_bytes, asset_id)?;
    Ok(VerifiedEffectiveAsset {
        metadata,
        canonical_bytes: canonical_bytes.to_vec(),
        source_kind,
        state_root,
        level,
        freshness,
    })
}

fn decode_record(
    bytes: &[u8],
    expected_id: [u8; 32],
) -> Result<AssetRecordMetadata, AssetEvidenceError> {
    if bytes.len() < 140 {
        return Err(AssetEvidenceError::Encoding);
    }
    let mut reader = Reader(bytes);
    if u16::from_be_bytes(reader.array()?) != 3 {
        return Err(AssetEvidenceError::Encoding);
    }
    let asset_id = reader.array()?;
    if asset_id != expected_id {
        return Err(AssetEvidenceError::Identity);
    }
    let symbol_length = usize::from(reader.byte()?);
    if !(1..=16).contains(&symbol_length) || reader.0.len() < symbol_length + 103 {
        return Err(AssetEvidenceError::Encoding);
    }
    let symbol = reader.take(symbol_length)?.to_vec();
    let decimals = reader.byte()?;
    let custody_kind = reader.byte()?;
    let reference_length = usize::from(u16::from_be_bytes(reader.array()?));
    if reference_length > 128 || reader.0.len() < reference_length + 99 {
        return Err(AssetEvidenceError::Encoding);
    }
    let custody_reference = reader.take(reference_length)?.to_vec();
    let pause = reader.byte()?;
    let name_length = usize::from(reader.byte()?);
    if pause > 1 || name_length > 32 || reader.0.len() != name_length + 97 {
        return Err(AssetEvidenceError::Encoding);
    }
    let name = reader.take(name_length)?.to_vec();
    let supply_cap = u128::from_be_bytes(reader.array()?);
    let issuer_did = reader.array()?;
    let issuer_kind = reader.byte()?;
    let total_units = u128::from_be_bytes(reader.array()?);
    let salt = reader.array()?;
    if !reader.0.is_empty()
        || !symbol.is_ascii()
        || decimals > 38
        || !matches!(issuer_kind, 1 | 2)
        || custody_kind != issuer_kind
        || name.is_empty()
        || issuer_did == [0; 32]
        || (issuer_kind == 1 && !custody_reference.is_empty())
        || (issuer_kind == 2 && custody_reference.is_empty())
    {
        return Err(AssetEvidenceError::Encoding);
    }
    Ok(AssetRecordMetadata {
        asset_id,
        symbol,
        name,
        decimals,
        custody_kind,
        custody_reference,
        paused: pause == 1,
        supply_cap,
        issuer_did,
        issuer_kind,
        total_units,
        salt,
    })
}

struct Reader<'a>(&'a [u8]);

impl<'a> Reader<'a> {
    fn take(&mut self, count: usize) -> Result<&'a [u8], AssetEvidenceError> {
        let bytes = self.0.get(..count).ok_or(AssetEvidenceError::Encoding)?;
        self.0 = &self.0[count..];
        Ok(bytes)
    }
    fn array<const N: usize>(&mut self) -> Result<[u8; N], AssetEvidenceError> {
        self.take(N)?
            .try_into()
            .map_err(|_| AssetEvidenceError::Encoding)
    }
    fn byte(&mut self) -> Result<u8, AssetEvidenceError> {
        Ok(self.array::<1>()?[0])
    }
}

impl std::fmt::Display for AssetEvidenceError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}

impl std::error::Error for AssetEvidenceError {}
