//! AI.CORE-A03 value boundary: the registered Program-derived rewards account,
//! the exact funding and `ProgramSpend` ceilings as the runtime decodes and
//! admits them, proof-bound balance sight, and conservation and rollback of
//! the staged value across the real F06 transitions.
use layerx_program_sdk::{
    payments::{PreparedProgramAccount, ProgramPaymentCapabilities},
    Capability as SdkCapability, CapabilitySet as SdkCapabilitySet, HostRefusal, ProgramError,
};
use layerx_programs_ai_market::{
    aggregation_codec::*,
    codec::domain_hash,
    errors::*,
    registry::derive_rewards_account,
    reward_math::*,
    rewards::*,
    state::{ActorSlot, ReplayRequest, ReplayTable},
    value_adapter::*,
    *,
};
use layerx_programs_runtime::{
    derive_program_account, Abi, AbiError, AuthorizationContext, Capability as RtCapability,
    CapabilitySet as RtCapabilitySet, PrincipalId as RtPrincipal, ProgramAccountError,
    ProgramId as RtProgram, Storage, StorageError, UnavailableReceiptOracle, ABI_V5_VERSION,
};

enum Failure {
    App(ApplicationError),
    Abi(AbiError),
    Sdk(ProgramError),
    Storage(StorageError),
    Account(ProgramAccountError),
}
impl core::fmt::Debug for Failure {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::App(error) => write!(f, "application refusal {error:?}"),
            Self::Abi(error) => write!(f, "runtime ABI refusal {error:?}"),
            Self::Sdk(error) => write!(f, "SDK refusal {error:?}"),
            Self::Storage(error) => write!(f, "runtime identifier refusal {error:?}"),
            Self::Account(error) => write!(f, "program account derivation {error:?}"),
        }
    }
}
impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self::App(error)
    }
}
impl From<AbiError> for Failure {
    fn from(error: AbiError) -> Self {
        Self::Abi(error)
    }
}
impl From<ProgramError> for Failure {
    fn from(error: ProgramError) -> Self {
        Self::Sdk(error)
    }
}
impl From<StorageError> for Failure {
    fn from(error: StorageError) -> Self {
        Self::Storage(error)
    }
}
impl From<ProgramAccountError> for Failure {
    fn from(error: ProgramAccountError) -> Self {
        Self::Account(error)
    }
}
type Checked = Result<(), Failure>;

const PROGRAM: [u8; 32] = [2; 32];
const OTHER_PROGRAM: [u8; 32] = [3; 32];
const ASSET: [u8; 32] = {
    let mut asset = [0; 32];
    asset[0] = 9;
    asset
};
const RECEIPT: [u8; 32] = [21; 32];
/// Runtime status of a granted sight whose account or asset is absent.
const RUNTIME_STATUS_ABSENT: i32 = -7;

fn program() -> CodecResult<ProgramId> {
    ProgramId::new(PROGRAM)
}
fn asset() -> CodecResult<AssetId> {
    AssetId::new(ASSET)
}
fn bound() -> CodecResult<RewardsAccount> {
    RewardsAccount::derive(program()?, asset()?)
}
fn refund_to() -> CodecResult<AccountId> {
    AccountId::new([14; 32])
}
fn ledger() -> CodecResult<RewardLedger> {
    RewardLedger::new(asset()?, bound()?.account(), refund_to()?)
}
fn version() -> CodecResult<Version> {
    Version::new(1)
}
fn wid(n: u8) -> CodecResult<WorkerId> {
    WorkerId::new([n; 32])
}
fn recipient(n: u8) -> CodecResult<AccountId> {
    AccountId::new([100 + n; 32])
}
fn owner() -> CodecResult<PrincipalId> {
    PrincipalId::new([60; 32])
}
fn actor() -> Result<RtPrincipal, Failure> {
    Ok(RtPrincipal::new(owner()?.bytes())?)
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
fn roster() -> CodecResult<[WorkerRosterEntry; 3]> {
    Ok([roster_entry(1)?, roster_entry(2)?, roster_entry(3)?])
}
fn positive(n: u8, score: u32) -> CodecResult<WorkerAggregate> {
    WorkerAggregate::new(
        wid(n)?,
        version()?,
        3,
        QualityStatus::ScoredPositive,
        score,
        score,
    )
}
fn binding() -> CodecResult<FrozenBinding> {
    Ok(FrozenBinding {
        chain: ChainDomain::new([1; 32])?,
        program: program()?,
        market: MarketId::new([3; 32])?,
        epoch: 7,
        config: version()?,
        roster: RosterDigest::new([4; 32])?,
    })
}

/// Owned bytes of one committed reward state section; each step runs the
/// real borrowed-view transition into a fresh next-state buffer.
#[derive(Clone, Debug, Eq, PartialEq)]
struct State(Vec<u8>);
type Step = CodecResult<(State, RewardEffect)>;
impl State {
    fn new(ledger: &RewardLedger) -> CodecResult<Self> {
        let mut bytes = vec![0xAA; REWARD_STATE_BYTES];
        RewardState::init(ledger, &mut bytes)?;
        Ok(Self(bytes))
    }
    fn apply(
        &self,
        transition: impl for<'b> FnOnce(
            &RewardState<'_>,
            &'b mut [u8],
        ) -> CodecResult<(RewardState<'b>, RewardEffect)>,
    ) -> Step {
        let mut next = vec![0u8; REWARD_STATE_BYTES];
        let effect = transition(&decode_reward_state(&self.0)?, &mut next)?.1;
        Ok((Self(next), effect))
    }
    fn ledger(&self) -> CodecResult<RewardLedger> {
        decode_reward_state(&self.0)?.ledger()
    }
    fn fund(
        &self,
        table: &mut ReplayTable,
        revision: &mut u64,
        sequence: u8,
        amount: Amount,
    ) -> Step {
        let payload = FundRequest {
            amount,
            refund_recipient: refund_to()?,
            policy_version: FUNDING_POLICY_VERSION,
            consent: true,
        };
        let mut bytes = [0u8; FUND_PAYLOAD_BYTES];
        payload.encode(&mut bytes)?;
        let request = ReplayRequest {
            slot: ActorSlot::OWNER,
            principal: owner()?,
            authority_version: version()?,
            sequence: u64::from(sequence),
            request_id: RequestId::new([sequence; 32])?,
            digest: RequestDigest::new(domain_hash("PAXAI/test-fund/v1", &bytes)?.bytes())?,
            expiry_height: 10_000,
        };
        let authority = FundingAuthority {
            owner: owner()?,
            treasury: Presence::Absent,
        };
        let mut replay = FundReplay {
            table,
            request: &request,
            height: 1,
            revision,
            result: ResultDigest::new([77; 32])?,
        };
        self.apply(|s, next| {
            s.fund(
                &authority,
                FundingPhase::Accepting,
                &payload,
                &mut replay,
                next,
            )
        })
    }
    fn reserve(&self) -> Step {
        let roster = roster()?;
        self.apply(|s, next| s.reserve_epoch(7, 101, binding()?.roster, &roster, 0, next))
    }
    fn terminalize(&self) -> Step {
        let roster = roster()?;
        let outputs = [positive(1, 4)?, positive(2, 2)?, positive(3, 1)?];
        let allocation = allocate(101, &outputs)?;
        let aggregation =
            EpochAggregation::structural(binding()?, Digest32::new([9; 32])?, &roster, &outputs)?;
        self.apply(|s, next| {
            s.terminalize(
                &binding()?,
                aggregation.root(),
                &allocation,
                &roster,
                900,
                next,
            )
        })
    }
    fn claim(&self, worker: u8, to: AccountId, amount: Amount) -> Step {
        let request = ClaimRequest {
            worker: wid(worker)?,
            recipient: to,
            amount,
        };
        self.apply(|s, next| s.claim(7, &request, 1000, next))
    }
    fn expire(&self) -> Step {
        self.apply(|s, next| s.expire_epoch_claims(7, 4996, next))
    }
    fn refund(&self, expected: Amount, amount: Amount) -> Step {
        let request = RefundRequest {
            expected_refunded: expected,
            amount,
            recipient: refund_to()?,
        };
        let mut bytes = [0u8; REFUND_PAYLOAD_BYTES];
        request.encode(&mut bytes)?;
        let digest = domain_hash("PAXAI/test-refund/v1", &bytes)?;
        let result = domain_hash("PAXAI/test-refund-result/v1", &bytes)?;
        self.apply(|s, next| {
            s.refund_free(
                FundingPhase::Closing,
                &request,
                RequestDigest::new(digest.bytes())?,
                ResultDigest::new(result.bytes())?,
                next,
            )
        })
    }
}

/// The registered account's physical balance across one committed transition:
/// plan the staged value, check cover against the observed sight balance and
/// carry the exact staged amount into the next observation.
fn settle(
    bound: &RewardsAccount,
    prev: &State,
    next: &State,
    effect: RewardEffect,
    balance: &mut Amount,
) -> CodecResult<Amount> {
    let next_ledger = next.ledger()?;
    let action = plan(bound, &prev.ledger()?, &next_ledger, effect)?;
    let surplus = cover(&next_ledger, *balance, action)?;
    *balance = match action {
        ValueAction::None => *balance,
        ValueAction::Fund { amount } => balance.checked_add(amount).ok_or(ARITHMETIC)?,
        ValueAction::Pay { amount, .. } => balance.checked_sub(amount).ok_or(ARITHMETIC)?,
    };
    let obligations = next_ledger.free + next_ledger.reserved + next_ledger.liability;
    assert_eq!(surplus, *balance - obligations);
    Ok(surplus)
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

#[test]
fn rewards_account_is_the_runtime_derived_registered_binding() -> Checked {
    let bound = bound()?;
    let runtime = derive_program_account(RtProgram::new(PROGRAM)?, REWARDS_ACCOUNT_SEED)?;
    assert_eq!(bound.account().bytes(), runtime.bytes());
    assert_eq!(
        bound.account(),
        derive_rewards_account(program()?, asset()?)?
    );
    assert_eq!((bound.program(), bound.asset()), (program()?, asset()?));

    let payload = bound.registration_payload()?;
    let mut expected = Vec::new();
    expected.extend_from_slice(&PROGRAM);
    expected.extend_from_slice(b"LXPA1");
    expected.extend_from_slice(&ASSET);
    expected.extend_from_slice(&16u32.to_be_bytes());
    expected.extend_from_slice(b"paxai/rewards/v1");
    assert_eq!(payload.as_slice(), expected.as_slice());
    assert_eq!(payload.len(), REGISTRATION_PAYLOAD_BYTES);

    let ledger = ledger()?;
    assert_eq!(RewardsAccount::for_ledger(program()?, &ledger)?, bound);
    assert_eq!(
        RewardsAccount::for_ledger(ProgramId::new(OTHER_PROGRAM)?, &ledger),
        Err(ACCOUNT_BINDING)
    );
    let other_asset = RewardLedger::new(AssetId::new([10; 32])?, bound.account(), refund_to()?)?;
    assert_eq!(bound.check(&other_asset), Err(F06_WRONG_ASSET));
    let other_account = RewardLedger::new(asset()?, AccountId::new([250; 32])?, refund_to()?)?;
    assert_eq!(bound.check(&other_account), Err(ACCOUNT_BINDING));
    Ok(())
}

#[test]
fn funding_grant_is_one_exact_transfer402_ceiling() -> Checked {
    let bound = bound()?;
    let set = SdkCapabilitySet::<4>::from_grants(&[
        SdkCapability::SharedStorageRead,
        SdkCapability::SharedStorageWrite,
        SdkCapability::EmitEvent,
        bound.funding_grant(120)?,
    ])?;
    let mut encoded = [0u8; 128];
    let n = set.encode_into(&mut encoded)?;
    let decoded = RtCapabilitySet::decode_v2_canonical(&encoded[..n])?;
    assert_eq!(decoded.len(), 4);
    assert!(decoded.contains(&RtCapability::Transfer402 {
        asset: ASSET,
        to: bound.account().bytes(),
        maximum_amount: 120,
    }));
    let admitted = Abi::new(
        ABI_V5_VERSION,
        RtProgram::new(PROGRAM)?,
        AuthorizationContext::new(actor()?, RtCapabilitySet::new(decoded)?),
        Storage::new(),
        &UnavailableReceiptOracle,
    );
    assert!(admitted.is_ok());
    assert_eq!(bound.funding_grant(0), Err(F06_INVALID_AMOUNT));
    assert_eq!(bound.deposit(0).err(), Some(F06_INVALID_AMOUNT));
    let deposit = bound.deposit(120)?;
    assert_eq!(
        (
            deposit.destination().bytes(),
            deposit.asset().bytes(),
            deposit.seed().bytes(),
            deposit.amount().value()
        ),
        (bound.account().bytes(), ASSET, REWARDS_ACCOUNT_SEED, 120)
    );
    Ok(())
}

#[test]
fn spend_grant_is_one_exact_program_spend_ceiling() -> Checked {
    let bound = bound()?;
    let to = recipient(1)?;
    let mut set = ProgramPaymentCapabilities::<1>::empty();
    set.insert(bound.spend_grant(to, 58)?)?;
    let mut encoded = [0u8; 256];
    let n = set.encode_into(&mut encoded)?;
    let sdk_account = PreparedProgramAccount::new(
        layerx_program_sdk::ProgramId::new(PROGRAM)?,
        REWARDS_ACCOUNT_SEED,
        layerx_program_sdk::AssetId::new(ASSET)?,
    )?;
    let single = sdk_account.spend_grant(
        layerx_program_sdk::AccountId::new(to.bytes())?,
        layerx_program_sdk::Amount::from_u128(58),
    )?;
    assert_eq!(&encoded[..n], single.as_slice());

    let decoded = RtCapabilitySet::decode_v2_canonical(&encoded[..n])?;
    let exact = RtCapability::ProgramSpend {
        owner_program: RtProgram::new(PROGRAM)?,
        seed: REWARDS_ACCOUNT_SEED.to_vec(),
        source_account: bound.account().bytes(),
        asset: ASSET,
        to: to.bytes(),
        maximum_amount: 58,
    };
    assert_eq!(decoded, vec![exact.clone()]);
    let owned = Abi::new(
        ABI_V5_VERSION,
        RtProgram::new(PROGRAM)?,
        AuthorizationContext::new(actor()?, RtCapabilitySet::new([exact.clone()])?),
        Storage::new(),
        &UnavailableReceiptOracle,
    );
    assert!(owned.is_ok());
    let foreign = Abi::new(
        ABI_V5_VERSION,
        RtProgram::new(OTHER_PROGRAM)?,
        AuthorizationContext::new(actor()?, RtCapabilitySet::new([exact])?),
        Storage::new(),
        &UnavailableReceiptOracle,
    );
    assert_eq!(foreign.err(), Some(AbiError::InvalidCapability));
    let wrong_source = RtCapability::ProgramSpend {
        owner_program: RtProgram::new(PROGRAM)?,
        seed: REWARDS_ACCOUNT_SEED.to_vec(),
        source_account: [250; 32],
        asset: ASSET,
        to: to.bytes(),
        maximum_amount: 58,
    };
    assert_eq!(
        RtCapabilitySet::new([wrong_source]).err(),
        Some(AbiError::InvalidCapability)
    );

    assert_eq!(bound.spend_grant(to, 0), Err(F06_INVALID_AMOUNT));
    assert_eq!(bound.spend_grant(bound.account(), 58), Err(ACCOUNT_BINDING));
    let payment = bound.payment(to, 58)?;
    assert_eq!(
        (
            payment.source().bytes(),
            payment.asset().bytes(),
            payment.to().bytes(),
            payment.seed().bytes(),
            payment.amount().value()
        ),
        (
            bound.account().bytes(),
            ASSET,
            to.bytes(),
            REWARDS_ACCOUNT_SEED,
            58
        )
    );
    Ok(())
}

#[test]
fn balance_sight_is_proof_bound_and_never_an_invented_zero() -> Checked {
    let bound = bound()?;
    let sight = bound.balance_sight(Digest32::new(RECEIPT)?);
    assert_eq!(sight.len(), BALANCE_SIGHT_BYTES);
    let mut encoded = vec![0, 1];
    encoded.extend_from_slice(&sight);
    let decoded = RtCapabilitySet::decode_v2_canonical(&encoded)?;
    assert_eq!(
        decoded,
        vec![RtCapability::BalanceView {
            account: bound.account().bytes(),
            asset: ASSET,
            receipt_digest: RECEIPT,
        }]
    );
    let sighted = Abi::new(
        ABI_V5_VERSION,
        RtProgram::new(PROGRAM)?,
        AuthorizationContext::new(actor()?, RtCapabilitySet::new(decoded)?),
        Storage::new(),
        &UnavailableReceiptOracle,
    )?;
    assert_eq!(
        sighted.balance_read(bound.account().bytes(), ASSET).err(),
        Some(AbiError::BalanceEvidenceUnavailable)
    );
    let blind = Abi::new(
        ABI_V5_VERSION,
        RtProgram::new(PROGRAM)?,
        AuthorizationContext::new(
            actor()?,
            RtCapabilitySet::new([RtCapability::SharedStorageRead])?,
        ),
        Storage::new(),
        &UnavailableReceiptOracle,
    )?;
    assert_eq!(
        blind.balance_read(bound.account().bytes(), ASSET).err(),
        Some(AbiError::CapabilityDenied)
    );

    assert_eq!(
        balance_refusal(ProgramError::Host(HostRefusal::Denied)),
        HOST_CAPABILITY
    );
    for status in [
        layerx_program_sdk::STATUS_EVIDENCE,
        RUNTIME_STATUS_ABSENT,
        layerx_program_sdk::STATUS_INVALID,
        layerx_program_sdk::STATUS_METER,
    ] {
        assert_eq!(
            balance_refusal(ProgramError::Host(HostRefusal::from_status(status))),
            READINESS_BLOCKED
        );
    }
    Ok(())
}

#[test]
fn transfer_refusals_are_typed() {
    assert_eq!(
        transfer_refusal(ProgramError::Host(HostRefusal::Denied)),
        HOST_CAPABILITY
    );
    for refusal in [
        HostRefusal::Invalid,
        HostRefusal::Bounds,
        HostRefusal::Meter,
        HostRefusal::Evidence,
        HostRefusal::Unknown(RUNTIME_STATUS_ABSENT),
    ] {
        assert_eq!(transfer_refusal(ProgramError::Host(refusal)), HOST_TRANSFER);
    }
    assert_eq!(
        transfer_refusal(ProgramError::value(
            layerx_program_sdk::Field::Amount,
            layerx_program_sdk::Reason::Zero
        )),
        F06_INVALID_AMOUNT
    );
}

/// A wrong claim refuses in the transition and every effect other than the
/// transition's exact payout is refused before value is staged, and a physical shortfall below `F + R + C` halts it.
fn claim_value_refusals(
    bound: &RewardsAccount,
    terminal: &State,
    paid: &State,
    effect: RewardEffect,
) -> Checked {
    // Wrong recipient or amount refuses in the transition: no effect, no
    // value staged, committed bytes untouched.
    let snapshot = terminal.clone();
    assert_eq!(
        terminal.claim(1, recipient(2)?, 58),
        Err(F06_WRONG_CLAIM_RECIPIENT)
    );
    assert_eq!(
        terminal.claim(1, recipient(1)?, 57),
        Err(F06_WRONG_CLAIM_AMOUNT)
    );
    assert_eq!(*terminal, snapshot);
    let (prev, next) = (terminal.ledger()?, paid.ledger()?);
    for (wrong, refusal) in [
        (
            RewardEffect::Payout {
                recipient: recipient(1)?,
                amount: 57,
            },
            F06_LEDGER_INVARIANT_VIOLATION,
        ),
        (
            RewardEffect::Payout {
                recipient: recipient(1)?,
                amount: 0,
            },
            F06_INVALID_AMOUNT,
        ),
        (
            RewardEffect::Payout {
                recipient: bound.account(),
                amount: 58,
            },
            ACCOUNT_BINDING,
        ),
        (
            RewardEffect::Deposit {
                principal: owner()?,
                amount: 58,
            },
            F06_LEDGER_INVARIANT_VIOLATION,
        ),
        (RewardEffect::NoTransfer, F06_LEDGER_INVARIANT_VIOLATION),
    ] {
        assert_eq!(plan(bound, &prev, &next, wrong), Err(refusal));
    }
    let foreign = RewardsAccount::derive(ProgramId::new(OTHER_PROGRAM)?, asset()?)?;
    assert_eq!(plan(&foreign, &prev, &next, effect), Err(ACCOUNT_BINDING));
    let action = plan(bound, &prev, &next, effect)?;
    assert_eq!(
        action,
        ValueAction::Pay {
            recipient: recipient(1)?,
            amount: 58
        }
    );
    // Physical shortfall below F+R+C after the payout halts the mutation.
    assert_eq!(
        cover(&next, 119, action),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
    assert_eq!(
        cover(&next, 57, action),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
    Ok(())
}

#[test]
fn staged_value_conserves_and_wrong_value_rolls_back() -> Checked {
    let bound = bound()?;
    let mut table = ReplayTable::new();
    table.bind(ActorSlot::OWNER, owner()?, version()?)?;
    let mut revision = 1;
    let mut balance: Amount = 0;

    let empty = State::new(&ledger()?)?;
    let (funded, effect) = empty.fund(&mut table, &mut revision, 1, 120)?;
    assert_eq!(
        effect,
        RewardEffect::Deposit {
            principal: owner()?,
            amount: 120
        }
    );
    assert_eq!(
        plan(&bound, &empty.ledger()?, &funded.ledger()?, effect),
        Ok(ValueAction::Fund { amount: 120 })
    );
    assert_eq!(settle(&bound, &empty, &funded, effect, &mut balance), Ok(0));
    assert_eq!(balance, 120);

    // An unsolicited gift is surplus only: it never raises a counter.
    balance += 5;
    let before_gift = counters(&funded)?;
    let (reserved, effect) = funded.reserve()?;
    assert_eq!(effect, RewardEffect::NoTransfer);
    assert_eq!(
        settle(&bound, &funded, &reserved, effect, &mut balance),
        Ok(5)
    );
    assert_eq!(before_gift, [120, 0, 0, 120, 0, 0]);
    assert_eq!(counters(&reserved)?, [120, 0, 0, 19, 101, 0]);

    let (terminal, effect) = reserved.terminalize()?;
    assert_eq!(effect, RewardEffect::NoTransfer);
    assert_eq!(
        settle(&bound, &reserved, &terminal, effect, &mut balance),
        Ok(5)
    );
    assert_eq!(counters(&terminal)?, [120, 0, 0, 19, 0, 101]);

    let (paid, effect) = terminal.claim(1, recipient(1)?, 58)?;
    assert_eq!(
        effect,
        RewardEffect::Payout {
            recipient: recipient(1)?,
            amount: 58
        }
    );
    claim_value_refusals(&bound, &terminal, &paid, effect)?;
    assert_eq!(balance, 125);
    assert_eq!(
        settle(&bound, &terminal, &paid, effect, &mut balance),
        Ok(5)
    );
    assert_eq!(balance, 67);
    assert_eq!(counters(&paid)?, [120, 58, 0, 19, 0, 43]);

    let (again, effect) = paid.claim(1, recipient(1)?, 58)?;
    assert!(matches!(effect, RewardEffect::AlreadyApplied(_)));
    assert_eq!(again, paid);
    assert_eq!(settle(&bound, &paid, &again, effect, &mut balance), Ok(5));
    assert_eq!(balance, 67);

    let (expired, effect) = paid.expire()?;
    assert_eq!(effect, RewardEffect::Released(43));
    assert_eq!(settle(&bound, &paid, &expired, effect, &mut balance), Ok(5));
    assert_eq!(counters(&expired)?, [120, 58, 0, 62, 0, 0]);

    let (closed, effect) = expired.refund(0, 62)?;
    assert_eq!(
        effect,
        RewardEffect::Payout {
            recipient: refund_to()?,
            amount: 62
        }
    );
    assert_eq!(
        plan(
            &bound,
            &expired.ledger()?,
            &closed.ledger()?,
            RewardEffect::Payout {
                recipient: recipient(1)?,
                amount: 62
            }
        ),
        Err(F06_REFUND_RECIPIENT_MISMATCH)
    );
    assert_eq!(
        settle(&bound, &expired, &closed, effect, &mut balance),
        Ok(5)
    );
    let l = closed.ledger()?;
    assert_eq!(counters(&closed)?, [120, 58, 62, 0, 0, 0]);
    assert_eq!(l.tracked_deposits, l.total_claimed + l.tracked_refunds);
    assert_eq!(balance, 5);
    Ok(())
}

#[test]
fn funding_cover_counts_the_staged_deposit_and_refuses_shortfall() -> Checked {
    let bound = bound()?;
    let mut table = ReplayTable::new();
    table.bind(ActorSlot::OWNER, owner()?, version()?)?;
    let mut revision = 1;
    let (first, _) = State::new(&ledger()?)?.fund(&mut table, &mut revision, 1, 120)?;
    let (second, effect) = first.fund(&mut table, &mut revision, 2, 10)?;
    let action = plan(&bound, &first.ledger()?, &second.ledger()?, effect)?;
    assert_eq!(action, ValueAction::Fund { amount: 10 });
    assert_eq!(cover(&second.ledger()?, 120, action), Ok(0));
    assert_eq!(cover(&second.ledger()?, 125, action), Ok(5));
    assert_eq!(
        cover(&second.ledger()?, 119, action),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
    assert_eq!(
        cover(&second.ledger()?, Amount::MAX, action),
        Err(ARITHMETIC)
    );
    assert_eq!(
        ValueAction::from_effect(RewardEffect::Deposit {
            principal: owner()?,
            amount: 0
        }),
        Err(F06_INVALID_AMOUNT)
    );
    Ok(())
}
