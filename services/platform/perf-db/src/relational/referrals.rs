use crate::core::types::FixedI128;

/// A referral tier definition. Matches PostgreSQL `referral_tiers` table.
#[derive(Debug, Clone)]
pub struct ReferralTier {
    pub tier: &'static str,
    pub min_referrals: u32,
    pub min_volume: FixedI128,
    pub reward_bps: u32,
    pub fee_discount_bps: u32,
}

/// Default tier table — sorted ascending by min_referrals.
/// These can be updated at runtime via `ReferralEngine::set_tiers`.
const DEFAULT_TIERS: &[ReferralTier] = &[
    ReferralTier {
        tier: "bronze",
        min_referrals: 0,
        min_volume: 0,
        reward_bps: 5,
        fee_discount_bps: 0,
    },
    ReferralTier {
        tier: "silver",
        min_referrals: 5,
        min_volume: 100_000_000_000_000_000_000_000, // 100k USD in 18-dec
        reward_bps: 10,
        fee_discount_bps: 5,
    },
    ReferralTier {
        tier: "gold",
        min_referrals: 20,
        min_volume: 1_000_000_000_000_000_000_000_000, // 1M USD
        reward_bps: 15,
        fee_discount_bps: 10,
    },
    ReferralTier {
        tier: "platinum",
        min_referrals: 50,
        min_volume: 10_000_000_000_000_000_000_000_000, // 10M USD
        reward_bps: 20,
        fee_discount_bps: 15,
    },
];

/// Referral tier engine. Computes the correct tier for a user based on
/// their referral count and total volume.
///
/// Replaces PostgreSQL `referral_tiers` table + the `run_tier_upgrade` task.
pub struct ReferralEngine {
    tiers: Vec<ReferralTier>,
}

impl ReferralEngine {
    pub fn new() -> Self {
        Self {
            tiers: DEFAULT_TIERS.to_vec(),
        }
    }

    /// Create with custom tier definitions.
    pub fn with_tiers(tiers: Vec<ReferralTier>) -> Self {
        Self { tiers }
    }

    /// Replace the tier table at runtime.
    pub fn set_tiers(&mut self, tiers: Vec<ReferralTier>) {
        self.tiers = tiers;
    }

    /// Get all tier definitions (for API: GET /referral-tiers).
    pub fn tiers(&self) -> &[ReferralTier] {
        &self.tiers
    }

    /// Compute the highest tier a user qualifies for.
    ///
    /// Walks the tier list in reverse (highest first) and returns the first
    /// tier where both min_referrals and min_volume are met.
    pub fn compute_tier(&self, referral_count: u32, total_volume: FixedI128) -> &ReferralTier {
        for tier in self.tiers.iter().rev() {
            if referral_count >= tier.min_referrals && total_volume >= tier.min_volume {
                return tier;
            }
        }
        &self.tiers[0]
    }

    /// Compute fee discount BPS for a user given their referral stats.
    pub fn fee_discount_bps(&self, referral_count: u32, total_volume: FixedI128) -> u32 {
        self.compute_tier(referral_count, total_volume)
            .fee_discount_bps
    }

    /// Compute reward BPS for a referrer given their stats.
    pub fn reward_bps(&self, referral_count: u32, total_volume: FixedI128) -> u32 {
        self.compute_tier(referral_count, total_volume).reward_bps
    }

    /// Number of tier definitions.
    pub fn tier_count(&self) -> usize {
        self.tiers.len()
    }
}

impl Default for ReferralEngine {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_tiers_exist() {
        let engine = ReferralEngine::new();
        assert_eq!(engine.tier_count(), 4);
    }

    #[test]
    fn compute_tier_bronze() {
        let engine = ReferralEngine::new();
        let tier = engine.compute_tier(0, 0);
        assert_eq!(tier.tier, "bronze");
        assert_eq!(tier.fee_discount_bps, 0);
    }

    #[test]
    fn compute_tier_silver() {
        let engine = ReferralEngine::new();
        let tier = engine.compute_tier(5, 100_000_000_000_000_000_000_000);
        assert_eq!(tier.tier, "silver");
        assert_eq!(tier.fee_discount_bps, 5);
        assert_eq!(tier.reward_bps, 10);
    }

    #[test]
    fn compute_tier_gold() {
        let engine = ReferralEngine::new();
        let tier = engine.compute_tier(20, 1_000_000_000_000_000_000_000_000);
        assert_eq!(tier.tier, "gold");
    }

    #[test]
    fn compute_tier_platinum() {
        let engine = ReferralEngine::new();
        let tier = engine.compute_tier(50, 10_000_000_000_000_000_000_000_000);
        assert_eq!(tier.tier, "platinum");
        assert_eq!(tier.fee_discount_bps, 15);
        assert_eq!(tier.reward_bps, 20);
    }

    #[test]
    fn compute_tier_enough_referrals_not_enough_volume() {
        let engine = ReferralEngine::new();
        let tier = engine.compute_tier(50, 0);
        assert_eq!(tier.tier, "bronze");
    }

    #[test]
    fn compute_tier_enough_volume_not_enough_referrals() {
        let engine = ReferralEngine::new();
        let tier = engine.compute_tier(0, 99_999_999_000_000_000_000_000_000_000);
        assert_eq!(tier.tier, "bronze");
    }

    #[test]
    fn compute_tier_between_tiers() {
        let engine = ReferralEngine::new();
        let tier = engine.compute_tier(10, 500_000_000_000_000_000_000_000);
        assert_eq!(tier.tier, "silver");
    }

    #[test]
    fn fee_discount_and_reward_bps() {
        let engine = ReferralEngine::new();
        assert_eq!(engine.fee_discount_bps(0, 0), 0);
        assert_eq!(engine.reward_bps(0, 0), 5);

        assert_eq!(engine.fee_discount_bps(5, 100_000_000_000_000_000_000_000), 5);
        assert_eq!(engine.reward_bps(5, 100_000_000_000_000_000_000_000), 10);
    }

    #[test]
    fn custom_tiers() {
        let engine = ReferralEngine::with_tiers(vec![
            ReferralTier {
                tier: "basic",
                min_referrals: 0,
                min_volume: 0,
                reward_bps: 1,
                fee_discount_bps: 0,
            },
            ReferralTier {
                tier: "pro",
                min_referrals: 10,
                min_volume: 0,
                reward_bps: 50,
                fee_discount_bps: 25,
            },
        ]);

        assert_eq!(engine.compute_tier(0, 0).tier, "basic");
        assert_eq!(engine.compute_tier(10, 0).tier, "pro");
    }

    #[test]
    fn set_tiers_at_runtime() {
        let mut engine = ReferralEngine::new();
        assert_eq!(engine.tier_count(), 4);

        engine.set_tiers(vec![ReferralTier {
            tier: "only",
            min_referrals: 0,
            min_volume: 0,
            reward_bps: 100,
            fee_discount_bps: 100,
        }]);
        assert_eq!(engine.tier_count(), 1);
        assert_eq!(engine.compute_tier(999, 999).tier, "only");
    }

    #[test]
    fn tiers_accessor() {
        let engine = ReferralEngine::new();
        let tiers = engine.tiers();
        assert_eq!(tiers.len(), 4);
        assert_eq!(tiers[0].tier, "bronze");
        assert_eq!(tiers[3].tier, "platinum");
    }
}
