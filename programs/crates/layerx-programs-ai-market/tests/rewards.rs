//! AI.F06-A01..A05/A15 arithmetic and encoding gate over the real F05 output
//! types, reward records and ledger counters. No transfer is staged here.
use layerx_programs_ai_market::{
    aggregation_codec::*, codec::domain_hash, errors::*, reward_math::*, rewards::*, *,
};

fn version() -> Version {
    Version::new(1).unwrap()
}
fn wid(n: u8) -> WorkerId {
    WorkerId::new([n; 32]).unwrap()
}
fn recipient(n: u8) -> AccountId {
    AccountId::new([100 + n; 32]).unwrap()
}
fn reserve_account() -> AccountId {
    AccountId::new([250; 32]).unwrap()
}
fn digest(n: u8) -> Digest32 {
    Digest32::new([n; 32]).unwrap()
}
fn roster_entry(n: u8) -> WorkerRosterEntry {
    WorkerRosterEntry {
        worker: wid(n),
        owner: PrincipalId::new([40 + n; 32]).unwrap(),
        recipient: recipient(n),
        generation: version(),
        key_version: version(),
        public_key: PublicKey32([n; 32]),
        metadata: MetadataDigest::new([1; 32]).unwrap(),
    }
}
fn binding() -> FrozenBinding {
    FrozenBinding {
        chain: ChainDomain::new([1; 32]).unwrap(),
        program: ProgramId::new([2; 32]).unwrap(),
        market: MarketId::new([3; 32]).unwrap(),
        epoch: 7,
        config: version(),
        roster: RosterDigest::new([4; 32]).unwrap(),
    }
}
/// F05 output record for one worker from its accepted scores.
fn aggregate(n: u8, scores: &[u32]) -> WorkerAggregate {
    let accepted: Vec<Score> = scores.iter().map(|s| Score::new(*s).unwrap()).collect();
    let support = u8::try_from(scores.len()).unwrap();
    let (status, score) = match lower_median(&accepted).unwrap() {
        Presence::Absent => (QualityStatus::InsufficientQuorum, 0),
        Presence::Present(s) if s.get() == 0 => (QualityStatus::ScoredZero, 0),
        Presence::Present(s) => (QualityStatus::ScoredPositive, s.get()),
    };
    let weight = if status == QualityStatus::ScoredPositive {
        score
    } else {
        0
    };
    WorkerAggregate::new(wid(n), version(), support, status, score, weight).unwrap()
}
fn amounts(a: &Allocation) -> Vec<(WorkerId, Amount)> {
    (0..a.len()).map(|i| a.entitlement(i).unwrap()).collect()
}
fn ledger() -> RewardLedger {
    RewardLedger::new(
        AssetId::new([5; 32]).unwrap(),
        reserve_account(),
        recipient(99),
    )
    .unwrap()
}
fn dictionary(workers: &[u8]) -> RecipientDictionary {
    let mut d = RecipientDictionary::new();
    for (i, n) in workers.iter().enumerate() {
        d.slots[i] = Some(RecipientSlot {
            worker: wid(*n),
            recipient: recipient(*n),
            references: 1,
        });
    }
    d
}
fn terminal_row(
    allocation: &Allocation,
    allocation_root: Digest32,
    terminal_height: u64,
    expiry: u64,
) -> RewardEpoch {
    let slots: Vec<u16> = (0..allocation.len() as u16).collect();
    let mut row = RewardEpoch::reserved(7, allocation.budget(), binding().roster, &slots).unwrap();
    row.status = EpochStatus::Terminal;
    row.outcome = allocation.outcome();
    row.terminal_height = terminal_height;
    row.expiry_height = expiry;
    row.aggregation = Presence::Present(digest(8));
    row.allocation = Presence::Present(allocation_root);
    for i in 0..allocation.len() {
        row.entries[i].entitlement = allocation.entitlement(i).unwrap().1;
    }
    row.validate().unwrap();
    row
}
fn roundtrip_epoch(row: &RewardEpoch) -> Vec<u8> {
    let mut out = [0u8; EPOCH_MAX_BYTES];
    let n = encode_epoch(row, &mut out).unwrap();
    assert_eq!(
        n,
        EPOCH_HEADER_BYTES + usize::from(row.entry_count) * ENTRY_BYTES
    );
    assert_eq!(decode_epoch(&out[..n]).unwrap(), *row);
    out[..n].to_vec()
}

fn a01_exact_rounding() {
    let outputs = [
        aggregate(1, &[4, 4, 4]),
        aggregate(2, &[2, 2, 2]),
        aggregate(3, &[1, 1, 1]),
    ];
    let roster = [roster_entry(1), roster_entry(2), roster_entry(3)];
    let allocation = allocate(101, &outputs).unwrap();
    assert_eq!(allocation.total_weight(), 7);
    assert_eq!(mul_div_rem(101, 4, 7).unwrap(), (57, 5));
    assert_eq!(mul_div_rem(101, 2, 7).unwrap(), (28, 6));
    assert_eq!(mul_div_rem(101, 1, 7).unwrap(), (14, 3));
    assert_eq!(allocation.outcome(), RewardOutcome::Allocated);
    assert_eq!(
        amounts(&allocation),
        vec![(wid(1), 58), (wid(2), 29), (wid(3), 14)]
    );
    let aggregation =
        EpochAggregation::structural(binding(), digest(9), &roster, &outputs).unwrap();
    assert_eq!(allocate_aggregation(101, &aggregation).unwrap(), allocation);
    let root = allocation_digest(
        &binding(),
        aggregation.root(),
        AssetId::new([5; 32]).unwrap(),
        &allocation,
        &roster,
    )
    .unwrap();

    // R=101 moves to C=101 with no transfer: P and X stay zero.
    let funded = ledger().deposit(101).unwrap();
    let reserved = funded.reserve(101).unwrap();
    assert_eq!((reserved.free, reserved.reserved), (0, 101));
    let reserved_row = RewardEpoch::reserved(7, 101, binding().roster, &[0, 1, 2]).unwrap();
    reserved.check_rows(&[reserved_row]).unwrap();
    let bytes = roundtrip_epoch(&reserved_row);
    assert_eq!(&bytes[172..174], &3u16.to_be_bytes());
    assert_eq!(&bytes[140..172], &[0u8; 32]);
    let (terminal, expiry) = reserved
        .terminalize(101, RewardOutcome::Allocated, 900)
        .unwrap();
    assert_eq!(expiry, 4996);
    assert_eq!(
        (
            terminal.reserved,
            terminal.liability,
            terminal.total_claimed,
            terminal.tracked_refunds
        ),
        (0, 101, 0, 0)
    );
    assert_eq!(terminal.tracked_deposits, 101);
    let row = terminal_row(&allocation, root, 900, expiry);
    terminal.check_rows(&[row]).unwrap();
    row.check_dictionary(&dictionary(&[1, 2, 3])).unwrap();
    let bytes = roundtrip_epoch(&row);
    assert_eq!(&bytes[0..8], &7u64.to_be_bytes());
    assert_eq!(bytes[8], 2);
    assert_eq!(bytes[9], 1);
    assert_eq!(&bytes[12..20], &900u64.to_be_bytes());
    assert_eq!(&bytes[20..28], &4996u64.to_be_bytes());
    assert_eq!(&bytes[28..44], &101u128.to_be_bytes());
    assert_eq!(&bytes[140..172], root.as_bytes());
    assert_eq!(&bytes[174..176], &0u16.to_be_bytes());
    assert_eq!(&bytes[176..192], &58u128.to_be_bytes());
    assert_eq!(bytes[192], 0);
    let mut out = [0u8; LEDGER_BYTES];
    assert_eq!(encode_ledger(&terminal, &mut out).unwrap(), LEDGER_BYTES);
    assert_eq!(decode_ledger(&out).unwrap(), terminal);
}

fn a02_ties() {
    let outputs = [
        aggregate(1, &[1, 1, 1]),
        aggregate(2, &[1, 1, 1]),
        aggregate(3, &[1, 1, 1]),
    ];
    let roster = [roster_entry(1), roster_entry(2), roster_entry(3)];
    let allocation = allocate(2, &outputs).unwrap();
    assert_eq!(
        amounts(&allocation),
        vec![(wid(1), 1), (wid(2), 1), (wid(3), 0)]
    );
    let shuffled = [outputs[2], outputs[0], outputs[1]];
    let reordered = allocate(2, &shuffled).unwrap();
    assert_eq!(reordered, allocation);
    let asset = AssetId::new([5; 32]).unwrap();
    let root = allocation_digest(&binding(), digest(8), asset, &allocation, &roster).unwrap();
    let shuffled_roster = [roster[1], roster[2], roster[0]];
    assert_eq!(
        allocation_digest(&binding(), digest(8), asset, &reordered, &shuffled_roster).unwrap(),
        root
    );
    let duplicate = [outputs[0], outputs[0]];
    assert_eq!(allocate(2, &duplicate), Err(NON_CANONICAL));

    let row = terminal_row(&allocation, root, 900, 4996);
    let dict = dictionary(&[1, 2, 3]);
    assert_eq!(
        row.check_claim(&dict, wid(3), recipient(3), 0, 1000),
        Err(F06_NOTHING_TO_CLAIM)
    );
    assert_eq!(
        row.check_claim(&dict, wid(1), recipient(1), 1, 1000),
        Ok(ClaimDecision::Payable {
            index: 0,
            amount: 1
        })
    );
    assert_eq!(
        row.check_claim(&dict, wid(1), recipient(2), 1, 1000),
        Err(F06_WRONG_CLAIM_RECIPIENT)
    );
    assert_eq!(
        row.check_claim(&dict, wid(1), recipient(1), 2, 1000),
        Err(F06_WRONG_CLAIM_AMOUNT)
    );
    assert_eq!(
        row.check_claim(&dict, wid(1), recipient(1), 1, 4996),
        Err(F06_CLAIM_EXPIRED)
    );
    let mut claimed = row;
    claimed.entries[0].disposition = Disposition::Claimed;
    claimed.paid_sum = 1;
    assert_eq!(
        claimed.check_claim(&dict, wid(1), recipient(1), 1, 1000),
        Ok(ClaimDecision::AlreadyApplied(
            entitlement_id(root, wid(1)).unwrap()
        ))
    );
    let mut id_preimage = [0u8; 64];
    id_preimage[..32].copy_from_slice(root.as_bytes());
    id_preimage[32..].copy_from_slice(wid(1).as_bytes());
    assert_eq!(
        entitlement_id(root, wid(1)).unwrap(),
        domain_hash("PAXAI/reward-entitlement/v1", &id_preimage).unwrap()
    );
}

fn a03_lower_median() {
    let s = |v: &[u32]| -> Vec<Score> { v.iter().map(|x| Score::new(*x).unwrap()).collect() };
    assert_eq!(
        lower_median(&s(&[10, 0, 9, 8])).unwrap(),
        Presence::Present(Score::new(8).unwrap())
    );
    assert_eq!(
        lower_median(&s(&[0, 0, 1])).unwrap(),
        Presence::Present(Score::new(0).unwrap())
    );
    assert_eq!(
        lower_median(&s(&[1_000_000, 1_000_000])).unwrap(),
        Presence::Absent
    );
    let outputs = [
        aggregate(1, &[0, 8, 9, 10]),
        aggregate(2, &[0, 0, 1]),
        aggregate(3, &[1_000_000, 1_000_000]),
    ];
    assert_eq!(outputs[1].status(), QualityStatus::ScoredZero);
    assert_eq!(outputs[2].status(), QualityStatus::InsufficientQuorum);
    let allocation = allocate(13, &outputs).unwrap();
    assert_eq!(
        amounts(&allocation),
        vec![(wid(1), 13), (wid(2), 0), (wid(3), 0)]
    );
}

fn a04_empty_zero() {
    let outputs = [
        aggregate(1, &[0, 0, 0]),
        aggregate(2, &[5, 5]),
        aggregate(3, &[]),
    ];
    let allocation = allocate(77, &outputs).unwrap();
    assert_eq!(allocation.outcome(), RewardOutcome::NoEligibleScore);
    assert!(allocation.is_empty());
    assert_eq!(allocation.budget(), 77);
    let root = allocation_digest(
        &binding(),
        digest(8),
        AssetId::new([5; 32]).unwrap(),
        &allocation,
        &[],
    )
    .unwrap();
    let reserved = ledger().deposit(77).unwrap().reserve(77).unwrap();
    let (terminal, expiry) = reserved
        .terminalize(77, RewardOutcome::NoEligibleScore, 900)
        .unwrap();
    assert_eq!(expiry, 0);
    assert_eq!(
        (
            terminal.reserved,
            terminal.free,
            terminal.liability,
            terminal.total_claimed
        ),
        (0, 77, 0, 0)
    );
    let mut row = RewardEpoch::reserved(7, 77, binding().roster, &[0, 1, 2]).unwrap();
    row.status = EpochStatus::Terminal;
    row.outcome = RewardOutcome::NoEligibleScore;
    row.terminal_height = 900;
    row.entry_count = 0;
    row.entries = [RewardEntry::EMPTY; 32];
    row.aggregation = Presence::Present(digest(8));
    row.allocation = Presence::Present(root);
    terminal.check_rows(&[row]).unwrap();
    assert_eq!(roundtrip_epoch(&row).len(), EPOCH_HEADER_BYTES);
    assert_eq!(
        row.check_claim(&dictionary(&[1, 2, 3]), wid(1), recipient(1), 0, 901),
        Err(F06_UNKNOWN_WORKER_ENTITLEMENT)
    );
}

fn a05_maximal_amount() {
    let outputs = [
        aggregate(1, &[500_000, 500_000, 500_000]),
        aggregate(2, &[500_000, 500_000, 500_000]),
    ];
    let allocation = allocate(u128::MAX, &outputs).unwrap();
    assert_eq!(
        amounts(&allocation),
        vec![
            (wid(1), 0x80000000000000000000000000000000),
            (wid(2), 0x7fffffffffffffffffffffffffffffff)
        ]
    );
    assert_eq!(
        mul_div_rem(u128::MAX, 1_000_000, 1_000_000).unwrap(),
        (u128::MAX, 0)
    );
    assert_eq!(mul_div_rem(u128::MAX, 1_000_000, 1), Err(ARITHMETIC));
    assert_eq!(mul_div_rem(1, 1, 0), Err(ARITHMETIC));
    assert_eq!(allocate(0, &outputs), Err(F06_INVALID_AMOUNT));
}

fn a15_overflow_corruption() {
    let full = ledger().deposit(u128::MAX).unwrap();
    assert_eq!(full.deposit(1), Err(ARITHMETIC));
    assert_eq!((full.tracked_deposits, full.free), (u128::MAX, u128::MAX));
    assert_eq!(ledger().deposit(0), Err(F06_INVALID_AMOUNT));
    assert_eq!(
        ledger().deposit(10).unwrap().reserve(11),
        Err(INSUFFICIENT_FREE)
    );

    let outputs = [
        aggregate(1, &[4, 4, 4]),
        aggregate(2, &[2, 2, 2]),
        aggregate(3, &[1, 1, 1]),
    ];
    let allocation = allocate(101, &outputs).unwrap();
    let reserved = ledger().deposit(120).unwrap().reserve(101).unwrap();
    assert_eq!(reserved.reserve(1), Err(F06_EPOCH_ALREADY_RESERVED));
    assert_eq!(
        reserved.terminalize(101, RewardOutcome::Allocated, u64::MAX - 4095),
        Err(ARITHMETIC)
    );
    assert_eq!(reserved.reserved, 101);
    assert!(reserved.active_reserve);
    let (terminal, expiry) = reserved
        .terminalize(101, RewardOutcome::Allocated, 900)
        .unwrap();
    let row = terminal_row(&allocation, digest(6), 900, expiry);
    terminal.check_rows(&[row]).unwrap();

    let mut forged = terminal;
    forged.liability = 100;
    forged.free = 20;
    forged.validate().unwrap();
    assert_eq!(
        forged.check_rows(&[row]),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
    let mut broken = terminal;
    broken.liability = 100;
    assert_eq!(broken.validate(), Err(F06_LEDGER_INVARIANT_VIOLATION));
    assert_eq!(
        broken.check_rows(&[row]),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
    let mut out = [0u8; LEDGER_BYTES];
    encode_ledger(&terminal, &mut out).unwrap();
    let mut bad = out;
    bad[96 + 8 + 80..96 + 8 + 96].copy_from_slice(&100u128.to_be_bytes());
    assert_eq!(decode_ledger(&bad), Err(F06_LEDGER_INVARIANT_VIOLATION));
    let mut bad_row = row;
    bad_row.entries[0].entitlement = 57;
    assert_eq!(bad_row.validate(), Err(NON_CANONICAL));
    let mut bad_expiry = row;
    bad_expiry.terminal_height = u64::MAX;
    assert_eq!(bad_expiry.validate(), Err(ARITHMETIC));
}

fn ledger_encoding() {
    let funded = ledger().deposit(120).unwrap().reserve(101).unwrap();
    let mut out = [0u8; LEDGER_BYTES + 1];
    assert_eq!(encode_ledger(&funded, &mut out).unwrap(), LEDGER_BYTES);
    assert_eq!(&out[96..104], &1u64.to_be_bytes());
    assert_eq!(&out[104..120], &120u128.to_be_bytes());
    assert_eq!(&out[168..184], &101u128.to_be_bytes());
    assert_eq!(out[203], 1);
    assert_eq!(decode_ledger(&out[..LEDGER_BYTES]).unwrap(), funded);
    assert_eq!(decode_ledger(&out), Err(NON_CANONICAL));
    assert_eq!(decode_ledger(&out[..LEDGER_BYTES - 1]), Err(NON_CANONICAL));
    let mut bad = out;
    bad[103] = 2;
    assert_eq!(
        decode_ledger(&bad[..LEDGER_BYTES]),
        Err(F06_FUNDING_POLICY_MISMATCH)
    );
    let mut bad = out;
    bad[205] = 1;
    assert_eq!(decode_ledger(&bad[..LEDGER_BYTES]), Err(NON_CANONICAL));
    let mut bad = out;
    bad[203] = 2;
    assert_eq!(decode_ledger(&bad[..LEDGER_BYTES]), Err(NON_CANONICAL));
    assert_eq!(
        RewardLedger::new(
            AssetId::new([5; 32]).unwrap(),
            reserve_account(),
            reserve_account()
        ),
        Err(F06_REFUND_RECIPIENT_MISMATCH)
    );
}

fn dictionary_encoding() {
    let dict = dictionary(&[1, 2]);
    let mut out = vec![0u8; DICTIONARY_BYTES];
    assert_eq!(
        encode_dictionary(&dict, reserve_account(), &mut out).unwrap(),
        17_152
    );
    assert_eq!(out[0], 1);
    assert_eq!(&out[65..67], &1u16.to_be_bytes());
    assert!(out[2 * SLOT_BYTES..].iter().all(|b| *b == 0));
    assert_eq!(decode_dictionary(&out, reserve_account()).unwrap(), dict);
    assert_eq!(dict.occupied(), 2);
    let mut bad = out.clone();
    bad[2 * SLOT_BYTES + 5] = 1;
    assert_eq!(
        decode_dictionary(&bad, reserve_account()),
        Err(NON_CANONICAL)
    );
    let mut bad = out.clone();
    bad[0] = 2;
    assert_eq!(
        decode_dictionary(&bad, reserve_account()),
        Err(NON_CANONICAL)
    );
    let mut bad = out.clone();
    bad[65..67].copy_from_slice(&1057u16.to_be_bytes());
    assert_eq!(
        decode_dictionary(&bad, reserve_account()),
        Err(NON_CANONICAL)
    );
    let mut dup = dict;
    dup.slots[5] = dict.slots[0];
    assert_eq!(
        encode_dictionary(&dup, reserve_account(), &mut out),
        Err(NON_CANONICAL)
    );
    assert_eq!(
        decode_dictionary(&out[..DICTIONARY_BYTES - 1], reserve_account()),
        Err(NON_CANONICAL)
    );
    assert_eq!(decode_dictionary(&out, recipient(1)), Err(ACCOUNT_BINDING));
    let row = RewardEpoch::reserved(7, 5, binding().roster, &[1, 0]).unwrap();
    assert_eq!(
        row.check_dictionary(&dict),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
    let row = RewardEpoch::reserved(7, 5, binding().roster, &[0, 3]).unwrap();
    assert_eq!(
        row.check_dictionary(&dict),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
}

fn epoch_encoding() {
    let row = RewardEpoch::reserved(7, 101, binding().roster, &[0, 1, 2]).unwrap();
    let bytes = roundtrip_epoch(&row);
    assert_eq!(bytes.len(), 231);
    let mut bad = bytes.clone();
    bad[8] = 4;
    assert_eq!(decode_epoch(&bad), Err(NON_CANONICAL));
    let mut bad = bytes.clone();
    bad[9] = 3;
    assert_eq!(decode_epoch(&bad), Err(NON_CANONICAL));
    let mut bad = bytes.clone();
    bad[11] = 1;
    assert_eq!(decode_epoch(&bad), Err(NON_CANONICAL));
    let mut bad = bytes.clone();
    bad[174..176].copy_from_slice(&256u16.to_be_bytes());
    assert_eq!(decode_epoch(&bad), Err(NON_CANONICAL));
    let mut bad = bytes.clone();
    bad[192] = 3;
    assert_eq!(decode_epoch(&bad), Err(NON_CANONICAL));
    let mut bad = bytes.clone();
    bad.push(0);
    assert_eq!(decode_epoch(&bad), Err(NON_CANONICAL));
    let mut bad = bytes.clone();
    bad[172..174].copy_from_slice(&33u16.to_be_bytes());
    assert_eq!(decode_epoch(&bad), Err(CAPACITY));
    assert_eq!(
        RewardEpoch::reserved(7, 0, binding().roster, &[0]),
        Err(F06_INVALID_AMOUNT)
    );
    assert_eq!(
        RewardEpoch::reserved(7, 1, binding().roster, &[]),
        Err(NON_CANONICAL)
    );
    assert_eq!(
        row.check_claim(&dictionary(&[1, 2, 3]), wid(1), recipient(1), 0, 10),
        Err(F06_CLAIM_NOT_READY)
    );
}

#[test]
fn reward_math_and_encoding() {
    a01_exact_rounding();
    a02_ties();
    a03_lower_median();
    a04_empty_zero();
    a05_maximal_amount();
    a15_overflow_corruption();
    ledger_encoding();
    dictionary_encoding();
    epoch_encoding();
}
