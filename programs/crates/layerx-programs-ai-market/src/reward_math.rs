//! F06 exact integer reward arithmetic over immutable F05 raw weights. No floating
//! point, saturation, random ties or traversal-order ties; every step is checked.
use crate::{
    aggregation_codec::{EpochAggregation, WorkerAggregate, MAX_TOTAL_WEIGHT},
    errors::*,
    rewards::RewardOutcome,
    types::*,
    MAX_EVALUATORS, MAX_WORKERS,
};

pub const QUORUM: usize = 3;
pub const CLAIM_EXPIRY_HEIGHTS: u64 = 4096;

/// Lower median of one worker's accepted scores: rank floor((n-1)/2) of the
/// ascending order. Fewer than quorum accepted observations is no score at all.
pub fn lower_median(scores: &[Score]) -> CodecResult<Presence<Score>> {
    if scores.len() > MAX_EVALUATORS {
        return Err(CAPACITY);
    }
    if scores.len() < QUORUM {
        return Ok(Presence::Absent);
    }
    let mut sorted = [0u32; MAX_EVALUATORS];
    let sorted = &mut sorted[..scores.len()];
    for (slot, score) in sorted.iter_mut().zip(scores) {
        *slot = score.get();
    }
    sorted.sort_unstable();
    Ok(Presence::Present(Score::new(
        sorted[(scores.len() - 1) / 2],
    )?))
}

/// Exact (floor(b*s/t), (b*s) mod t). The product is held as three 64-bit limbs
/// (below 2^192, inside the required 256-bit domain) and divided limb by limb.
pub fn mul_div_rem(b: u128, s: u64, t: u64) -> CodecResult<(u128, u64)> {
    if t == 0 {
        return Err(ARITHMETIC);
    }
    let s = u128::from(s);
    let lo = u128::from(b as u64) * s;
    let hi = (b >> 64) * s;
    let mid = (lo >> 64) + u128::from(hi as u64);
    let top = u64::try_from((hi >> 64) + (mid >> 64)).map_err(|_| ARITHMETIC)?;
    let t = u128::from(t);
    let mut rem = 0u128;
    let mut quotient = [0u64; 3];
    for (q, limb) in quotient.iter_mut().zip([top, mid as u64, lo as u64]) {
        let current = (rem << 64) | u128::from(limb);
        *q = u64::try_from(current / t).map_err(|_| ARITHMETIC)?;
        rem = current % t;
    }
    if quotient[0] != 0 {
        return Err(ARITHMETIC);
    }
    let rem = u64::try_from(rem).map_err(|_| ARITHMETIC)?;
    Ok((
        (u128::from(quotient[1]) << 64) | u128::from(quotient[2]),
        rem,
    ))
}

/// Exact epoch entitlements in ascending WorkerId order. A NO_ELIGIBLE_SCORE
/// result carries no entries and releases the whole budget.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Allocation {
    budget: Amount,
    total_weight: u64,
    count: usize,
    workers: [Option<WorkerId>; MAX_WORKERS],
    amounts: [Amount; MAX_WORKERS],
}
impl Allocation {
    pub const fn budget(&self) -> Amount {
        self.budget
    }
    pub const fn total_weight(&self) -> u64 {
        self.total_weight
    }
    pub const fn outcome(&self) -> RewardOutcome {
        if self.total_weight == 0 {
            RewardOutcome::NoEligibleScore
        } else {
            RewardOutcome::Allocated
        }
    }
    pub const fn len(&self) -> usize {
        self.count
    }
    pub const fn is_empty(&self) -> bool {
        self.count == 0
    }
    pub fn entitlement(&self, index: usize) -> CodecResult<(WorkerId, Amount)> {
        if index >= self.count {
            return Err(F06_UNKNOWN_WORKER_ENTITLEMENT);
        }
        let worker = self.workers[index].ok_or(F06_LEDGER_INVARIANT_VIOLATION)?;
        Ok((worker, self.amounts[index]))
    }
}

/// Largest-remainder allocation of budget B over positive F05 raw weights.
/// Input order is irrelevant: rows are canonicalized by WorkerId first.
pub fn allocate(budget: Amount, outputs: &[WorkerAggregate]) -> CodecResult<Allocation> {
    if budget == 0 {
        return Err(F06_INVALID_AMOUNT);
    }
    let n = outputs.len();
    if n > MAX_WORKERS {
        return Err(CAPACITY);
    }
    let mut order = [0usize; MAX_WORKERS];
    for (i, slot) in order[..n].iter_mut().enumerate() {
        *slot = i;
    }
    order[..n].sort_unstable_by_key(|&i| outputs[i].worker());
    if order[..n]
        .windows(2)
        .any(|p| outputs[p[0]].worker() == outputs[p[1]].worker())
    {
        return Err(NON_CANONICAL);
    }
    let mut total = 0u64;
    for output in outputs {
        total = total
            .checked_add(u64::from(output.weight()))
            .ok_or(ARITHMETIC)?;
    }
    if total > MAX_TOTAL_WEIGHT {
        return Err(ARITHMETIC);
    }
    let mut allocation = Allocation {
        budget,
        total_weight: total,
        count: 0,
        workers: [None; MAX_WORKERS],
        amounts: [0; MAX_WORKERS],
    };
    if total == 0 {
        return Ok(allocation);
    }
    allocation.count = n;
    let mut weights = [0u64; MAX_WORKERS];
    let mut remainders = [0u64; MAX_WORKERS];
    let mut base_sum = 0u128;
    for (k, &i) in order[..n].iter().enumerate() {
        let weight = u64::from(outputs[i].weight());
        let (q, r) = mul_div_rem(budget, weight, total)?;
        allocation.workers[k] = Some(outputs[i].worker());
        allocation.amounts[k] = q;
        weights[k] = weight;
        remainders[k] = r;
        base_sum = base_sum.checked_add(q).ok_or(ARITHMETIC)?;
    }
    let mut left = budget
        .checked_sub(base_sum)
        .ok_or(F06_LEDGER_INVARIANT_VIOLATION)?;
    let mut rank = [0usize; MAX_WORKERS];
    for (k, slot) in rank[..n].iter_mut().enumerate() {
        *slot = k;
    }
    rank[..n].sort_unstable_by(|&a, &b| remainders[b].cmp(&remainders[a]).then(a.cmp(&b)));
    for &k in &rank[..n] {
        if left == 0 {
            break;
        }
        if weights[k] == 0 || remainders[k] == 0 {
            return Err(F06_LEDGER_INVARIANT_VIOLATION);
        }
        allocation.amounts[k] = allocation.amounts[k].checked_add(1).ok_or(ARITHMETIC)?;
        left -= 1;
    }
    let mut sum = 0u128;
    for amount in &allocation.amounts[..n] {
        sum = sum.checked_add(*amount).ok_or(ARITHMETIC)?;
    }
    if left != 0 || sum != budget {
        return Err(F06_LEDGER_INVARIANT_VIOLATION);
    }
    Ok(allocation)
}

/// Allocation over the immutable F05 output commitment; its recorded total
/// weight must agree with the recomputed sum.
pub fn allocate_aggregation(
    budget: Amount,
    aggregation: &EpochAggregation<'_>,
) -> CodecResult<Allocation> {
    let allocation = allocate(budget, aggregation.outputs())?;
    if allocation.total_weight != aggregation.total_weight() {
        return Err(F06_AGGREGATION_MISMATCH);
    }
    Ok(allocation)
}

/// Claim window end; overflow refuses rather than saturating.
pub fn claim_expiry(terminal_height: u64) -> CodecResult<u64> {
    terminal_height
        .checked_add(CLAIM_EXPIRY_HEIGHTS)
        .ok_or(ARITHMETIC)
}

/// D = P + X + F + R + C, every intermediate sum checked.
pub fn conservation_holds(
    deposits: Amount,
    claimed: Amount,
    refunds: Amount,
    free: Amount,
    reserved: Amount,
    liability: Amount,
) -> CodecResult<bool> {
    let sum = claimed
        .checked_add(refunds)
        .and_then(|v| v.checked_add(free))
        .and_then(|v| v.checked_add(reserved))
        .and_then(|v| v.checked_add(liability))
        .ok_or(ARITHMETIC)?;
    Ok(sum == deposits)
}
