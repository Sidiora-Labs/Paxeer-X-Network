//! AI.F06-A01..A05/A15 arithmetic and encoding gate over the real F05 output
//! types, reward records and ledger counters. No transfer is staged here.
use layerx_programs_ai_market::{
    aggregation_codec::*,
    codec::domain_hash,
    errors::*,
    reward_math::*,
    rewards::*,
    state::{ActorSlot, ReplayRequest, ReplayTable},
    *,
};

fn version() -> CodecResult<Version> {
    Version::new(1)
}
fn wid(n: u8) -> CodecResult<WorkerId> {
    WorkerId::new([n; 32])
}
fn recipient(n: u8) -> CodecResult<AccountId> {
    AccountId::new([100 + n; 32])
}
fn reserve_account() -> CodecResult<AccountId> {
    AccountId::new([250; 32])
}
fn digest(n: u8) -> CodecResult<Digest32> {
    Digest32::new([n; 32])
}
fn roster_entry(n: u8) -> CodecResult<WorkerRosterEntry> {
    Ok(WorkerRosterEntry {
        worker: wid(n)?,
        owner: PrincipalId::new([40 + n; 32])?,
        recipient: recipient(n)?,
        generation: version()?,
        key_version: version()?,
        public_key: PublicKey32([n; 32]),
        metadata: MetadataDigest::new([1; 32])?,
    })
}
fn binding() -> CodecResult<FrozenBinding> {
    Ok(FrozenBinding {
        chain: ChainDomain::new([1; 32])?,
        program: ProgramId::new([2; 32])?,
        market: MarketId::new([3; 32])?,
        epoch: 7,
        config: version()?,
        roster: RosterDigest::new([4; 32])?,
    })
}
/// F05 output record for one worker from its accepted scores.
fn aggregate(n: u8, scores: &[u32]) -> CodecResult<WorkerAggregate> {
    let accepted = scores
        .iter()
        .map(|s| Score::new(*s))
        .collect::<CodecResult<Vec<Score>>>()?;
    let support = u8::try_from(scores.len()).map_err(|_| ARITHMETIC)?;
    let (status, score) = match lower_median(&accepted)? {
        Presence::Absent => (QualityStatus::InsufficientQuorum, 0),
        Presence::Present(s) if s.get() == 0 => (QualityStatus::ScoredZero, 0),
        Presence::Present(s) => (QualityStatus::ScoredPositive, s.get()),
    };
    let weight = if status == QualityStatus::ScoredPositive {
        score
    } else {
        0
    };
    WorkerAggregate::new(wid(n)?, version()?, support, status, score, weight)
}
fn amounts(a: &Allocation) -> CodecResult<Vec<(WorkerId, Amount)>> {
    (0..a.len()).map(|i| a.entitlement(i)).collect()
}
fn ledger() -> CodecResult<RewardLedger> {
    RewardLedger::new(AssetId::new([5; 32])?, reserve_account()?, recipient(99)?)
}
/// Dictionary section bytes with `workers` in slots 0.. and one reference each.
fn dictionary(workers: &[u8]) -> CodecResult<Vec<u8>> {
    let mut d = vec![0xAA; DICTIONARY_BYTES];
    RecipientDictionary::init_empty(&mut d)?;
    for (index, n) in (0u16..).zip(workers) {
        RecipientDictionary::write_slot(
            &mut d,
            index,
            Some(&RecipientSlot {
                worker: wid(*n)?,
                recipient: recipient(*n)?,
                references: 1,
            }),
        )?;
    }
    Ok(d)
}
fn terminal_row(
    allocation: &Allocation,
    allocation_root: Digest32,
    terminal_height: u64,
    expiry: u64,
) -> CodecResult<RewardEpoch> {
    let count = u16::try_from(allocation.len()).map_err(|_| ARITHMETIC)?;
    let slots: Vec<u16> = (0..count).collect();
    let mut row = RewardEpoch::reserved(7, allocation.budget(), binding()?.roster, &slots)?;
    row.status = EpochStatus::Terminal;
    row.outcome = allocation.outcome();
    row.terminal_height = terminal_height;
    row.expiry_height = expiry;
    row.aggregation = Presence::Present(digest(8)?);
    row.allocation = Presence::Present(allocation_root);
    for (i, entry) in row.entries.iter_mut().take(allocation.len()).enumerate() {
        entry.entitlement = allocation.entitlement(i)?.1;
    }
    row.validate()?;
    Ok(row)
}
fn roundtrip_epoch(row: &RewardEpoch) -> CodecResult<Vec<u8>> {
    let mut out = [0u8; EPOCH_MAX_BYTES];
    let n = encode_epoch(row, &mut out)?;
    assert_eq!(
        n,
        EPOCH_HEADER_BYTES + usize::from(row.entry_count) * ENTRY_BYTES
    );
    assert_eq!(decode_epoch(&out[..n])?, *row);
    Ok(out[..n].to_vec())
}

fn a01_exact_rounding() -> CodecResult<()> {
    let outputs = [
        aggregate(1, &[4, 4, 4])?,
        aggregate(2, &[2, 2, 2])?,
        aggregate(3, &[1, 1, 1])?,
    ];
    let roster = [roster_entry(1)?, roster_entry(2)?, roster_entry(3)?];
    let allocation = allocate(101, &outputs)?;
    assert_eq!(allocation.total_weight(), 7);
    assert_eq!(mul_div_rem(101, 4, 7)?, (57, 5));
    assert_eq!(mul_div_rem(101, 2, 7)?, (28, 6));
    assert_eq!(mul_div_rem(101, 1, 7)?, (14, 3));
    assert_eq!(allocation.outcome(), RewardOutcome::Allocated);
    assert_eq!(
        amounts(&allocation)?,
        vec![(wid(1)?, 58), (wid(2)?, 29), (wid(3)?, 14)]
    );
    let aggregation = EpochAggregation::structural(binding()?, digest(9)?, &roster, &outputs)?;
    assert_eq!(allocate_aggregation(101, &aggregation)?, allocation);
    let root = allocation_digest(
        &binding()?,
        aggregation.root(),
        AssetId::new([5; 32])?,
        &allocation,
        &roster,
    )?;

    // R=101 moves to C=101 with no transfer: P and X stay zero.
    let funded = ledger()?.deposit(101)?;
    let reserved = funded.reserve(101)?;
    assert_eq!((reserved.free, reserved.reserved), (0, 101));
    let reserved_row = RewardEpoch::reserved(7, 101, binding()?.roster, &[0, 1, 2])?;
    reserved.check_rows(&[reserved_row])?;
    let bytes = roundtrip_epoch(&reserved_row)?;
    assert_eq!(&bytes[172..174], &3u16.to_be_bytes());
    assert_eq!(&bytes[140..172], &[0u8; 32]);
    let (terminal, expiry) = reserved.terminalize(101, RewardOutcome::Allocated, 900)?;
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
    let row = terminal_row(&allocation, root, 900, expiry)?;
    terminal.check_rows(&[row])?;
    row.check_dictionary(&RecipientDictionary::new(&dictionary(&[1, 2, 3])?)?)?;
    let bytes = roundtrip_epoch(&row)?;
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
    assert_eq!(encode_ledger(&terminal, &mut out)?, LEDGER_BYTES);
    assert_eq!(decode_ledger(&out)?, terminal);
    Ok(())
}

fn a02_ties() -> CodecResult<()> {
    let outputs = [
        aggregate(1, &[1, 1, 1])?,
        aggregate(2, &[1, 1, 1])?,
        aggregate(3, &[1, 1, 1])?,
    ];
    let roster = [roster_entry(1)?, roster_entry(2)?, roster_entry(3)?];
    let allocation = allocate(2, &outputs)?;
    assert_eq!(
        amounts(&allocation)?,
        vec![(wid(1)?, 1), (wid(2)?, 1), (wid(3)?, 0)]
    );
    let shuffled = [outputs[2], outputs[0], outputs[1]];
    let reordered = allocate(2, &shuffled)?;
    assert_eq!(reordered, allocation);
    let asset = AssetId::new([5; 32])?;
    let root = allocation_digest(&binding()?, digest(8)?, asset, &allocation, &roster)?;
    let shuffled_roster = [roster[1], roster[2], roster[0]];
    assert_eq!(
        allocation_digest(&binding()?, digest(8)?, asset, &reordered, &shuffled_roster)?,
        root
    );
    let duplicate = [outputs[0], outputs[0]];
    assert_eq!(allocate(2, &duplicate), Err(NON_CANONICAL));

    let row = terminal_row(&allocation, root, 900, 4996)?;
    let dict_bytes = dictionary(&[1, 2, 3])?;
    let dict = RecipientDictionary::new(&dict_bytes)?;
    assert_eq!(
        row.check_claim(&dict, wid(3)?, recipient(3)?, 0, 1000),
        Err(F06_NOTHING_TO_CLAIM)
    );
    assert_eq!(
        row.check_claim(&dict, wid(1)?, recipient(1)?, 1, 1000),
        Ok(ClaimDecision::Payable {
            index: 0,
            amount: 1
        })
    );
    assert_eq!(
        row.check_claim(&dict, wid(1)?, recipient(2)?, 1, 1000),
        Err(F06_WRONG_CLAIM_RECIPIENT)
    );
    assert_eq!(
        row.check_claim(&dict, wid(1)?, recipient(1)?, 2, 1000),
        Err(F06_WRONG_CLAIM_AMOUNT)
    );
    assert_eq!(
        row.check_claim(&dict, wid(1)?, recipient(1)?, 1, 4996),
        Err(F06_CLAIM_EXPIRED)
    );
    let mut claimed = row;
    claimed.entries[0].disposition = Disposition::Claimed;
    claimed.paid_sum = 1;
    assert_eq!(
        claimed.check_claim(&dict, wid(1)?, recipient(1)?, 1, 1000),
        Ok(ClaimDecision::AlreadyApplied(entitlement_id(
            root,
            wid(1)?
        )?))
    );
    let mut id_preimage = [0u8; 64];
    id_preimage[..32].copy_from_slice(root.as_bytes());
    id_preimage[32..].copy_from_slice(wid(1)?.as_bytes());
    assert_eq!(
        entitlement_id(root, wid(1)?)?,
        domain_hash("PAXAI/reward-entitlement/v1", &id_preimage)?
    );
    Ok(())
}

fn a03_lower_median() -> CodecResult<()> {
    let s = |v: &[u32]| -> CodecResult<Vec<Score>> { v.iter().map(|x| Score::new(*x)).collect() };
    assert_eq!(
        lower_median(&s(&[10, 0, 9, 8])?)?,
        Presence::Present(Score::new(8)?)
    );
    assert_eq!(
        lower_median(&s(&[0, 0, 1])?)?,
        Presence::Present(Score::new(0)?)
    );
    assert_eq!(
        lower_median(&s(&[1_000_000, 1_000_000])?)?,
        Presence::Absent
    );
    let outputs = [
        aggregate(1, &[0, 8, 9, 10])?,
        aggregate(2, &[0, 0, 1])?,
        aggregate(3, &[1_000_000, 1_000_000])?,
    ];
    assert_eq!(outputs[1].status(), QualityStatus::ScoredZero);
    assert_eq!(outputs[2].status(), QualityStatus::InsufficientQuorum);
    let allocation = allocate(13, &outputs)?;
    assert_eq!(
        amounts(&allocation)?,
        vec![(wid(1)?, 13), (wid(2)?, 0), (wid(3)?, 0)]
    );
    Ok(())
}

fn a04_empty_zero() -> CodecResult<()> {
    let outputs = [
        aggregate(1, &[0, 0, 0])?,
        aggregate(2, &[5, 5])?,
        aggregate(3, &[])?,
    ];
    let allocation = allocate(77, &outputs)?;
    assert_eq!(allocation.outcome(), RewardOutcome::NoEligibleScore);
    assert!(allocation.is_empty());
    assert_eq!(allocation.budget(), 77);
    let root = allocation_digest(
        &binding()?,
        digest(8)?,
        AssetId::new([5; 32])?,
        &allocation,
        &[],
    )?;
    let reserved = ledger()?.deposit(77)?.reserve(77)?;
    let (terminal, expiry) = reserved.terminalize(77, RewardOutcome::NoEligibleScore, 900)?;
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
    let mut row = RewardEpoch::reserved(7, 77, binding()?.roster, &[0, 1, 2])?;
    row.status = EpochStatus::Terminal;
    row.outcome = RewardOutcome::NoEligibleScore;
    row.terminal_height = 900;
    row.entry_count = 0;
    row.entries = [RewardEntry::EMPTY; 32];
    row.aggregation = Presence::Present(digest(8)?);
    row.allocation = Presence::Present(root);
    terminal.check_rows(&[row])?;
    assert_eq!(roundtrip_epoch(&row)?.len(), EPOCH_HEADER_BYTES);
    assert_eq!(
        row.check_claim(
            &RecipientDictionary::new(&dictionary(&[1, 2, 3])?)?,
            wid(1)?,
            recipient(1)?,
            0,
            901
        ),
        Err(F06_UNKNOWN_WORKER_ENTITLEMENT)
    );
    Ok(())
}

fn a05_maximal_amount() -> CodecResult<()> {
    let outputs = [
        aggregate(1, &[500_000, 500_000, 500_000])?,
        aggregate(2, &[500_000, 500_000, 500_000])?,
    ];
    let allocation = allocate(u128::MAX, &outputs)?;
    assert_eq!(
        amounts(&allocation)?,
        vec![
            (wid(1)?, 0x8000_0000_0000_0000_0000_0000_0000_0000),
            (wid(2)?, 0x7fff_ffff_ffff_ffff_ffff_ffff_ffff_ffff)
        ]
    );
    assert_eq!(
        mul_div_rem(u128::MAX, 1_000_000, 1_000_000)?,
        (u128::MAX, 0)
    );
    assert_eq!(mul_div_rem(u128::MAX, 1_000_000, 1), Err(ARITHMETIC));
    assert_eq!(mul_div_rem(1, 1, 0), Err(ARITHMETIC));
    assert_eq!(allocate(0, &outputs), Err(F06_INVALID_AMOUNT));
    Ok(())
}

fn a15_overflow_corruption() -> CodecResult<()> {
    let full = ledger()?.deposit(u128::MAX)?;
    assert_eq!(full.deposit(1), Err(ARITHMETIC));
    assert_eq!((full.tracked_deposits, full.free), (u128::MAX, u128::MAX));
    assert_eq!(ledger()?.deposit(0), Err(F06_INVALID_AMOUNT));
    assert_eq!(ledger()?.deposit(10)?.reserve(11), Err(INSUFFICIENT_FREE));

    let outputs = [
        aggregate(1, &[4, 4, 4])?,
        aggregate(2, &[2, 2, 2])?,
        aggregate(3, &[1, 1, 1])?,
    ];
    let allocation = allocate(101, &outputs)?;
    let reserved = ledger()?.deposit(120)?.reserve(101)?;
    assert_eq!(reserved.reserve(1), Err(F06_EPOCH_ALREADY_RESERVED));
    assert_eq!(
        reserved.terminalize(101, RewardOutcome::Allocated, u64::MAX - 4095),
        Err(ARITHMETIC)
    );
    assert_eq!(reserved.reserved, 101);
    assert!(reserved.active_reserve);
    let (terminal, expiry) = reserved.terminalize(101, RewardOutcome::Allocated, 900)?;
    let row = terminal_row(&allocation, digest(6)?, 900, expiry)?;
    terminal.check_rows(&[row])?;

    let mut forged = terminal;
    forged.liability = 100;
    forged.free = 20;
    forged.validate()?;
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
    encode_ledger(&terminal, &mut out)?;
    let mut bad = out;
    bad[96 + 8 + 80..96 + 8 + 96].copy_from_slice(&100u128.to_be_bytes());
    assert_eq!(decode_ledger(&bad), Err(F06_LEDGER_INVARIANT_VIOLATION));
    let mut bad_row = row;
    bad_row.entries[0].entitlement = 57;
    assert_eq!(bad_row.validate(), Err(NON_CANONICAL));
    let mut bad_expiry = row;
    bad_expiry.terminal_height = u64::MAX;
    assert_eq!(bad_expiry.validate(), Err(ARITHMETIC));
    Ok(())
}

fn ledger_encoding() -> CodecResult<()> {
    let funded = ledger()?.deposit(120)?.reserve(101)?;
    let mut out = [0u8; LEDGER_BYTES + 1];
    assert_eq!(encode_ledger(&funded, &mut out)?, LEDGER_BYTES);
    assert_eq!(&out[96..104], &1u64.to_be_bytes());
    assert_eq!(&out[104..120], &120u128.to_be_bytes());
    assert_eq!(&out[168..184], &101u128.to_be_bytes());
    assert_eq!(out[203], 1);
    assert_eq!(decode_ledger(&out[..LEDGER_BYTES])?, funded);
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
            AssetId::new([5; 32])?,
            reserve_account()?,
            reserve_account()?
        ),
        Err(F06_REFUND_RECIPIENT_MISMATCH)
    );
    Ok(())
}

fn dictionary_encoding() -> CodecResult<()> {
    let dict_bytes = dictionary(&[1, 2])?;
    let dict = RecipientDictionary::new(&dict_bytes)?;
    let mut out = vec![0u8; DICTIONARY_BYTES];
    assert_eq!(
        encode_dictionary(&dict, reserve_account()?, &mut out)?,
        17_152
    );
    assert_eq!(out[0], 1);
    assert_eq!(&out[65..67], &1u16.to_be_bytes());
    assert!(out[2 * SLOT_BYTES..].iter().all(|b| *b == 0));
    assert_eq!(decode_dictionary(&out, reserve_account()?)?, dict);
    assert_eq!(dict.occupied(), 2);
    let mut bad = out.clone();
    bad[2 * SLOT_BYTES + 5] = 1;
    assert_eq!(
        decode_dictionary(&bad, reserve_account()?),
        Err(NON_CANONICAL)
    );
    let mut bad = out.clone();
    bad[0] = 2;
    assert_eq!(
        decode_dictionary(&bad, reserve_account()?),
        Err(NON_CANONICAL)
    );
    let mut bad = out.clone();
    bad[65..67].copy_from_slice(&1057u16.to_be_bytes());
    assert_eq!(
        decode_dictionary(&bad, reserve_account()?),
        Err(NON_CANONICAL)
    );
    let mut dup_bytes = dict_bytes.clone();
    dup_bytes.copy_within(..SLOT_BYTES, 5 * SLOT_BYTES);
    let dup = RecipientDictionary::new(&dup_bytes)?;
    assert_eq!(
        encode_dictionary(&dup, reserve_account()?, &mut out),
        Err(NON_CANONICAL)
    );
    assert_eq!(
        decode_dictionary(&out[..DICTIONARY_BYTES - 1], reserve_account()?),
        Err(NON_CANONICAL)
    );
    assert_eq!(decode_dictionary(&out, recipient(1)?), Err(ACCOUNT_BINDING));
    let row = RewardEpoch::reserved(7, 5, binding()?.roster, &[1, 0])?;
    assert_eq!(
        row.check_dictionary(&dict),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
    let row = RewardEpoch::reserved(7, 5, binding()?.roster, &[0, 3])?;
    assert_eq!(
        row.check_dictionary(&dict),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
    Ok(())
}

fn epoch_encoding() -> CodecResult<()> {
    let row = RewardEpoch::reserved(7, 101, binding()?.roster, &[0, 1, 2])?;
    let bytes = roundtrip_epoch(&row)?;
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
        RewardEpoch::reserved(7, 0, binding()?.roster, &[0]),
        Err(F06_INVALID_AMOUNT)
    );
    assert_eq!(
        RewardEpoch::reserved(7, 1, binding()?.roster, &[]),
        Err(NON_CANONICAL)
    );
    assert_eq!(
        row.check_claim(
            &RecipientDictionary::new(&dictionary(&[1, 2, 3])?)?,
            wid(1)?,
            recipient(1)?,
            0,
            10
        ),
        Err(F06_CLAIM_NOT_READY)
    );
    Ok(())
}

#[test]
fn reward_math_and_encoding() {
    assert_eq!(a01_exact_rounding(), Ok(()));
    assert_eq!(a02_ties(), Ok(()));
    assert_eq!(a03_lower_median(), Ok(()));
    assert_eq!(a04_empty_zero(), Ok(()));
    assert_eq!(a05_maximal_amount(), Ok(()));
    assert_eq!(a15_overflow_corruption(), Ok(()));
    assert_eq!(ledger_encoding(), Ok(()));
    assert_eq!(dictionary_encoding(), Ok(()));
    assert_eq!(epoch_encoding(), Ok(()));
}

// AI.F06-A06/A07/A10/A11/A13/A14/A16/A19 (with A02/A04/A15 refusals) over the
// complete RewardState transitions: Fund, ReserveEpoch, TerminalizeRewards,
// DeclinePendingEpoch, Claim, ExpireEpochClaims, RefundFree and PruneEpoch.
/// Owned bytes of one committed reward state section. Each method runs the
/// real borrowed-view transition into a fresh section buffer.
#[derive(Clone, Debug, Eq, PartialEq)]
struct State(Vec<u8>);
type Step = CodecResult<(State, RewardEffect)>;
impl State {
    fn new(ledger: &RewardLedger) -> CodecResult<Self> {
        let mut bytes = vec![0xAA; REWARD_STATE_BYTES];
        RewardState::init(ledger, &mut bytes)?;
        Ok(Self(bytes))
    }
    fn view(&self) -> CodecResult<RewardState<'_>> {
        decode_reward_state(&self.0)
    }
    fn apply(
        &self,
        transition: impl for<'b> FnOnce(
            &RewardState<'_>,
            &'b mut [u8],
        ) -> CodecResult<(RewardState<'b>, RewardEffect)>,
    ) -> Step {
        let mut next = vec![0u8; REWARD_STATE_BYTES];
        let effect = transition(&self.view()?, &mut next)?.1;
        Ok((Self(next), effect))
    }
    fn ledger(&self) -> CodecResult<RewardLedger> {
        self.view()?.ledger()
    }
    fn row(&self, epoch: u64) -> CodecResult<RewardEpoch> {
        self.view()?.row(epoch)
    }
    fn rows(&self) -> CodecResult<usize> {
        Ok(self.view()?.rows().used())
    }
    fn occupied(&self) -> CodecResult<usize> {
        Ok(self.view()?.dictionary().occupied())
    }
    fn slot(&self, index: u16) -> CodecResult<RecipientSlot> {
        self.view()?.dictionary().slot(index)
    }
    fn last_refund(&self) -> CodecResult<Option<LastRefund>> {
        self.view()?.last_refund()
    }
    fn fund(
        &self,
        authority: &FundingAuthority,
        phase: FundingPhase,
        payload: &FundRequest,
        replay: &mut FundReplay<'_>,
    ) -> Step {
        self.apply(|s, next| s.fund(authority, phase, payload, replay, next))
    }
    fn reserve_epoch(
        &self,
        epoch: u64,
        budget: Amount,
        roster_digest: RosterDigest,
        roster: &[WorkerRosterEntry],
        opening_height: u64,
    ) -> Step {
        self.apply(|s, next| {
            s.reserve_epoch(epoch, budget, roster_digest, roster, opening_height, next)
        })
    }
    fn terminalize(
        &self,
        binding: &FrozenBinding,
        aggregation: Digest32,
        allocation: &Allocation,
        roster: &[WorkerRosterEntry],
        height: u64,
    ) -> Step {
        self.apply(|s, next| s.terminalize(binding, aggregation, allocation, roster, height, next))
    }
    fn decline_pending_epoch(&self, epoch: u64) -> Step {
        self.apply(|s, next| s.decline_pending_epoch(epoch, next))
    }
    fn claim(&self, epoch: u64, request: &ClaimRequest, height: u64) -> Step {
        self.apply(|s, next| s.claim(epoch, request, height, next))
    }
    fn expire_epoch_claims(&self, epoch: u64, height: u64) -> Step {
        self.apply(|s, next| s.expire_epoch_claims(epoch, height, next))
    }
    fn refund_free(
        &self,
        phase: FundingPhase,
        request: &RefundRequest,
        request_digest: RequestDigest,
        result: ResultDigest,
    ) -> Step {
        self.apply(|s, next| s.refund_free(phase, request, request_digest, result, next))
    }
    fn prune_epoch(&self, epoch: u64) -> Step {
        self.apply(|s, next| s.prune_epoch(epoch, next))
    }
}
fn owner() -> CodecResult<PrincipalId> {
    PrincipalId::new([60; 32])
}
fn treasury() -> CodecResult<PrincipalId> {
    PrincipalId::new([61; 32])
}
fn refund_to() -> CodecResult<AccountId> {
    recipient(99)
}
fn epoch_binding(epoch: u64) -> CodecResult<FrozenBinding> {
    Ok(FrozenBinding {
        epoch,
        ..binding()?
    })
}
fn wide_worker(i: u16) -> CodecResult<WorkerId> {
    let mut bytes = [7u8; 32];
    bytes[..2].copy_from_slice(&i.to_be_bytes());
    WorkerId::new(bytes)
}
fn wide_recipient(i: u16) -> CodecResult<AccountId> {
    let mut bytes = [9u8; 32];
    bytes[..2].copy_from_slice(&i.to_be_bytes());
    AccountId::new(bytes)
}
fn wide_entry(i: u16) -> CodecResult<WorkerRosterEntry> {
    Ok(WorkerRosterEntry {
        worker: wide_worker(i)?,
        recipient: wide_recipient(i)?,
        ..roster_entry(1)?
    })
}
fn positive(worker: WorkerId, score: u32) -> CodecResult<WorkerAggregate> {
    WorkerAggregate::new(
        worker,
        version()?,
        3,
        QualityStatus::ScoredPositive,
        score,
        score,
    )
}
fn ineligible(worker: WorkerId) -> CodecResult<WorkerAggregate> {
    WorkerAggregate::new(
        worker,
        version()?,
        2,
        QualityStatus::InsufficientQuorum,
        0,
        0,
    )
}
fn replay_table(with_treasury: bool) -> CodecResult<ReplayTable> {
    let mut table = ReplayTable::new();
    table.bind(ActorSlot::OWNER, owner()?, version()?)?;
    if with_treasury {
        table.bind(ActorSlot::TREASURY, treasury()?, version()?)?;
    }
    Ok(table)
}
fn fund_request(
    slot: ActorSlot,
    principal: PrincipalId,
    sequence: u64,
    payload: &FundRequest,
) -> CodecResult<ReplayRequest> {
    let mut bytes = [0u8; FUND_PAYLOAD_BYTES];
    payload.encode(&mut bytes)?;
    let digest = domain_hash("PAXAI/test-fund/v1", &bytes)?;
    Ok(ReplayRequest {
        slot,
        principal,
        authority_version: version()?,
        sequence,
        request_id: RequestId::new([u8::try_from(sequence).map_err(|_| ARITHMETIC)?; 32])?,
        digest: RequestDigest::new(digest.bytes())?,
        expiry_height: 10_000,
    })
}
fn consented(amount: Amount) -> CodecResult<FundRequest> {
    Ok(FundRequest {
        amount,
        refund_recipient: refund_to()?,
        policy_version: FUNDING_POLICY_VERSION,
        consent: true,
    })
}
/// Owner funds through the real Fund transition with a fresh role sequence.
fn owner_fund(
    state: &State,
    table: &mut ReplayTable,
    revision: &mut u64,
    sequence: u64,
    amount: Amount,
) -> CodecResult<State> {
    let authority = FundingAuthority {
        owner: owner()?,
        treasury: Presence::Absent,
    };
    let payload = consented(amount)?;
    let request = fund_request(ActorSlot::OWNER, owner()?, sequence, &payload)?;
    let (next, effect) = state.fund(
        &authority,
        FundingPhase::Accepting,
        &payload,
        &mut FundReplay {
            table,
            request: &request,
            height: 1,
            revision,
            result: ResultDigest::new([77; 32])?,
        },
    )?;
    assert_eq!(
        effect,
        RewardEffect::Deposit {
            principal: owner()?,
            amount
        }
    );
    Ok(next)
}
fn funded_state(amount: Amount) -> CodecResult<State> {
    let mut table = replay_table(false)?;
    let mut revision = 1;
    owner_fund(
        &State::new(&ledger()?)?,
        &mut table,
        &mut revision,
        1,
        amount,
    )
}
fn a01_roster() -> CodecResult<[WorkerRosterEntry; 3]> {
    Ok([roster_entry(1)?, roster_entry(2)?, roster_entry(3)?])
}
fn a01_outputs() -> CodecResult<[WorkerAggregate; 3]> {
    Ok([
        aggregate(1, &[4, 4, 4])?,
        aggregate(2, &[2, 2, 2])?,
        aggregate(3, &[1, 1, 1])?,
    ])
}
/// Reserve epoch 7 for B=101 and terminalize the A01 result at `height`.
fn a01_terminal(funded: Amount, height: u64) -> CodecResult<(State, Digest32, Digest32)> {
    let roster = a01_roster()?;
    let outputs = a01_outputs()?;
    let state = funded_state(funded)?;
    let (reserved, effect) = state.reserve_epoch(7, 101, binding()?.roster, &roster, 0)?;
    assert_eq!(effect, RewardEffect::NoTransfer);
    let allocation = allocate(101, &outputs)?;
    let aggregation = EpochAggregation::structural(binding()?, digest(9)?, &roster, &outputs)?;
    let (terminal, effect) = reserved.terminalize(
        &binding()?,
        aggregation.root(),
        &allocation,
        &roster,
        height,
    )?;
    assert_eq!(effect, RewardEffect::NoTransfer);
    let root = allocation_digest(
        &binding()?,
        aggregation.root(),
        ledger()?.asset,
        &allocation,
        &roster,
    )?;
    Ok((terminal, aggregation.root(), root))
}
fn counters(state: &State) -> CodecResult<[Amount; 6]> {
    let l = state.ledger()?;
    Ok([
        l.tracked_deposits,
        l.total_claimed,
        l.tracked_refunds,
        l.free,
        l.reserved,
        l.liability,
    ])
}
fn claim_of(n: u8, amount: Amount) -> CodecResult<ClaimRequest> {
    Ok(ClaimRequest {
        worker: wid(n)?,
        recipient: recipient(n)?,
        amount,
    })
}
fn refund_of(expected: Amount, amount: Amount) -> CodecResult<RefundRequest> {
    Ok(RefundRequest {
        expected_refunded: expected,
        amount,
        recipient: refund_to()?,
    })
}
fn refund(state: &State, expected: Amount, amount: Amount) -> Step {
    let mut bytes = [0u8; REFUND_PAYLOAD_BYTES];
    let request = refund_of(expected, amount)?;
    request.encode(&mut bytes)?;
    let digest = domain_hash("PAXAI/test-refund/v1", &bytes)?;
    let result = domain_hash("PAXAI/test-refund-result/v1", &bytes)?;
    state.refund_free(
        FundingPhase::Closing,
        &request,
        RequestDigest::new(digest.bytes())?,
        ResultDigest::new(result.bytes())?,
    )
}

fn a06_conservation_lifecycle() -> CodecResult<()> {
    let state = funded_state(120)?;
    assert_eq!(counters(&state)?, [120, 0, 0, 120, 0, 0]);
    let (reserved, _) = state.reserve_epoch(7, 101, binding()?.roster, &a01_roster()?, 0)?;
    assert_eq!(counters(&reserved)?, [120, 0, 0, 19, 101, 0]);
    assert_eq!(reserved.ledger()?.recipient_count, 3);
    let row = reserved.row(7)?;
    assert_eq!(row.status, EpochStatus::Reserved);
    assert_eq!(
        row.entries().iter().map(|e| e.slot).collect::<Vec<_>>(),
        vec![0, 1, 2]
    );

    let (terminal, aggregation, root) = a01_terminal(120, 900)?;
    assert_eq!(counters(&terminal)?, [120, 0, 0, 19, 0, 101]);
    let row = terminal.row(7)?;
    assert_eq!(
        (
            row.status,
            row.outcome,
            row.terminal_height,
            row.expiry_height
        ),
        (EpochStatus::Terminal, RewardOutcome::Allocated, 900, 4996)
    );
    assert_eq!(row.aggregation, Presence::Present(aggregation));
    assert_eq!(row.allocation, Presence::Present(root));
    assert_eq!(
        row.entries()
            .iter()
            .map(|e| e.entitlement)
            .collect::<Vec<_>>(),
        vec![58, 29, 14]
    );

    let (paid, effect) = terminal.claim(7, &claim_of(1, 58)?, 1000)?;
    assert_eq!(
        effect,
        RewardEffect::Payout {
            recipient: recipient(1)?,
            amount: 58
        }
    );
    assert_eq!(counters(&paid)?, [120, 58, 0, 19, 0, 43]);
    assert_eq!(paid.row(7)?.paid_sum, 58);
    let (again, effect) = paid.claim(7, &claim_of(1, 58)?, 1001)?;
    assert_eq!(
        effect,
        RewardEffect::AlreadyApplied(entitlement_id(root, wid(1)?)?)
    );
    assert_eq!(again, paid);

    let (expired, effect) = paid.expire_epoch_claims(7, 4996)?;
    assert_eq!(effect, RewardEffect::Released(43));
    assert_eq!(counters(&expired)?, [120, 58, 0, 62, 0, 0]);
    let row = expired.row(7)?;
    assert_eq!((row.status, row.expired_sum), (EpochStatus::Expired, 43));
    assert_eq!(
        row.entries()
            .iter()
            .map(|e| e.disposition)
            .collect::<Vec<_>>(),
        vec![
            Disposition::Claimed,
            Disposition::Expired,
            Disposition::Expired
        ]
    );
    let (twice, effect) = expired.expire_epoch_claims(7, 5000)?;
    assert_eq!(effect, RewardEffect::AlreadyApplied(root));
    assert_eq!(twice, expired);

    let (closed, effect) = refund(&expired, 0, 62)?;
    assert_eq!(
        effect,
        RewardEffect::Payout {
            recipient: refund_to()?,
            amount: 62
        }
    );
    assert_eq!(counters(&closed)?, [120, 58, 62, 0, 0, 0]);
    let l = closed.ledger()?;
    assert_eq!(l.tracked_deposits, l.total_claimed + l.tracked_refunds);
    Ok(())
}

fn a07_insufficient_free() -> CodecResult<()> {
    let state = funded_state(10)?;
    assert_eq!(
        state.reserve_epoch(7, 11, binding()?.roster, &a01_roster()?, 0),
        Err(INSUFFICIENT_FREE)
    );
    assert_eq!(
        state.reserve_epoch(7, 0, binding()?.roster, &a01_roster()?, 0),
        Err(F06_INVALID_AMOUNT)
    );
    assert_eq!(counters(&state)?, [10, 0, 0, 10, 0, 0]);
    assert_eq!(state.rows()?, 0);
    assert_eq!(state.occupied()?, 0);
    Ok(())
}

fn a10_rotation_and_authority() -> CodecResult<()> {
    let (terminal, _, _) = a01_terminal(120, 900)?;
    let live = AccountId::new([200; 32])?;
    let rotated = ClaimRequest {
        recipient: live,
        ..claim_of(1, 58)?
    };
    assert_eq!(
        terminal.claim(7, &rotated, 1000),
        Err(F06_WRONG_CLAIM_RECIPIENT)
    );
    assert_eq!(
        terminal.claim(7, &claim_of(1, 57)?, 1000),
        Err(F06_WRONG_CLAIM_AMOUNT)
    );
    let (_, effect) = terminal.claim(7, &claim_of(1, 58)?, 1000)?;
    assert_eq!(
        effect,
        RewardEffect::Payout {
            recipient: recipient(1)?,
            amount: 58
        }
    );
    assert_eq!(
        terminal.claim(7, &claim_of(3, 0)?, 1000),
        Err(F06_WRONG_CLAIM_AMOUNT)
    );
    assert_eq!(
        terminal.claim(7, &claim_of(4, 1)?, 1000),
        Err(F06_UNKNOWN_WORKER_ENTITLEMENT)
    );
    assert_eq!(terminal.claim(8, &claim_of(1, 58)?, 1000), Err(NOT_FOUND));

    let mut table = replay_table(true)?;
    let mut revision = 1;
    let authority = FundingAuthority {
        owner: owner()?,
        treasury: Presence::Present(treasury()?),
    };
    let proposed = PrincipalId::new([62; 32])?;
    let payload = consented(5)?;
    for request in [
        fund_request(ActorSlot::TREASURY, proposed, 1, &payload)?,
        fund_request(ActorSlot::OWNER, proposed, 1, &payload)?,
        fund_request(ActorSlot::OPERATOR, treasury()?, 1, &payload)?,
    ] {
        assert_eq!(
            terminal.fund(
                &authority,
                FundingPhase::Accepting,
                &payload,
                &mut FundReplay {
                    table: &mut table,
                    request: &request,
                    height: 1,
                    revision: &mut revision,
                    result: ResultDigest::new([77; 32])?,
                },
            ),
            Err(UNAUTHORIZED)
        );
    }
    let v2 = FundRequest {
        policy_version: 2,
        ..payload
    };
    let request = fund_request(ActorSlot::OWNER, owner()?, 1, &v2)?;
    assert_eq!(
        terminal.fund(
            &authority,
            FundingPhase::Accepting,
            &v2,
            &mut FundReplay {
                table: &mut table,
                request: &request,
                height: 1,
                revision: &mut revision,
                result: ResultDigest::new([77; 32])?,
            },
        ),
        Err(F06_FUNDING_POLICY_MISMATCH)
    );
    assert_eq!(table, replay_table(true)?);
    assert_eq!(revision, 1);
    Ok(())
}

fn a11_expiry_edge() -> CodecResult<()> {
    let (terminal, _, _) = a01_terminal(120, 900)?;
    let (paid, _) = terminal.claim(7, &claim_of(2, 29)?, 4995)?;
    assert_eq!(paid.ledger()?.liability, 72);
    assert_eq!(
        paid.claim(7, &claim_of(3, 14)?, 4996),
        Err(F06_CLAIM_EXPIRED)
    );
    assert_eq!(paid.expire_epoch_claims(7, 4995), Err(WRONG_PHASE));
    let (expired, effect) = paid.expire_epoch_claims(7, 4996)?;
    assert_eq!(effect, RewardEffect::Released(72));
    assert_eq!(counters(&expired)?, [120, 29, 0, 91, 0, 0]);
    assert_eq!(
        expired.claim(7, &claim_of(1, 58)?, 4000),
        Err(F06_CLAIM_EXPIRED)
    );
    let (twice, effect) = expired.expire_epoch_claims(7, 4997)?;
    assert!(matches!(effect, RewardEffect::AlreadyApplied(_)));
    assert_eq!(counters(&twice)?, counters(&expired)?);
    Ok(())
}

fn a13_late_settlement_and_close() -> CodecResult<()> {
    let roster = a01_roster()?;
    let outputs = a01_outputs()?;
    let (reserved, _) = funded_state(120)?.reserve_epoch(7, 101, binding()?.roster, &roster, 0)?;
    assert_eq!(
        reserved.reserve_epoch(8, 1, binding()?.roster, &roster, 200),
        Err(F06_EPOCH_ALREADY_RESERVED)
    );
    assert_eq!(reserved.decline_pending_epoch(7), Err(WRONG_PHASE));
    let (declined, effect) = reserved.decline_pending_epoch(8)?;
    assert_eq!(
        (declined, effect),
        (reserved.clone(), RewardEffect::NoTransfer)
    );
    assert_eq!(refund(&reserved, 0, 20), Err(INSUFFICIENT_FREE));
    let (closing, _) = refund(&reserved, 0, 19)?;
    assert_eq!(counters(&closing)?, [120, 0, 19, 0, 101, 0]);
    assert_eq!(
        reserved.refund_free(
            FundingPhase::Accepting,
            &refund_of(0, 19)?,
            RequestDigest::new([1; 32])?,
            ResultDigest::new([2; 32])?,
        ),
        Err(WRONG_PHASE)
    );

    let allocation = allocate(101, &outputs)?;
    let aggregation = EpochAggregation::structural(binding()?, digest(9)?, &roster, &outputs)?;
    assert_eq!(
        closing.terminalize(
            &epoch_binding(8)?,
            aggregation.root(),
            &allocation,
            &roster,
            5000
        ),
        Err(NOT_FOUND)
    );
    let other = FrozenBinding {
        roster: RosterDigest::new([44; 32])?,
        ..binding()?
    };
    assert_eq!(
        closing.terminalize(&other, aggregation.root(), &allocation, &roster, 5000),
        Err(WRONG_ROSTER)
    );
    assert_eq!(
        closing.terminalize(
            &binding()?,
            aggregation.root(),
            &allocate(100, &outputs)?,
            &roster,
            5000
        ),
        Err(F06_AGGREGATION_MISMATCH)
    );
    let (late, _) =
        closing.terminalize(&binding()?, aggregation.root(), &allocation, &roster, 5000)?;
    assert_eq!(counters(&late)?, [120, 0, 19, 0, 0, 101]);
    assert_eq!(late.row(7)?.expiry_height, 9096);
    assert_eq!(
        late.terminalize(&binding()?, aggregation.root(), &allocation, &roster, 5001),
        Err(F06_EPOCH_TERMINAL)
    );
    assert_eq!(refund(&late, 19, 1), Err(INSUFFICIENT_FREE));
    Ok(())
}

/// Fund-free epoch runner: reserve `epoch` for one positive unit per worker
/// and terminalize it at `height`.
fn run_epoch(
    state: &State,
    epoch: u64,
    roster: &[WorkerRosterEntry],
    height: u64,
) -> CodecResult<State> {
    let budget = Amount::try_from(roster.len()).map_err(|_| ARITHMETIC)?;
    let binding = epoch_binding(epoch)?;
    let (reserved, _) = state.reserve_epoch(epoch, budget, binding.roster, roster, height)?;
    let mut outputs = Vec::new();
    for entry in roster {
        outputs.push(positive(entry.worker, 1)?);
    }
    let allocation = allocate(budget, &outputs)?;
    Ok(reserved
        .terminalize(&binding, digest(9)?, &allocation, roster, height + 128)?
        .0)
}

fn a14_retention_and_dictionary() -> CodecResult<()> {
    let mut state = funded_state(10_000)?;
    for e in 0..8u16 {
        let mut roster = Vec::new();
        for k in 0..32u16 {
            roster.push(wide_entry(e * 32 + k)?);
        }
        state = run_epoch(&state, u64::from(e) + 1, &roster, u64::from(e) * 200)?;
    }
    assert_eq!(state.occupied()?, 256);
    assert_eq!(state.ledger()?.recipient_count, 256);
    let fresh = [wide_entry(300)?];
    assert_eq!(
        state.reserve_epoch(9, 1, binding()?.roster, &fresh, 2000),
        Err(CAPACITY)
    );
    assert_eq!(counters(&state)?, [10_000, 0, 0, 9_744, 0, 256]);
    let reused = [wide_entry(5)?];
    let (reserved, _) = state.reserve_epoch(9, 1, binding()?.roster, &reused, 2000)?;
    assert_eq!(reserved.slot(5)?.references, 2);
    assert_eq!(reserved.row(9)?.entries()[0].slot, 5);

    let single = [roster_entry(1)?];
    let mut ring = funded_state(100)?;
    for epoch in 1..=32u64 {
        ring = run_epoch(&ring, epoch, &single, epoch * 200)?;
    }
    assert_eq!(ring.ledger()?.retained_epochs, 32);
    assert_eq!(ring.slot(0)?.references, 32);
    assert_eq!(
        ring.reserve_epoch(33, 1, binding()?.roster, &single, 10_000),
        Err(RETENTION_FULL)
    );
    assert_eq!(ring.prune_epoch(1), Err(WRONG_PHASE));
    assert_eq!(ring.prune_epoch(2), Err(WRONG_PHASE));
    assert_eq!(ring.prune_epoch(99), Err(NOT_FOUND));
    let (paid, _) = ring.claim(1, &claim_of(1, 1)?, 1000)?;
    let oldest = paid.row(1)?.allocation;
    let (opened, effect) = paid.reserve_epoch(33, 1, binding()?.roster, &single, 10_000)?;
    assert_eq!(effect, RewardEffect::Pruned(oldest));
    assert_eq!(opened.ledger()?.retained_epochs, 31);
    assert_eq!(opened.rows()?, 32);
    assert_eq!(opened.claim(1, &claim_of(1, 1)?, 1000), Err(NOT_FOUND));
    assert_eq!(opened.slot(0)?.references, 32);

    let (claimed, _) = paid.claim(2, &claim_of(1, 1)?, 1000)?;
    let (pruned, effect) = claimed.prune_epoch(1)?;
    assert_eq!(effect, RewardEffect::Pruned(oldest));
    assert_eq!(counters(&pruned)?, counters(&claimed)?);
    assert_eq!(pruned.ledger()?.retained_epochs, 31);
    let (pruned, _) = pruned.prune_epoch(2)?;
    assert_eq!(pruned.slot(0)?.references, 30);
    Ok(())
}

fn a16_consent_refusals() -> CodecResult<()> {
    let base = State::new(&ledger()?)?;
    let authority = FundingAuthority {
        owner: owner()?,
        treasury: Presence::Present(treasury()?),
    };
    let mut table = replay_table(true)?;
    let mut revision = 1;
    let result = ResultDigest::new([77; 32])?;
    let refusals = [
        (
            FundRequest {
                refund_recipient: recipient(1)?,
                ..consented(5)?
            },
            F06_REFUND_RECIPIENT_MISMATCH,
        ),
        (
            FundRequest {
                policy_version: 0,
                ..consented(5)?
            },
            F06_FUNDING_POLICY_MISMATCH,
        ),
        (
            FundRequest {
                policy_version: 2,
                ..consented(5)?
            },
            F06_FUNDING_POLICY_MISMATCH,
        ),
        (
            FundRequest {
                consent: false,
                ..consented(5)?
            },
            F06_CONTRIBUTION_CONSENT_REQUIRED,
        ),
        (consented(0)?, F06_INVALID_AMOUNT),
    ];
    for (payload, error) in refusals {
        let request = fund_request(ActorSlot::TREASURY, treasury()?, 1, &payload)?;
        assert_eq!(
            base.fund(
                &authority,
                FundingPhase::Accepting,
                &payload,
                &mut FundReplay {
                    table: &mut table,
                    request: &request,
                    height: 1,
                    revision: &mut revision,
                    result,
                },
            ),
            Err(error)
        );
    }
    assert_eq!((table.clone(), revision), (replay_table(true)?, 1));

    let payload = consented(5)?;
    let request = fund_request(ActorSlot::TREASURY, treasury()?, 1, &payload)?;
    assert_eq!(
        base.fund(
            &authority,
            FundingPhase::Closing,
            &payload,
            &mut FundReplay {
                table: &mut table,
                request: &request,
                height: 1,
                revision: &mut revision,
                result,
            },
        ),
        Err(WRONG_PHASE)
    );
    Ok(())
}

fn a16_funding_consent() -> CodecResult<()> {
    let base = State::new(&ledger()?)?;
    let authority = FundingAuthority {
        owner: owner()?,
        treasury: Presence::Present(treasury()?),
    };
    let mut table = replay_table(true)?;
    let mut revision = 1;
    let result = ResultDigest::new([77; 32])?;
    let payload = consented(5)?;
    let request = fund_request(ActorSlot::TREASURY, treasury()?, 1, &payload)?;
    let (funded, effect) = base.fund(
        &authority,
        FundingPhase::Accepting,
        &payload,
        &mut FundReplay {
            table: &mut table,
            request: &request,
            height: 1,
            revision: &mut revision,
            result,
        },
    )?;
    assert_eq!(
        effect,
        RewardEffect::Deposit {
            principal: treasury()?,
            amount: 5
        }
    );
    assert_eq!(counters(&funded)?, [5, 0, 0, 5, 0, 0]);
    assert_eq!(revision, 2);
    let (replayed, effect) = funded.fund(
        &authority,
        FundingPhase::Accepting,
        &payload,
        &mut FundReplay {
            table: &mut table,
            request: &request,
            height: 2,
            revision: &mut revision,
            result,
        },
    )?;
    assert_eq!(effect, RewardEffect::ReplayedFund(result));
    assert_eq!((replayed, revision), (funded.clone(), 2));
    let altered = consented(6)?;
    let conflict = ReplayRequest {
        digest: fund_request(ActorSlot::TREASURY, treasury()?, 1, &altered)?.digest,
        ..request
    };
    assert_eq!(
        funded.fund(
            &authority,
            FundingPhase::Accepting,
            &altered,
            &mut FundReplay {
                table: &mut table,
                request: &conflict,
                height: 2,
                revision: &mut revision,
                result,
            },
        ),
        Err(REPLAY_CONFLICT)
    );

    a16_principal_scope(&funded, &mut table, &mut revision, &payload, result)
}

fn a16_principal_scope(
    funded: &State,
    table: &mut ReplayTable,
    revision: &mut u64,
    payload: &FundRequest,
    result: ResultDigest,
) -> CodecResult<()> {
    let owner_only = FundingAuthority {
        owner: owner()?,
        treasury: Presence::Absent,
    };
    let request = fund_request(ActorSlot::TREASURY, treasury()?, 2, payload)?;
    assert_eq!(
        funded.fund(
            &owner_only,
            FundingPhase::Accepting,
            payload,
            &mut FundReplay {
                table,
                request: &request,
                height: 2,
                revision,
                result,
            },
        ),
        Err(UNAUTHORIZED)
    );
    let same = FundingAuthority {
        owner: owner()?,
        treasury: Presence::Present(owner()?),
    };
    let request = fund_request(ActorSlot::TREASURY, owner()?, 1, payload)?;
    assert_eq!(
        funded.fund(
            &same,
            FundingPhase::Accepting,
            payload,
            &mut FundReplay {
                table,
                request: &request,
                height: 2,
                revision,
                result,
            },
        ),
        Err(UNAUTHORIZED)
    );
    let owner_funded = owner_fund(funded, table, revision, 1, 7)?;
    assert_eq!(counters(&owner_funded)?, [12, 0, 0, 12, 0, 0]);
    assert_eq!(*revision, 3);

    let full = owner_fund(
        &State::new(&ledger()?)?,
        &mut replay_table(false)?,
        &mut 1,
        1,
        u128::MAX,
    )?;
    assert_eq!(
        owner_fund(&full, &mut replay_table(false)?, &mut 1, 1, 1),
        Err(ARITHMETIC)
    );

    Ok(())
}

fn fund_payload_codecs() -> CodecResult<()> {
    let payload = consented(5)?;
    let mut bytes = [0u8; FUND_PAYLOAD_BYTES + 1];
    assert_eq!(payload.encode(&mut bytes)?, FUND_PAYLOAD_BYTES);
    assert_eq!(&bytes[48..56], &1u64.to_be_bytes());
    assert_eq!(bytes[56], 1);
    assert_eq!(FundRequest::decode(&bytes[..FUND_PAYLOAD_BYTES])?, payload);
    assert_eq!(FundRequest::decode(&bytes), Err(NON_CANONICAL));
    bytes[56] = 2;
    assert_eq!(
        FundRequest::decode(&bytes[..FUND_PAYLOAD_BYTES]),
        Err(NON_CANONICAL)
    );
    let mut claim = [0u8; CLAIM_PAYLOAD_BYTES];
    assert_eq!(claim_of(1, 58)?.encode(&mut claim)?, CLAIM_PAYLOAD_BYTES);
    assert_eq!(ClaimRequest::decode(&claim)?, claim_of(1, 58)?);
    assert_eq!(ClaimRequest::decode(&claim[..79]), Err(NON_CANONICAL));
    let mut refund_bytes = [0u8; REFUND_PAYLOAD_BYTES];
    assert_eq!(
        refund_of(20, 42)?.encode(&mut refund_bytes)?,
        REFUND_PAYLOAD_BYTES
    );
    assert_eq!(RefundRequest::decode(&refund_bytes)?, refund_of(20, 42)?);
    Ok(())
}

fn a19_refund_cursor() -> CodecResult<()> {
    let (terminal, _, _) = a01_terminal(163, 900)?;
    let (state, _) = terminal.claim(7, &claim_of(1, 58)?, 1000)?;
    assert_eq!(counters(&state)?, [163, 58, 0, 62, 0, 43]);
    let wrong = RefundRequest {
        recipient: recipient(1)?,
        ..refund_of(0, 20)?
    };
    assert_eq!(
        state.refund_free(
            FundingPhase::Closing,
            &wrong,
            RequestDigest::new([1; 32])?,
            ResultDigest::new([2; 32])?,
        ),
        Err(F06_REFUND_RECIPIENT_MISMATCH)
    );
    assert_eq!(refund(&state, 0, 0), Err(F06_INVALID_AMOUNT));
    assert_eq!(refund(&state, 0, 63), Err(INSUFFICIENT_FREE));
    let (first, effect) = refund(&state, 0, 20)?;
    assert_eq!(
        effect,
        RewardEffect::Payout {
            recipient: refund_to()?,
            amount: 20
        }
    );
    assert_eq!(counters(&first)?, [163, 58, 20, 42, 0, 43]);
    let retained = first.last_refund()?;
    let (repeat, effect) = refund(&first, 0, 20)?;
    assert!(matches!(effect, RewardEffect::RepeatedRefund(_)));
    assert_eq!(repeat, first);
    assert_eq!(refund(&first, 0, 30), Err(STALE_CURSOR));
    assert_eq!(refund(&first, 40, 1), Err(STALE_CURSOR));
    let (second, effect) = refund(&first, 20, 42)?;
    assert_eq!(
        effect,
        RewardEffect::Payout {
            recipient: refund_to()?,
            amount: 42
        }
    );
    assert_eq!(counters(&second)?, [163, 58, 62, 0, 0, 43]);
    assert_ne!(second.last_refund()?, retained);

    let (expired, _) = second.expire_epoch_claims(7, 4996)?;
    assert_eq!(counters(&expired)?, [163, 58, 62, 43, 0, 0]);
    assert_eq!(refund(&expired, 0, 20), Err(STALE_CURSOR));
    let (old, effect) = refund(&expired, 20, 42)?;
    assert!(matches!(effect, RewardEffect::RepeatedRefund(_)));
    assert_eq!(old, expired);
    let (last, _) = refund(&expired, 62, 43)?;
    assert_eq!(counters(&last)?, [163, 58, 105, 0, 0, 0]);

    let mut record = [0u8; LAST_REFUND_BYTES + 1];
    assert_eq!(
        encode_last_refund(last.last_refund()?.as_ref(), &mut record)?,
        LAST_REFUND_BYTES + 1
    );
    assert_eq!(decode_last_refund(&record)?, last.last_refund()?);
    assert_eq!(
        encode_last_refund(None, &mut record)?,
        LAST_REFUND_BYTES + 1
    );
    assert_eq!(decode_last_refund(&record)?, None);
    Ok(())
}

fn a02_zero_entitlement() -> CodecResult<()> {
    let ties = [roster_entry(1)?, roster_entry(2)?, roster_entry(3)?];
    let (reserved, _) = funded_state(2)?.reserve_epoch(7, 2, binding()?.roster, &ties, 0)?;
    let outputs = [
        positive(wid(1)?, 1)?,
        positive(wid(2)?, 1)?,
        positive(wid(3)?, 1)?,
    ];
    let aggregation = EpochAggregation::structural(binding()?, digest(9)?, &ties, &outputs)?;
    let (terminal, _) = reserved.terminalize(
        &binding()?,
        aggregation.root(),
        &allocate(2, &outputs)?,
        &ties,
        900,
    )?;
    assert_eq!(
        terminal.claim(7, &claim_of(3, 0)?, 1000),
        Err(F06_NOTHING_TO_CLAIM)
    );
    let (expired, effect) = terminal.expire_epoch_claims(7, 4996)?;
    assert_eq!(effect, RewardEffect::Released(2));
    assert_eq!(
        expired.row(7)?.entries()[2].disposition,
        Disposition::Expired
    );
    Ok(())
}

fn a04_no_eligible_release() -> CodecResult<()> {
    let ties = [roster_entry(1)?, roster_entry(2)?, roster_entry(3)?];
    let (reserved, _) = funded_state(77)?.reserve_epoch(7, 77, binding()?.roster, &ties, 0)?;
    let none = [
        ineligible(wid(1)?)?,
        ineligible(wid(2)?)?,
        ineligible(wid(3)?)?,
    ];
    let allocation = allocate(77, &none)?;
    assert_eq!(allocation.outcome(), RewardOutcome::NoEligibleScore);
    let aggregation = EpochAggregation::structural(binding()?, digest(9)?, &ties, &none)?;
    let (released, _) =
        reserved.terminalize(&binding()?, aggregation.root(), &allocation, &ties, 900)?;
    assert_eq!(counters(&released)?, [77, 0, 0, 77, 0, 0]);
    let row = released.row(7)?;
    assert_eq!(
        (row.outcome, row.entry_count, row.expiry_height),
        (RewardOutcome::NoEligibleScore, 0, 0)
    );
    assert_eq!(released.occupied()?, 0);
    assert_eq!(released.ledger()?.recipient_count, 0);
    assert_eq!(released.expire_epoch_claims(7, 5000), Err(WRONG_PHASE));
    assert_eq!(
        released.claim(7, &claim_of(1, 1)?, 1000),
        Err(F06_UNKNOWN_WORKER_ENTITLEMENT)
    );
    let (pruned, effect) = released.prune_epoch(7)?;
    assert_eq!(effect, RewardEffect::Pruned(row.allocation));
    assert_eq!(pruned.rows()?, 0);
    Ok(())
}

fn a15_state_refusals() -> CodecResult<()> {
    let ties = [roster_entry(1)?, roster_entry(2)?, roster_entry(3)?];
    let state = funded_state(120)?;
    assert_eq!(
        state.reserve_epoch(7, 101, binding()?.roster, &ties, u64::MAX - 4096),
        Err(ARITHMETIC)
    );
    let unsorted = [roster_entry(2)?, roster_entry(1)?];
    assert_eq!(
        state.reserve_epoch(7, 101, binding()?.roster, &unsorted, 0),
        Err(WRONG_ROSTER)
    );
    let custodial = [WorkerRosterEntry {
        recipient: reserve_account()?,
        ..roster_entry(1)?
    }];
    assert_eq!(
        state.reserve_epoch(7, 101, binding()?.roster, &custodial, 0),
        Err(ACCOUNT_BINDING)
    );
    assert_eq!(
        state.reserve_epoch(7, 101, binding()?.roster, &[], 0),
        Err(WRONG_ROSTER)
    );

    let (reserved, _) = state.reserve_epoch(7, 101, binding()?.roster, &a01_roster()?, 0)?;
    let outputs = a01_outputs()?;
    let allocation = allocate(101, &outputs)?;
    let aggregation =
        EpochAggregation::structural(binding()?, digest(9)?, &a01_roster()?, &outputs)?;
    assert_eq!(
        reserved.terminalize(
            &binding()?,
            aggregation.root(),
            &allocation,
            &a01_roster()?,
            u64::MAX - 4095
        ),
        Err(ARITHMETIC)
    );
    assert_eq!(counters(&reserved)?, [120, 0, 0, 19, 101, 0]);
    let (terminal, _) = reserved.terminalize(
        &binding()?,
        aggregation.root(),
        &allocation,
        &a01_roster()?,
        900,
    )?;
    let mut forged = terminal.clone();
    let mut ledger = terminal.ledger()?;
    ledger.liability = 100;
    ledger.free = 20;
    encode_ledger(&ledger, &mut forged.0[..LEDGER_BYTES])?;
    assert_eq!(
        forged.claim(7, &claim_of(1, 58)?, 1000),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
    let mut miscounted = terminal.clone();
    let mut ledger = terminal.ledger()?;
    ledger.recipient_count = 2;
    encode_ledger(&ledger, &mut miscounted.0[..LEDGER_BYTES])?;
    assert_eq!(
        miscounted.claim(7, &claim_of(1, 58)?, 1000),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
    let mut dangling = terminal.clone();
    RecipientDictionary::write_slot(
        &mut dangling.0[LEDGER_BYTES..LEDGER_BYTES + DICTIONARY_BYTES],
        0,
        Some(&RecipientSlot {
            references: 2,
            ..terminal.slot(0)?
        }),
    )?;
    assert_eq!(
        dangling.claim(7, &claim_of(1, 58)?, 1000),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
    Ok(())
}

/// The whole state lives in one 43261-byte section written in place: ledger,
/// 256 dictionary slots, 33 fixed 782-byte rows and the refund record.
fn state_layout() -> CodecResult<()> {
    assert_eq!(EPOCH_ROWS_BYTES, 25_806);
    assert_eq!(REWARD_STATE_BYTES, 43_261);
    let rows_at = LEDGER_BYTES + DICTIONARY_BYTES;
    let refund_at = rows_at + EPOCH_ROWS_BYTES;
    let empty = State::new(&ledger()?)?;
    let mut header = [0u8; LEDGER_BYTES];
    encode_ledger(&ledger()?, &mut header)?;
    assert_eq!(&empty.0[..LEDGER_BYTES], &header);
    assert!(empty.0[LEDGER_BYTES..].iter().all(|b| *b == 0));
    assert_eq!(empty.view()?.bytes(), &empty.0[..]);
    assert_eq!(decode_reward_state(&empty.0[1..]), Err(NON_CANONICAL));
    assert_eq!(
        RewardState::init(&ledger()?, &mut [0u8; 16]),
        Err(NON_CANONICAL)
    );

    let (reserved, _) =
        funded_state(120)?.reserve_epoch(7, 101, binding()?.roster, &a01_roster()?, 0)?;
    let row = RewardEpoch::reserved(7, 101, binding()?.roster, &[0, 1, 2])?;
    let encoded = roundtrip_epoch(&row)?;
    assert_eq!(&reserved.0[rows_at..rows_at + encoded.len()], &encoded[..]);
    assert!(reserved.0[rows_at + encoded.len()..refund_at]
        .iter()
        .all(|b| *b == 0));
    let mut dict = [0u8; SLOT_BYTES];
    dict[0] = 1;
    dict[1..33].copy_from_slice(wid(1)?.as_bytes());
    dict[33..65].copy_from_slice(recipient(1)?.as_bytes());
    dict[65..67].copy_from_slice(&1u16.to_be_bytes());
    assert_eq!(&reserved.0[LEDGER_BYTES..LEDGER_BYTES + SLOT_BYTES], &dict);
    let rows = EpochRows::new(&reserved.0[rows_at..refund_at])?;
    assert_eq!(
        (rows.used(), rows.get(0)?, rows.get(1)?),
        (1, Some(row), None)
    );
    assert_eq!(rows.get(MAX_EPOCH_ROWS), Err(NOT_FOUND));
    assert_eq!(
        EpochRows::new(&reserved.0[rows_at..refund_at - 1]),
        Err(NON_CANONICAL)
    );

    let mut padded = reserved.clone();
    padded.0[rows_at + encoded.len()] = 1;
    assert_eq!(padded.view(), Err(NON_CANONICAL));
    let mut gap = reserved.clone();
    gap.0.copy_within(
        rows_at..rows_at + EPOCH_MAX_BYTES,
        rows_at + EPOCH_MAX_BYTES,
    );
    gap.0[rows_at..rows_at + EPOCH_MAX_BYTES].fill(0);
    assert_eq!(gap.view(), Err(F06_LEDGER_INVARIANT_VIOLATION));
    let mut short = vec![0u8; REWARD_STATE_BYTES - 1];
    assert_eq!(
        reserved.view()?.decline_pending_epoch(8, &mut short),
        Err(NON_CANONICAL)
    );
    Ok(())
}

#[test]
fn reward_state_transitions() {
    assert_eq!(state_layout(), Ok(()));
    assert_eq!(a06_conservation_lifecycle(), Ok(()));
    assert_eq!(a07_insufficient_free(), Ok(()));
    assert_eq!(a10_rotation_and_authority(), Ok(()));
    assert_eq!(a11_expiry_edge(), Ok(()));
    assert_eq!(a13_late_settlement_and_close(), Ok(()));
    assert_eq!(a14_retention_and_dictionary(), Ok(()));
    assert_eq!(a16_consent_refusals(), Ok(()));
    assert_eq!(a16_funding_consent(), Ok(()));
    assert_eq!(fund_payload_codecs(), Ok(()));
    assert_eq!(a19_refund_cursor(), Ok(()));
    assert_eq!(a02_zero_entitlement(), Ok(()));
    assert_eq!(a04_no_eligible_release(), Ok(()));
    assert_eq!(a15_state_refusals(), Ok(()));
}
