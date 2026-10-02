use std::collections::BTreeMap;

use layerx_client::evidence::VerifiedNativeExecutionPrestate;
use layerx_programs::{ProgramId, ProgramValueAccountBinding};
use layerx_proof::state::CanonicalAccount;
use layerx_types::activity::Authority;
use layerx_types::ids::Did;
use layerx_types::payload::{ModuleId, ModuleRegistry};
use layerx_types::program_lifecycle::{NativeProgramWindDown, ProgramWindDownOperation};
use layerx_types::verify::VerificationLevel;
use sha2::{Digest as _, Sha256};

use crate::budget::{ProgramBudgetReservation, ProgramChargeKind};
use crate::prepare::{verify_disclosure_binding, Prepared};
use crate::protocol_evidence::VerifiedReceiptEvidence;
use crate::sign::VerifiedSubmission;

use super::{ProgramExecutedDebit, ProgramSettlementError};

type DebitKey = ([u8; 32], [u8; 32], ProgramChargeKind, Option<[u8; 32]>);

pub(super) fn verify_wind_down_debits(
    prepared: &Prepared, submission: &VerifiedSubmission, receipt: &VerifiedReceiptEvidence,
    registry: &ModuleRegistry, prestate: &VerifiedNativeExecutionPrestate,
    reservation: &ProgramBudgetReservation,
) -> Result<Vec<ProgramExecutedDebit>, ProgramSettlementError> {
    verify_disclosure_binding(prepared).map_err(|_| ProgramSettlementError::Preparation)?;
    if !matches!(prepared.envelope.authority(), Authority::Owner(_)) {
        return Err(ProgramSettlementError::Preparation);
    }
    let activity = layerx_wire::activity::decode_signed(submission.exact_bytes(), registry)
        .map_err(|_| ProgramSettlementError::Preparation)?;
    if layerx_wire::activity::encode_unsigned(&activity).map_err(|_| ProgramSettlementError::Preparation)? != prepared.canonical_bytes
        || layerx_wire::hash::activity_id(&activity).map_err(|_| ProgramSettlementError::Preparation)? != submission.activity_id()
    { return Err(ProgramSettlementError::Preparation); }
    if activity.protocol_version() != 3 || activity.activity_type().module() != ModuleId::Programs
        || activity.activity_type().ordinal() != 4
    { return Err(ProgramSettlementError::UnsupportedOperation); }
    let wind_down = NativeProgramWindDown::decode(activity.payload()).map_err(|_| ProgramSettlementError::UnsupportedOperation)?;
    let (account, maximum) = match wind_down.operation {
        ProgramWindDownOperation::Exit { account } => (account, None),
        ProgramWindDownOperation::BoundedExit { account, maximum_exit_amount } => (account, Some(maximum_exit_amount)),
        _ => return Err(ProgramSettlementError::UnsupportedOperation),
    };
    let decoded = layerx_wire::receipt::decode(receipt.canonical_receipt()).map_err(|_| ProgramSettlementError::Receipt)?;
    let protocol = decoded.protocol().ok_or(ProgramSettlementError::Receipt)?;
    let digest = layerx_wire::hash::receipt_digest(
        &layerx_wire::receipt::encode_unsigned(&decoded).map_err(|_| ProgramSettlementError::Receipt)?)
        .map_err(|_| ProgramSettlementError::Receipt)?;
    if receipt.level() < VerificationLevel::BATCH_INCLUDED || receipt.activity_id() != submission.activity_id()
        || receipt.global_sequence() != protocol.global_sequence() || protocol.protocol_version() != 3
        || protocol.module_id() != 9 || protocol.operation() != 0 || protocol.program_outcome().is_some()
        || protocol.activity_id() != submission.activity_id() || protocol.transfer_set_root() != [0; 32]
        || prestate.network_id() != activity.network_id() || prestate.activity_id() != submission.activity_id()
        || prestate.receipt_digest() != digest || prestate.execution_sequence() != protocol.global_sequence()
        || prestate.state_root() != protocol.previous_state_root()
    { return Err(ProgramSettlementError::SourceSnapshot); }
    let actor = layerx_wire::hash::did_id_for_protocol(prepared.envelope.actor_did(), 3)
        .map_err(|_| ProgramSettlementError::Preparation)?;
    let preparation_digest: [u8;32] = Sha256::digest(&prepared.canonical_bytes).into();
    reservation.validate().map_err(|_| ProgramSettlementError::Allocation)?;
    if reservation.id != preparation_digest || reservation.allocation_actor() != Some(actor)
        || reservation.allocation_preparation_digest() != Some(preparation_digest)
        || reservation.allocation_sequence() != Some(prepared.observed_head_sequence)
        || reservation.allocation_sequence().is_none_or(|sequence| sequence >= protocol.global_sequence())
    { return Err(ProgramSettlementError::Allocation); }
    let mut expected = BTreeMap::new();
    for row in reservation.allocations().ok_or(ProgramSettlementError::MissingAllocation)? {
        let key = (row.asset, row.source, row.kind, row.destination);
        if expected.insert(key, row.maximum_amount).is_some() { return Err(ProgramSettlementError::Allocation); }
    }
    let mut actual = BTreeMap::new();
    if protocol.result_code() == 0 {
        let debit = exit_debit(prestate.program_records(), prestate.all_accounts(), actor,
            wind_down.program_id.bytes(), account, maximum, protocol.global_sequence())?;
        let event = if protocol.effects().len() == 1 { &protocol.effects()[0] }
            else { return Err(ProgramSettlementError::Terminal); };
        if event.module_id() != 9 || event.kind() != 3 || event.event_type() != 12
            || event.monetary() || event.transfer_set_root() != [0;32] || event.body().len() != 96
            || event.body()[..32] != wind_down.program_id.bytes() || event.body()[32..64] != account
        { return Err(ProgramSettlementError::Terminal); }
        let root = fixed(&event.body()[64..96])?;
        verify_exit_transfer_root(&debit, root)?;
        add_actual(&mut actual, &expected, debit)?;
    } else if !protocol.effects().is_empty() {
        return Err(ProgramSettlementError::Terminal);
    }
    if protocol.fee_charged() > activity.fee_limit() { return Err(ProgramSettlementError::FeeProvenance); }
    if protocol.fee_charged() != 0 {
        let asset = active_fee_asset(prestate.program_records())?;
        let payer = principal_fee_account(prestate.all_accounts(), prepared.envelope.actor_did(), asset)?;
        if (protocol.result_code() == 0 && payer.account_id == account) || payer.balance() < protocol.fee_charged() {
            return Err(ProgramSettlementError::FeeProvenance);
        }
        add_actual(&mut actual, &expected, ProgramExecutedDebit { kind: ProgramChargeKind::Fee,
            source: payer.account_id, asset, destination: None, actual_amount: protocol.fee_charged() })?;
    }
    Ok(actual.into_iter().map(|((asset, source, kind, destination), actual_amount)|
        ProgramExecutedDebit { kind, source, asset, destination, actual_amount }).collect())
}

fn add_actual(actual: &mut BTreeMap<DebitKey, u128>, expected: &BTreeMap<DebitKey, u128>, debit: ProgramExecutedDebit)
    -> Result<(), ProgramSettlementError>
{
    let key = (debit.asset, debit.source, debit.kind, debit.destination);
    if debit.actual_amount == 0 { return Err(ProgramSettlementError::UnreservedDebit); }
    let total = actual.entry(key).or_default();
    *total = total.checked_add(debit.actual_amount).ok_or(ProgramSettlementError::Arithmetic)?;
    if *total > *expected.get(&key).ok_or(ProgramSettlementError::UnreservedDebit)? {
        return Err(ProgramSettlementError::UnreservedDebit);
    }
    Ok(())
}

fn state<'a>(records: &'a BTreeMap<Vec<u8>, Vec<u8>>, prefix: &[u8], suffix: &[u8]) -> Result<&'a [u8], ProgramSettlementError> {
    let mut key = prefix.to_vec(); key.extend_from_slice(suffix);
    records.get(&key).map(Vec::as_slice).ok_or(ProgramSettlementError::SourceSnapshot)
}

fn fixed<const N: usize>(bytes: &[u8]) -> Result<[u8;N], ProgramSettlementError> {
    bytes.try_into().map_err(|_| ProgramSettlementError::SourceSnapshot)
}

fn binding(records: &BTreeMap<Vec<u8>, Vec<u8>>, account: [u8;32]) -> Result<ProgramValueAccountBinding, ProgramSettlementError> {
    let bytes = state(records, b"program-account\0r", &account)?;
    if bytes.len() < 139 || bytes[0] != 2 { return Err(ProgramSettlementError::SourceSnapshot); }
    let seed_length = usize::from(u16::from_be_bytes(fixed(&bytes[97..99])?));
    if seed_length > 128 || bytes.len() != 139 + seed_length { return Err(ProgramSettlementError::SourceSnapshot); }
    let value = ProgramValueAccountBinding { record_version: 2,
        program: ProgramId::new(fixed(&bytes[1..33])?).map_err(|_| ProgramSettlementError::SourceSnapshot)?,
        account_id: fixed(&bytes[33..65])?, asset_id: fixed(&bytes[65..97])?,
        registered_sequence: u64::from_be_bytes(fixed(&bytes[99..107])?),
        registration_event_digest: fixed(&bytes[107..139])?, seed: bytes[139..].to_vec() };
    if value.account_id != account || value.primary_value().map_err(|_| ProgramSettlementError::SourceSnapshot)? != bytes
        || records.get(&value.primary_key()).map(Vec::as_slice) != Some(bytes)
    { return Err(ProgramSettlementError::SourceSnapshot); }
    Ok(value)
}

fn exit_debit(
    records: &BTreeMap<Vec<u8>, Vec<u8>>, accounts: &BTreeMap<[u8;32], CanonicalAccount>,
    actor: [u8;32], program: [u8;32], account: [u8;32], maximum: Option<u128>, sequence: u64,
) -> Result<ProgramExecutedDebit, ProgramSettlementError> {
    let deployed = state(records, b"program\0", &program)?;
    if deployed.len() != 71 || deployed[33..65] == [0;32] || deployed[65..67] != 2_u16.to_be_bytes()
        || deployed[67..71] == [0;4]
        || !matches!(deployed[0], 0 | 1)
        || (deployed[0] == 0) != (deployed[1..33] == [0;32])
    { return Err(ProgramSettlementError::SourceSnapshot); }
    let owner = state(records, b"program-owner\0", &program)?;
    if owner.len() != 33 || owner[0] != 1 || owner[1..] != actor { return Err(ProgramSettlementError::Preparation); }
    let lifecycle = state(records, b"wind-down\0s", &program)?;
    if lifecycle.len() != 54 || lifecycle[0] != 1 || !matches!(lifecycle[1], 2 | 3)
        || lifecycle[2..34] != program || lifecycle[34..42] == [0;8]
        || lifecycle[42..50] == [0;8] || u64::from_be_bytes(fixed(&lifecycle[42..50])?) >= sequence
    { return Err(ProgramSettlementError::SourceSnapshot); }
    let mut suffix = program.to_vec(); suffix.extend(account);
    let route = state(records, b"wind-down\0r", &suffix)?;
    if route.len() < 67 || route[0] != 1 { return Err(ProgramSettlementError::SourceSnapshot); }
    let seed_length = usize::from(u16::from_be_bytes(fixed(&route[65..67])?));
    if seed_length > 128 || route.len() != 67 + seed_length { return Err(ProgramSettlementError::SourceSnapshot); }
    let asset = fixed(&route[1..33])?; let destination = fixed(&route[33..65])?;
    let binding = binding(records, account)?;
    if binding.program.bytes() != program || binding.asset_id != asset || binding.seed != route[67..]
        || binding.registered_sequence >= sequence
    { return Err(ProgramSettlementError::SourceSnapshot); }
    let source = accounts.get(&account).ok_or(ProgramSettlementError::SourceSnapshot)?;
    let target = accounts.get(&destination).ok_or(ProgramSettlementError::SourceSnapshot)?;
    if source.kind != 13 || source.authority_key.is_some() || source.created_at_sequence != binding.registered_sequence
        || source.name != format!("module:programs:value:{}",hex::encode(account)).as_bytes()
        || !source.has_asset() || source.asset_id() != asset || source.frozen || source.balance() == 0
        || destination == account || !target.has_asset() || target.asset_id() != asset || target.frozen
        || maximum.is_some_and(|bound| bound == 0 || source.balance() > bound)
    { return Err(ProgramSettlementError::UnreservedDebit); }
    Ok(ProgramExecutedDebit { kind: ProgramChargeKind::ProgramSpend, source: account, asset,
        destination: Some(destination), actual_amount: source.balance() })
}

fn verify_exit_transfer_root(debit: &ProgramExecutedDebit, root: [u8;32]) -> Result<(), ProgramSettlementError> {
    let destination = debit.destination.ok_or(ProgramSettlementError::Terminal)?;
    let mut leg = Vec::with_capacity(115); leg.push(0); leg.extend(debit.source); leg.extend(destination);
    leg.extend(debit.asset); leg.extend(debit.actual_amount.to_be_bytes()); leg.extend(1_u16.to_be_bytes());
    layerx_programs_runtime::transfer::verify_applied_kernel_legs(&leg,root).map_err(|_| ProgramSettlementError::Terminal)
}

fn active_fee_asset(records: &BTreeMap<Vec<u8>, Vec<u8>>) -> Result<[u8;32], ProgramSettlementError> {
    let bytes = state(records,b"progfee/active/v1",&[]).map_err(|_| ProgramSettlementError::FeeProvenance)?;
    if bytes.len() != 217 || &bytes[..5] != b"LXFR1" || bytes[5..9] == [0;4] {
        return Err(ProgramSettlementError::FeeProvenance);
    }
    let value = |offset| -> Result<u64,ProgramSettlementError> { Ok(u64::from_be_bytes(fixed(&bytes[offset..offset+8])?)) };
    let mut prices = [0;7]; for (index,price) in prices.iter_mut().enumerate() { *price=value(9+index*8)?; }
    let asset = fixed(&bytes[65..97])?;
    if prices.contains(&0) || asset == [0;32] || value(97)? == 0 || value(105)? == 0
        || value(113)? == 0 || value(121)? == 0 || value(113)? > value(121)?
        || value(129)? == 0 || value(129)? > value(137)? || prices[6] < value(129)? || prices[6] > value(137)?
        || value(145)? == 0 || value(153)? == u64::MAX || value(161)? == 0 || bytes[169..201] == [0;32]
    { return Err(ProgramSettlementError::FeeProvenance); }
    Ok(asset)
}

fn principal_fee_account<'a>(accounts: &'a BTreeMap<[u8;32], CanonicalAccount>, actor: &Did, asset: [u8;32])
    -> Result<&'a CanonicalAccount, ProgramSettlementError>
{
    let expected = layerx_wire::hash::did_id_for_protocol(actor,3).map_err(|_| ProgramSettlementError::FeeProvenance)?;
    let mut selected = None;
    for account in accounts.values() {
        if !matches!(account.kind,1|14) || account.asset_id() != asset { continue; }
        let name = &account.name;
        let end = match account.kind {
            1 if name.starts_with(b"agent:") && name.ends_with(b":main") => name.len().checked_sub(5),
            14 if name.starts_with(b"agent:") && name.len()>77 && &name[name.len()-71..name.len()-64] == b":asset:" => name.len().checked_sub(71),
            _ => None,
        }.ok_or(ProgramSettlementError::FeeProvenance)?;
        let owner = Did::new(name.get(6..end).ok_or(ProgramSettlementError::FeeProvenance)?)
            .map_err(|_| ProgramSettlementError::FeeProvenance)?;
        if layerx_wire::hash::did_id_for_protocol(&owner,3).map_err(|_| ProgramSettlementError::FeeProvenance)? != expected { continue; }
        if selected.replace(account).is_some() { return Err(ProgramSettlementError::FeeProvenance); }
    }
    let account = selected.ok_or(ProgramSettlementError::FeeProvenance)?;
    if !account.has_asset() || account.frozen { return Err(ProgramSettlementError::FeeProvenance); }
    Ok(account)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn must<T,E: std::fmt::Debug>(value: Result<T,E>) -> T {
        value.unwrap_or_else(|error| panic!("lifecycle settlement: {error:?}"))
    }

    fn account(name: Vec<u8>, kind: u8, asset: [u8;32], balance: u128, frozen: bool) -> CanonicalAccount {
        let id = if kind == 13 {
            must(hex::decode(&name[name.len()-64..])).try_into().unwrap_or_else(|_| panic!("account id"))
        } else {
            let mut identity=b"LX:ACCOUNT:v1".to_vec();
            identity.extend(must(u32::try_from(name.len())).to_be_bytes()); identity.extend(&name);
            Sha256::digest(identity).into()
        };
        let mut bytes=must(u16::try_from(name.len())).to_be_bytes().to_vec();
        bytes.extend(name); bytes.push(kind); bytes.extend(balance.to_be_bytes()); bytes.extend(asset);
        bytes.push(1); bytes.extend(1_u64.to_be_bytes()); bytes.extend(1_u64.to_be_bytes());
        bytes.push(u8::from(frozen)); bytes.push(0); bytes.extend([0;32]); bytes.push(0);
        must(layerx_proof::state::decode_account_value(id,&bytes))
    }

    fn exit_state(amount: u128) -> (BTreeMap<Vec<u8>,Vec<u8>>,BTreeMap<[u8;32],CanonicalAccount>,ProgramValueAccountBinding,[u8;32]) {
        let program=must(ProgramId::new([1;32])); let seed=b"vault".to_vec();
        let source=must(layerx_programs_runtime::derive_program_account(program,&seed)).bytes();
        let mut binding=ProgramValueAccountBinding { record_version:2,program,seed,account_id:source,
            asset_id:[2;32],registered_sequence:1,registration_event_digest:[0;32] };
        binding.registration_event_digest=layerx_programs::program_account_registration_commitment(&binding);
        let source_account=account(format!("module:programs:value:{}",hex::encode(source)).into_bytes(),13,[2;32],amount,false);
        let target=account(b"agent:did:layerx:alice:main".to_vec(),1,[2;32],10,false);
        let destination=target.account_id;
        let mut records=BTreeMap::new();
        let mut put=|prefix:&[u8],suffix:&[u8],value:Vec<u8>| {
            let mut key=prefix.to_vec();key.extend(suffix);records.insert(key,value);
        };
        let mut deployed=vec![0];deployed.extend([0;32]);deployed.extend([3;32]);
        deployed.extend(2_u16.to_be_bytes());deployed.extend(1_u32.to_be_bytes());
        put(b"program\0",&program.bytes(),deployed);
        let mut owner=vec![1];owner.extend([4;32]);put(b"program-owner\0",&program.bytes(),owner);
        let mut lifecycle=vec![1,2];lifecycle.extend(program.bytes());lifecycle.extend(100_u64.to_be_bytes());
        lifecycle.extend(2_u64.to_be_bytes());lifecycle.extend(1_u16.to_be_bytes());lifecycle.extend(1_u16.to_be_bytes());
        put(b"wind-down\0s",&program.bytes(),lifecycle);
        let mut route=vec![1];route.extend(binding.asset_id);route.extend(destination);
        route.extend(must(u16::try_from(binding.seed.len())).to_be_bytes());route.extend(&binding.seed);
        let mut suffix=program.bytes().to_vec();suffix.extend(source);put(b"wind-down\0r",&suffix,route);
        put(b"program-account\0r",&source,must(binding.primary_value()));
        records.insert(binding.primary_key(),must(binding.primary_value()));
        (records,BTreeMap::from([(source,source_account),(destination,target)]),binding,destination)
    }

    #[test]
    fn actual_exit_balance_and_incoming_credit_obey_signed_and_retained_maxima() {
        let (records,accounts,binding,destination)=exit_state(37);
        let debit=must(exit_debit(&records,&accounts,[4;32],binding.program.bytes(),binding.account_id,Some(40),3));
        assert_eq!(debit.actual_amount,37);
        assert_eq!(debit.destination,Some(destination));
        let key=(binding.asset_id,binding.account_id,ProgramChargeKind::ProgramSpend,Some(destination));
        let expected=BTreeMap::from([(key,40)]);
        let mut actual=BTreeMap::new();must(add_actual(&mut actual,&expected,debit.clone()));
        assert_eq!(actual.get(&key),Some(&37));
        assert!(add_actual(&mut BTreeMap::new(),&BTreeMap::from([(key,30)]),debit).is_err());
        assert!(exit_debit(&records,&accounts,[4;32],binding.program.bytes(),binding.account_id,Some(36),3).is_err());
        let mut changed=accounts.clone();
        changed.get_mut(&binding.account_id).unwrap_or_else(||panic!("source")).asset.as_mut()
            .unwrap_or_else(||panic!("asset")).balance=41;
        assert!(exit_debit(&records,&changed,[4;32],binding.program.bytes(),binding.account_id,Some(40),3).is_err());
    }

    #[test]
    fn exit_requires_exact_owner_route_registration_asset_and_live_account() {
        let (records,accounts,binding,_)=exit_state(37);
        let check=|records:&BTreeMap<Vec<u8>,Vec<u8>>,accounts:&BTreeMap<[u8;32],CanonicalAccount>| {
            exit_debit(records,accounts,[4;32],binding.program.bytes(),binding.account_id,Some(40),3)
        };
        assert!(exit_debit(&records,&accounts,[5;32],binding.program.bytes(),binding.account_id,Some(40),3).is_err());
        let mut changed=records.clone();changed.remove(&binding.primary_key());assert!(check(&changed,&accounts).is_err());
        let mut reverse=b"program-account\0r".to_vec();reverse.extend(binding.account_id);
        let mut changed=records.clone();changed.get_mut(&reverse).unwrap_or_else(||panic!("binding"))[107]^=1;
        assert!(check(&changed,&accounts).is_err());
        for offset in [1,33,67] {
            let mut key=b"wind-down\0r".to_vec();key.extend(binding.program.bytes());key.extend(binding.account_id);
            let mut changed=records.clone();changed.get_mut(&key).unwrap_or_else(||panic!("route"))[offset]^=1;
            assert!(check(&changed,&accounts).is_err());
        }
        for (frozen,balance) in [(true,37),(false,0)] {
            let mut changed=accounts.clone();let source=changed.get_mut(&binding.account_id).unwrap_or_else(||panic!("source"));
            source.frozen=frozen;source.asset.as_mut().unwrap_or_else(||panic!("asset")).balance=balance;
            assert!(check(&records,&changed).is_err());
        }
    }

    #[test]
    fn exit_transfer_root_commits_each_actual_debit_coordinate() {
        let (records,accounts,binding,_)=exit_state(37);
        let debit=must(exit_debit(&records,&accounts,[4;32],binding.program.bytes(),binding.account_id,Some(40),3));
        let mut leaf=b"LXP/v1/merkle-leaf\0".to_vec();leaf.push(0);leaf.extend(debit.source);
        leaf.extend(debit.destination.unwrap_or_else(||panic!("destination")));leaf.extend(debit.asset);
        leaf.extend(debit.actual_amount.to_be_bytes());leaf.extend(1_u16.to_be_bytes());
        let root=Sha256::digest(leaf).into();must(verify_exit_transfer_root(&debit,root));
        for changed in [ProgramExecutedDebit{actual_amount:38,..debit.clone()},
            ProgramExecutedDebit{source:[8;32],..debit.clone()},
            ProgramExecutedDebit{asset:[8;32],..debit.clone()},
            ProgramExecutedDebit{destination:Some([8;32]),..debit.clone()}] {
            assert!(verify_exit_transfer_root(&changed,root).is_err());
        }
    }

    #[test]
    fn active_fee_selector_is_canonical_and_does_not_guess_an_asset() {
        let mut value=b"LXFR1".to_vec();value.extend(1_u32.to_be_bytes());
        for _ in 0..7 {value.extend(1_u64.to_be_bytes());}value.extend([2;32]);
        for field in [100_u64,10,1,2,1,100,1,0,1] {value.extend(field.to_be_bytes());}
        value.extend([3;32]);value.extend(0_u128.to_be_bytes());
        let key=b"progfee/active/v1".to_vec();let mut records=BTreeMap::from([(key.clone(),value.clone())]);
        assert_eq!(active_fee_asset(&records),Ok([2;32]));
        for (offset,length) in [(5,4),(9,8),(65,32),(105,8),(169,32)] {
            let mut bad=value.clone();bad[offset..offset+length].fill(0);records.insert(key.clone(),bad);
            assert!(active_fee_asset(&records).is_err());
        }
        records.clear();assert!(active_fee_asset(&records).is_err());
    }

    #[test]
    fn fee_source_requires_native_unique_owner_asset_even_when_duplicate_is_frozen() {
        let actor=must(Did::new(b"did:layerx:alice"));let asset=[2;32];
        let payer=account(b"agent:did:layerx:alice:main".to_vec(),1,asset,100,false);
        let other=account(b"agent:did:layerx:bob:main".to_vec(),1,asset,100,false);
        let mut accounts=BTreeMap::from([(payer.account_id,payer.clone()),(other.account_id,other)]);
        assert_eq!(must(principal_fee_account(&accounts,&actor,asset)).account_id,payer.account_id);
        let duplicate=account(format!("agent:did:layerx:alice:asset:{}",hex::encode(asset)).into_bytes(),14,asset,100,true);
        accounts.insert(duplicate.account_id,duplicate.clone());assert!(principal_fee_account(&accounts,&actor,asset).is_err());
        accounts.remove(&payer.account_id);assert!(principal_fee_account(&accounts,&actor,asset).is_err());
        accounts.remove(&duplicate.account_id);assert!(principal_fee_account(&accounts,&actor,asset).is_err());
    }
}
