use dashmap::DashMap;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ApiKey {
    pub id: String,
    pub address: [u8; 20],
    pub key_hash: String,
    pub label: Option<String>,
    pub rate_limit_rpm: u32,
    pub last_used_at: Option<u64>,
    pub expires_at: Option<u64>,
    pub created_at: u64,
}

pub struct ApiKeyStore {
    primary: DashMap<String, ApiKey>,
    by_user: DashMap<[u8; 20], Vec<String>>,
    by_hash: DashMap<String, String>,
}

impl ApiKeyStore {
    pub fn new() -> Self {
        Self {
            primary: DashMap::new(),
            by_user: DashMap::new(),
            by_hash: DashMap::new(),
        }
    }

    pub fn insert(&self, key: ApiKey) {
        let id = key.id.clone();
        let addr = key.address;
        let hash = key.key_hash.clone();
        self.by_hash.insert(hash, id.clone());
        self.by_user.entry(addr).or_default().push(id.clone());
        self.primary.insert(id, key);
    }

    pub fn get(&self, id: &str) -> Option<ApiKey> {
        self.primary.get(id).map(|r| r.clone())
    }

    pub fn get_by_hash(&self, hash: &str) -> Option<ApiKey> {
        let id = self.by_hash.get(hash)?;
        self.primary.get(id.value().as_str()).map(|r| r.clone())
    }

    pub fn by_user(&self, address: &[u8; 20]) -> Vec<ApiKey> {
        let ids = self.by_user.get(address);
        match ids {
            Some(ids) => ids
                .iter()
                .filter_map(|id| self.primary.get(id.as_str()).map(|r| r.clone()))
                .collect(),
            None => vec![],
        }
    }

    pub fn remove(&self, id: &str) -> Option<ApiKey> {
        if let Some((_, key)) = self.primary.remove(id) {
            self.by_hash.remove(&key.key_hash);
            self.by_user.entry(key.address).and_modify(|v| {
                v.retain(|i| i != id);
            });
            Some(key)
        } else {
            None
        }
    }

    pub fn remove_by_user_and_id(&self, address: &[u8; 20], id: &str) -> Option<ApiKey> {
        if let Some(key) = self.primary.get(id) {
            if key.address != *address {
                return None;
            }
        } else {
            return None;
        }
        self.remove(id)
    }

    pub fn touch_last_used(&self, id: &str, timestamp: u64) -> bool {
        if let Some(mut key) = self.primary.get_mut(id) {
            key.last_used_at = Some(timestamp);
            true
        } else {
            false
        }
    }

    pub fn all(&self) -> Vec<ApiKey> {
        self.primary.iter().map(|r| r.value().clone()).collect()
    }

    pub fn count(&self) -> usize {
        self.primary.len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_addr(n: u8) -> [u8; 20] {
        let mut a = [0u8; 20];
        a[19] = n;
        a
    }

    fn make_key(addr: [u8; 20], id: &str, hash: &str) -> ApiKey {
        ApiKey {
            id: id.to_string(),
            address: addr,
            key_hash: hash.to_string(),
            label: Some("test".to_string()),
            rate_limit_rpm: 60,
            last_used_at: None,
            expires_at: None,
            created_at: 1000,
        }
    }

    #[test]
    fn insert_and_get() {
        let store = ApiKeyStore::new();
        store.insert(make_key(test_addr(1), "uuid-1", "hash-1"));
        let key = store.get("uuid-1").unwrap();
        assert_eq!(key.key_hash, "hash-1");
    }

    #[test]
    fn get_by_hash() {
        let store = ApiKeyStore::new();
        store.insert(make_key(test_addr(1), "uuid-1", "hash-abc"));
        let key = store.get_by_hash("hash-abc").unwrap();
        assert_eq!(key.id, "uuid-1");
    }

    #[test]
    fn by_user() {
        let store = ApiKeyStore::new();
        let addr = test_addr(1);
        store.insert(make_key(addr, "k1", "h1"));
        store.insert(make_key(addr, "k2", "h2"));
        store.insert(make_key(test_addr(2), "k3", "h3"));
        assert_eq!(store.by_user(&addr).len(), 2);
    }

    #[test]
    fn remove() {
        let store = ApiKeyStore::new();
        store.insert(make_key(test_addr(1), "uuid-1", "hash-1"));
        assert!(store.remove("uuid-1").is_some());
        assert!(store.get("uuid-1").is_none());
        assert!(store.get_by_hash("hash-1").is_none());
    }

    #[test]
    fn remove_wrong_user() {
        let store = ApiKeyStore::new();
        store.insert(make_key(test_addr(1), "uuid-1", "hash-1"));
        assert!(store.remove_by_user_and_id(&test_addr(2), "uuid-1").is_none());
        assert!(store.get("uuid-1").is_some());
    }

    #[test]
    fn touch_last_used() {
        let store = ApiKeyStore::new();
        store.insert(make_key(test_addr(1), "uuid-1", "hash-1"));
        assert!(store.touch_last_used("uuid-1", 5000));
        assert_eq!(store.get("uuid-1").unwrap().last_used_at, Some(5000));
    }
}
