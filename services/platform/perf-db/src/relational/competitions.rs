use dashmap::DashMap;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Competition {
    pub id: String,
    pub name: String,
    pub description: Option<String>,
    pub start_at: u64,
    pub end_at: u64,
    pub metric: String,
    pub prize_pool: Option<String>,
    pub active: bool,
    pub created_at: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CompetitionEntry {
    pub competition_id: String,
    pub address: [u8; 20],
    pub score: i128,
    pub rank: Option<u32>,
    pub joined_at: u64,
}

pub struct CompetitionStore {
    competitions: DashMap<String, Competition>,
    entries: DashMap<String, Vec<CompetitionEntry>>,
    by_user: DashMap<[u8; 20], Vec<String>>,
}

impl CompetitionStore {
    pub fn new() -> Self {
        Self {
            competitions: DashMap::new(),
            entries: DashMap::new(),
            by_user: DashMap::new(),
        }
    }

    pub fn insert_competition(&self, comp: Competition) {
        self.competitions.insert(comp.id.clone(), comp);
    }

    pub fn get_competition(&self, id: &str) -> Option<Competition> {
        self.competitions.get(id).map(|r| r.clone())
    }

    pub fn list_competitions(&self, limit: usize) -> Vec<Competition> {
        let mut comps: Vec<Competition> = self
            .competitions
            .iter()
            .map(|r| r.value().clone())
            .collect();
        comps.sort_by(|a, b| b.start_at.cmp(&a.start_at));
        comps.truncate(limit);
        comps
    }

    pub fn join(&self, competition_id: &str, address: [u8; 20], timestamp: u64) -> bool {
        if !self.competitions.contains_key(competition_id) {
            return false;
        }
        let mut entries = self
            .entries
            .entry(competition_id.to_string())
            .or_default();
        if entries.iter().any(|e| e.address == address) {
            return false;
        }
        entries.push(CompetitionEntry {
            competition_id: competition_id.to_string(),
            address,
            score: 0,
            rank: None,
            joined_at: timestamp,
        });
        self.by_user
            .entry(address)
            .or_default()
            .push(competition_id.to_string());
        true
    }

    pub fn get_entries(&self, competition_id: &str, limit: usize) -> Vec<CompetitionEntry> {
        match self.entries.get(competition_id) {
            Some(entries) => {
                let mut sorted = entries.clone();
                sorted.sort_by(|a, b| b.score.cmp(&a.score));
                sorted.truncate(limit);
                sorted
            }
            None => vec![],
        }
    }

    pub fn update_score(&self, competition_id: &str, address: &[u8; 20], score: i128) -> bool {
        if let Some(mut entries) = self.entries.get_mut(competition_id) {
            if let Some(entry) = entries.iter_mut().find(|e| &e.address == address) {
                entry.score = score;
                return true;
            }
        }
        false
    }

    pub fn user_competitions(&self, address: &[u8; 20]) -> Vec<String> {
        self.by_user
            .get(address)
            .map(|r| r.clone())
            .unwrap_or_default()
    }

    pub fn all_competitions(&self) -> Vec<Competition> {
        self.competitions
            .iter()
            .map(|r| r.value().clone())
            .collect()
    }

    pub fn all_entries(&self) -> Vec<CompetitionEntry> {
        self.entries
            .iter()
            .flat_map(|r| r.value().clone())
            .collect()
    }

    pub fn competition_count(&self) -> usize {
        self.competitions.len()
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

    fn make_comp(id: &str) -> Competition {
        Competition {
            id: id.to_string(),
            name: format!("Competition {id}"),
            description: None,
            start_at: 1000,
            end_at: 2000,
            metric: "pnl".to_string(),
            prize_pool: Some("10000".to_string()),
            active: true,
            created_at: 900,
        }
    }

    #[test]
    fn insert_and_get() {
        let store = CompetitionStore::new();
        store.insert_competition(make_comp("c1"));
        let comp = store.get_competition("c1").unwrap();
        assert_eq!(comp.name, "Competition c1");
    }

    #[test]
    fn list_sorted_by_start() {
        let store = CompetitionStore::new();
        let mut c1 = make_comp("c1");
        c1.start_at = 500;
        let mut c2 = make_comp("c2");
        c2.start_at = 1500;
        store.insert_competition(c1);
        store.insert_competition(c2);
        let list = store.list_competitions(10);
        assert_eq!(list[0].id, "c2");
    }

    #[test]
    fn join_and_get_entries() {
        let store = CompetitionStore::new();
        store.insert_competition(make_comp("c1"));
        assert!(store.join("c1", test_addr(1), 1000));
        assert!(store.join("c1", test_addr(2), 1001));
        assert!(!store.join("c1", test_addr(1), 1002));
        let entries = store.get_entries("c1", 10);
        assert_eq!(entries.len(), 2);
    }

    #[test]
    fn join_nonexistent_competition() {
        let store = CompetitionStore::new();
        assert!(!store.join("nonexistent", test_addr(1), 1000));
    }

    #[test]
    fn update_score_and_rank() {
        let store = CompetitionStore::new();
        store.insert_competition(make_comp("c1"));
        store.join("c1", test_addr(1), 1000);
        store.join("c1", test_addr(2), 1001);
        store.update_score("c1", &test_addr(1), 500);
        store.update_score("c1", &test_addr(2), 1000);
        let entries = store.get_entries("c1", 10);
        assert_eq!(entries[0].address, test_addr(2));
        assert_eq!(entries[0].score, 1000);
    }

    #[test]
    fn user_competitions() {
        let store = CompetitionStore::new();
        store.insert_competition(make_comp("c1"));
        store.insert_competition(make_comp("c2"));
        store.join("c1", test_addr(1), 1000);
        store.join("c2", test_addr(1), 1001);
        let comps = store.user_competitions(&test_addr(1));
        assert_eq!(comps.len(), 2);
    }
}
