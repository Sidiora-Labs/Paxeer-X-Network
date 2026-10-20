use std::collections::HashSet;

use dashmap::DashMap;

use crate::core::types::{Balance, FixedI128};

/// Composite key for the balance map: (user_address, token_address).
type BalanceKey = ([u8; 20], [u8; 20]);

/// In-memory per-user-per-token balance cache.
///
/// Replaces DragonflyDB keys:
/// - `user:balance:{addr}:{token}` → `get()`
/// - `user:tokens:{addr}` → `user_tokens()`
///
/// Concurrency: DashMap provides sharded RwLock.
pub struct BalanceCache {
    balances: DashMap<BalanceKey, Balance>,
    user_tokens: DashMap<[u8; 20], HashSet<[u8; 20]>>,
}

impl BalanceCache {
    pub fn new() -> Self {
        Self {
            balances: DashMap::new(),
            user_tokens: DashMap::new(),
        }
    }

    /// Set a full balance (from WAL replay or indexer event).
    pub fn set(&self, balance: Balance) {
        let user = balance.user_address;
        let token = balance.token;

        self.user_tokens
            .entry(user)
            .or_insert_with(HashSet::new)
            .insert(token);

        self.balances.insert((user, token), balance);
    }

    /// Set balance from raw fields.
    pub fn set_raw(
        &self,
        user: [u8; 20],
        token: [u8; 20],
        amount: FixedI128,
        locked: FixedI128,
        available: FixedI128,
    ) {
        self.set(Balance {
            user_address: user,
            token,
            amount,
            locked,
            available,
        });
    }

    /// Get balance for a (user, token) pair.
    pub fn get(&self, user: &[u8; 20], token: &[u8; 20]) -> Option<Balance> {
        self.balances.get(&(*user, *token)).map(|r| r.clone())
    }

    /// Update a balance in place. Returns `true` if it exists.
    pub fn update(
        &self,
        user: &[u8; 20],
        token: &[u8; 20],
        f: impl FnOnce(&mut Balance),
    ) -> bool {
        match self.balances.get_mut(&(*user, *token)) {
            Some(mut entry) => {
                f(entry.value_mut());
                true
            }
            None => false,
        }
    }

    /// All balances for a user.
    pub fn user_balances(&self, user: &[u8; 20]) -> Vec<Balance> {
        match self.user_tokens.get(user) {
            Some(tokens) => tokens
                .iter()
                .filter_map(|t| self.balances.get(&(*user, *t)).map(|r| r.clone()))
                .collect(),
            None => Vec::new(),
        }
    }

    /// Token addresses held by a user.
    pub fn user_tokens(&self, user: &[u8; 20]) -> Vec<[u8; 20]> {
        match self.user_tokens.get(user) {
            Some(tokens) => tokens.iter().copied().collect(),
            None => Vec::new(),
        }
    }

    /// Remove a specific balance. Returns the removed balance if it existed.
    pub fn remove(&self, user: &[u8; 20], token: &[u8; 20]) -> Option<Balance> {
        let (_, bal) = self.balances.remove(&(*user, *token))?;

        if let Some(mut tokens) = self.user_tokens.get_mut(user) {
            tokens.remove(token);
            if tokens.is_empty() {
                drop(tokens);
                self.user_tokens.remove(user);
            }
        }

        Some(bal)
    }

    /// Total number of balance entries.
    pub fn count(&self) -> usize {
        self.balances.len()
    }

    /// Number of unique users with balances.
    pub fn user_count(&self) -> usize {
        self.user_tokens.len()
    }

    /// All balances (for snapshots, diagnostics).
    pub fn all(&self) -> Vec<Balance> {
        self.balances.iter().map(|r| r.value().clone()).collect()
    }
}

impl Default for BalanceCache {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn user(id: u8) -> [u8; 20] {
        let mut buf = [0u8; 20];
        buf[19] = id;
        buf
    }

    fn token(id: u8) -> [u8; 20] {
        let mut buf = [0u8; 20];
        buf[0] = id;
        buf
    }

    fn make_balance(user_id: u8, token_id: u8, amount: i128) -> Balance {
        Balance {
            user_address: user(user_id),
            token: token(token_id),
            amount,
            locked: 0,
            available: amount,
        }
    }

    #[test]
    fn set_and_get() {
        let cache = BalanceCache::new();
        cache.set(make_balance(1, 10, 1_000_000));

        let bal = cache.get(&user(1), &token(10)).unwrap();
        assert_eq!(bal.amount, 1_000_000);
        assert_eq!(bal.available, 1_000_000);
        assert_eq!(bal.locked, 0);
    }

    #[test]
    fn get_nonexistent() {
        let cache = BalanceCache::new();
        assert!(cache.get(&user(1), &token(10)).is_none());
    }

    #[test]
    fn set_raw() {
        let cache = BalanceCache::new();
        cache.set_raw(user(1), token(10), 1_000, 200, 800);

        let bal = cache.get(&user(1), &token(10)).unwrap();
        assert_eq!(bal.amount, 1_000);
        assert_eq!(bal.locked, 200);
        assert_eq!(bal.available, 800);
    }

    #[test]
    fn set_overwrites() {
        let cache = BalanceCache::new();
        cache.set(make_balance(1, 10, 1_000));
        cache.set(make_balance(1, 10, 2_000));

        let bal = cache.get(&user(1), &token(10)).unwrap();
        assert_eq!(bal.amount, 2_000);
        assert_eq!(cache.count(), 1);
    }

    #[test]
    fn update_balance() {
        let cache = BalanceCache::new();
        cache.set(make_balance(1, 10, 1_000));

        let updated = cache.update(&user(1), &token(10), |b| {
            b.locked = 300;
            b.available = b.amount - b.locked;
        });
        assert!(updated);

        let bal = cache.get(&user(1), &token(10)).unwrap();
        assert_eq!(bal.locked, 300);
        assert_eq!(bal.available, 700);
    }

    #[test]
    fn update_nonexistent() {
        let cache = BalanceCache::new();
        assert!(!cache.update(&user(1), &token(10), |_| {}));
    }

    #[test]
    fn user_balances() {
        let cache = BalanceCache::new();
        cache.set(make_balance(1, 10, 1_000));
        cache.set(make_balance(1, 20, 2_000));
        cache.set(make_balance(2, 10, 500));

        let bals = cache.user_balances(&user(1));
        assert_eq!(bals.len(), 2);

        let total: i128 = bals.iter().map(|b| b.amount).sum();
        assert_eq!(total, 3_000);
    }

    #[test]
    fn user_tokens() {
        let cache = BalanceCache::new();
        cache.set(make_balance(1, 10, 1_000));
        cache.set(make_balance(1, 20, 2_000));

        let tokens = cache.user_tokens(&user(1));
        assert_eq!(tokens.len(), 2);
        assert!(tokens.contains(&token(10)));
        assert!(tokens.contains(&token(20)));
    }

    #[test]
    fn remove_balance() {
        let cache = BalanceCache::new();
        cache.set(make_balance(1, 10, 1_000));
        cache.set(make_balance(1, 20, 2_000));

        let removed = cache.remove(&user(1), &token(10)).unwrap();
        assert_eq!(removed.amount, 1_000);

        assert!(cache.get(&user(1), &token(10)).is_none());
        assert_eq!(cache.user_balances(&user(1)).len(), 1);
    }

    #[test]
    fn remove_last_token_cleans_user() {
        let cache = BalanceCache::new();
        cache.set(make_balance(1, 10, 1_000));

        cache.remove(&user(1), &token(10));

        assert!(cache.user_tokens(&user(1)).is_empty());
        assert_eq!(cache.user_count(), 0);
    }

    #[test]
    fn remove_nonexistent() {
        let cache = BalanceCache::new();
        assert!(cache.remove(&user(1), &token(10)).is_none());
    }

    #[test]
    fn count_and_user_count() {
        let cache = BalanceCache::new();
        assert_eq!(cache.count(), 0);
        assert_eq!(cache.user_count(), 0);

        cache.set(make_balance(1, 10, 1_000));
        cache.set(make_balance(1, 20, 2_000));
        cache.set(make_balance(2, 10, 500));

        assert_eq!(cache.count(), 3);
        assert_eq!(cache.user_count(), 2);
    }

    #[test]
    fn all_balances() {
        let cache = BalanceCache::new();
        cache.set(make_balance(1, 10, 1_000));
        cache.set(make_balance(2, 20, 2_000));

        let all = cache.all();
        assert_eq!(all.len(), 2);
    }
}
