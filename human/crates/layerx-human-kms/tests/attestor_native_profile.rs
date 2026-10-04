use base64::engine::general_purpose::STANDARD;
use base64::Engine as _;
use layerx_agent_api::identity::{NativeLocalGrantConsentV1, NativePreparationPurposeV1};
use layerx_human_kms::attestor::{native_local_grant_signing_bytes, AttestorClient, AttestorError, AttestorSigner};
use layerx_human_service::custody::{CustodyError, CustodySigner, KeyClass, KeyId, Keystore, NativeConsent,
    NativeConsentRequest, Operation, PrincipalKeyBinding, SignAuthorization, SigningLimits};
use layerx_human_service::server::production_components::{AttestorCustodyConfig, AttestorKms};
use layerx_human_service::store::{AgentTenantId, PrincipalId, PrincipalStore, RetentionPeriod, RetentionPolicy, TenancyMap};
use layerx_human_service::trace::TraceId;
use layerx_types::payload::ModuleRegistry;
use serde::Deserialize;
use serde_json::json;
use sha2::{Digest, Sha256};
use std::error::Error;
use std::fs;
use std::future::Future;
use std::net::SocketAddr;
use std::os::unix::fs::{MetadataExt as _, PermissionsExt as _};
use std::path::Path;
use std::sync::Arc;
use std::task::{Context, Poll, Wake, Waker};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

type Result<T> = std::result::Result<T, Box<dyn Error>>;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Node { id: String, address: SocketAddr }

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Fixture {
    schema: String, directory: String, nodes: Vec<Node>, signers: Vec<String>,
    root_der: String, client_der: String, client_pkcs8: String,
    owner: String, account: String, owner_assertion: String, second_assertion: String, foreign_assertion: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Material { purpose: String, capability: String, native_session: String, expiry_ms: u64 }

fn read_private(path: &Path) -> Result<Vec<u8>> {
    let metadata = fs::symlink_metadata(path)?;
    if !metadata.is_file() || metadata.mode() & 0o077 != 0 || metadata.nlink() != 1 || metadata.len() > 2_097_152 {
        return Err("private live fixture ownership, permissions or bounds refused".into());
    }
    Ok(fs::read(path)?)
}

fn write_private(path: &Path, value: serde_json::Value) -> Result<()> {
    use std::io::Write as _;
    let temporary = path.with_extension("pending");
    let mut file = fs::OpenOptions::new().write(true).create_new(true).open(&temporary)?;
    fs::set_permissions(&temporary, fs::Permissions::from_mode(0o600))?;
    file.write_all(&serde_json::to_vec(&value)?)?;file.sync_all()?;
    fs::rename(temporary, path)?;
    Ok(())
}

fn wait(path: &Path) -> Result<()> {
    let deadline = Instant::now() + Duration::from_secs(120);
    while !path.exists() {
        if Instant::now() >= deadline { return Err("actual provider authority handshake deadline".into()); }
        std::thread::sleep(Duration::from_millis(20));
    }
    read_private(path)?;
    Ok(())
}

struct CurrentThread;
impl Wake for CurrentThread { fn wake(self: Arc<Self>) { std::thread::current().unpark(); } }

fn ready<F: Future>(future: F) -> Result<F::Output> {
    let waker = Waker::from(Arc::new(CurrentThread));
    let mut context = Context::from_waker(&waker);
    let mut future = std::pin::pin!(future);
    match future.as_mut().poll(&mut context) {
        Poll::Ready(result) => Ok(result),
        Poll::Pending => Err("production synchronous attestor provider unexpectedly yielded".into()),
    }
}

fn client(fixture: &Fixture) -> Result<AttestorClient> {
    let nodes: Vec<_> = fixture.nodes.iter().map(|node| (node.id.clone(), node.address)).collect();
    Ok(AttestorClient::new(&nodes, &[STANDARD.decode(&fixture.root_der)?],
        &[STANDARD.decode(&fixture.client_der)?], &STANDARD.decode(&fixture.client_pkcs8)?, Duration::from_secs(120))?)
}

#[test]
fn native_provider_and_sdk_real_cluster() -> Result<()> {
    let path = std::env::var("PAXEER_X_NATIVE_CUSTODY_LIVE_FIXTURE")?;
    let fixture: Fixture = serde_json::from_slice(&read_private(Path::new(&path))?)?;
    if fixture.schema != "layerx-human-native-custody-live.v1" { return Err("closed genuine live fixture profile".into()); }
    let directory = Path::new(&fixture.directory);
    let nodes: Vec<_> = fixture.nodes.iter().map(|node| (node.id.clone(), node.address)).collect();
    let config = AttestorCustodyConfig::new(nodes, fixture.signers.clone(), STANDARD.decode(&fixture.root_der)?,
        STANDARD.decode(&fixture.client_der)?, STANDARD.decode(&fixture.client_pkcs8)?, Duration::from_secs(120))?;
    let provider = AttestorKms::connect(config)?;
    provider.admit_assertion(&fixture.owner, &fixture.owner_assertion)?;
    let principal = PrincipalId::new("native-owner")?;
    let key = KeyId::new("primary")?;
    let binding = PrincipalKeyBinding::new(b"layerx-human-custody/v1\0native-owner\0primary\0human-primary".to_vec(),
        125, KeyClass::HumanPrimary, "attestor-quorum/v1")?;
    let provider_key = format!("lx-{}", binding.digest()[..16].iter().map(|byte| format!("{byte:02x}")).collect::<String>());
    write_private(&directory.join("provision.json"), json!({"key_id":provider_key}))?;
    wait(&directory.join("provision-ready"))?;
    let keystore = Keystore::open_production(directory.join("custody"), 125, provider.clone())?;
    let public = provider.create_owned_key(&keystore, &principal, &key, &fixture.owner, &fixture.account)?;
    assert_eq!(keystore.describe(&principal, &key)?.public_key, public);
    write_private(&directory.join("generated.json"), json!({"public":public.to_vec()}))?;
    wait(&directory.join("signing.json"))?;
    let material: Material = serde_json::from_slice(&read_private(&directory.join("signing.json"))?)?;
    let purpose = NativePreparationPurposeV1::from_canonical_bytes(&STANDARD.decode(&material.purpose)?).map_err(|error| format!("{error:?}"))?;
    let tenancy = TenancyMap::new([(principal.clone(), AgentTenantId::new("tenant-a")?)])?;
    let store_root = directory.join("human-store");let tenancy_digest = tenancy.install(&store_root)?;
    let period = RetentionPeriod::new(86400);
    let mut store = PrincipalStore::open(&store_root, RetentionPolicy {journeys:period,notifications:period,audit:period,telemetry:period,cache:period}, tenancy_digest)?;
    let custody = CustodySigner::new_shared(keystore, Arc::new(std::sync::Mutex::new(
        PrincipalStore::open(&store_root, RetentionPolicy {journeys:period,notifications:period,audit:period,telemetry:period,cache:period}, tenancy_digest)?)),
        ModuleRegistry::new(&[]).map_err(|error| format!("{error:?}"))?, SigningLimits::new(32,60)?);
    let now = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
    let mut trace_entropy = [0;16];getrandom::fill(&mut trace_entropy).map_err(|error| std::io::Error::other(error.to_string()))?;
    let trace = TraceId::mint(trace_entropy);
    let mut scope = store.principal(&principal)?;
    let result = ready(custody.sign_native_consent_in_scope(&mut scope, NativeConsentRequest::new(&principal,&key,
        NativeConsent::PreparationPurpose(&purpose), SignAuthorization::new(Operation::ProtocolMutation,None),now,trace)))??;
    let digest: [u8;32] = Sha256::digest(purpose.canonical_bytes().map_err(|error| format!("{error:?}"))?).into();
    assert_eq!(result.signer_public_key(),public);
    ring::signature::UnparsedPublicKey::new(&ring::signature::ED25519,public).verify(&digest,result.signature())?;
    let grant = NativeLocalGrantConsentV1 {capability:STANDARD.decode(&material.capability)?,session_scope:STANDARD.decode(&material.native_session)?,
        expires_at_ms:material.expiry_ms,owner_public_key:public,signature:[0;64]};
    let missing = ready(custody.sign_native_consent_in_scope(&mut scope, NativeConsentRequest::new(&principal,&key,
        NativeConsent::LocalGrant(&grant),SignAuthorization::new(Operation::SecuritySettings,None),now,trace)))?;
    assert!(matches!(missing,Err(CustodyError::StepUpRequired)));
    let signers: Vec<_> = fixture.signers.iter().map(String::as_str).collect();
    let sdk = AttestorSigner::new(client(&fixture)?, &provider_key, public, &signers,125)?;
    let signature = sdk.sign_native_local_grant(&grant,"rust-genuine-grant",&fixture.second_assertion)?;
    let grant_digest:[u8;32] = Sha256::digest(native_local_grant_signing_bytes(&grant)?).into();
    assert_eq!(signature.digest(),&grant_digest);
    ring::signature::UnparsedPublicKey::new(&ring::signature::ED25519,public).verify(&grant_digest,signature.signature())?;
    let stranger = sdk.sign_native_preparation_purpose(&purpose,"rust-foreign-owner",&fixture.foreign_assertion);
    assert!(matches!(stranger,Err(AttestorError::Refused{code,..}) if code=="token_not_owner"));
    Ok(())
}
