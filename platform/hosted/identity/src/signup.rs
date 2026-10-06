use crate::store::{PendingSignup, Principal, StarterCredit, Store, SUBJECT_TENANT_CONFLICT};
use serde_json::json;
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, BTreeSet};

pub const STARTER_CREDIT_AMOUNT: &str = "10000000000000000000";
pub const STARTER_CREDIT_ASSET: &str = "PAX";
const HOUR: u64 = 3_600;
const DAY: u64 = 86_400;
const MAX_TRACKED_KEYS: usize = 65_536;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Limits {
    pub per_ip_per_hour: u32,
    pub per_email_per_hour: u32,
    pub daily_cap: u32,
    pub token_ttl_seconds: u64,
}

impl Default for Limits {
    fn default() -> Self {
        Self {
            per_ip_per_hour: 5,
            per_email_per_hour: 3,
            daily_cap: 1_000,
            token_ttl_seconds: DAY,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Refused {
    Invalid,
    DisposableDomain,
    IpRateLimited(u64),
    EmailRateLimited(u64),
    DailyCapReached(u64),
    EmailRegistered,
    SubjectConflict,
    TokenUnknown,
    TokenExpired,
    NotVerified,
    AlreadyCredited,
    GrantMismatch,
    Store,
}

impl Refused {
    #[must_use]
    pub const fn status(self) -> (u16, &'static str, Option<u64>) {
        match self {
            Self::Invalid => (400, "invalid_argument", None),
            Self::DisposableDomain => (400, "email_domain_refused", None),
            Self::IpRateLimited(after) => (429, "signup_ip_rate_limited", Some(after)),
            Self::EmailRateLimited(after) => (429, "signup_email_rate_limited", Some(after)),
            Self::DailyCapReached(after) => (429, "signup_daily_cap_reached", Some(after)),
            Self::EmailRegistered => (409, "email_registered", None),
            Self::SubjectConflict => (409, "subject_tenant_conflict", None),
            Self::TokenUnknown => (404, "verification_not_found", None),
            Self::TokenExpired => (410, "verification_expired", None),
            Self::NotVerified => (403, "verification_required", None),
            Self::AlreadyCredited => (409, "starter_credit_already_claimed", None),
            Self::GrantMismatch => (409, "starter_credit_grant_mismatch", None),
            Self::Store => (503, "store_unavailable", Some(5)),
        }
    }
}

pub struct SignupRequest<'a> {
    pub email: &'a str,
    pub client_ip: &'a str,
    pub tenant: &'a str,
    pub sub: &'a str,
    pub signer_public_key: &'a str,
}

pub struct Desk {
    limits: Limits,
    disposable: BTreeSet<String>,
    ip: BTreeMap<String, (u64, u32)>,
    email: BTreeMap<String, (u64, u32)>,
}

#[must_use]
pub fn sha256_hex(bytes: &[u8]) -> String {
    Sha256::digest(bytes)
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect()
}

/// Lowercases and checks an address; returns it with its domain.
#[must_use]
pub fn normalize_email(email: &str) -> Option<(String, String)> {
    let email = email.trim().to_ascii_lowercase();
    let (local, domain) = email.split_once('@')?;
    let labels_ok = domain.split('.').count() >= 2
        && domain.split('.').all(|label| {
            !label.is_empty()
                && label.len() <= 63
                && label
                    .bytes()
                    .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-')
        });
    let local_ok = !local.is_empty()
        && local.len() <= 64
        && local
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || b"._%+-".contains(&byte));
    let domain = domain.to_owned();
    (email.len() <= 254 && labels_ok && local_ok).then(|| (email, domain))
}

fn take(window: &mut (u64, u32), bucket: u64, limit: u32) -> bool {
    if window.0 != bucket {
        *window = (bucket, 0);
    }
    if window.1 >= limit {
        return false;
    }
    window.1 += 1;
    true
}

fn prune(map: &mut BTreeMap<String, (u64, u32)>, bucket: u64) {
    if map.len() >= MAX_TRACKED_KEYS {
        map.retain(|_, window| window.0 == bucket);
    }
}

impl Desk {
    #[must_use]
    pub fn new(limits: Limits, disposable_domains: impl IntoIterator<Item = String>) -> Self {
        Self {
            limits,
            disposable: disposable_domains
                .into_iter()
                .map(|domain| domain.trim().trim_start_matches('.').to_ascii_lowercase())
                .filter(|domain| !domain.is_empty() && !domain.starts_with('#'))
                .collect(),
            ip: BTreeMap::new(),
            email: BTreeMap::new(),
        }
    }

    #[must_use]
    pub const fn limits(&self) -> Limits {
        self.limits
    }

    fn disposable(&self, domain: &str) -> bool {
        let mut rest = domain;
        loop {
            if self.disposable.contains(rest) {
                return true;
            }
            match rest.split_once('.') {
                Some((_, parent)) if parent.contains('.') => rest = parent,
                _ => return false,
            }
        }
    }

    /// Records a pending signup whose verification token hashes to `token_digest`.
    ///
    /// # Errors
    /// Refuses malformed input, denylisted domains, exhausted limits, a verified address, or store failure.
    pub fn start(
        &mut self,
        store: &mut Store,
        request: &SignupRequest<'_>,
        token_digest: &str,
        now: u64,
    ) -> Result<PendingSignup, Refused> {
        let (email, domain) = normalize_email(request.email).ok_or(Refused::Invalid)?;
        if request.client_ip.is_empty()
            || request.client_ip.len() > 64
            || request.tenant.is_empty()
            || request.sub.is_empty()
            || request.signer_public_key.len() != 64
        {
            return Err(Refused::Invalid);
        }
        let email_digest = sha256_hex(email.as_bytes());
        let outcome = self.admit(store, &domain, request.client_ip, &email_digest, now);
        let _ = store.audit(&json!({
            "event": "signup_requested",
            "at": now,
            "email_digest": email_digest,
            "ip_digest": sha256_hex(request.client_ip.as_bytes()),
            "sub": request.sub,
            "outcome": outcome.err().map_or("accepted", |refused| refused.status().1),
        }));
        outcome?;
        let signup = PendingSignup {
            token_digest: token_digest.to_owned(),
            email_digest,
            tenant: request.tenant.to_owned(),
            sub: request.sub.to_owned(),
            signer_public_key: request.signer_public_key.to_owned(),
            created_at: now,
            expires_at: now.saturating_add(self.limits.token_ttl_seconds),
        };
        store
            .put_signup(signup.clone())
            .map_err(|_| Refused::Store)?;
        Ok(signup)
    }

    fn admit(
        &mut self,
        store: &Store,
        domain: &str,
        client_ip: &str,
        email_digest: &str,
        now: u64,
    ) -> Result<(), Refused> {
        if self.disposable(domain) {
            return Err(Refused::DisposableDomain);
        }
        if store.email_in_use(email_digest) {
            return Err(Refused::EmailRegistered);
        }
        let hour = now / HOUR;
        let next_hour = (hour + 1) * HOUR - now;
        if store.signups_on_day(now / DAY) >= self.limits.daily_cap {
            return Err(Refused::DailyCapReached((now / DAY + 1) * DAY - now));
        }
        prune(&mut self.ip, hour);
        prune(&mut self.email, hour);
        let ip = self.ip.entry(client_ip.to_owned()).or_default();
        if !take(ip, hour, self.limits.per_ip_per_hour) {
            return Err(Refused::IpRateLimited(next_hour));
        }
        let email = self.email.entry(email_digest.to_owned()).or_default();
        if !take(email, hour, self.limits.per_email_per_hour) {
            return Err(Refused::EmailRateLimited(next_hour));
        }
        Ok(())
    }
}

/// Verifies a signup token and creates its principal.
///
/// # Errors
/// Refuses unknown or expired tokens, an address verified meanwhile, a tenant conflict, or store failure.
pub fn verify(store: &mut Store, token_digest: &str, now: u64) -> Result<Principal, Refused> {
    let signup = store
        .pending_signup(token_digest)
        .ok_or(Refused::TokenUnknown)?
        .clone();
    let outcome = if signup.expires_at <= now {
        Err(Refused::TokenExpired)
    } else if store.email_in_use(&signup.email_digest) {
        Err(Refused::EmailRegistered)
    } else {
        store.verify_signup(token_digest, now).map_err(|error| {
            if error == SUBJECT_TENANT_CONFLICT {
                Refused::SubjectConflict
            } else {
                Refused::Store
            }
        })
    };
    let _ = store.audit(&json!({
        "event": "signup_verified",
        "at": now,
        "email_digest": signup.email_digest,
        "sub": signup.sub,
        "outcome": outcome.as_ref().err().map_or("accepted", |refused| refused.status().1),
    }));
    outcome
}

/// Reserves the one starter credit of a verified subject. An unsettled grant is
/// returned again so a failed funding call can be retried under the same grant.
///
/// # Errors
/// Refuses unverified subjects, a settled credit, or store failure.
pub fn grant_starter_credit(
    store: &mut Store,
    sub: &str,
    grant_id: &str,
    now: u64,
) -> Result<StarterCredit, Refused> {
    let outcome = match store.starter_credit(sub) {
        _ if store.verified_email(sub).is_none() => Err(Refused::NotVerified),
        Some(credit) if credit.settled_at.is_some() => Err(Refused::AlreadyCredited),
        Some(credit) => Ok(credit.clone()),
        None => {
            let credit = StarterCredit {
                sub: sub.to_owned(),
                grant_id: grant_id.to_owned(),
                amount: STARTER_CREDIT_AMOUNT.to_owned(),
                granted_at: now,
                settled_at: None,
            };
            store
                .put_starter_credit(credit.clone())
                .map(|()| credit)
                .map_err(|_| Refused::Store)
        }
    };
    let _ = store.audit(&json!({
        "event": "starter_credit_granted",
        "at": now,
        "sub": sub,
        "outcome": outcome.as_ref().err().map_or("accepted", |refused| refused.status().1),
    }));
    outcome
}

/// Marks the starter credit funded so no second grant is issued.
///
/// # Errors
/// Refuses an unknown or different grant, or store failure.
pub fn settle_starter_credit(
    store: &mut Store,
    sub: &str,
    grant_id: &str,
    now: u64,
) -> Result<StarterCredit, Refused> {
    let outcome = match store.starter_credit(sub) {
        Some(credit) if credit.grant_id != grant_id => Err(Refused::GrantMismatch),
        Some(credit) if credit.settled_at.is_some() => Ok(credit.clone()),
        Some(_) => store
            .settle_starter_credit(sub, now)
            .map_err(|_| Refused::Store)
            .and_then(|()| store.starter_credit(sub).cloned().ok_or(Refused::Store)),
        None => Err(Refused::NotVerified),
    };
    let _ = store.audit(&json!({
        "event": "starter_credit_settled",
        "at": now,
        "sub": sub,
        "grant_id": grant_id,
        "outcome": outcome.as_ref().err().map_or("accepted", |refused| refused.status().1),
    }));
    outcome
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;
    use std::sync::atomic::{AtomicU64, Ordering};

    const NOW: u64 = 1_790_000_000;

    fn store(name: &str) -> (Store, PathBuf) {
        static NEXT: AtomicU64 = AtomicU64::new(0);
        let root = std::env::temp_dir().join(format!(
            "identity-signup-{name}-{}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        ));
        let _ = std::fs::remove_dir_all(&root);
        (
            Store::open(&root).unwrap_or_else(|error| panic!("{error}")),
            root,
        )
    }

    fn request<'a>(email: &'a str, ip: &'a str, sub: &'a str) -> SignupRequest<'a> {
        SignupRequest {
            email,
            client_ip: ip,
            tenant: "beta",
            sub,
            signer_public_key: "ab000000000000000000000000000000000000000000000000000000000000cd",
        }
    }

    fn token(n: u32) -> String {
        sha256_hex(format!("token-{n}").as_bytes())
    }

    #[test]
    fn signup_happy_path_creates_the_principal_on_verification() {
        let (mut store, root) = store("happy");
        let mut desk = Desk::new(Limits::default(), Vec::new());
        let pending = desk
            .start(
                &mut store,
                &request(" Alice@Example.COM ", "198.51.100.7", "beta.alice"),
                &token(1),
                NOW,
            )
            .unwrap_or_else(|refused| panic!("{refused:?}"));
        assert_eq!(pending.email_digest, sha256_hex(b"alice@example.com"));
        assert_eq!(pending.expires_at, NOW + DAY);
        assert!(store.principal("beta", "beta.alice").is_none());

        let principal =
            verify(&mut store, &token(1), NOW + 60).unwrap_or_else(|refused| panic!("{refused:?}"));
        assert_eq!(principal.sub, "beta.alice");
        assert_eq!(
            principal.allowed_signer_public_keys,
            vec![request("", "", "").signer_public_key.to_owned()]
        );
        assert_eq!(store.principal("beta", "beta.alice"), Some(&principal));
        assert_eq!(
            store.verified_email("beta.alice"),
            Some(sha256_hex(b"alice@example.com").as_str())
        );
        assert_eq!(
            verify(&mut store, &token(1), NOW + 61),
            Err(Refused::TokenUnknown)
        );
        assert_eq!(
            desk.start(
                &mut store,
                &request("alice@example.com", "198.51.100.8", "beta.other"),
                &token(2),
                NOW + 62,
            ),
            Err(Refused::EmailRegistered)
        );

        drop(store);
        let reopened = Store::open(&root).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(reopened.principal("beta", "beta.alice"), Some(&principal));
        assert!(reopened.email_in_use(&sha256_hex(b"alice@example.com")));
        let audit = std::fs::read_to_string(root.join("audit.log"))
            .unwrap_or_else(|error| panic!("{error}"));
        assert!(audit.contains("\"event\":\"signup_requested\""));
        assert!(audit.contains("\"event\":\"signup_verified\""));
        assert!(!audit.contains("alice@example.com"));
        assert!(!audit.contains("198.51.100.7"));
    }

    #[test]
    fn signup_rate_limits_per_ip_per_email_and_per_day() {
        let (mut store, _root) = store("limits");
        let limits = Limits {
            per_ip_per_hour: 2,
            per_email_per_hour: 1,
            daily_cap: 4,
            token_ttl_seconds: DAY,
        };
        let mut desk = Desk::new(limits, vec!["mailinator.com".to_owned()]);
        let mut n = 0;
        let mut start = |desk: &mut Desk, store: &mut Store, email: &str, ip: &str, at: u64| {
            n += 1;
            desk.start(store, &request(email, ip, "beta.sub"), &token(n), at)
                .map(|_| ())
        };
        assert_eq!(
            start(&mut desk, &mut store, "a@example.com", "ip-1", NOW),
            Ok(())
        );
        assert_eq!(
            start(&mut desk, &mut store, "a@example.com", "ip-2", NOW),
            Err(Refused::EmailRateLimited(HOUR - NOW % HOUR))
        );
        assert_eq!(
            start(&mut desk, &mut store, "b@example.com", "ip-1", NOW),
            Ok(())
        );
        assert_eq!(
            start(&mut desk, &mut store, "c@example.com", "ip-1", NOW),
            Err(Refused::IpRateLimited(HOUR - NOW % HOUR))
        );
        assert_eq!(
            start(&mut desk, &mut store, "d@sub.mailinator.com", "ip-3", NOW),
            Err(Refused::DisposableDomain)
        );
        assert_eq!(
            start(&mut desk, &mut store, "not-an-address", "ip-3", NOW),
            Err(Refused::Invalid)
        );
        let later = NOW - NOW % HOUR + HOUR;
        assert_eq!(later / DAY, NOW / DAY);
        assert_eq!(
            start(&mut desk, &mut store, "c@example.com", "ip-1", later),
            Ok(())
        );
        assert_eq!(
            start(&mut desk, &mut store, "e@example.com", "ip-4", later),
            Ok(())
        );
        assert_eq!(
            start(&mut desk, &mut store, "f@example.com", "ip-5", later),
            Err(Refused::DailyCapReached(DAY - later % DAY))
        );
        let tomorrow = (NOW / DAY + 1) * DAY;
        assert_eq!(
            start(&mut desk, &mut store, "f@example.com", "ip-5", tomorrow),
            Ok(())
        );
    }

    #[test]
    fn signup_verification_is_required_and_expires() {
        let (mut store, _root) = store("verify");
        let mut desk = Desk::new(Limits::default(), Vec::new());
        desk.start(
            &mut store,
            &request("v@example.com", "ip", "beta.v"),
            &token(1),
            NOW,
        )
        .unwrap_or_else(|refused| panic!("{refused:?}"));
        assert_eq!(
            grant_starter_credit(&mut store, "beta.v", "grant-1", NOW),
            Err(Refused::NotVerified)
        );
        assert!(store.starter_credit("beta.v").is_none());
        assert_eq!(
            verify(&mut store, &token(1), NOW + DAY),
            Err(Refused::TokenExpired)
        );
        assert!(store.principal("beta", "beta.v").is_none());
        assert_eq!(
            verify(&mut store, &token(9), NOW),
            Err(Refused::TokenUnknown)
        );
    }

    #[test]
    fn signup_starter_credit_is_granted_once() {
        let (mut store, root) = store("credit");
        let mut desk = Desk::new(Limits::default(), Vec::new());
        desk.start(
            &mut store,
            &request("c@example.com", "ip", "beta.c"),
            &token(1),
            NOW,
        )
        .unwrap_or_else(|refused| panic!("{refused:?}"));
        verify(&mut store, &token(1), NOW + 1).unwrap_or_else(|refused| panic!("{refused:?}"));

        let grant = grant_starter_credit(&mut store, "beta.c", "grant-1", NOW + 2)
            .unwrap_or_else(|refused| panic!("{refused:?}"));
        assert_eq!(grant.amount, STARTER_CREDIT_AMOUNT);
        assert_eq!(grant.settled_at, None);
        let retried = grant_starter_credit(&mut store, "beta.c", "grant-2", NOW + 3)
            .unwrap_or_else(|refused| panic!("{refused:?}"));
        assert_eq!(
            retried.grant_id, "grant-1",
            "an unsettled grant is reused, never doubled"
        );
        assert_eq!(
            settle_starter_credit(&mut store, "beta.c", "grant-2", NOW + 4),
            Err(Refused::GrantMismatch)
        );
        let settled = settle_starter_credit(&mut store, "beta.c", "grant-1", NOW + 4)
            .unwrap_or_else(|refused| panic!("{refused:?}"));
        assert_eq!(settled.settled_at, Some(NOW + 4));
        assert_eq!(
            grant_starter_credit(&mut store, "beta.c", "grant-3", NOW + 5),
            Err(Refused::AlreadyCredited)
        );

        drop(store);
        let mut reopened = Store::open(&root).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(reopened.starter_credit("beta.c"), Some(&settled));
        assert_eq!(
            grant_starter_credit(&mut reopened, "beta.c", "grant-4", NOW + 6),
            Err(Refused::AlreadyCredited)
        );
    }
}
