//! On-chain bisection over a provider's committed sandbox trace.
//!
//! The provider (defender) reveals boundaries, the challenger agrees or disagrees with each, and
//! the interval halves until one step remains. That step is judged by the host through the
//! market-step adjudication call, never by a caller-supplied verdict. Every move has a deadline;
//! an absent party loses by the bisection rules. Each move is absorbed into a running transcript
//! hash that becomes the dispute commitment of the settlement.

use layerx_program_sdk::arbiter::{
    MarketStepOutcome, MARKET_STEP_TERMINAL, MARKET_STEP_TRANSITION,
};
use layerx_program_sdk::{Field, ProgramError, Reason};

use crate::settle::{ArbiterVerdict, UsageChallenge, UsageClaim, MAX_CHALLENGE_WINDOW_BATCHES};

pub const REVEAL_BOUNDARY: u8 = 15;
pub const RESPOND_BOUNDARY: u8 = 16;
pub const RESOLVE_DISPUTE: u8 = 17;
pub const DISPUTE_PREFIX: &[u8] = b"lx.market.dispute/";
pub const TOPIC_DISPUTE: &[u8] = b"lx.market.dispute";
pub const TRANSCRIPT_DOMAIN: &[u8] = b"LXP/market-dispute-transcript/v1\0";
pub const DISPUTE_CAPACITY: usize = 80;
pub const MOVE_BYTES: usize = 46;
pub const TRANSCRIPT_PREIMAGE_BYTES: usize = TRANSCRIPT_DOMAIN.len() + 32 + MOVE_BYTES;
pub const MAX_BOUNDARIES: u32 = 4096;
const VERSION: u8 = 1;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum Party {
    Provider = 1,
    Challenger = 2,
    Arbiter = 3,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum MoveKind {
    Open = 1,
    Reveal = 2,
    Agree = 3,
    Disagree = 4,
    Adjudicate = 5,
    Timeout = 6,
}

/// Why a dispute settled the way it did.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum Resolution {
    /// The provider missed a reveal deadline.
    ProviderAbsent = 1,
    /// The challenger missed a response deadline.
    ChallengerAbsent = 2,
    /// The first committed boundary is not the authorized initial execution state.
    InitialState = 3,
    /// The host judged the single disputed transition.
    Step = 4,
    /// The host judged the last committed boundary as the end of the execution.
    Terminal = 5,
    /// Nobody brought the collapsed step to the host before its deadline; the defender carries
    /// the burden of proof.
    Unadjudicated = 6,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Awaiting {
    Reveal(u32),
    Respond(u32),
    Adjudicate(u32),
    Settled(ArbiterVerdict, Resolution),
}

/// One dispute move as absorbed into the transcript: party, kind, height, position and the
/// digest it carries.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Move {
    pub party: Party,
    pub kind: MoveKind,
    pub height: u64,
    pub position: u32,
    pub digest: [u8; 32],
}

impl Move {
    #[must_use]
    pub fn encode(&self) -> [u8; MOVE_BYTES] {
        let mut bytes = [0; MOVE_BYTES];
        bytes[0] = self.party as u8;
        bytes[1] = self.kind as u8;
        bytes[2..10].copy_from_slice(&self.height.to_be_bytes());
        bytes[10..14].copy_from_slice(&self.position.to_be_bytes());
        bytes[14..].copy_from_slice(&self.digest);
        bytes
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Dispute {
    pub count: u32,
    pub lo: u32,
    pub hi: u32,
    pub rounds: u32,
    pub window: u64,
    pub deadline: u64,
    pub last: u64,
    pub awaiting: Awaiting,
    pub transcript: [u8; 32],
}

impl Dispute {
    /// Opens the bisection over a frozen claim's committed trace of `count` boundaries. The
    /// transcript starts at the challenge identity; the returned open move binds the trace root.
    ///
    /// # Errors
    /// Returns an error for an unfrozen or unrelated claim, an empty or oversized trace, a zero
    /// trace root or a height outside the claim's challenge window.
    pub fn open(
        claim: &UsageClaim,
        challenge: &UsageChallenge<'_>,
        trace_root: [u8; 32],
        count: u32,
        height: u64,
    ) -> Result<(Self, Move), ProgramError> {
        let window = claim
            .challenge_deadline
            .checked_sub(claim.committed_at)
            .ok_or_else(malformed)?;
        if claim.status != crate::settle::ClaimStatus::Frozen
            || challenge.claim_id != claim.id
            || challenge.lease_id != claim.lease_id
            || challenge.opened_at != height
            || challenge.id == [0; 32]
            || trace_root == [0; 32]
            || count == 0
            || count > MAX_BOUNDARIES
            || window == 0
            || window > MAX_CHALLENGE_WINDOW_BATCHES
            || height < claim.committed_at
            || height > claim.challenge_deadline
        {
            return Err(malformed());
        }
        Ok((
            Self {
                count,
                lo: 0,
                hi: count,
                rounds: 0,
                window,
                deadline: height.checked_add(window).ok_or_else(malformed)?,
                last: height,
                awaiting: Awaiting::Reveal(0),
                transcript: challenge.id,
            },
            Move {
                party: Party::Challenger,
                kind: MoveKind::Open,
                height,
                position: count,
                digest: trace_root,
            },
        ))
    }

    /// Returns the transcript preimage that absorbs `next` into the running dispute transcript.
    #[must_use]
    pub fn transcript_preimage(&self, next: &Move) -> [u8; TRANSCRIPT_PREIMAGE_BYTES] {
        let mut bytes = [0; TRANSCRIPT_PREIMAGE_BYTES];
        let domain = TRANSCRIPT_DOMAIN.len();
        bytes[..domain].copy_from_slice(TRANSCRIPT_DOMAIN);
        bytes[domain..domain + 32].copy_from_slice(&self.transcript);
        bytes[domain + 32..].copy_from_slice(&next.encode());
        bytes
    }

    #[must_use]
    pub const fn verdict(&self) -> Option<ArbiterVerdict> {
        match self.awaiting {
            Awaiting::Settled(verdict, _) => Some(verdict),
            _ => None,
        }
    }

    fn timely(&self, height: u64) -> Result<(), ProgramError> {
        if matches!(self.awaiting, Awaiting::Settled(..))
            || height < self.last
            || height > self.deadline
        {
            return Err(malformed());
        }
        Ok(())
    }

    fn reset(&mut self, height: u64) -> Result<(), ProgramError> {
        self.last = height;
        self.deadline = height.checked_add(self.window).ok_or_else(malformed)?;
        Ok(())
    }

    fn next(&mut self, height: u64) -> Result<(), ProgramError> {
        self.awaiting = if self.hi - self.lo > 1 {
            Awaiting::Reveal(self.lo + (self.hi - self.lo) / 2)
        } else {
            Awaiting::Adjudicate(self.lo)
        };
        self.reset(height)
    }

    /// Records the provider's reveal of the awaited boundary, opened by the host against the
    /// committed trace. Revealing boundary zero also proves the authorized initial state.
    ///
    /// # Errors
    /// Returns an error out of turn, past the deadline or for any other position.
    pub fn reveal(
        mut self,
        height: u64,
        position: u32,
        opened: &MarketStepOutcome,
        initial_state_root: [u8; 32],
    ) -> Result<(Self, Move), ProgramError> {
        self.timely(height)?;
        let Awaiting::Reveal(expected) = self.awaiting else {
            return Err(malformed());
        };
        if position != expected {
            return Err(malformed());
        }
        let record = Move {
            party: Party::Provider,
            kind: MoveKind::Reveal,
            height,
            position,
            digest: opened.leaf_digest,
        };
        if position == 0 {
            if !opened.holds || opened.commitment != initial_state_root {
                self.last = height;
                self.awaiting =
                    Awaiting::Settled(ArbiterVerdict::Challenger, Resolution::InitialState);
                return Ok((self, record));
            }
            self.next(height)?;
        } else {
            if !opened.holds {
                return Err(malformed());
            }
            self.awaiting = Awaiting::Respond(position);
            self.reset(height)?;
        }
        Ok((self, record))
    }

    /// Records the challenger's agreement or disagreement with the last revealed boundary.
    ///
    /// # Errors
    /// Returns an error out of turn or past the deadline.
    pub fn respond(mut self, height: u64, agree: bool) -> Result<(Self, Move), ProgramError> {
        self.timely(height)?;
        let Awaiting::Respond(position) = self.awaiting else {
            return Err(malformed());
        };
        let kind = if agree {
            self.lo = position;
            MoveKind::Agree
        } else {
            self.hi = position;
            MoveKind::Disagree
        };
        self.rounds = self.rounds.checked_add(1).ok_or_else(malformed)?;
        self.next(height)?;
        Ok((
            self,
            Move {
                party: Party::Challenger,
                kind,
                height,
                position,
                digest: [0; 32],
            },
        ))
    }

    /// Returns the host adjudication mode and position of the collapsed step.
    ///
    /// # Errors
    /// Returns an error unless the interval has collapsed to one step.
    pub fn adjudication(&self) -> Result<(u8, u32), ProgramError> {
        let Awaiting::Adjudicate(position) = self.awaiting else {
            return Err(malformed());
        };
        if self.hi == self.count {
            Ok((MARKET_STEP_TERMINAL, position))
        } else {
            Ok((MARKET_STEP_TRANSITION, position))
        }
    }

    /// Settles the collapsed step on the host's authenticated verdict.
    ///
    /// # Errors
    /// Returns an error before the interval collapses or past the adjudication deadline.
    pub fn adjudicate(
        mut self,
        height: u64,
        judged: &MarketStepOutcome,
    ) -> Result<(Self, Move), ProgramError> {
        self.timely(height)?;
        let (mode, position) = self.adjudication()?;
        let verdict = if judged.holds {
            ArbiterVerdict::Provider
        } else {
            ArbiterVerdict::Challenger
        };
        let resolution = if mode == MARKET_STEP_TERMINAL {
            Resolution::Terminal
        } else {
            Resolution::Step
        };
        self.last = height;
        self.awaiting = Awaiting::Settled(verdict, resolution);
        Ok((
            self,
            Move {
                party: Party::Arbiter,
                kind: MoveKind::Adjudicate,
                height,
                position,
                digest: judged.leaf_digest,
            },
        ))
    }

    /// Settles a dispute whose awaited party let the deadline pass.
    ///
    /// # Errors
    /// Returns an error for a settled dispute or before the deadline has passed.
    pub fn timeout(mut self, height: u64) -> Result<(Self, Move), ProgramError> {
        if height <= self.deadline || height < self.last {
            return Err(malformed());
        }
        let (position, verdict, resolution) = match self.awaiting {
            Awaiting::Reveal(position) => (
                position,
                ArbiterVerdict::Challenger,
                Resolution::ProviderAbsent,
            ),
            Awaiting::Respond(position) => (
                position,
                ArbiterVerdict::Provider,
                Resolution::ChallengerAbsent,
            ),
            Awaiting::Adjudicate(position) => (
                position,
                ArbiterVerdict::Challenger,
                Resolution::Unadjudicated,
            ),
            Awaiting::Settled(..) => return Err(malformed()),
        };
        self.last = height;
        self.awaiting = Awaiting::Settled(verdict, resolution);
        Ok((
            self,
            Move {
                party: Party::Arbiter,
                kind: MoveKind::Timeout,
                height,
                position,
                digest: [0; 32],
            },
        ))
    }

    fn validate(&self) -> Result<(), ProgramError> {
        let positioned = match self.awaiting {
            Awaiting::Reveal(0) => self.lo == 0 && self.hi == self.count && self.rounds == 0,
            Awaiting::Reveal(position) | Awaiting::Respond(position) => {
                self.lo < position && position < self.hi
            }
            Awaiting::Adjudicate(position) => {
                position == self.lo && self.hi.checked_sub(self.lo) == Some(1)
            }
            Awaiting::Settled(..) => true,
        };
        if self.count == 0
            || self.count > MAX_BOUNDARIES
            || self.lo >= self.hi
            || self.hi > self.count
            || self.window == 0
            || self.window > MAX_CHALLENGE_WINDOW_BATCHES
            || (self.last > self.deadline && !matches!(self.awaiting, Awaiting::Settled(..)))
            || self.transcript == [0; 32]
            || !positioned
        {
            return Err(malformed());
        }
        Ok(())
    }
}

/// # Errors
/// Returns an error for inconsistent dispute state or a short output buffer.
pub fn encode_dispute(dispute: &Dispute, output: &mut [u8]) -> Result<usize, ProgramError> {
    dispute.validate()?;
    let (state, position, verdict, resolution) = match dispute.awaiting {
        Awaiting::Reveal(position) => (1, position, 0, 0),
        Awaiting::Respond(position) => (2, position, 0, 0),
        Awaiting::Adjudicate(position) => (3, position, 0, 0),
        Awaiting::Settled(verdict, resolution) => (4, dispute.lo, verdict as u8, resolution as u8),
    };
    let mut offset = 0;
    crate::append(output, &mut offset, &[VERSION, state])?;
    crate::append(output, &mut offset, &position.to_be_bytes())?;
    crate::append(output, &mut offset, &[verdict, resolution])?;
    for value in [dispute.count, dispute.lo, dispute.hi, dispute.rounds] {
        crate::append(output, &mut offset, &value.to_be_bytes())?;
    }
    for value in [dispute.window, dispute.deadline, dispute.last] {
        crate::append(output, &mut offset, &value.to_be_bytes())?;
    }
    crate::append(output, &mut offset, &dispute.transcript)?;
    Ok(offset)
}

/// # Errors
/// Returns an error for a wrong version, unknown state, truncated or trailing bytes, or
/// inconsistent bisection state.
pub fn decode_dispute(input: &[u8]) -> Result<Dispute, ProgramError> {
    let mut cursor = crate::Cursor::new(input);
    if cursor.byte()? != VERSION {
        return Err(malformed());
    }
    let state = cursor.byte()?;
    let position = cursor.u32()?;
    let verdict = cursor.byte()?;
    let resolution = cursor.byte()?;
    let count = cursor.u32()?;
    let lo = cursor.u32()?;
    let hi = cursor.u32()?;
    let rounds = cursor.u32()?;
    let window = cursor.u64()?;
    let deadline = cursor.u64()?;
    let last = cursor.u64()?;
    let transcript = cursor.array()?;
    cursor.finish()?;
    let awaiting = match (state, verdict, resolution) {
        (1, 0, 0) => Awaiting::Reveal(position),
        (2, 0, 0) => Awaiting::Respond(position),
        (3, 0, 0) => Awaiting::Adjudicate(position),
        (4, verdict, resolution) if position == lo => Awaiting::Settled(
            match verdict {
                1 => ArbiterVerdict::Provider,
                2 => ArbiterVerdict::Challenger,
                _ => return Err(malformed()),
            },
            match resolution {
                1 => Resolution::ProviderAbsent,
                2 => Resolution::ChallengerAbsent,
                3 => Resolution::InitialState,
                4 => Resolution::Step,
                5 => Resolution::Terminal,
                6 => Resolution::Unadjudicated,
                _ => return Err(malformed()),
            },
        ),
        _ => return Err(malformed()),
    };
    let dispute = Dispute {
        count,
        lo,
        hi,
        rounds,
        window,
        deadline,
        last,
        awaiting,
        transcript,
    };
    dispute.validate()?;
    Ok(dispute)
}

fn malformed() -> ProgramError {
    ProgramError::value(Field::CallInput, Reason::Malformed)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::settle::{
        challenge, commit_usage, ChallengeRequest, ContradictingCommitment, MeteredUsageClaim,
        ProviderCommitment,
    };
    use crate::{open, register, OpenLease, RegisterOffer, VerificationModel};
    use layerx_program_sdk::{AccountId, Amount, AssetId};

    const INITIAL: [u8; 32] = [21; 32];
    const TRACE: [u8; 32] = [22; 32];

    fn ok<T>(result: Result<T, ProgramError>) -> T {
        result.unwrap_or_else(|error| panic!("dispute fixture: {error}"))
    }
    fn account(tag: u8) -> AccountId {
        ok(AccountId::new([tag; 32]))
    }
    fn frozen(committed_at: u64, challenged_at: u64) -> (UsageClaim, UsageChallenge<'static>) {
        let offer = ok(register(
            RegisterOffer {
                id: [1; 32],
                provider: account(1),
                payout: account(2),
                asset: ok(AssetId::new([9; 32])),
                stake_account: account(3),
                stake_seed: b"stake/offer-1",
                stake: Amount::from_integer(500_u128),
                unit_price: Amount::from_integer(4_u128),
                capacity: 100,
                minimum_units: 2,
                maximum_units: 20,
                expires_at: 1_000_000,
                verification: VerificationModel::FraudProvable,
            },
            account(1),
            1,
        ));
        let (offer, lease) = ok(open(
            offer,
            OpenLease {
                id: [5; 32],
                offer_id: offer.id,
                tenant: account(4),
                refund: account(4),
                escrow_account: account(6),
                escrow_seed: b"escrow",
                units: 10,
                funded: Amount::from_integer(40_u128),
                expires_at: 200,
            },
            account(4),
            2,
        ));
        let (claim, _) = ok(commit_usage(
            offer,
            &lease,
            ProviderCommitment {
                id: [7; 32],
                lease_id: lease.id,
                input_commitment: [8; 32],
                output_digest: [9; 32],
                execution_state_root: [10; 32],
                usage: MeteredUsageClaim {
                    compute_units: 10,
                    memory_byte_batches: 1,
                    storage_read_bytes: 2,
                    storage_written_bytes: 3,
                    ingress_bytes: 4,
                    egress_bytes: 5,
                },
                payable: Amount::from_integer(40_u128),
                challenger_stake: Amount::from_integer(25_u128),
                challenge_window_batches: 10,
            },
            account(1),
            committed_at,
        ));
        ok(challenge(
            offer,
            &lease,
            claim,
            &ChallengeRequest {
                challenge_id: [12; 32],
                challenger: account(13),
                stake_account: account(14),
                stake_seed: b"challenge",
                stake: claim.challenger_stake,
                contradictory: ContradictingCommitment {
                    input_commitment: claim.input_commitment,
                    output_digest: [11; 32],
                    execution_state_root: claim.execution_state_root,
                    usage: claim.usage,
                },
            },
            challenged_at,
        ))
    }
    fn opened(count: u32) -> Dispute {
        let (claim, dispute) = frozen(10, 12);
        let (state, record) = ok(Dispute::open(&claim, &dispute, TRACE, count, 12));
        assert_eq!(record.digest, TRACE);
        assert_eq!(state.deadline, 22);
        assert_eq!(state.transcript, dispute.id);
        state
    }
    fn leaf(byte: u8, holds: bool, commitment: [u8; 32]) -> MarketStepOutcome {
        MarketStepOutcome {
            holds,
            leaf_digest: [byte; 32],
            commitment,
        }
    }
    fn round_trips(state: &Dispute) {
        let mut bytes = [0; DISPUTE_CAPACITY];
        let written = ok(encode_dispute(state, &mut bytes));
        assert_eq!(written, DISPUTE_CAPACITY);
        assert_eq!(ok(decode_dispute(&bytes[..written])), *state);
    }

    #[test]
    fn opening_requires_a_frozen_claim_challenged_now_with_a_bounded_trace() {
        let (claim, dispute) = frozen(10, 12);
        for (root, count, height) in [
            ([0; 32], 4, 12),
            (TRACE, 0, 12),
            (TRACE, MAX_BOUNDARIES + 1, 12),
            (TRACE, 4, 13),
        ] {
            assert!(Dispute::open(&claim, &dispute, root, count, height).is_err());
        }
        let mut unfrozen = claim;
        unfrozen.status = crate::settle::ClaimStatus::Challengeable;
        assert!(Dispute::open(&unfrozen, &dispute, TRACE, 4, 12).is_err());
        let mut unrelated = dispute;
        unrelated.claim_id = [99; 32];
        assert!(Dispute::open(&claim, &unrelated, TRACE, 4, 12).is_err());
        round_trips(&opened(4));
    }

    #[test]
    fn bisection_collapses_to_one_authenticated_transition() {
        let state = opened(8);
        let (state, _) = ok(state.reveal(13, 0, &leaf(1, true, INITIAL), INITIAL));
        assert_eq!(state.awaiting, Awaiting::Reveal(4));
        assert!(state.respond(13, true).is_err());
        assert!(state
            .reveal(13, 5, &leaf(1, true, [1; 32]), INITIAL)
            .is_err());
        assert!(state.adjudication().is_err());
        let (state, _) = ok(state.reveal(14, 4, &leaf(2, true, [2; 32]), INITIAL));
        round_trips(&state);
        let (state, _) = ok(state.respond(15, true));
        assert_eq!(
            (state.lo, state.hi, state.awaiting),
            (4, 8, Awaiting::Reveal(6))
        );
        let (state, _) = ok(state.reveal(16, 6, &leaf(3, true, [3; 32]), INITIAL));
        let (state, _) = ok(state.respond(17, false));
        assert_eq!(
            (state.lo, state.hi, state.awaiting),
            (4, 6, Awaiting::Reveal(5))
        );
        let (state, _) = ok(state.reveal(18, 5, &leaf(4, true, [4; 32]), INITIAL));
        let (state, _) = ok(state.respond(19, true));
        assert_eq!(state.awaiting, Awaiting::Adjudicate(5));
        assert_eq!(ok(state.adjudication()), (MARKET_STEP_TRANSITION, 5));
        round_trips(&state);
        let (provider, record) = ok(state.adjudicate(20, &leaf(4, true, [4; 32])));
        assert_eq!(record.party, Party::Arbiter);
        assert_eq!(provider.verdict(), Some(ArbiterVerdict::Provider));
        let (challenger, _) = ok(state.adjudicate(20, &leaf(4, false, [4; 32])));
        assert_eq!(
            challenger.awaiting,
            Awaiting::Settled(ArbiterVerdict::Challenger, Resolution::Step)
        );
        round_trips(&provider);
        assert!(provider.adjudicate(20, &leaf(4, true, [4; 32])).is_err());
        assert!(provider.timeout(1_000).is_err());
        assert!(provider.respond(20, true).is_err());
    }

    #[test]
    fn agreeing_to_every_boundary_judges_the_terminal_state() {
        let state = opened(4);
        let (state, _) = ok(state.reveal(13, 0, &leaf(1, true, INITIAL), INITIAL));
        let (state, _) = ok(state.reveal(13, 2, &leaf(2, true, [2; 32]), INITIAL));
        let (state, _) = ok(state.respond(13, true));
        let (state, _) = ok(state.reveal(13, 3, &leaf(3, true, [3; 32]), INITIAL));
        let (state, _) = ok(state.respond(13, true));
        assert_eq!(ok(state.adjudication()), (MARKET_STEP_TERMINAL, 3));
        let (state, _) = ok(state.adjudicate(14, &leaf(3, false, [3; 32])));
        assert_eq!(
            state.awaiting,
            Awaiting::Settled(ArbiterVerdict::Challenger, Resolution::Terminal)
        );
        let single = opened(1);
        let (single, _) = ok(single.reveal(13, 0, &leaf(1, true, INITIAL), INITIAL));
        assert_eq!(ok(single.adjudication()), (MARKET_STEP_TERMINAL, 0));
    }

    #[test]
    fn a_wrong_initial_state_or_unopenable_boundary_loses_for_the_provider() {
        let state = opened(4);
        let (settled, _) = ok(state.reveal(13, 0, &leaf(1, true, [77; 32]), INITIAL));
        assert_eq!(
            settled.awaiting,
            Awaiting::Settled(ArbiterVerdict::Challenger, Resolution::InitialState)
        );
        round_trips(&settled);
        let (settled, _) = ok(state.reveal(13, 0, &leaf(1, false, INITIAL), INITIAL));
        assert_eq!(settled.verdict(), Some(ArbiterVerdict::Challenger));
        let (state, _) = ok(state.reveal(13, 0, &leaf(1, true, INITIAL), INITIAL));
        assert!(state
            .reveal(13, 2, &leaf(2, false, [2; 32]), INITIAL)
            .is_err());
    }

    #[test]
    fn absent_parties_lose_by_the_bisection_rules_and_only_after_the_deadline() {
        let state = opened(4);
        assert!(state.timeout(22).is_err());
        assert!(state
            .reveal(23, 0, &leaf(1, true, INITIAL), INITIAL)
            .is_err());
        let (absent, record) = ok(state.timeout(23));
        assert_eq!(record.kind, MoveKind::Timeout);
        assert_eq!(
            absent.awaiting,
            Awaiting::Settled(ArbiterVerdict::Challenger, Resolution::ProviderAbsent)
        );
        let (state, _) = ok(state.reveal(20, 0, &leaf(1, true, INITIAL), INITIAL));
        assert_eq!(state.deadline, 30);
        let (state, _) = ok(state.reveal(30, 2, &leaf(2, true, [2; 32]), INITIAL));
        assert!(state.timeout(40).is_err());
        assert!(state.respond(41, true).is_err());
        let (absent, _) = ok(state.timeout(41));
        assert_eq!(
            absent.awaiting,
            Awaiting::Settled(ArbiterVerdict::Provider, Resolution::ChallengerAbsent)
        );
        let (state, _) = ok(state.respond(31, false));
        let (state, _) = ok(state.reveal(31, 1, &leaf(3, true, [3; 32]), INITIAL));
        let (state, _) = ok(state.respond(31, true));
        assert_eq!(ok(state.adjudication()), (MARKET_STEP_TRANSITION, 1));
        assert!(state.adjudicate(42, &leaf(3, true, [3; 32])).is_err());
        let (absent, _) = ok(state.timeout(42));
        assert_eq!(
            absent.awaiting,
            Awaiting::Settled(ArbiterVerdict::Challenger, Resolution::Unadjudicated)
        );
        assert!(absent.timeout(1_000).is_err());
    }

    #[test]
    fn every_dispute_settles_within_a_bounded_number_of_moves() {
        for count in [1, 2, 3, 7, 100, MAX_BOUNDARIES] {
            for agree in [true, false] {
                let mut state = opened(count);
                let mut moves = 1_u32;
                let mut height = 12;
                while state.verdict().is_none() {
                    height += 1;
                    state = match state.awaiting {
                        Awaiting::Reveal(position) => {
                            let commitment = if position == 0 { INITIAL } else { [5; 32] };
                            ok(state.reveal(height, position, &leaf(6, true, commitment), INITIAL))
                                .0
                        }
                        Awaiting::Respond(_) => ok(state.respond(height, agree)).0,
                        Awaiting::Adjudicate(_) => {
                            ok(state.adjudicate(height, &leaf(6, true, [5; 32]))).0
                        }
                        Awaiting::Settled(..) => unreachable!(),
                    };
                    moves += 1;
                    round_trips(&state);
                }
                assert!(moves <= 2 * 12 + 3, "{count} boundaries took {moves} moves");
                assert!(height <= 12 + u64::from(moves) * state.window);
            }
        }
    }

    #[test]
    fn transcript_absorbs_every_move_in_order() {
        let state = opened(2);
        let first = Move {
            party: Party::Provider,
            kind: MoveKind::Reveal,
            height: 13,
            position: 0,
            digest: [1; 32],
        };
        let preimage = state.transcript_preimage(&first);
        assert_eq!(&preimage[..TRANSCRIPT_DOMAIN.len()], TRANSCRIPT_DOMAIN);
        assert_eq!(
            &preimage[TRANSCRIPT_DOMAIN.len()..][..32],
            &state.transcript
        );
        assert_eq!(&preimage[TRANSCRIPT_DOMAIN.len() + 32..], &first.encode());
        let mut second = first;
        second.height = 14;
        assert_ne!(preimage, state.transcript_preimage(&second));
    }

    #[test]
    fn dispute_rows_are_strictly_decoded() {
        let state = opened(8);
        let mut bytes = [0; DISPUTE_CAPACITY];
        let written = ok(encode_dispute(&state, &mut bytes));
        let mut trailing = [0; DISPUTE_CAPACITY + 1];
        trailing[..written].copy_from_slice(&bytes[..written]);
        assert!(decode_dispute(&trailing).is_err());
        assert!(decode_dispute(&bytes[..written - 1]).is_err());
        for (offset, value) in [(0, 2), (1, 9), (6, 1), (7, 1), (1, 3), (1, 4)] {
            let mut corrupt = bytes;
            corrupt[offset] = value;
            assert!(decode_dispute(&corrupt).is_err(), "byte {offset} = {value}");
        }
        let mut zero_transcript = bytes;
        zero_transcript[48..].fill(0);
        assert!(decode_dispute(&zero_transcript).is_err());
        assert!(encode_dispute(&state, &mut [0; DISPUTE_CAPACITY - 1]).is_err());
    }
}
