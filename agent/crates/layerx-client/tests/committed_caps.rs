use layerx_client::budget::{
    budget_state_key, ProtocolBudgetRecord, ReconcileError, BUDGET_MODULE_ID,
};
use layerx_client::grants::{grant_state_key, CommittedGrant, GRANT_MODULE_ID};
use serde_json::Value;

fn vectors() -> Value {
    let path =
        std::env::var("LAYERX_CAPS_VECTORS").expect("native vector producer output required");
    serde_json::from_slice(&std::fs::read(path).expect("native vectors readable"))
        .expect("native JSON")
}
fn bytes(v: &Value, name: &str) -> Vec<u8> {
    let s = v[name].as_str().expect("native field");
    assert_eq!(s.len() % 2, 0);
    (0..s.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&s[i..i + 2], 16).expect("hex"))
        .collect()
}

#[test]
fn native_budget_versions_fields_and_extremes() {
    let v = vectors();
    assert_eq!(BUDGET_MODULE_ID, 3);
    for name in ["budget_v1", "budget_v2"] {
        let r = ProtocolBudgetRecord::decode(&bytes(&v, name)).expect("C budget codec");
        assert_eq!(budget_state_key(r.budget_id), bytes(&v, "budget_key"));
        assert_eq!(
            (r.owner, r.budget_account, r.asset_id, r.purpose_hash),
            ([2; 32], [3; 32], [4; 32], [5; 32])
        );
        assert_eq!(
            (
                r.per_period_limit,
                r.configured_period_limit,
                r.spent_this_period,
                r.carry_cap,
                r.carried
            ),
            (100, 100, 23, 0, 0)
        );
        assert_eq!(
            (
                r.period_length,
                r.period_start,
                r.expiry,
                r.revocation_sequence
            ),
            (1000, 1000, 9000, 31)
        );
        assert_eq!((r.remaining(), r.window_end_sequence()), (77, 2000));
        assert_eq!(r.delegates, vec![[6; 32], [7; 32]]);
        assert_eq!(
            r.source_account,
            if name == "budget_v2" {
                Some([8; 32])
            } else {
                None
            }
        );
        assert!(!r.closed && !r.revoked);
    }
    let r = ProtocolBudgetRecord::decode(&bytes(&v, "budget_extreme")).expect("native max fields");
    assert_eq!(r.delegates.len(), 16);
    assert_eq!(
        (
            r.per_period_limit,
            r.configured_period_limit,
            r.carry_cap,
            r.spent_this_period,
            r.carried
        ),
        (u128::MAX, u128::MAX, u128::MAX, u128::MAX, u128::MAX)
    );
    assert_eq!(
        (r.period_length, r.expiry, r.revocation_sequence),
        (u64::MAX, u64::MAX, u64::MAX)
    );
    assert_eq!((r.remaining(), r.window_end_sequence()), (0, u64::MAX));
    assert!(r.closed && r.revoked);
}

#[test]
fn budget_refuses_malformed_and_arithmetic_bounds() {
    let v = vectors();
    for name in ["budget_v1", "budget_v2", "budget_extreme"] {
        let b = bytes(&v, name);
        let key = bytes(&v, "budget_key");
        assert!(ProtocolBudgetRecord::decode_state(&key, &b).is_ok());
        let mut wrong = key.clone();
        wrong[7] ^= 1;
        assert!(ProtocolBudgetRecord::decode_state(&wrong, &b).is_err());
        assert!(ProtocolBudgetRecord::decode_state(&key[..38], &b).is_err());
        for length in 0..b.len() {
            assert!(
                ProtocolBudgetRecord::decode(&b[..length]).is_err(),
                "{name} truncated at {length}"
            );
        }
        let mut x = b.clone();
        x.push(0);
        assert!(ProtocolBudgetRecord::decode(&x).is_err());
        for (offset, value) in [(0, 1), (1, 0), (1, 3), (275, 2), (276, 2), (277, 17)] {
            let mut x = b.clone();
            x[offset] = value;
            assert!(ProtocolBudgetRecord::decode(&x).is_err(), "offset {offset}");
        }
        let mut x = b.clone();
        x[2..34].fill(0);
        assert!(ProtocolBudgetRecord::decode(&x).is_err());
        let mut x = b.clone();
        x[242..250].fill(0);
        assert!(ProtocolBudgetRecord::decode(&x).is_err());
        let mut x = b.clone();
        x[250..258].fill(255);
        assert!(ProtocolBudgetRecord::decode(&x).is_err());
        let mut x = b.clone();
        x[162..178].fill(0);
        assert!(ProtocolBudgetRecord::decode(&x).is_err());
        let mut x = b.clone();
        x[310..342].copy_from_slice(&b[278..310]);
        assert!(ProtocolBudgetRecord::decode(&x).is_err());
        let mut x = b.clone();
        x[278..310].copy_from_slice(&b[310..342]);
        x[310..342].copy_from_slice(&b[278..310]);
        assert!(ProtocolBudgetRecord::decode(&x).is_err());
    }
    assert_eq!(
        ProtocolBudgetRecord::decode(b"bad"),
        Err(ReconcileError::ProtocolStateSchemaUnavailable)
    );
}

#[test]
fn native_grant_signatures_and_every_committed_field() {
    let v = vectors();
    assert_eq!(GRANT_MODULE_ID, 1);
    for name in ["grant_once", "grant_recurring", "grant_extreme"] {
        let b = bytes(&v, name);
        let key = bytes(&v, &format!("{name}_key"));
        let r = CommittedGrant::decode(&key, &b).expect("real native signed grant and grant_save");
        assert_eq!(key, grant_state_key(r.grant.id));
        assert_eq!(
            (
                r.grant.from,
                r.grant.recipient,
                r.grant.asset,
                r.grant.purpose_hash,
                r.grant.reference_hash
            ),
            ([2; 32], [3; 32], [4; 32], [5; 32], [6; 32])
        );
        assert!(r.grant.has_reference);
        assert_eq!(r.grant.recurring, name != "grant_once");
        assert_eq!(
            r.grant.window_length,
            if name == "grant_once" { 0 } else { u64::MAX }
        );
        assert_eq!(
            (r.grant.expiration, r.grant.revocation_sequence),
            (u64::MAX, u64::MAX)
        );
        if name == "grant_extreme" {
            assert_eq!(
                (
                    r.grant.per_draw_maximum,
                    r.grant.allowance,
                    r.drawn_total,
                    r.drawn_this_period
                ),
                (u128::MAX, u128::MAX, u128::MAX, u128::MAX)
            );
            assert_eq!(
                (r.window_start, r.revoked_at_sequence),
                (u64::MAX, u64::MAX)
            );
            assert!(r.revoked && r.invoice_settled);
        } else {
            assert_eq!(
                (
                    r.grant.per_draw_maximum,
                    r.grant.allowance,
                    r.drawn_total,
                    r.drawn_this_period
                ),
                (10, 100, 7, 7)
            );
            assert_eq!((r.window_start, r.revoked_at_sequence), (9, 11));
            assert!(!r.revoked && !r.invoice_settled);
        }
    }
}

#[test]
fn grant_refuses_key_signature_identity_lengths_and_booleans() {
    let v = vectors();
    let b = bytes(&v, "grant_once");
    let key = bytes(&v, "grant_once_key");
    for length in 0..b.len() {
        assert!(CommittedGrant::decode(&key, &b[..length]).is_err());
    }
    let mut x = b.clone();
    x.push(0);
    assert!(CommittedGrant::decode(&key, &x).is_err());
    for offset in [
        0, 32, 64, 96, 128, 144, 160, 161, 169, 177, 209, 210, 242, 250, 282, 345,
    ] {
        let mut x = b.clone();
        x[offset] ^= 1;
        assert!(
            CommittedGrant::decode(&key, &x).is_err(),
            "signed offset {offset}"
        );
    }
    for offset in [160, 209, 394, 395] {
        let mut x = b.clone();
        x[offset] = 2;
        assert!(CommittedGrant::decode(&key, &x).is_err());
    }
    for length in 0..key.len() {
        assert!(CommittedGrant::decode(&key[..length], &b).is_err());
    }
    let mut k = key.clone();
    k[6] ^= 1;
    assert!(CommittedGrant::decode(&k, &b).is_err());
    let mut k = key.clone();
    k[0] ^= 1;
    assert!(CommittedGrant::decode(&k, &b).is_err());
    let mut k = key;
    k.push(0);
    assert!(CommittedGrant::decode(&k, &b).is_err());
}
