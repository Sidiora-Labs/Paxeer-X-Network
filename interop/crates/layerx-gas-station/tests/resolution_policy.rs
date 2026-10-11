use std::cell::Cell;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

use k256::ecdsa::SigningKey;
use k256::elliptic_curve::rand_core::OsRng;
use layerx_gas_station::config::{
    ConfigError, DidGasLimit, PaymasterRequired, ServiceConfig, StationConfig,
};
use layerx_gas_station::journal::{
    Completion, Entry, Journal, JournalError, Key, QuoteRecord, Submission,
};
use layerx_gas_station::policy::{DidBudget, PolicyRefusal, QuotePolicy};
use layerx_gas_station::price::{GovernedRate, PAX_BASE_UNITS};
use layerx_gas_station::quote::{address_word, keccak, word, Address, Quote, Word, SIDIORA};
use layerx_gas_station::rpc::hex;
use layerx_gas_station::signer::{LocalSigner, QuoteSigner, SignerError};
use layerx_gas_station::station::{prepare_submission, StationError};
use layerx_gas_station::tx::{self, Authorization, Call, Fees, TransactionRequest};
use layerx_gas_station::{QuoteError, QuoteRequest, SignedQuote};

type TestResult = Result<(), Box<dyn std::error::Error>>;
type RlpParts<'a> = (&'a [u8], &'a [u8]);

static NEXT_ID: AtomicU64 = AtomicU64::new(0);

struct CountedSigner {
    inner: LocalSigner,
    calls: Cell<usize>,
}

impl QuoteSigner for CountedSigner {
    fn address(&self) -> Address {
        self.inner.address()
    }
    fn sign_digest(&self, digest: Word) -> Result<[u8; 65], SignerError> {
        self.calls.set(self.calls.get() + 1);
        self.inner.sign_digest(digest)
    }
}

fn config() -> StationConfig {
    StationConfig {
        chain_id: 1325,
        endpoints: vec![format!("https://{}", std::process::id())],
        paymaster: [0x44; 20],
        token: SIDIORA,
        decimals: 6,
        max_rate_age: 300,
        spread_bps: 500,
        margin_bps: 100,
        per_account_limit: 8_000_000,
        per_interval_limit: 16_000_000,
        per_quote_limit: 4_000_000,
        interval_seconds: 60,
        balance_floor: 100,
        relayer_key_env: "PAXEER_RESOLUTION_KEY".into(),
    }
}

fn signer() -> Result<LocalSigner, Box<dyn std::error::Error>> {
    let key = SigningKey::random(&mut OsRng);
    let name = format!(
        "PAXEER_RESOLUTION_KEY_{}_{}",
        std::process::id(),
        NEXT_ID.fetch_add(1, Ordering::Relaxed)
    );
    std::env::set_var(&name, hex(&key.to_bytes()));
    let result = LocalSigner::from_env(&name);
    std::env::remove_var(&name);
    Ok(result?)
}

fn journal_path() -> Result<PathBuf, Box<dyn std::error::Error>> {
    use std::os::unix::fs::DirBuilderExt as _;
    let directory = std::env::temp_dir().join(format!(
        "paxeer-resolution-{}-{}",
        std::process::id(),
        NEXT_ID.fetch_add(1, Ordering::Relaxed)
    ));
    std::fs::DirBuilder::new().mode(0o700).create(&directory)?;
    Ok(directory.join("journal.jsonl"))
}

fn remove_journal(path: &Path) -> TestResult {
    std::fs::remove_file(path)?;
    std::fs::remove_dir(path.parent().ok_or("journal directory missing")?)?;
    Ok(())
}

fn did() -> String {
    format!("did:layerx:{}", "12".repeat(32))
}

fn fees(gas_limit: u64) -> Fees {
    Fees {
        gas_limit,
        max_fee_per_gas: 2,
        max_priority_fee_per_gas: 1,
    }
}

struct Batch {
    record: QuoteRecord,
    quote: SignedQuote,
    calls: Vec<Call>,
    authorizations: Vec<Authorization>,
    account_signature: [u8; 65],
}

impl Batch {
    fn new(
        sponsor: &LocalSigner,
        account: &LocalSigner,
        quote_nonce: u128,
        batch_nonce: u128,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        let config = config();
        let fees = fees(200_000);
        let quote = Quote {
            sponsor: sponsor.address(),
            token: config.token,
            max_token_amount: word(2_000_000),
            token_amount: word(1_000_000),
            deadline: word(1019),
            nonce: word(quote_nonce),
            gas_cost: word(fees.gas_cost()?),
        };
        let digest = layerx_gas_station::quote::quote_digest(
            word(u128::from(config.chain_id)),
            account.address(),
            &quote,
        );
        let signature = sponsor.sign_digest(digest)?;
        let record = QuoteRecord {
            key: Key {
                sponsor: sponsor.address(),
                quote_nonce: quote.nonce,
            },
            did: did(),
            batch_nonce: word(batch_nonce),
            chain_id: config.chain_id,
            paymaster: config.paymaster,
            account: account.address(),
            token: quote.token,
            maximum: quote.max_token_amount,
            amount: quote.token_amount,
            deadline: 1019,
            gas_cost: quote.gas_cost,
            issued_at: 1000,
            signature: signature.to_vec(),
            fees,
        };
        let quote = record.signed_quote()?;
        let calls = vec![
            Call {
                to: [0x55; 20],
                value: word(0),
                data: vec![1, 2, 3],
            },
            Call {
                to: [0x66; 20],
                value: word(7),
                data: vec![4, 5],
            },
        ];
        let mut authorization = Authorization {
            chain_id: config.chain_id,
            delegate: config.paymaster,
            nonce: u64::try_from(batch_nonce)?,
            signature: [0; 65],
        };
        authorization.signature = account.sign_digest(tx::authorization_digest(&authorization)?)?;
        let account_signature = account.sign_digest(tx::batch_digest(
            config.chain_id,
            account.address(),
            record.batch_nonce,
            &calls,
            &quote.quote,
        )?)?;
        Ok(Self {
            record,
            quote,
            calls,
            authorizations: vec![authorization],
            account_signature,
        })
    }

    fn request(&self, nonce: u64) -> TransactionRequest<'_> {
        TransactionRequest {
            chain_id: self.record.chain_id,
            account: self.record.account,
            paymaster: self.record.paymaster,
            nonce,
            batch_nonce: self.record.batch_nonce,
            fees: self.record.fees,
            calls: &self.calls,
            authorizations: &self.authorizations,
            quote: &self.quote,
            account_signature: &self.account_signature,
        }
    }

    fn reserve(&self, journal: &mut Journal) -> TestResult {
        journal.append(&Entry::QuoteReserved {
            quote: Box::new(self.record.clone()),
        })?;
        Ok(())
    }
}

fn rlp_item(input: &[u8]) -> Result<RlpParts<'_>, Box<dyn std::error::Error>> {
    let first = *input.first().ok_or("empty RLP")?;
    let (offset, length): (usize, usize) = match first {
        0..=127 => (0, 1),
        128..=183 => (1, usize::from(first - 128)),
        184..=191 | 248..=255 => {
            let count = usize::from(if first < 192 {
                first - 183
            } else {
                first - 247
            });
            let mut length = 0_usize;
            for byte in input.get(1..=count).ok_or("short RLP length")? {
                length = length
                    .checked_mul(256)
                    .and_then(|value| value.checked_add(usize::from(*byte)))
                    .ok_or("RLP overflow")?;
            }
            (1 + count, length)
        }
        192..=247 => (1, usize::from(first - 192)),
    };
    let end = offset.checked_add(length).ok_or("RLP overflow")?;
    Ok((
        input.get(offset..end).ok_or("short RLP payload")?,
        input.get(end..).ok_or("short RLP tail")?,
    ))
}

fn envelope_fields(raw: &[u8]) -> Result<Vec<&[u8]>, Box<dyn std::error::Error>> {
    assert_eq!(raw.first(), Some(&4));
    let (mut payload, rest) = rlp_item(raw.get(1..).ok_or("missing envelope")?)?;
    assert!(rest.is_empty());
    let mut fields = Vec::new();
    while !payload.is_empty() {
        let (field, rest) = rlp_item(payload)?;
        fields.push(field);
        payload = rest;
    }
    assert_eq!(fields.len(), 13);
    Ok(fields)
}

fn abi_word(data: &[u8], index: usize) -> Result<Word, Box<dyn std::error::Error>> {
    let start = index
        .checked_mul(32)
        .and_then(|value| value.checked_add(4))
        .ok_or("ABI overflow")?;
    Ok(data
        .get(start..start.checked_add(32).ok_or("ABI overflow")?)
        .ok_or("missing ABI word")?
        .try_into()?)
}

fn assert_sponsored_envelope(batch: &Batch, raw: &[u8], nonce: u64) -> TestResult {
    let fields = envelope_fields(raw)?;
    assert_eq!(fields[1], [u8::try_from(nonce)?]);
    assert_eq!(fields[5], batch.record.account);
    let calldata = fields[7];
    assert_eq!(&calldata[..4], &keccak(b"executeSponsored((address,uint256,bytes)[],(address,address,uint256,uint256,uint256,uint256,uint256),bytes,bytes)")[..4]);
    assert_eq!(
        calldata,
        tx::encode_sponsored(&batch.calls, &batch.quote, &batch.account_signature)?
    );
    for (index, expected) in [
        word(320),
        address_word(batch.record.key.sponsor),
        address_word(SIDIORA),
        word(2_000_000),
        word(1_000_000),
        word(1019),
        batch.record.key.quote_nonce,
        word(batch.record.fees.gas_cost()?),
    ]
    .into_iter()
    .enumerate()
    {
        assert_eq!(abi_word(calldata, index)?, expected);
    }
    assert_eq!(calldata.get(324..356).ok_or("missing call count")?, word(2));
    let (authorization, rest) = rlp_item(fields[9])?;
    assert!(rest.is_empty());
    let (chain, rest) = rlp_item(authorization)?;
    assert_eq!(chain, [5, 45]);
    let (delegate, _) = rlp_item(rest)?;
    assert_eq!(delegate, config().paymaster);
    assert_eq!(
        tx::recover(
            tx::authorization_digest(&batch.authorizations[0])?,
            &batch.authorizations[0].signature
        )?,
        batch.record.account
    );
    assert_eq!(
        tx::recover(
            tx::batch_digest(
                config().chain_id,
                batch.record.account,
                batch.record.batch_nonce,
                &batch.calls,
                &batch.quote.quote
            )?,
            &batch.account_signature
        )?,
        batch.record.account
    );
    Ok(())
}

#[test]
fn one_paymaster_nonce() -> TestResult {
    let sponsor = signer()?;
    let account = signer()?;
    let path = journal_path()?;
    let mut journal = Journal::open(&path)?;
    let first = Batch::new(&sponsor, &account, 11, 0)?;
    let second = Batch::new(&sponsor, &account, 12, 1)?;
    for batch in [&first, &second] {
        batch.reserve(&mut journal)?;
    }
    for (batch, nonce) in [(&first, 5_u64), (&second, 6_u64)] {
        let prepared =
            prepare_submission(&mut journal, batch.request(5), &batch.record.did, &sponsor)?;
        assert_eq!(prepared.nonce, nonce);
        assert_eq!(prepared.batch_nonce, batch.record.batch_nonce);
        assert_eq!(prepared.did, batch.record.did);
        let signed = tx::sign(&batch.request(nonce), &sponsor)?;
        assert_eq!(prepared.raw, signed.raw);
        assert_eq!(prepared.hash, signed.hash);
        assert_sponsored_envelope(batch, &prepared.raw, nonce)?;
    }
    assert_eq!(journal.state().next_nonce(sponsor.address(), 5), Some(7));
    let third = Batch::new(&sponsor, &account, 13, 2)?;
    third.reserve(&mut journal)?;
    journal.append(&Entry::SigningIntent {
        key: third.record.key,
        nonce: 7,
        digest: third.quote.digest,
    })?;
    let before = journal.state().clone();
    let saved = std::fs::read(&path)?;
    assert_eq!(journal.state().next_nonce(sponsor.address(), 5), None);
    assert!(matches!(
        prepare_submission(&mut journal, third.request(5), &third.record.did, &sponsor),
        Err(StationError::SigningUnknown)
    ));
    assert_eq!(journal.state(), &before);
    assert_eq!(std::fs::read(&path)?, saved);
    drop(journal);
    remove_journal(&path)
}

fn assert_budget_rollover(policy: &mut QuotePolicy, did: &str) {
    let before = policy.outstanding_pax();
    for invalid in [
        "",
        "did:layerx:invalid",
        "did:layerx:",
        "0x0000000000000000000000000000000000000000",
    ] {
        assert_eq!(
            policy.reserve_for_did(invalid, [3; 20], 1, fees(1), u128::MAX, 1000),
            Err(PolicyRefusal::InvalidAmount)
        );
        assert_eq!(policy.outstanding_pax(), before);
    }
    assert_eq!(
        policy.reserve_for_did(did, [3; 20], 1, fees(0), u128::MAX, 1000),
        Err(PolicyRefusal::InvalidAmount)
    );
    assert_eq!(
        policy.reserve_for_did(
            did,
            [3; 20],
            1,
            Fees {
                gas_limit: 2,
                max_fee_per_gas: u128::MAX,
                max_priority_fee_per_gas: 0
            },
            u128::MAX,
            1000
        ),
        Err(PolicyRefusal::InvalidAmount)
    );
    assert_eq!(
        policy.reserve_for_did(did, [3; 20], 4_000_001, fees(1), u128::MAX, 1060),
        Err(PolicyRefusal::PerQuote)
    );
    assert_eq!(policy.did_budget().used(did, 16), 1_000_000);
    assert_eq!(
        policy.reserve_for_did(did, [3; 20], 1, fees(20_000), u128::MAX, 1060),
        Ok(())
    );
    assert_eq!(policy.did_budget().used(did, 17), 20_000);
    assert_eq!(policy.did_budget().used(did, 16), 0);
    assert_eq!(
        policy.reserve_for_did(did, [3; 20], 1, fees(1), u128::MAX, 1059),
        Err(PolicyRefusal::ClockRegression)
    );
}

fn assert_budget_boundaries(config: &StationConfig, did: &str) -> TestResult {
    let mut unavailable = QuotePolicy::new(config)?;
    assert_eq!(
        unavailable.reserve_for_did(did, [1; 20], 1, fees(1), u128::MAX, 1000),
        Err(PolicyRefusal::InvalidAmount)
    );
    assert_eq!(unavailable.outstanding_pax(), 0);
    let mut overflow = QuotePolicy::new(config)?.with_did_budget(DidBudget::new(u64::MAX));
    assert_eq!(
        overflow.reserve_for_did(did, [1; 20], 1, fees(u64::MAX), u128::MAX, 1000),
        Ok(())
    );
    let before = overflow.outstanding_pax();
    assert_eq!(
        overflow.reserve_for_did(did, [2; 20], 1, fees(1), u128::MAX, 1000),
        Err(PolicyRefusal::PerAccount)
    );
    assert_eq!(overflow.did_budget().used(did, 16), u64::MAX);
    assert_eq!(overflow.outstanding_pax(), before);
    Ok(())
}

#[test]
fn did_budget_refuses_over_budget() -> TestResult {
    let config = config();
    let did = did();
    let mut policy = QuotePolicy::new(&config)?.with_did_budget(DidBudget::new(1_000_000));
    assert_eq!(
        policy.reserve_for_did(&did, [1; 20], 1, fees(990_000), u128::MAX, 1000),
        Ok(())
    );
    let before = policy.outstanding_pax();
    let sponsor = CountedSigner {
        inner: signer()?,
        calls: Cell::new(0),
    };
    let account = signer()?;
    let request = QuoteRequest {
        account: account.address(),
        max_token_amount: config.per_quote_limit,
        gas_cost: fees(20_000).gas_cost()?,
        deadline: 1019,
        quote_nonce: word(41),
    };
    let rate = GovernedRate {
        rate: PAX_BASE_UNITS,
        updated_at: 1000,
    };
    assert_eq!(
        policy
            .quote_for_did(
                &request,
                &rate,
                fees(20_000),
                &sponsor,
                &did,
                (u128::MAX, 1000)
            )
            .err(),
        Some(QuoteError::Policy(PolicyRefusal::PerAccount))
    );
    assert_eq!(sponsor.calls.get(), 0);
    assert_eq!(policy.outstanding_pax(), before);
    assert_eq!(
        policy.reserve_for_did(&did, [2; 20], 1, fees(20_000), u128::MAX, 1000),
        Err(PolicyRefusal::PerAccount)
    );
    assert_eq!(
        policy
            .did_budget()
            .used(&did, 1000 / config.interval_seconds),
        990_000
    );
    assert_eq!(policy.outstanding_pax(), before);
    assert_eq!(
        policy.reserve_for_did(&did, [2; 20], 1, fees(10_000), u128::MAX, 1000),
        Ok(())
    );
    assert_eq!(policy.did_budget().used(&did, 16), 1_000_000);
    assert_budget_rollover(&mut policy, &did);
    assert_budget_boundaries(&config, &did)?;
    let mut admitted = QuotePolicy::new(&config)?.with_did_budget(DidBudget::new(1_000_000));
    let signed = admitted
        .quote_for_did(
            &request,
            &rate,
            fees(20_000),
            &sponsor,
            &did,
            (u128::MAX, 1000),
        )
        .map_err(StationError::Quote)?;
    assert_eq!(sponsor.calls.get(), 1);
    assert_eq!(signed.quote.token_amount, word(40_400));
    assert_eq!(
        tx::recover(signed.digest, &signed.signature)?,
        sponsor.address()
    );
    assert_eq!(admitted.did_budget().used(&did, 16), 20_000);
    Ok(())
}

#[test]
fn journal_carries_did() -> TestResult {
    let sponsor = signer()?;
    let account = signer()?;
    let batch = Batch::new(&sponsor, &account, 11, 0)?;
    let path = journal_path()?;
    let mut journal = Journal::open(&path)?;
    batch.reserve(&mut journal)?;
    let submission =
        prepare_submission(&mut journal, batch.request(5), &batch.record.did, &sponsor)?;
    let state = journal.state().clone();
    let saved = std::fs::read(&path)?;
    drop(journal);
    let journal = Journal::open(&path)?;
    assert_eq!(journal.state(), &state);
    let item = journal
        .state()
        .items
        .get(&batch.record.key)
        .ok_or("journal item missing")?;
    assert_eq!(
        item.quote.as_ref().ok_or("quote missing")?.did,
        batch.record.did
    );
    assert_eq!(item.submission.as_ref(), Some(&submission));
    assert_eq!(submission.did, batch.record.did);
    let entries = saved
        .split(|byte| *byte == b'\n')
        .filter(|line| !line.is_empty())
        .map(serde_json::from_slice::<Entry>)
        .collect::<Result<Vec<_>, _>>()?;
    assert!(entries.iter().any(|entry| matches!(entry, Entry::Prepared { key, submission: saved } if *key == batch.record.key && saved.did == batch.record.did && saved == &submission)));
    assert_eq!(std::fs::read(&path)?, saved);
    drop(journal);
    remove_journal(&path)
}

fn assert_retired_kinds_refused(path: &Path, bytes: &[u8], key: Key) -> TestResult {
    for retired in [
        serde_json::json!({"kind": "replacement_signing_intent", "key": key, "nonce": 5}),
        serde_json::json!({"kind": "replaced", "key": key}),
        serde_json::json!({"kind": "cancelled", "key": key, "block_number": 17}),
        serde_json::json!({"kind": "released", "key": key, "nonce": 5}),
    ] {
        let mut legacy = bytes.to_vec();
        serde_json::to_writer(&mut legacy, &retired)?;
        legacy.push(b'\n');
        std::fs::write(path, &legacy)?;
        assert!(matches!(Journal::open(path), Err(JournalError::Version)));
    }
    Ok(())
}

fn assert_refusal_is_final(
    journal: &mut Journal,
    path: &Path,
    batch: &Batch,
    original: &Submission,
    sponsor: &CountedSigner,
) -> TestResult {
    let key = batch.record.key;
    let refused = journal.state().clone();
    let refused_bytes = std::fs::read(path)?;
    let refusal = Entry::Refused {
        key,
        hash: original.hash,
    };
    assert_eq!(journal.append(&refusal), Err(JournalError::Conflict));
    assert_eq!(
        journal.append(&Entry::Prepared {
            key,
            submission: original.clone()
        }),
        Err(JournalError::Conflict)
    );
    assert_eq!(
        journal.append(&Entry::Completed {
            key,
            account: batch.record.account,
            completion: Completion::Reverted {
                hash: original.hash
            },
        }),
        Err(JournalError::Conflict)
    );
    assert!(matches!(
        prepare_submission(journal, batch.request(5), &batch.record.did, sponsor),
        Err(StationError::Conflict)
    ));
    assert_eq!(sponsor.calls.get(), 1);
    assert_eq!(journal.state(), &refused);
    assert_eq!(std::fs::read(path)?, refused_bytes);
    Ok(())
}

#[test]
fn no_same_nonce_replacement() -> TestResult {
    let sponsor = signer()?;
    let account = signer()?;
    let batch = Batch::new(&sponsor, &account, 11, 0)?;
    let next = Batch::new(&sponsor, &account, 12, 1)?;
    let sponsor = CountedSigner {
        inner: sponsor,
        calls: Cell::new(0),
    };
    let key = batch.record.key;
    let path = journal_path()?;
    let mut journal = Journal::open(&path)?;
    batch.reserve(&mut journal)?;
    next.reserve(&mut journal)?;
    let original = prepare_submission(&mut journal, batch.request(5), &batch.record.did, &sponsor)?;
    assert_eq!(original.nonce, 5);
    assert_eq!(sponsor.calls.get(), 1);
    let before = journal.state().clone();
    let bytes = std::fs::read(&path)?;
    let mut higher_tip = batch.request(5);
    higher_tip.fees.max_priority_fee_per_gas = higher_tip.fees.max_fee_per_gas;
    assert!(matches!(
        prepare_submission(&mut journal, higher_tip, &batch.record.did, &sponsor),
        Err(StationError::Conflict)
    ));
    assert_eq!(
        journal.append(&Entry::Prepared {
            key,
            submission: original.clone()
        }),
        Err(JournalError::Conflict)
    );
    assert!(matches!(
        prepare_submission(&mut journal, batch.request(5), &batch.record.did, &sponsor),
        Err(StationError::Conflict)
    ));
    assert_eq!(sponsor.calls.get(), 1);
    assert!(journal.state().holds(sponsor.address(), 5));
    assert_eq!(journal.state().next_nonce(sponsor.address(), 5), Some(6));
    assert_eq!(journal.state(), &before);
    assert_eq!(std::fs::read(&path)?, bytes);
    drop(journal);
    assert_retired_kinds_refused(&path, &bytes, key)?;
    std::fs::write(&path, &bytes)?;
    let mut journal = Journal::open(&path)?;
    assert_eq!(journal.state(), &before);
    for conflicting in [
        Entry::Refused {
            key,
            hash: keccak(&original.hash),
        },
        Entry::Refused {
            key: next.record.key,
            hash: original.hash,
        },
    ] {
        assert_eq!(journal.append(&conflicting), Err(JournalError::Conflict));
    }
    assert_eq!(std::fs::read(&path)?, bytes);
    let refusal = Entry::Refused {
        key,
        hash: original.hash,
    };
    journal.append(&refusal)?;
    assert!(journal.state().refused(&key));
    assert!(!journal.state().holds(sponsor.address(), 5));
    assert_eq!(journal.state().next_nonce(sponsor.address(), 5), Some(5));
    assert_eq!(journal.state().liability(key)?, 0);
    assert_refusal_is_final(&mut journal, &path, &batch, &original, &sponsor)?;
    assert_eq!(
        journal.state().items[&key].submission.as_ref(),
        Some(&original)
    );
    let following = prepare_submission(&mut journal, next.request(5), &next.record.did, &sponsor)?;
    assert_eq!(sponsor.calls.get(), 2);
    assert_eq!(following.nonce, 5);
    assert_ne!(following.hash, original.hash);
    assert!(journal.state().holds(sponsor.address(), 5));
    let state = journal.state().clone();
    drop(journal);
    let journal = Journal::open(&path)?;
    assert_eq!(journal.state(), &state);
    assert!(journal.state().refused(&key));
    assert_eq!(
        journal.state().items[&key].submission.as_ref(),
        Some(&original)
    );
    assert_eq!(
        journal.state().items[&next.record.key].submission.as_ref(),
        Some(&following)
    );
    drop(journal);
    remove_journal(&path)
}

fn assert_paymaster_values() {
    let required = ConfigError {
        field: "GAS_STATION_PAYMASTER",
    };
    assert_eq!(PaymasterRequired::try_from(None), Err(required.clone()));
    for invalid in [
        "",
        "invalid",
        "0x44",
        "0x0000000000000000000000000000000000000000",
    ] {
        assert_eq!(
            PaymasterRequired::try_from(Some(invalid)),
            Err(required.clone())
        );
    }
    assert_eq!(
        PaymasterRequired::try_from(Some(hex(&SIDIORA).as_str())),
        Err(required)
    );
    let encoded = hex(&config().paymaster);
    assert_eq!(
        PaymasterRequired::try_from(Some(encoded.as_str())),
        Ok(PaymasterRequired(config().paymaster))
    );
    assert_eq!(
        DidGasLimit::try_from(None),
        Err(ConfigError {
            field: "GAS_STATION_DID_GAS_LIMIT"
        })
    );
    for invalid in ["", "0", "01", "-1", "18446744073709551616"] {
        assert_eq!(
            DidGasLimit::try_from(Some(invalid)),
            Err(ConfigError {
                field: "GAS_STATION_DID_GAS_LIMIT"
            })
        );
    }
    assert_eq!(
        DidGasLimit::try_from(Some("1000000")),
        Ok(DidGasLimit(1_000_000))
    );
}

#[test]
fn paymaster_required() -> TestResult {
    if let Ok(mode) = std::env::var("PAXEER_RESOLUTION_PAYMASTER_CASE") {
        let path = PathBuf::from(std::env::var("PAXEER_RESOLUTION_CONFIG_PATH")?);
        let loaded = ServiceConfig::load(&path);
        if mode == "present" {
            assert_eq!(
                loaded?,
                ServiceConfig::parse(&std::fs::read_to_string(&path)?)?
            );
        } else {
            assert_eq!(
                loaded.err(),
                Some(ConfigError {
                    field: "GAS_STATION_PAYMASTER"
                })
            );
        }
        return Ok(());
    }
    assert_paymaster_values();
    let configuration = config();
    let listen = std::net::SocketAddr::from((std::net::Ipv4Addr::UNSPECIFIED, 1)).to_string();
    let value = serde_json::json!({
        "listen": listen, "gas_limit": 200_000, "max_priority_fee_per_gas": 1,
        "chain_id": configuration.chain_id, "endpoints": configuration.endpoints,
        "paymaster": hex(&configuration.paymaster), "token": hex(&configuration.token),
        "decimals": configuration.decimals, "max_rate_age": configuration.max_rate_age,
        "spread_bps": configuration.spread_bps, "margin_bps": configuration.margin_bps,
        "per_account_limit": configuration.per_account_limit,
        "per_interval_limit": configuration.per_interval_limit,
        "per_quote_limit": configuration.per_quote_limit,
        "interval_seconds": configuration.interval_seconds, "balance_floor": configuration.balance_floor,
        "relayer_key_env": configuration.relayer_key_env,
    });
    let path = journal_path()?.with_file_name("config.json");
    std::fs::write(&path, value.to_string())?;
    for (mode, paymaster) in [
        ("missing", None),
        ("present", Some(hex(&config().paymaster))),
        ("mismatch", Some(hex(&[0x55; 20]))),
        ("invalid", Some("invalid".into())),
    ] {
        let mut command = std::process::Command::new(std::env::current_exe()?);
        command
            .args(["--exact", "paymaster_required"])
            .env("PAXEER_RESOLUTION_PAYMASTER_CASE", mode)
            .env("PAXEER_RESOLUTION_CONFIG_PATH", &path)
            .env_remove("GAS_STATION_PAYMASTER");
        if let Some(paymaster) = paymaster {
            command.env("GAS_STATION_PAYMASTER", paymaster);
        }
        assert_eq!(command.status()?.code(), Some(0));
    }
    remove_journal(&path)
}
