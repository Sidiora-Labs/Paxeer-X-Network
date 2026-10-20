use std::sync::atomic::{AtomicU64, Ordering};

use dashmap::DashMap;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Alert {
    pub id: u64,
    pub address: [u8; 20],
    pub alert_type: String,
    pub market_id: Option<u16>,
    pub threshold: Option<String>,
    pub enabled: bool,
    pub triggered_at: Option<u64>,
    pub created_at: u64,
}

pub struct AlertStore {
    primary: DashMap<u64, Alert>,
    by_user: DashMap<[u8; 20], Vec<u64>>,
    next_id: AtomicU64,
}

impl AlertStore {
    pub fn new() -> Self {
        Self {
            primary: DashMap::new(),
            by_user: DashMap::new(),
            next_id: AtomicU64::new(1),
        }
    }

    pub fn insert(&self, mut alert: Alert) -> u64 {
        let id = self.next_id.fetch_add(1, Ordering::Relaxed);
        alert.id = id;
        let addr = alert.address;
        self.primary.insert(id, alert);
        self.by_user.entry(addr).or_default().push(id);
        id
    }

    pub fn get(&self, id: u64) -> Option<Alert> {
        self.primary.get(&id).map(|r| r.clone())
    }

    pub fn by_user(&self, address: &[u8; 20]) -> Vec<Alert> {
        let ids = self.by_user.get(address);
        match ids {
            Some(ids) => ids
                .iter()
                .filter_map(|id| self.primary.get(id).map(|r| r.clone()))
                .collect(),
            None => vec![],
        }
    }

    pub fn remove(&self, id: u64) -> Option<Alert> {
        if let Some((_, alert)) = self.primary.remove(&id) {
            self.by_user.entry(alert.address).and_modify(|v| {
                v.retain(|i| *i != id);
            });
            Some(alert)
        } else {
            None
        }
    }

    pub fn remove_by_user_and_id(&self, address: &[u8; 20], id: u64) -> Option<Alert> {
        if let Some(alert) = self.primary.get(&id) {
            if alert.address != *address {
                return None;
            }
        } else {
            return None;
        }
        self.remove(id)
    }

    pub fn update_triggered(&self, id: u64, timestamp: u64) -> bool {
        if let Some(mut alert) = self.primary.get_mut(&id) {
            alert.triggered_at = Some(timestamp);
            true
        } else {
            false
        }
    }

    pub fn set_enabled(&self, id: u64, enabled: bool) -> bool {
        if let Some(mut alert) = self.primary.get_mut(&id) {
            alert.enabled = enabled;
            true
        } else {
            false
        }
    }

    pub fn all(&self) -> Vec<Alert> {
        self.primary.iter().map(|r| r.value().clone()).collect()
    }

    pub fn count(&self) -> usize {
        self.primary.len()
    }

    pub fn set_next_id(&self, id: u64) {
        self.next_id.store(id, Ordering::Relaxed);
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

    fn make_alert(addr: [u8; 20], atype: &str) -> Alert {
        Alert {
            id: 0,
            address: addr,
            alert_type: atype.to_string(),
            market_id: Some(0),
            threshold: Some("67000".to_string()),
            enabled: true,
            triggered_at: None,
            created_at: 1000,
        }
    }

    #[test]
    fn insert_and_get() {
        let store = AlertStore::new();
        let id = store.insert(make_alert(test_addr(1), "price_above"));
        assert_eq!(id, 1);
        let alert = store.get(id).unwrap();
        assert_eq!(alert.alert_type, "price_above");
    }

    #[test]
    fn by_user() {
        let store = AlertStore::new();
        let addr = test_addr(1);
        store.insert(make_alert(addr, "price_above"));
        store.insert(make_alert(addr, "price_below"));
        store.insert(make_alert(test_addr(2), "liquidation_warning"));
        let alerts = store.by_user(&addr);
        assert_eq!(alerts.len(), 2);
    }

    #[test]
    fn remove() {
        let store = AlertStore::new();
        let id = store.insert(make_alert(test_addr(1), "price_above"));
        assert!(store.remove(id).is_some());
        assert!(store.get(id).is_none());
        assert_eq!(store.count(), 0);
    }

    #[test]
    fn remove_by_user_and_id_wrong_user() {
        let store = AlertStore::new();
        let id = store.insert(make_alert(test_addr(1), "price_above"));
        assert!(store.remove_by_user_and_id(&test_addr(2), id).is_none());
        assert!(store.get(id).is_some());
    }

    #[test]
    fn update_triggered() {
        let store = AlertStore::new();
        let id = store.insert(make_alert(test_addr(1), "price_above"));
        assert!(store.update_triggered(id, 2000));
        assert_eq!(store.get(id).unwrap().triggered_at, Some(2000));
    }

    #[test]
    fn set_enabled() {
        let store = AlertStore::new();
        let id = store.insert(make_alert(test_addr(1), "price_above"));
        assert!(store.set_enabled(id, false));
        assert!(!store.get(id).unwrap().enabled);
    }

    #[test]
    fn auto_increment_ids() {
        let store = AlertStore::new();
        let id1 = store.insert(make_alert(test_addr(1), "a"));
        let id2 = store.insert(make_alert(test_addr(1), "b"));
        assert_eq!(id1, 1);
        assert_eq!(id2, 2);
    }
}
