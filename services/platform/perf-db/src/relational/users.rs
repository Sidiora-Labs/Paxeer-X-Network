use dashmap::DashMap;

use crate::core::types::User;

/// In-memory user store, replacing PostgreSQL `users` table.
///
/// Primary key: `address: [u8; 20]`.
/// Secondary index: `referral_code` → `address` (for referral lookups).
///
/// Concurrency: DashMap provides sharded RwLock — concurrent reads/writes
/// without external synchronization.
pub struct UserStore {
    users: DashMap<[u8; 20], User>,
    by_referral_code: DashMap<String, [u8; 20]>,
}

impl UserStore {
    pub fn new() -> Self {
        Self {
            users: DashMap::new(),
            by_referral_code: DashMap::new(),
        }
    }

    /// Insert or replace a user. Updates referral code index.
    pub fn upsert(&self, user: User) {
        let addr = user.address;

        if let Some(ref code) = user.referral_code {
            if !code.is_empty() {
                self.by_referral_code.insert(code.clone(), addr);
            }
        }

        self.users.insert(addr, user);
    }

    /// Get user by address. O(1).
    pub fn get(&self, address: &[u8; 20]) -> Option<User> {
        self.users.get(address).map(|r| r.clone())
    }

    /// Update a user in place. Returns `true` if the user exists.
    /// Callback receives a mutable reference under the DashMap shard lock.
    pub fn update(&self, address: &[u8; 20], f: impl FnOnce(&mut User)) -> bool {
        match self.users.get_mut(address) {
            Some(mut entry) => {
                let old_code = entry.referral_code.clone();
                f(entry.value_mut());

                let new_code = entry.referral_code.clone();
                drop(entry);

                if old_code != new_code {
                    if let Some(old) = old_code {
                        if !old.is_empty() {
                            self.by_referral_code.remove(&old);
                        }
                    }
                    if let Some(ref new) = new_code {
                        if !new.is_empty() {
                            self.by_referral_code.insert(new.clone(), *address);
                        }
                    }
                }
                true
            }
            None => false,
        }
    }

    /// Ensure a user exists. If not, create with defaults (matching PG upsert_user).
    pub fn ensure(&self, address: [u8; 20], now: u64) {
        self.users.entry(address).or_insert_with(|| User {
            address,
            vault_address: None,
            referral_code: None,
            referred_by: None,
            tier: "bronze".to_string(),
            fee_discount_bps: 0,
            created_at: now,
            first_trade_at: None,
            last_active_at: Some(now),
        });

        if let Some(mut entry) = self.users.get_mut(&address) {
            entry.last_active_at = Some(now);
        }
    }

    /// Look up a user by referral code. O(1).
    pub fn by_referral_code(&self, code: &str) -> Option<User> {
        let addr = self.by_referral_code.get(code)?;
        self.get(addr.value())
    }

    /// Set a user's vault address.
    pub fn set_vault(&self, address: &[u8; 20], vault: [u8; 20]) {
        if let Some(mut entry) = self.users.get_mut(address) {
            entry.vault_address = Some(vault);
        }
    }

    /// Set a user's referral code. Returns `false` if user not found.
    pub fn set_referral_code(&self, address: &[u8; 20], code: String) -> bool {
        self.update(address, |u| {
            u.referral_code = Some(code);
        })
    }

    /// Set the referrer (referred_by). Returns `false` if user not found.
    pub fn set_referred_by(&self, address: &[u8; 20], referrer: [u8; 20]) -> bool {
        if let Some(mut entry) = self.users.get_mut(address) {
            entry.referred_by = Some(referrer);
            true
        } else {
            false
        }
    }

    /// Count users referred by a given address.
    pub fn referral_count(&self, referrer: &[u8; 20]) -> u32 {
        self.users
            .iter()
            .filter(|r| r.value().referred_by.as_ref() == Some(referrer))
            .count() as u32
    }

    /// Remove a user. Returns the removed user.
    pub fn remove(&self, address: &[u8; 20]) -> Option<User> {
        let (_, user) = self.users.remove(address)?;
        if let Some(ref code) = user.referral_code {
            if !code.is_empty() {
                self.by_referral_code.remove(code);
            }
        }
        Some(user)
    }

    /// Total number of users.
    pub fn count(&self) -> usize {
        self.users.len()
    }

    /// All users (for snapshots, diagnostics).
    pub fn all(&self) -> Vec<User> {
        self.users.iter().map(|r| r.value().clone()).collect()
    }

    /// Check if a user exists.
    pub fn contains(&self, address: &[u8; 20]) -> bool {
        self.users.contains_key(address)
    }

    /// Users created within a time range [from, to). For daily stats.
    pub fn created_between(&self, from: u64, to: u64) -> Vec<User> {
        self.users
            .iter()
            .filter(|r| r.value().created_at >= from && r.value().created_at < to)
            .map(|r| r.value().clone())
            .collect()
    }
}

impl Default for UserStore {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn addr(id: u8) -> [u8; 20] {
        let mut buf = [0u8; 20];
        buf[19] = id;
        buf
    }

    fn make_user(id: u8) -> User {
        User {
            address: addr(id),
            vault_address: None,
            referral_code: None,
            referred_by: None,
            tier: "bronze".to_string(),
            fee_discount_bps: 0,
            created_at: 1700000000,
            first_trade_at: None,
            last_active_at: None,
        }
    }

    #[test]
    fn upsert_and_get() {
        let store = UserStore::new();
        store.upsert(make_user(1));

        let user = store.get(&addr(1)).unwrap();
        assert_eq!(user.address, addr(1));
        assert_eq!(user.tier, "bronze");
    }

    #[test]
    fn get_nonexistent() {
        let store = UserStore::new();
        assert!(store.get(&addr(99)).is_none());
    }

    #[test]
    fn ensure_creates_if_missing() {
        let store = UserStore::new();
        store.ensure(addr(1), 1700000000);

        let user = store.get(&addr(1)).unwrap();
        assert_eq!(user.tier, "bronze");
        assert_eq!(user.last_active_at, Some(1700000000));
    }

    #[test]
    fn ensure_touches_last_active() {
        let store = UserStore::new();
        store.ensure(addr(1), 1700000000);
        store.ensure(addr(1), 1700001000);

        let user = store.get(&addr(1)).unwrap();
        assert_eq!(user.last_active_at, Some(1700001000));
        assert_eq!(store.count(), 1);
    }

    #[test]
    fn update_user() {
        let store = UserStore::new();
        store.upsert(make_user(1));

        let updated = store.update(&addr(1), |u| {
            u.tier = "silver".to_string();
            u.fee_discount_bps = 10;
        });
        assert!(updated);

        let user = store.get(&addr(1)).unwrap();
        assert_eq!(user.tier, "silver");
        assert_eq!(user.fee_discount_bps, 10);
    }

    #[test]
    fn update_nonexistent() {
        let store = UserStore::new();
        assert!(!store.update(&addr(99), |_| {}));
    }

    #[test]
    fn referral_code_index() {
        let store = UserStore::new();
        let mut user = make_user(1);
        user.referral_code = Some("ABC123".to_string());
        store.upsert(user);

        let found = store.by_referral_code("ABC123").unwrap();
        assert_eq!(found.address, addr(1));
        assert!(store.by_referral_code("XYZ999").is_none());
    }

    #[test]
    fn set_referral_code() {
        let store = UserStore::new();
        store.upsert(make_user(1));

        assert!(store.set_referral_code(&addr(1), "CODE1".to_string()));

        let found = store.by_referral_code("CODE1").unwrap();
        assert_eq!(found.address, addr(1));
    }

    #[test]
    fn set_referred_by() {
        let store = UserStore::new();
        store.upsert(make_user(1));
        store.upsert(make_user(2));

        assert!(store.set_referred_by(&addr(2), addr(1)));

        let user2 = store.get(&addr(2)).unwrap();
        assert_eq!(user2.referred_by, Some(addr(1)));
    }

    #[test]
    fn referral_count() {
        let store = UserStore::new();
        store.upsert(make_user(1));

        let mut u2 = make_user(2);
        u2.referred_by = Some(addr(1));
        store.upsert(u2);

        let mut u3 = make_user(3);
        u3.referred_by = Some(addr(1));
        store.upsert(u3);

        let mut u4 = make_user(4);
        u4.referred_by = Some(addr(2));
        store.upsert(u4);

        assert_eq!(store.referral_count(&addr(1)), 2);
        assert_eq!(store.referral_count(&addr(2)), 1);
        assert_eq!(store.referral_count(&addr(99)), 0);
    }

    #[test]
    fn set_vault() {
        let store = UserStore::new();
        store.upsert(make_user(1));
        store.set_vault(&addr(1), addr(99));

        let user = store.get(&addr(1)).unwrap();
        assert_eq!(user.vault_address, Some(addr(99)));
    }

    #[test]
    fn remove_user() {
        let store = UserStore::new();
        let mut user = make_user(1);
        user.referral_code = Some("DEL1".to_string());
        store.upsert(user);

        let removed = store.remove(&addr(1)).unwrap();
        assert_eq!(removed.address, addr(1));
        assert!(store.get(&addr(1)).is_none());
        assert!(store.by_referral_code("DEL1").is_none());
    }

    #[test]
    fn remove_nonexistent() {
        let store = UserStore::new();
        assert!(store.remove(&addr(99)).is_none());
    }

    #[test]
    fn count() {
        let store = UserStore::new();
        assert_eq!(store.count(), 0);

        store.upsert(make_user(1));
        store.upsert(make_user(2));
        assert_eq!(store.count(), 2);
    }

    #[test]
    fn all_users() {
        let store = UserStore::new();
        store.upsert(make_user(1));
        store.upsert(make_user(2));
        assert_eq!(store.all().len(), 2);
    }

    #[test]
    fn contains() {
        let store = UserStore::new();
        assert!(!store.contains(&addr(1)));
        store.upsert(make_user(1));
        assert!(store.contains(&addr(1)));
    }

    #[test]
    fn created_between() {
        let store = UserStore::new();
        let mut u1 = make_user(1);
        u1.created_at = 100;
        let mut u2 = make_user(2);
        u2.created_at = 200;
        let mut u3 = make_user(3);
        u3.created_at = 300;
        store.upsert(u1);
        store.upsert(u2);
        store.upsert(u3);

        let range = store.created_between(100, 300);
        assert_eq!(range.len(), 2);
    }

    #[test]
    fn referral_code_update_reindexes() {
        let store = UserStore::new();
        let mut user = make_user(1);
        user.referral_code = Some("OLD".to_string());
        store.upsert(user);

        assert!(store.by_referral_code("OLD").is_some());

        store.update(&addr(1), |u| {
            u.referral_code = Some("NEW".to_string());
        });

        assert!(store.by_referral_code("OLD").is_none());
        assert!(store.by_referral_code("NEW").is_some());
    }
}
