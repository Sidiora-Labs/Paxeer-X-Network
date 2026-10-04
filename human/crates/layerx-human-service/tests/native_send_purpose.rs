use base64::engine::general_purpose::{STANDARD, URL_SAFE_NO_PAD};
use base64::Engine as _;
use layerx_agent_api::identity::{
    NativeLocalGrantConsentV1, NativePreparationPurposeV1, NativeSendPurposeV1,
    SignedNativeSendPurposeV1,
};
use layerx_human_kms::attestor::{
    native_local_grant_signing_bytes, AttestorClient, AttestorError, AttestorSigner,
};
use layerx_human_service::auth::{
    AccountIdentity, AuthConfig, Device, OperationDigest, Passkeys, RateLimit,
};
use layerx_human_service::custody::{
    CustodyError, CustodySigner, KeyClass, KeyId, Keystore, NativeConsent, NativeConsentRequest,
    Operation, PrincipalKeyBinding, SignAuthorization, SigningLimits,
};
use layerx_human_service::server::production_components::{AttestorCustodyConfig, AttestorKms};
use layerx_human_service::store::{
    AgentTenantId, PrincipalId, PrincipalStore, RetentionPeriod, RetentionPolicy, TenancyMap,
};
use layerx_human_service::trace::TraceId;
use layerx_types::payload::ModuleRegistry;
use serde::Deserialize;
use serde_json::json;
use sha2::{Digest, Sha256};
use std::error::Error;
use std::fs;
use std::future::Future;
use std::io::{BufRead, BufReader, Write};
use std::net::SocketAddr;
use std::os::unix::fs::{MetadataExt as _, PermissionsExt as _};
use std::path::Path;
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};
use std::sync::Arc;
use std::task::{Context, Poll, Wake, Waker};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

type Result<T> = std::result::Result<T, Box<dyn Error>>;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Node {
    id: String,
    address: SocketAddr,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Fixture {
    schema: String,
    directory: String,
    nodes: Vec<Node>,
    signers: Vec<String>,
    root_der: String,
    client_der: String,
    client_pkcs8: String,
    owner: String,
    account: String,
    owner_assertion: String,
    second_assertion: String,
    foreign_assertion: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Material {
    purpose: String,
    capability: String,
    native_session: String,
    expiry_ms: u64,
}

fn read_private(path: &Path) -> Result<Vec<u8>> {
    let metadata = fs::symlink_metadata(path)?;
    if !metadata.is_file()
        || metadata.mode() & 0o077 != 0
        || metadata.nlink() != 1
        || metadata.len() > 2_097_152
    {
        return Err("private live fixture ownership, permissions or bounds refused".into());
    }
    Ok(fs::read(path)?)
}

fn write_private(path: &Path, value: serde_json::Value) -> Result<()> {
    use std::io::Write as _;
    let temporary = path.with_extension("pending");
    let mut file = fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&temporary)?;
    fs::set_permissions(&temporary, fs::Permissions::from_mode(0o600))?;
    file.write_all(&serde_json::to_vec(&value)?)?;
    file.sync_all()?;
    fs::rename(temporary, path)?;
    Ok(())
}

fn wait(path: &Path) -> Result<()> {
    let deadline = Instant::now() + Duration::from_secs(120);
    while !path.exists() {
        if Instant::now() >= deadline {
            return Err("actual provider authority handshake deadline".into());
        }
        std::thread::sleep(Duration::from_millis(20));
    }
    read_private(path)?;
    Ok(())
}

struct CurrentThread;
impl Wake for CurrentThread {
    fn wake(self: Arc<Self>) {
        std::thread::current().unpark();
    }
}

fn ready<F: Future>(future: F) -> Result<F::Output> {
    let waker = Waker::from(Arc::new(CurrentThread));
    let mut context = Context::from_waker(&waker);
    let mut future = std::pin::pin!(future);
    match future.as_mut().poll(&mut context) {
        Poll::Ready(result) => Ok(result),
        Poll::Pending => {
            Err("production synchronous attestor provider unexpectedly yielded".into())
        }
    }
}

fn client(fixture: &Fixture) -> Result<AttestorClient> {
    let nodes: Vec<_> = fixture
        .nodes
        .iter()
        .map(|node| (node.id.clone(), node.address))
        .collect();
    Ok(AttestorClient::new(
        &nodes,
        &[STANDARD.decode(&fixture.root_der)?],
        &[STANDARD.decode(&fixture.client_der)?],
        &STANDARD.decode(&fixture.client_pkcs8)?,
        Duration::from_secs(120),
    )?)
}

struct Authenticator {
    process: Child,
    input: ChildStdin,
    output: BufReader<ChildStdout>,
}

impl Authenticator {
    fn open() -> Result<Self> {
        let script = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../../apps/web/e2e/software-authenticator.ts");
        let mut process = Command::new("node")
            .arg(script)
            .arg("https://paxportwallet.com")
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            .spawn()?;
        let input = process
            .stdin
            .take()
            .ok_or("authenticator stdin unavailable")?;
        let output = BufReader::new(
            process
                .stdout
                .take()
                .ok_or("authenticator stdout unavailable")?,
        );
        Ok(Self {
            process,
            input,
            output,
        })
    }

    fn credential(&mut self, operation: &str, ceremony: &str) -> Result<String> {
        writeln!(
            self.input,
            "{}",
            json!({"operation":operation,"ceremony":ceremony})
        )?;
        self.input.flush()?;
        let mut response = Vec::new();
        let count =
            std::io::Read::take(&mut self.output, 32_769).read_until(b'\n', &mut response)?;
        if count == 0 || count > 32_768 || response.last() != Some(&b'\n') {
            return Err("authenticator response bound or EOF refused".into());
        }
        let value: serde_json::Value = serde_json::from_slice(&response)?;
        let credential = value
            .get("credential")
            .and_then(serde_json::Value::as_str)
            .ok_or("authenticator credential missing")?;
        if credential.is_empty() || credential.len() > 16_384 {
            return Err("credential bound refused".into());
        }
        Ok(credential.to_owned())
    }
}

impl Drop for Authenticator {
    fn drop(&mut self) {
        let _ = self.process.kill();
        let _ = self.process.wait();
    }
}

fn current_time() -> Result<u64> {
    Ok(SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs())
}

#[test]
fn native_send_provider_and_sdk_real_cluster() -> Result<()> {
    let path = std::env::var("PAXEER_X_NATIVE_SEND_LIVE_FIXTURE")?;
    let fixture: Fixture = serde_json::from_slice(&read_private(Path::new(&path))?)?;
    if fixture.schema != "layerx-human-native-send-live.v1" {
        return Err("closed genuine live fixture profile".into());
    }
    let directory = Path::new(&fixture.directory);
    let nodes: Vec<_> = fixture
        .nodes
        .iter()
        .map(|node| (node.id.clone(), node.address))
        .collect();
    let config = AttestorCustodyConfig::new(
        nodes,
        fixture.signers.clone(),
        STANDARD.decode(&fixture.root_der)?,
        STANDARD.decode(&fixture.client_der)?,
        STANDARD.decode(&fixture.client_pkcs8)?,
        Duration::from_secs(120),
    )?;
    let provider = AttestorKms::connect(config)?;
    provider.admit_assertion(&fixture.owner, &fixture.owner_assertion)?;
    let principal = PrincipalId::new("native-owner")?;
    let key = KeyId::new("primary")?;
    let binding = PrincipalKeyBinding::new(
        b"layerx-human-custody/v1\0native-owner\0primary\0human-primary".to_vec(),
        125,
        KeyClass::HumanPrimary,
        "attestor-quorum/v1",
    )?;
    let provider_key = format!(
        "lx-{}",
        binding.digest()[..16]
            .iter()
            .map(|byte| format!("{byte:02x}"))
            .collect::<String>()
    );
    write_private(
        &directory.join("provision.json"),
        json!({"key_id":provider_key}),
    )?;
    wait(&directory.join("provision-ready"))?;
    let keystore = Keystore::open_production(directory.join("custody"), 125, provider.clone())?;
    let public = provider.create_owned_key(
        &keystore,
        &principal,
        &key,
        &fixture.owner,
        &fixture.account,
    )?;
    assert_eq!(keystore.describe(&principal, &key)?.public_key, public);
    write_private(
        &directory.join("generated.json"),
        json!({"public":public.to_vec()}),
    )?;
    wait(&directory.join("signing.json"))?;
    let material: Material =
        serde_json::from_slice(&read_private(&directory.join("signing.json"))?)?;
    let purpose = NativeSendPurposeV1::from_canonical_bytes(&STANDARD.decode(&material.purpose)?)
        .map_err(|error| format!("{error:?}"))?;
    let foreign_principal = PrincipalId::new("native-foreign")?;
    let tenancy = TenancyMap::new([
        (principal.clone(), AgentTenantId::new("tenant-a")?),
        (foreign_principal.clone(), AgentTenantId::new("tenant-b")?),
    ])?;
    let store_root = directory.join("human-store");
    let tenancy_digest = tenancy.install(&store_root)?;
    let period = RetentionPeriod::new(86400);
    let mut store = PrincipalStore::open(
        &store_root,
        RetentionPolicy {
            journeys: period,
            notifications: period,
            audit: period,
            telemetry: period,
            cache: period,
        },
        tenancy_digest,
    )?;
    let custody = CustodySigner::new_shared(
        keystore,
        Arc::new(std::sync::Mutex::new(PrincipalStore::open(
            &store_root,
            RetentionPolicy {
                journeys: period,
                notifications: period,
                audit: period,
                telemetry: period,
                cache: period,
            },
            tenancy_digest,
        )?)),
        ModuleRegistry::new(&[]).map_err(|error| format!("{error:?}"))?,
        SigningLimits::new(32, 60)?,
    );
    let now = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
    let mut trace_entropy = [0; 16];
    getrandom::fill(&mut trace_entropy)
        .map_err(|error| std::io::Error::other(error.to_string()))?;
    let trace = TraceId::mint(trace_entropy);
    let mut scope = store.principal(&principal)?;
    let result = ready(custody.sign_native_consent_in_scope(
        &mut scope,
        NativeConsentRequest::new(
            &principal,
            &key,
            NativeConsent::SendPurpose(&purpose),
            SignAuthorization::new(Operation::ProtocolMutation, None),
            now,
            trace.clone(),
        ),
    ))??;
    let digest: [u8; 32] = Sha256::digest(
        purpose
            .canonical_bytes()
            .map_err(|error| format!("{error:?}"))?,
    )
    .into();
    assert_eq!(result.signer_public_key(), public);
    ed25519_dalek::Verifier::verify(
        &ed25519_dalek::VerifyingKey::from_bytes(&public)?,
        &digest,
        &ed25519_dalek::Signature::from_bytes(result.signature()),
    )
    .map_err(|_| "native purpose signature verification refused")?;
    let canonical = purpose
        .canonical_bytes()
        .map_err(|error| format!("{error:?}"))?;
    assert_eq!(
        NativeSendPurposeV1::from_canonical_bytes(&canonical)
            .map_err(|error| format!("{error:?}"))?,
        purpose
    );
    assert!(NativePreparationPurposeV1::from_canonical_bytes(&canonical).is_err());
    assert_eq!(purpose.owner_public_key, public);
    let signed_purpose = SignedNativeSendPurposeV1 {
        purpose: purpose.clone(),
        owner_public_key: public,
        signature: *result.signature(),
    }
    .validate()
    .map_err(|error| format!("{error:?}"))?;
    let verifier = ed25519_dalek::VerifyingKey::from_bytes(&public)?;
    let signature = ed25519_dalek::Signature::from_bytes(&signed_purpose.signature);
    for field in 0..16 {
        let mut changed = purpose.clone();
        match field {
            0 => changed.generation += 1,
            1 => changed.expires_at_ms -= 1,
            2 => changed.preparation_id[0] ^= 1,
            3 => changed.canonical_digest[0] ^= 1,
            4 => changed.economic_action[0] ^= 1,
            5 => changed.idempotency_key[0] ^= 1,
            6 => changed.commitment[0] ^= 1,
            7 => changed.network_id += 1,
            8 => changed.owner_public_key[0] ^= 1,
            9 => {
                changed.activity = layerx_agent_api::identity::NativeActivity::new(1, 6)
                    .map_err(|error| format!("{error:?}"))?
            }
            10 => changed.protocol_version += 1,
            11 => {
                changed.session_id = layerx_agent_api::identity::SessionId::new("33".repeat(32))
                    .map_err(|error| format!("{error:?}"))?
            }
            12 => {
                changed.capability_id =
                    layerx_agent_api::identity::CapabilityId::new("44".repeat(32))
                        .map_err(|error| format!("{error:?}"))?
            }
            13 => {
                changed.tenant = layerx_agent_api::identity::TenantId::new("tenant-b")
                    .map_err(|error| format!("{error:?}"))?
            }
            14 => {
                changed.agent_did = layerx_agent_api::identity::AgentDid::new("did:layerx:bob")
                    .map_err(|error| format!("{error:?}"))?
            }
            _ => {
                changed.owner_did = layerx_agent_api::identity::AgentDid::new("did:layerx:bob")
                    .map_err(|error| format!("{error:?}"))?
            }
        }
        if let Ok(bytes) = changed.canonical_bytes() {
            let changed_digest: [u8; 32] = Sha256::digest(bytes).into();
            assert!(
                ed25519_dalek::Verifier::verify(&verifier, &changed_digest, &signature).is_err()
            );
        } else {
            assert!(changed.validate().is_err());
        }
    }
    for length in 0..canonical.len() {
        assert!(NativeSendPurposeV1::from_canonical_bytes(&canonical[..length]).is_err());
    }
    let mut trailing = canonical.clone();
    trailing.push(0);
    assert!(NativeSendPurposeV1::from_canonical_bytes(&trailing).is_err());
    let mut wrong_owner = signed_purpose.clone();
    wrong_owner.owner_public_key[0] ^= 1;
    assert!(wrong_owner.validate().is_err());
    let mut expired = purpose.clone();
    expired.expires_at_ms = now.saturating_mul(1000);
    assert!(NativeConsent::SendPurpose(&expired)
        .validate_at(now.saturating_mul(1000), public)
        .is_err());
    let mut foreign_key = public;
    foreign_key[0] ^= 1;
    assert!(NativeConsent::SendPurpose(&purpose)
        .validate_at(now.saturating_mul(1000), foreign_key)
        .is_err());
    let grant = NativeLocalGrantConsentV1 {
        capability: STANDARD.decode(&material.capability)?,
        session_scope: STANDARD.decode(&material.native_session)?,
        expires_at_ms: material.expiry_ms,
        owner_public_key: public,
        signature: [0; 64],
    };
    let missing = ready(custody.sign_native_consent_in_scope(
        &mut scope,
        NativeConsentRequest::new(
            &principal,
            &key,
            NativeConsent::LocalGrant(&grant),
            SignAuthorization::new(Operation::SecuritySettings, None),
            now,
            trace.clone(),
        ),
    ))?;
    assert!(matches!(missing, Err(CustodyError::StepUpRequired)));
    let signers: Vec<_> = fixture.signers.iter().map(String::as_str).collect();
    let sdk = AttestorSigner::new(client(&fixture)?, &provider_key, public, &signers, 125)?;
    let signature =
        sdk.sign_native_local_grant(&grant, "rust-genuine-grant", &fixture.second_assertion)?;
    let grant_digest: [u8; 32] = Sha256::digest(native_local_grant_signing_bytes(&grant)?).into();
    assert_eq!(signature.digest(), &grant_digest);
    ed25519_dalek::Verifier::verify(
        &ed25519_dalek::VerifyingKey::from_bytes(&public)?,
        &grant_digest,
        &ed25519_dalek::Signature::from_bytes(signature.signature()),
    )
    .map_err(|_| "native SDK grant signature verification refused")?;

    let passkeys = Passkeys::new(AuthConfig {
        rp_id: "paxportwallet.com".to_owned(),
        rp_name: "LayerX".to_owned(),
        origin: "https://paxportwallet.com".to_owned(),
        ceremony_ttl_secs: 300,
        assertion_ttl_secs: 60,
        session_ttl_secs: 300,
        refresh_ttl_secs: 3600,
        step_up_ttl_secs: 60,
        rate_limit: RateLimit {
            attempts: 100,
            window_secs: 60,
        },
    })?;
    let account = AccountIdentity::new(principal.as_str(), "Native consent qualification")?;
    let mut authenticator = Authenticator::open()?;
    let registration =
        passkeys.begin_registration(&mut scope, &account, "Disposable passkey", current_time()?)?;
    let credential = authenticator.credential("register", &registration.ceremony)?;
    passkeys.finish_registration(
        &mut scope,
        &registration.registration_id,
        &credential,
        current_time()?,
    )?;
    let assertion = passkeys.begin_assertion(&mut scope, current_time()?)?;
    let credential = authenticator.credential("assert", &assertion.ceremony)?;
    passkeys.finish_assertion(
        &mut scope,
        &assertion.assertion_id,
        &credential,
        current_time()?,
    )?;
    let session = passkeys.open_session(
        &mut scope,
        &assertion.assertion_id,
        Device::mint("Disposable authenticator", "qualification")?,
        current_time()?,
    )?;
    let confirms = OperationDigest::new(grant_digest);
    let challenge = passkeys.begin_step_up(
        &mut scope,
        session.access_token().expose(),
        session.csrf_token().expose(),
        confirms,
        current_time()?,
    )?;
    let credential = authenticator.credential("assert", &challenge.ceremony)?;
    let mut forged: serde_json::Value =
        serde_json::from_slice(&URL_SAFE_NO_PAD.decode(&credential)?)?;
    let mut altered_signature = URL_SAFE_NO_PAD.decode(
        forged["signature"]
            .as_str()
            .ok_or("assertion signature missing")?,
    )?;
    altered_signature[0] ^= 1;
    forged["signature"] = json!(URL_SAFE_NO_PAD.encode(altered_signature));
    let forged = URL_SAFE_NO_PAD.encode(serde_json::to_vec(&forged)?);
    assert!(passkeys
        .finish_step_up(
            &mut scope,
            &challenge.challenge_id,
            &forged,
            current_time()?
        )
        .is_err());
    let authenticated = passkeys.finish_step_up(
        &mut scope,
        &challenge.challenge_id,
        &credential,
        current_time()?,
    )?;
    assert!(passkeys
        .finish_step_up(
            &mut scope,
            &challenge.challenge_id,
            &credential,
            current_time()?
        )
        .is_err());
    let capability_digest: [u8; 32] = Sha256::digest(&grant.capability).into();
    assert!(matches!(
        CustodySigner::bind_authenticated_step_up(
            &passkeys,
            &mut scope,
            &authenticated,
            OperationDigest::new(digest),
            Operation::SecuritySettings,
            grant_digest,
            capability_digest,
            current_time()?
        ),
        Err(CustodyError::InvalidEvidence)
    ));
    let mut foreign_store = PrincipalStore::open(
        &store_root,
        RetentionPolicy {
            journeys: period,
            notifications: period,
            audit: period,
            telemetry: period,
            cache: period,
        },
        tenancy_digest,
    )?;
    let mut foreign_scope = foreign_store.principal(&foreign_principal)?;
    assert!(matches!(
        CustodySigner::bind_authenticated_step_up(
            &passkeys,
            &mut foreign_scope,
            &authenticated,
            confirms,
            Operation::SecuritySettings,
            grant_digest,
            capability_digest,
            current_time()?
        ),
        Err(CustodyError::InvalidEvidence)
    ));
    let evidence = CustodySigner::bind_authenticated_step_up(
        &passkeys,
        &mut scope,
        &authenticated,
        confirms,
        Operation::SecuritySettings,
        grant_digest,
        capability_digest,
        current_time()?,
    )?;
    let changed_grant = NativeLocalGrantConsentV1 {
        expires_at_ms: grant.expires_at_ms - 1,
        ..grant.clone()
    };
    let mismatched = ready(custody.sign_native_consent_in_scope(
        &mut scope,
        NativeConsentRequest::new(
            &principal,
            &key,
            NativeConsent::LocalGrant(&changed_grant),
            SignAuthorization::new(Operation::SecuritySettings, Some(&evidence)),
            current_time()?,
            trace.clone(),
        ),
    ))?;
    assert!(matches!(mismatched, Err(CustodyError::StepUpMismatch)));
    provider.admit_assertion(&fixture.owner, &fixture.second_assertion)?;
    let signed = ready(custody.sign_native_consent_in_scope(
        &mut scope,
        NativeConsentRequest::new(
            &principal,
            &key,
            NativeConsent::LocalGrant(&grant),
            SignAuthorization::new(Operation::SecuritySettings, Some(&evidence)),
            current_time()?,
            trace.clone(),
        ),
    ))??;
    assert_eq!(signed.signer_public_key(), public);
    assert_eq!(signed.disclosure_digest(), grant_digest);
    ed25519_dalek::Verifier::verify(
        &ed25519_dalek::VerifyingKey::from_bytes(&public)?,
        &grant_digest,
        &ed25519_dalek::Signature::from_bytes(signed.signature()),
    )
    .map_err(|_| "native custody grant signature verification refused")?;
    let replayed = ready(custody.sign_native_consent_in_scope(
        &mut scope,
        NativeConsentRequest::new(
            &principal,
            &key,
            NativeConsent::LocalGrant(&grant),
            SignAuthorization::new(Operation::SecuritySettings, Some(&evidence)),
            current_time()?,
            trace.clone(),
        ),
    ))?;
    assert!(matches!(replayed, Err(CustodyError::StepUpReplayed)));
    println!("native custody genuine passkey issuance and grant consent verified");
    println!("native custody principal digest forgery ceremony-replay and consumed-evidence refusals verified");
    let stranger =
        sdk.sign_native_send_purpose(&purpose, "rust-foreign-owner", &fixture.foreign_assertion);
    assert!(matches!(stranger,Err(AttestorError::Refused{code,..}) if code=="token_not_owner"));
    Ok(())
}
