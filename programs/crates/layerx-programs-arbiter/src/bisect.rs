use std::collections::BTreeMap;

use layerx_program_sdk::Amount;
use layerx_programs_market::settle::{ArbiterVerdict, MAX_CHALLENGE_WINDOW_BATCHES};
use layerx_programs_runtime::execute::observe_market_sandbox_step;
use layerx_programs_runtime::portable_replay::{
    replay_leaf_hash, replay_node_hash, PortableBoundary,
};
use layerx_programs_runtime::replay::MarketSandboxReplayAuthority;
use layerx_programs_runtime::{ArbitrationStepCommitment, ValidatedModule};
use sha2::{Digest, Sha256};

use crate::{BoundaryProof, MarketStepVerdict, VerifiedMarketSandbox};

const MAX_BYTES: u32 = 1_048_576;
const MAX_BOUNDARIES: u32 = 4096;
const MAX_SIBLINGS: usize = 12;
const ARGUMENT_DOMAIN: &[u8] = b"LXP/arbiter-bisection-argument/v1\0";

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct TraceCommitment {
    pub root: [u8; 32],
    pub boundary_count: u32,
    pub initial_state_root: [u8; 32],
    pub maximum_bytes: u32,
}
impl TraceCommitment {
    fn validate(self) -> Result<Self, GameError> {
        if self.root == [0; 32]
            || self.initial_state_root == [0; 32]
            || self.boundary_count == 0
            || self.boundary_count > MAX_BOUNDARIES
            || self.maximum_bytes == 0
            || self.maximum_bytes > MAX_BYTES
        {
            return Err(GameError::Trace);
        }
        Ok(self)
    }
    pub fn open_leaf(&self, proof: &BoundaryProof) -> Option<PortableBoundary> {
        if proof.index >= self.boundary_count
            || proof.leaf.is_empty()
            || proof.leaf.len() > self.maximum_bytes as usize
            || proof.siblings.len() > MAX_SIBLINGS
        {
            return None;
        }
        let mut node = replay_leaf_hash(proof.index, &proof.leaf).ok()?;
        let mut index = proof.index;
        let mut count = self.boundary_count;
        for sibling in &proof.siblings {
            if count <= 1 || ((index ^ 1) >= count && sibling != &node) {
                return None;
            }
            node = if index & 1 == 0 {
                replay_node_hash(node, *sibling)
            } else {
                replay_node_hash(*sibling, node)
            };
            index /= 2;
            count = count.div_ceil(2);
        }
        if count != 1 || node != self.root {
            return None;
        }
        PortableBoundary::decode_untrusted(&proof.leaf, self.maximum_bytes as usize).ok()
    }
}

pub struct CommittedTrace {
    leaves: Vec<Vec<u8>>,
    levels: Vec<Vec<[u8; 32]>>,
}
impl CommittedTrace {
    pub fn new(leaves: Vec<Vec<u8>>) -> Result<Self, GameError> {
        if leaves.is_empty() || leaves.len() > MAX_BOUNDARIES as usize {
            return Err(GameError::Trace);
        }
        let mut level = Vec::with_capacity(leaves.len());
        for (index, leaf) in leaves.iter().enumerate() {
            level.push(replay_leaf_hash(index as u32, leaf).map_err(|_| GameError::Trace)?);
        }
        let mut levels = vec![level];
        while levels[levels.len() - 1].len() > 1 {
            let below = &levels[levels.len() - 1];
            let above = below
                .chunks(2)
                .map(|pair| replay_node_hash(pair[0], *pair.get(1).unwrap_or(&pair[0])))
                .collect();
            levels.push(above);
        }
        Ok(Self { leaves, levels })
    }
    pub fn root(&self) -> [u8; 32] {
        self.levels[self.levels.len() - 1][0]
    }
    pub fn boundary_count(&self) -> u32 {
        self.leaves.len() as u32
    }
    pub fn proof(&self, index: u32) -> Option<BoundaryProof> {
        let leaf = self.leaves.get(index as usize)?.clone();
        let mut position = index as usize;
        let mut siblings = Vec::with_capacity(self.levels.len() - 1);
        for level in &self.levels[..self.levels.len() - 1] {
            siblings.push(*level.get(position ^ 1).unwrap_or(&level[position]));
            position /= 2;
        }
        Some(BoundaryProof {
            index,
            leaf,
            siblings,
        })
    }
}

pub trait StepJudge {
    fn trace(&self) -> TraceCommitment;
    fn step_holds(&self, pre: &BoundaryProof, post: &BoundaryProof) -> bool;
}

impl StepJudge for VerifiedMarketSandbox {
    fn trace(&self) -> TraceCommitment {
        TraceCommitment {
            root: self.billing().provider_trace_root,
            boundary_count: self.billing().boundary_count,
            initial_state_root: self.profile().initial_execution_state_root,
            maximum_bytes: self.profile().maximum_bytes,
        }
    }
    fn step_holds(&self, pre: &BoundaryProof, post: &BoundaryProof) -> bool {
        self.verify_step(pre, post) == MarketStepVerdict::Correct
    }
}

pub struct SandboxStepJudge<'a> {
    module: &'a ValidatedModule,
    authority: &'a MarketSandboxReplayAuthority,
    trace: TraceCommitment,
}
impl<'a> SandboxStepJudge<'a> {
    pub fn new(
        module: &'a ValidatedModule,
        authority: &'a MarketSandboxReplayAuthority,
        trace: TraceCommitment,
    ) -> Result<Self, GameError> {
        if module.code_hash() != authority.code_hash
            || module.metering_schedule_version() != authority.metering_schedule_version
        {
            return Err(GameError::Trace);
        }
        Ok(Self {
            module,
            authority,
            trace: trace.validate()?,
        })
    }
}
impl StepJudge for SandboxStepJudge<'_> {
    fn trace(&self) -> TraceCommitment {
        self.trace
    }
    fn step_holds(&self, pre: &BoundaryProof, post: &BoundaryProof) -> bool {
        let final_trap = pre == post && pre.index.checked_add(1) == Some(self.trace.boundary_count);
        if !final_trap && pre.index.checked_add(1) != Some(post.index) {
            return false;
        }
        let (Some(before), Some(mut after)) =
            (self.trace.open_leaf(pre), self.trace.open_leaf(post))
        else {
            return false;
        };
        if final_trap != before.trap.is_some() {
            return false;
        }
        let maximum = self.trace.maximum_bytes as usize;
        let Ok(observed) =
            observe_market_sandbox_step(self.module, &before, self.authority, maximum)
        else {
            return false;
        };
        if !final_trap && observed.trap.is_none() {
            after.trap = None;
        }
        matches!(
            (observed.reencode_untrusted(maximum), after.reencode_untrusted(maximum)),
            (Ok(observed), Ok(claimed)) if observed == claimed
        )
    }
}

pub fn same_state(claimed: &PortableBoundary, truth: &PortableBoundary, maximum: u32) -> bool {
    let (mut claimed, mut truth) = (claimed.clone(), truth.clone());
    claimed.trap = None;
    truth.trap = None;
    matches!(
        (claimed.reencode_untrusted(maximum as usize), truth.reencode_untrusted(maximum as usize)),
        (Ok(claimed), Ok(truth)) if claimed == truth
    )
}

pub const fn max_rounds(boundary_count: u32) -> u32 {
    if boundary_count <= 1 {
        0
    } else {
        u32::BITS - (boundary_count - 1).leading_zeros()
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum GameError {
    Trace,
    Stake,
    Window,
    Height,
    Late,
    Turn,
    Proof,
    Settled,
}
impl std::fmt::Display for GameError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}
impl std::error::Error for GameError {}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u8)]
pub enum Party {
    Defender = 1,
    Challenger = 2,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Awaiting {
    Reveal(u32),
    Respond(u32),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u8)]
pub enum MoveKind {
    Open = 1,
    Reveal = 2,
    Agree = 3,
    Disagree = 4,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Move {
    pub height: u64,
    pub party: Party,
    pub kind: MoveKind,
    pub position: u32,
    pub leaf_digest: [u8; 32],
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Reason {
    Timeout(Party),
    InitialState,
    Step,
    Terminal,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Stakes {
    pub defender: Amount,
    pub challenger: Amount,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Settlement {
    pub verdict: ArbiterVerdict,
    pub reason: Reason,
    pub lo: u32,
    pub hi: u32,
    pub height: u64,
    pub defender_payout: Amount,
    pub challenger_payout: Amount,
    pub argument: [u8; 32],
}

pub struct BisectionGame<J> {
    judge: J,
    trace: TraceCommitment,
    stakes: Stakes,
    total: Amount,
    window: u64,
    height: u64,
    deadline: u64,
    lo: u32,
    hi: u32,
    awaiting: Awaiting,
    revealed: BTreeMap<u32, BoundaryProof>,
    record: Vec<Move>,
    rounds: u32,
    settlement: Option<Settlement>,
}

impl<J: StepJudge> BisectionGame<J> {
    pub fn open(judge: J, stakes: Stakes, window: u64, height: u64) -> Result<Self, GameError> {
        let trace = judge.trace().validate()?;
        if stakes.defender.is_zero() || stakes.challenger.is_zero() {
            return Err(GameError::Stake);
        }
        let total = stakes
            .defender
            .checked_add(stakes.challenger)
            .map_err(|_| GameError::Stake)?;
        if window == 0 || window > MAX_CHALLENGE_WINDOW_BATCHES {
            return Err(GameError::Window);
        }
        let deadline = height.checked_add(window).ok_or(GameError::Height)?;
        Ok(Self {
            judge,
            trace,
            stakes,
            total,
            window,
            height,
            deadline,
            lo: 0,
            hi: trace.boundary_count,
            awaiting: Awaiting::Reveal(0),
            revealed: BTreeMap::new(),
            record: vec![Move {
                height,
                party: Party::Challenger,
                kind: MoveKind::Open,
                position: trace.boundary_count,
                leaf_digest: trace.root,
            }],
            rounds: 0,
            settlement: None,
        })
    }

    pub fn trace(&self) -> TraceCommitment {
        self.trace
    }
    pub fn stakes(&self) -> Stakes {
        self.stakes
    }
    pub fn awaiting(&self) -> Option<Awaiting> {
        self.settlement.is_none().then_some(self.awaiting)
    }
    pub fn deadline(&self) -> u64 {
        self.deadline
    }
    pub fn interval(&self) -> (u32, u32) {
        (self.lo, self.hi)
    }
    pub fn rounds(&self) -> u32 {
        self.rounds
    }
    pub fn record(&self) -> &[Move] {
        &self.record
    }
    pub fn settlement(&self) -> Option<&Settlement> {
        self.settlement.as_ref()
    }

    pub fn advance(&mut self, height: u64) -> Result<Option<&Settlement>, GameError> {
        if self.settlement.is_none() {
            if height < self.height {
                return Err(GameError::Height);
            }
            if height > self.deadline {
                self.default(height);
            }
        }
        Ok(self.settlement.as_ref())
    }

    pub fn reveal(&mut self, height: u64, proof: BoundaryProof) -> Result<(), GameError> {
        let Awaiting::Reveal(position) = self.begin(height, Party::Defender)? else {
            return Err(GameError::Turn);
        };
        if proof.index != position {
            return Err(GameError::Proof);
        }
        let leaf = self.trace.open_leaf(&proof).ok_or(GameError::Proof)?;
        self.push(
            height,
            Party::Defender,
            MoveKind::Reveal,
            position,
            &proof.leaf,
        );
        self.revealed.insert(position, proof);
        if position == 0 {
            let initial = ArbitrationStepCommitment::from_state(&leaf.arbitration)
                .map(|commitment| commitment.digest);
            if initial != Ok(self.trace.initial_state_root) {
                self.settle(height, ArbiterVerdict::Challenger, Reason::InitialState);
                return Ok(());
            }
            self.next(height);
        } else {
            self.awaiting = Awaiting::Respond(position);
            self.deadline = height.saturating_add(self.window);
        }
        Ok(())
    }

    pub fn respond(&mut self, height: u64, agree: bool) -> Result<(), GameError> {
        let Awaiting::Respond(position) = self.begin(height, Party::Challenger)? else {
            return Err(GameError::Turn);
        };
        let kind = if agree {
            self.lo = position;
            MoveKind::Agree
        } else {
            self.hi = position;
            MoveKind::Disagree
        };
        self.push(height, Party::Challenger, kind, position, &[]);
        self.rounds += 1;
        self.next(height);
        Ok(())
    }

    fn begin(&mut self, height: u64, party: Party) -> Result<Awaiting, GameError> {
        if self.settlement.is_some() {
            return Err(GameError::Settled);
        }
        if height < self.height {
            return Err(GameError::Height);
        }
        if height > self.deadline {
            self.default(height);
            return Err(GameError::Late);
        }
        let expected = match self.awaiting {
            Awaiting::Reveal(_) => Party::Defender,
            Awaiting::Respond(_) => Party::Challenger,
        };
        if expected != party {
            return Err(GameError::Turn);
        }
        Ok(self.awaiting)
    }

    fn next(&mut self, height: u64) {
        if self.hi - self.lo > 1 {
            self.awaiting = Awaiting::Reveal(self.lo + (self.hi - self.lo) / 2);
            self.deadline = height.saturating_add(self.window);
            return;
        }
        let pre = &self.revealed[&self.lo];
        let (holds, reason) = if self.hi == self.trace.boundary_count {
            let terminal = match self.trace.open_leaf(pre) {
                Some(leaf) if leaf.trap.is_some() => self.judge.step_holds(pre, pre),
                Some(leaf) => leaf.replay.snapshot.call_frames.is_empty(),
                None => false,
            };
            (terminal, Reason::Terminal)
        } else {
            (
                self.judge.step_holds(pre, &self.revealed[&self.hi]),
                Reason::Step,
            )
        };
        let verdict = if holds {
            ArbiterVerdict::Provider
        } else {
            ArbiterVerdict::Challenger
        };
        self.settle(height, verdict, reason);
    }

    fn default(&mut self, height: u64) {
        let (absent, verdict) = match self.awaiting {
            Awaiting::Reveal(_) => (Party::Defender, ArbiterVerdict::Challenger),
            Awaiting::Respond(_) => (Party::Challenger, ArbiterVerdict::Provider),
        };
        self.settle(height, verdict, Reason::Timeout(absent));
    }

    fn push(&mut self, height: u64, party: Party, kind: MoveKind, position: u32, leaf: &[u8]) {
        self.height = height;
        self.record.push(Move {
            height,
            party,
            kind,
            position,
            leaf_digest: if leaf.is_empty() {
                [0; 32]
            } else {
                Sha256::digest(leaf).into()
            },
        });
    }

    fn settle(&mut self, height: u64, verdict: ArbiterVerdict, reason: Reason) {
        let (defender_payout, challenger_payout) = match verdict {
            ArbiterVerdict::Provider => (self.total, Amount::ZERO),
            ArbiterVerdict::Challenger => (Amount::ZERO, self.total),
        };
        let mut hash = Sha256::new();
        hash.update(ARGUMENT_DOMAIN);
        hash.update(self.trace.root);
        hash.update(self.trace.boundary_count.to_be_bytes());
        hash.update(self.trace.initial_state_root);
        hash.update(self.stakes.defender.to_be_bytes());
        hash.update(self.stakes.challenger.to_be_bytes());
        hash.update(self.window.to_be_bytes());
        hash.update((self.record.len() as u32).to_be_bytes());
        for entry in &self.record {
            hash.update(entry.height.to_be_bytes());
            hash.update([entry.party as u8, entry.kind as u8]);
            hash.update(entry.position.to_be_bytes());
            hash.update(entry.leaf_digest);
        }
        hash.update(height.to_be_bytes());
        hash.update([verdict as u8]);
        match reason {
            Reason::Timeout(party) => hash.update([1, party as u8]),
            Reason::InitialState => hash.update([2, 0]),
            Reason::Step => hash.update([3, 0]),
            Reason::Terminal => hash.update([4, 0]),
        }
        hash.update(self.lo.to_be_bytes());
        hash.update(self.hi.to_be_bytes());
        self.settlement = Some(Settlement {
            verdict,
            reason,
            lo: self.lo,
            hi: self.hi,
            height,
            defender_payout,
            challenger_payout,
            argument: hash.finalize().into(),
        });
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use layerx_programs_runtime::execute::{
        instantiate_market_sandbox_untrusted, market_sandbox_input_digest, MarketSandboxRequest,
    };
    use layerx_programs_runtime::replay::{market_sandbox_baseline_root, market_sandbox_namespace};
    use layerx_programs_runtime::test_support::{
        code_section, export_section, func_body, function_section, import_section, module,
        raw_section, type_section, OP_CALL, OP_END, OP_I32_ADD, OP_I32_CONST, TYPE_I32,
    };
    use layerx_programs_runtime::{
        FeeSchedule, PrincipalId, ProgramId, ProgramReplayProfile, ResourceBudget, Storage,
        WasmEngine, RUNTIME_VERSION,
    };

    const WINDOW: u64 = 6;
    const OPENED: u64 = 1_000;

    struct Execution {
        module: ValidatedModule,
        authority: MarketSandboxReplayAuthority,
        leaves: Vec<Vec<u8>>,
        truth: Vec<PortableBoundary>,
        initial: [u8; 32],
        terminal_status: u8,
    }

    impl Execution {
        fn run(wasm: &[u8], entrypoint: &str) -> Self {
            let engine = WasmEngine::declared().expect("declared engine");
            let module = engine.validate_versioned(2, wasm).expect("validated guest");
            let program =
                ProgramId::new(Sha256::digest(wasm).into()).expect("code-derived program");
            let tenant = PrincipalId::new(Sha256::digest(b"bisection-tenant").into())
                .expect("tenant principal");
            let lease: [u8; 32] = Sha256::digest(b"bisection-lease").into();
            let baseline = Storage::new();
            let budget = ResourceBudget::new_complete(10_000_000, 65_536, 1024, 1024, 1, 64, 0);
            let fees = FeeSchedule::declared();
            let authority = MarketSandboxReplayAuthority {
                profile_binding: Sha256::digest(b"bisection-profile").into(),
                namespace: market_sandbox_namespace(program, lease).expect("namespace"),
                lease_id: lease,
                namespace_limit: 1024,
                program,
                tenant,
                payment_account: tenant.bytes(),
                code_hash: module.code_hash(),
                input_digest: market_sandbox_input_digest(entrypoint, &[]).expect("input digest"),
                runtime_version: RUNTIME_VERSION,
                abi_version: 2,
                fee_schedule_version: fees.version(),
                metering_schedule_version: module.metering_schedule_version(),
                budget,
                fees,
                fee_budget: 1_000_000_000,
                baseline_state_root: market_sandbox_baseline_root(&baseline)
                    .expect("baseline root"),
                baseline_storage: baseline,
            };
            let mut instance = instantiate_market_sandbox_untrusted(&module, &authority)
                .expect("sandbox instance");
            let execution = instance
                .call_market_sandbox_untrusted(MarketSandboxRequest {
                    module: &module,
                    entrypoint,
                    args: &[],
                    authority: &authority,
                    replay_profile: ProgramReplayProfile::new(128, MAX_BYTES)
                        .expect("replay profile"),
                })
                .expect("captured execution");
            let truth = execution
                .boundary_leaves
                .iter()
                .map(|leaf| {
                    PortableBoundary::decode_untrusted(leaf, MAX_BYTES as usize)
                        .expect("captured boundary")
                })
                .collect();
            let committed =
                CommittedTrace::new(execution.boundary_leaves.clone()).expect("committed trace");
            assert_eq!(committed.root(), execution.record.boundary_root());
            assert_eq!(
                committed.boundary_count(),
                execution.record.boundary_count()
            );
            Self {
                module,
                authority,
                leaves: execution.boundary_leaves,
                truth,
                initial: execution.initial_commitment.digest,
                terminal_status: execution.record.terminal_status(),
            }
        }

        fn game(&self, trace: &CommittedTrace) -> BisectionGame<SandboxStepJudge<'_>> {
            let judge = SandboxStepJudge::new(
                &self.module,
                &self.authority,
                TraceCommitment {
                    root: trace.root(),
                    boundary_count: trace.boundary_count(),
                    initial_state_root: self.initial,
                    maximum_bytes: MAX_BYTES,
                },
            )
            .expect("sandbox judge");
            BisectionGame::open(judge, stakes(), WINDOW, OPENED).expect("opened dispute")
        }

        fn honest(&self, position: u32, claimed: &PortableBoundary) -> bool {
            self.truth
                .get(position as usize)
                .is_some_and(|truth| same_state(claimed, truth, MAX_BYTES))
        }

        fn lie(&self, from: usize) -> Vec<Vec<u8>> {
            self.truth
                .iter()
                .enumerate()
                .map(|(index, boundary)| {
                    let mut boundary = boundary.clone();
                    if index >= from {
                        boundary.arbitration.host_state_root[0] ^= 1;
                    }
                    boundary
                        .reencode_untrusted(MAX_BYTES as usize)
                        .expect("canonical divergent boundary")
                })
                .collect()
        }
    }

    fn stakes() -> Stakes {
        Stakes {
            defender: Amount::from_u128(700),
            challenger: Amount::from_u128(300),
        }
    }

    fn play<J: StepJudge>(
        game: &mut BisectionGame<J>,
        trace: &CommittedTrace,
        mut challenger: impl FnMut(u32, &PortableBoundary) -> bool,
    ) -> Settlement {
        let mut height = OPENED;
        while let Some(awaiting) = game.awaiting() {
            height += 1;
            assert!(height <= game.deadline());
            match awaiting {
                Awaiting::Reveal(position) => game
                    .reveal(height, trace.proof(position).expect("committed proof"))
                    .expect("receipt-backed reveal"),
                Awaiting::Respond(position) => {
                    let claimed = game
                        .trace()
                        .open_leaf(&trace.proof(position).expect("committed proof"))
                        .expect("revealed boundary");
                    game.respond(height, challenger(position, &claimed))
                        .expect("timely response");
                }
            }
        }
        let settlement = *game.settlement().expect("automatic settlement");
        assert!(game.rounds() <= max_rounds(trace.boundary_count()));
        assert_eq!(
            settlement
                .defender_payout
                .checked_add(settlement.challenger_payout)
                .expect("bounded payout"),
            Amount::from_u128(1_000)
        );
        let winner = match settlement.verdict {
            ArbiterVerdict::Provider => settlement.defender_payout,
            ArbiterVerdict::Challenger => settlement.challenger_payout,
        };
        assert_eq!(winner, Amount::from_u128(1_000));
        assert_ne!(settlement.argument, [0; 32]);
        assert!(matches!(
            game.respond(height + 1, false),
            Err(GameError::Settled)
        ));
        assert!(matches!(
            game.reveal(height + 1, trace.proof(0).expect("proof")),
            Err(GameError::Settled)
        ));
        settlement
    }

    fn integer_guest() -> Vec<u8> {
        let mut body = vec![OP_I32_CONST, 1];
        for value in 2..=6 {
            body.extend([OP_I32_CONST, value, OP_I32_ADD]);
        }
        body.push(OP_END);
        module(&[
            type_section(&[(&[], &[TYPE_I32])]),
            function_section(&[0]),
            export_section(&[("compute", 0)]),
            code_section(&[func_body(&[], &body)]),
        ])
    }

    fn storage_guest() -> Vec<u8> {
        module(&[
            type_section(&[(&[TYPE_I32; 4], &[TYPE_I32]), (&[], &[TYPE_I32])]),
            import_section(&[("layerx_v1", "storage_write", 0)]),
            function_section(&[1]),
            raw_section(5, &[1, 0, 1]),
            raw_section(
                7,
                &[
                    2, 3, b'r', b'u', b'n', 0, 1, 6, b'm', b'e', b'm', b'o', b'r', b'y', 2, 0,
                ],
            ),
            code_section(&[func_body(
                &[],
                &[
                    OP_I32_CONST,
                    0,
                    OP_I32_CONST,
                    1,
                    OP_I32_CONST,
                    1,
                    OP_I32_CONST,
                    1,
                    OP_CALL,
                    0,
                    OP_END,
                ],
            )]),
            raw_section(11, &[1, 0, OP_I32_CONST, 0, OP_END, 2, b'k', b'v']),
        ])
    }

    fn trapping_guest() -> Vec<u8> {
        module(&[
            type_section(&[(&[], &[TYPE_I32])]),
            function_section(&[0]),
            export_section(&[("compute", 0)]),
            code_section(&[func_body(&[], &[0x00, OP_END])]),
        ])
    }

    #[test]
    fn honest_defender_wins_against_every_lying_challenger_strategy() {
        let execution = Execution::run(&integer_guest(), "compute");
        assert_eq!(execution.terminal_status, 0);
        let trace = CommittedTrace::new(execution.leaves.clone()).expect("honest trace");
        assert!(trace.boundary_count() >= 8);
        let rounds = max_rounds(trace.boundary_count());
        for strategy in 0..(1u32 << rounds) {
            let mut game = execution.game(&trace);
            let mut round = 0;
            let settlement = play(&mut game, &trace, |_, _| {
                round += 1;
                strategy & (1 << (round - 1)) != 0
            });
            assert_eq!(settlement.verdict, ArbiterVerdict::Provider);
            assert!(matches!(settlement.reason, Reason::Step | Reason::Terminal));
            assert_eq!(settlement.hi, settlement.lo + 1);
            assert_eq!(settlement.defender_payout, Amount::from_u128(1_000));
            assert_eq!(settlement.challenger_payout, Amount::ZERO);
            assert_eq!(game.record().len(), 2 + 2 * game.rounds() as usize);
        }
        let mut honest_challenger = execution.game(&trace);
        let settlement = play(&mut honest_challenger, &trace, |position, claimed| {
            execution.honest(position, claimed)
        });
        assert_eq!(settlement.verdict, ArbiterVerdict::Provider);
        assert_eq!(settlement.reason, Reason::Terminal);
    }

    #[test]
    fn honest_challenger_defeats_every_lying_defender_divergence() {
        let execution = Execution::run(&integer_guest(), "compute");
        let count = execution.leaves.len();
        let honest_root = CommittedTrace::new(execution.leaves.clone())
            .expect("honest trace")
            .root();
        for from in 0..count {
            let trace = CommittedTrace::new(execution.lie(from)).expect("lying trace");
            assert_ne!(trace.root(), honest_root);
            let mut game = execution.game(&trace);
            let settlement = play(&mut game, &trace, |position, claimed| {
                execution.honest(position, claimed)
            });
            assert_eq!(settlement.verdict, ArbiterVerdict::Challenger);
            assert_eq!(settlement.challenger_payout, Amount::from_u128(1_000));
            assert_eq!(settlement.defender_payout, Amount::ZERO);
            if from == 0 {
                assert_eq!(settlement.reason, Reason::InitialState);
                assert_eq!(game.rounds(), 0);
            } else {
                assert_eq!(settlement.reason, Reason::Step);
                assert_eq!(
                    (settlement.lo, settlement.hi),
                    (from as u32 - 1, from as u32)
                );
            }
        }
        let mut arguments = std::collections::BTreeSet::new();
        for from in 1..count {
            let trace = CommittedTrace::new(execution.lie(from)).expect("lying trace");
            let mut game = execution.game(&trace);
            let settlement = play(&mut game, &trace, |position, claimed| {
                execution.honest(position, claimed)
            });
            assert!(arguments.insert(settlement.argument));
        }
    }

    #[test]
    fn dispute_over_host_call_isolates_the_storage_write_step() {
        let execution = Execution::run(&storage_guest(), "run");
        assert_eq!(execution.terminal_status, 0);
        let call = execution
            .truth
            .windows(2)
            .position(|pair| {
                pair[0].arbitration.host_state_root != pair[1].arbitration.host_state_root
            })
            .expect("storage write host call changes host state")
            + 1;
        let honest = CommittedTrace::new(execution.leaves.clone()).expect("honest trace");
        let pre = honest.proof(call as u32 - 1).expect("host call pre");
        let post = honest.proof(call as u32).expect("host call post");
        let mut game = execution.game(&honest);
        assert!(game.judge.step_holds(&pre, &post));
        let settlement = play(&mut game, &honest, |position, _| position < call as u32);
        assert_eq!(settlement.verdict, ArbiterVerdict::Provider);
        assert_eq!(settlement.reason, Reason::Step);
        assert_eq!(
            (settlement.lo, settlement.hi),
            (call as u32 - 1, call as u32)
        );

        let lying = CommittedTrace::new(execution.lie(call)).expect("lying host effect");
        let mut game = execution.game(&lying);
        let settlement = play(&mut game, &lying, |position, claimed| {
            execution.honest(position, claimed)
        });
        assert_eq!(settlement.verdict, ArbiterVerdict::Challenger);
        assert_eq!(settlement.reason, Reason::Step);
        assert_eq!(
            (settlement.lo, settlement.hi),
            (call as u32 - 1, call as u32)
        );
    }

    #[test]
    fn dispute_over_trap_judges_the_terminal_trap_step() {
        let execution = Execution::run(&trapping_guest(), "compute");
        assert_eq!(execution.terminal_status, 1);
        let last = execution.truth.last().expect("terminal boundary");
        assert!(last.trap.is_some());
        let honest = CommittedTrace::new(execution.leaves.clone()).expect("honest trap trace");
        let mut game = execution.game(&honest);
        let settlement = play(&mut game, &honest, |_, _| true);
        assert_eq!(settlement.verdict, ArbiterVerdict::Provider);
        assert_eq!(settlement.reason, Reason::Terminal);
        assert_eq!(settlement.hi, honest.boundary_count());

        let mut denied = execution.truth.clone();
        denied.last_mut().expect("terminal boundary").trap = None;
        let denied = CommittedTrace::new(
            denied
                .iter()
                .map(|boundary| {
                    boundary
                        .reencode_untrusted(MAX_BYTES as usize)
                        .expect("canonical denial")
                })
                .collect(),
        )
        .expect("trap-denying trace");
        let mut game = execution.game(&denied);
        let settlement = play(&mut game, &denied, |position, claimed| {
            execution.honest(position, claimed)
        });
        assert_eq!(settlement.verdict, ArbiterVerdict::Challenger);
        assert_eq!(settlement.reason, Reason::Terminal);
        assert_eq!(settlement.hi, denied.boundary_count());
    }

    #[test]
    fn absent_party_loses_by_default_at_the_declared_deadline() {
        let execution = Execution::run(&integer_guest(), "compute");
        let trace = CommittedTrace::new(execution.leaves.clone()).expect("honest trace");

        let mut game = execution.game(&trace);
        assert_eq!(game.awaiting(), Some(Awaiting::Reveal(0)));
        assert_eq!(game.deadline(), OPENED + WINDOW);
        assert_eq!(game.advance(OPENED + WINDOW), Ok(None));
        assert_eq!(game.respond(OPENED + 1, true), Err(GameError::Turn));
        let mut forged = trace.proof(0).expect("proof");
        forged.leaf[0] ^= 1;
        assert_eq!(game.reveal(OPENED + 1, forged), Err(GameError::Proof));
        assert_eq!(
            game.reveal(OPENED + 1, trace.proof(1).expect("proof")),
            Err(GameError::Proof)
        );
        let settlement = *game
            .advance(OPENED + WINDOW + 1)
            .expect("deadline")
            .expect("defender default");
        assert_eq!(settlement.verdict, ArbiterVerdict::Challenger);
        assert_eq!(settlement.reason, Reason::Timeout(Party::Defender));
        assert_eq!(settlement.challenger_payout, Amount::from_u128(1_000));
        assert_eq!(game.awaiting(), None);

        let mut game = execution.game(&trace);
        game.reveal(OPENED + 1, trace.proof(0).expect("proof"))
            .expect("initial reveal");
        let Some(Awaiting::Reveal(mid)) = game.awaiting() else {
            panic!("midpoint reveal required");
        };
        game.reveal(OPENED + 2, trace.proof(mid).expect("proof"))
            .expect("midpoint reveal");
        assert_eq!(game.awaiting(), Some(Awaiting::Respond(mid)));
        assert_eq!(game.deadline(), OPENED + 2 + WINDOW);
        assert_eq!(game.respond(OPENED + 1, false), Err(GameError::Height));
        assert_eq!(
            game.respond(OPENED + 3 + WINDOW, false),
            Err(GameError::Late)
        );
        let settlement = *game.settlement().expect("challenger default");
        assert_eq!(settlement.verdict, ArbiterVerdict::Provider);
        assert_eq!(settlement.reason, Reason::Timeout(Party::Challenger));
        assert_eq!(settlement.height, OPENED + 3 + WINDOW);
        assert_eq!(settlement.defender_payout, Amount::from_u128(1_000));
        assert_eq!(settlement.challenger_payout, Amount::ZERO);
        assert_eq!(
            game.respond(OPENED + 4 + WINDOW, true),
            Err(GameError::Settled)
        );

        let judge = || {
            SandboxStepJudge::new(
                &execution.module,
                &execution.authority,
                TraceCommitment {
                    root: trace.root(),
                    boundary_count: trace.boundary_count(),
                    initial_state_root: execution.initial,
                    maximum_bytes: MAX_BYTES,
                },
            )
            .expect("judge")
        };
        let zero = Stakes {
            defender: Amount::ZERO,
            challenger: Amount::from_u128(1),
        };
        assert!(matches!(
            BisectionGame::open(judge(), zero, WINDOW, OPENED),
            Err(GameError::Stake)
        ));
        let overflow = Stakes {
            defender: Amount::MAX,
            challenger: Amount::from_u128(1),
        };
        assert!(matches!(
            BisectionGame::open(judge(), overflow, WINDOW, OPENED),
            Err(GameError::Stake)
        ));
        for window in [0, MAX_CHALLENGE_WINDOW_BATCHES + 1] {
            assert!(matches!(
                BisectionGame::open(judge(), stakes(), window, OPENED),
                Err(GameError::Window)
            ));
        }
        assert!(matches!(
            SandboxStepJudge::new(
                &execution.module,
                &execution.authority,
                TraceCommitment {
                    root: trace.root(),
                    boundary_count: 0,
                    initial_state_root: execution.initial,
                    maximum_bytes: MAX_BYTES,
                },
            ),
            Err(GameError::Trace)
        ));
    }
}
