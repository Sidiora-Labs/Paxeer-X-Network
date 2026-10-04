use std::process::Command;

#[test]
fn executable_refuses_missing_configuration_without_exposing_environment() -> std::io::Result<()> {
    let result = Command::new(env!("CARGO_BIN_EXE_layerx-human-movement-provider"))
        .env_clear()
        .output()?;
    assert_eq!(result.status.code(), Some(1));
    assert!(result.stdout.is_empty());
    assert_eq!(
        result.stderr,
        b"movement provider refused configuration or encountered an integrity/transport failure\n"
    );
    Ok(())
}

#[test]
fn probe_refuses_incomplete_transport_configuration_and_an_absent_listener() -> std::io::Result<()>
{
    let unconfigured = Command::new(env!("CARGO_BIN_EXE_layerx-human-movement-provider"))
        .arg("probe")
        .env_clear()
        .output()?;
    assert_eq!(unconfigured.status.code(), Some(1));
    assert!(unconfigured.stdout.is_empty());
    let absent = Command::new(env!("CARGO_BIN_EXE_layerx-human-movement-provider"))
        .arg("probe")
        .env_clear()
        .env(
            "LAYERX_HUMAN_MOVEMENT_PROVIDER_SOCKET",
            "/var/empty/layerx-human-movement-probe.sock",
        )
        .env("LAYERX_HUMAN_MOVEMENT_PROVIDER_DEADLINE_SECONDS", "2")
        .env("LAYERX_HUMAN_MOVEMENT_PROVIDER_MAX_FRAME_BYTES", "1048576")
        .env("LAYERX_HUMAN_MOVEMENT_PROVIDER_PROTOCOL_VERSION", "2")
        .output()?;
    assert_eq!(absent.status.code(), Some(1));
    assert!(absent.stdout.is_empty());
    Ok(())
}

use sha2::{Digest, Sha256};
use std::fs::{self, DirBuilder, OpenOptions};
use std::io::Write;
use std::os::unix::fs::{symlink, DirBuilderExt, MetadataExt, OpenOptionsExt, PermissionsExt};
use std::path::{Path, PathBuf};

const CAPTURED_PROFILE: &[u8] =
    include_bytes!("../../../../tests/fixtures/custody/native-credit-receipt/profile");
const REFUSAL: &[u8] =
    b"movement provider refused configuration or encountered an integrity/transport failure\n";

struct CustodyStartup {
    root: PathBuf,
    profile: Vec<u8>,
    cases: Vec<String>,
}

impl CustodyStartup {
    fn new(parent: &Path, label: &str, profile: &[u8]) -> std::io::Result<Self> {
        let root = parent.join(label);
        DirBuilder::new().mode(0o700).create(&root)?;
        for name in ["state", "evidence", "material"] {
            DirBuilder::new().mode(0o700).create(root.join(name))?;
        }
        let value = Self {
            root,
            profile: profile.to_vec(),
            cases: Vec::new(),
        };
        value.write("material/profile", profile)?;
        let ca = fs::read(
            std::env::var_os("LAYERX_MOVEMENT_CUSTODY_CA_DER").expect("actual owner CA required"),
        )?;
        value.write("material/ca.der", &ca)?;
        Ok(value)
    }

    fn write(&self, name: &str, bytes: &[u8]) -> std::io::Result<()> {
        let path = self.root.join(name);
        if path.exists() {
            fs::remove_file(&path)?;
        }
        let mut file = OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(path)?;
        file.write_all(bytes)?;
        file.sync_all()
    }

    fn pin(&self) -> Vec<u8> {
        let mut bytes = b"LXMPA1".to_vec();
        bytes.extend_from_slice(&self.profile);
        for key in [
            "LAYERX_MOVEMENT_CUSTODY_CHECKPOINT_AUTHORITY",
            "LAYERX_MOVEMENT_CUSTODY_REFERENCE",
            "LAYERX_MOVEMENT_CUSTODY_CHECKPOINT_REGISTRY",
        ] {
            let value = std::env::var(key).expect("actual owner authority");
            let digits = value.strip_prefix("0x").expect("canonical authority hex");
            for offset in (0..digits.len()).step_by(2) {
                bytes.push(
                    u8::from_str_radix(&digits[offset..offset + 2], 16).expect("authority hex"),
                );
            }
        }
        assert_eq!(bytes.len(), 313);
        bytes
    }

    fn command(&self) -> Command {
        let executable = std::env::var_os("LAYERX_MOVEMENT_CUSTODY_BINARY")
            .unwrap_or_else(|| env!("CARGO_BIN_EXE_layerx-human-movement-provider").into());
        let mut command = Command::new(executable);
        command.arg("validate-config").env_clear();
        let values = [
            ("MODE", "evidence-only".to_owned()),
            ("DEADLINE_SECONDS", "2".to_owned()),
            ("MAX_FRAME_BYTES", "1048576".to_owned()),
            ("PAXEER_CHAIN_ID", "125".to_owned()),
            (
                "PAXEER_CA_DER",
                self.root.join("material/ca.der").display().to_string(),
            ),
            (
                "PAXEER_RPC_URLS",
                std::env::var("LAYERX_MOVEMENT_CUSTODY_RPC_URLS")
                    .expect("actual projected Paxeer origins"),
            ),
            ("PAXEER_MINIMUM_AGREEMENT", "2".to_owned()),
            ("PAXEER_CONFIRMATIONS", "1".to_owned()),
            ("PROTOCOL_VERSION", "3".to_owned()),
            (
                "NETWORK_ID",
                u32::from_be_bytes(self.profile[201..205].try_into().expect("network")).to_string(),
            ),
            (
                "CUSTODY_PROFILE",
                self.root.join("material/profile").display().to_string(),
            ),
            (
                "CUSTODY_PROFILE_SHA256",
                format!("0x{:x}", Sha256::digest(&self.profile)),
            ),
            (
                "SOCKET",
                self.root.join("provider.sock").display().to_string(),
            ),
            (
                "ALLOWED_UID",
                rustix::process::geteuid().as_raw().to_string(),
            ),
            (
                "ALLOWED_GID",
                rustix::process::getegid().as_raw().to_string(),
            ),
            ("STATE_ROOT", self.root.join("state").display().to_string()),
            (
                "EVIDENCE_ROOT",
                self.root.join("evidence").display().to_string(),
            ),
            (
                "PAXEER_CHECKPOINT_REGISTRY",
                std::env::var("LAYERX_MOVEMENT_CUSTODY_CHECKPOINT_REGISTRY")
                    .expect("actual checkpoint registry"),
            ),
            (
                "PAXEER_CHECKPOINT_AUTHORITY",
                std::env::var("LAYERX_MOVEMENT_CUSTODY_CHECKPOINT_AUTHORITY")
                    .expect("actual checkpoint authority"),
            ),
            (
                "CUSTODY_REFERENCE",
                std::env::var("LAYERX_MOVEMENT_CUSTODY_REFERENCE")
                    .expect("actual custody reference"),
            ),
            ("CHECKPOINT_INTERVAL_SECONDS", "1".to_owned()),
            ("PAXEER_BLOCK_SECONDS", "1".to_owned()),
            ("REMINDER_INTERVAL_SECONDS", "1".to_owned()),
            ("POLL_SECONDS", "1".to_owned()),
            ("DELAYED_AFTER_POLLS", "1".to_owned()),
        ];
        for (key, value) in values {
            command.env(format!("LAYERX_HUMAN_MOVEMENT_PROVIDER_{key}"), value);
        }
        command
    }

    fn check(&mut self, name: &str, command: &mut Command, success: bool) -> std::io::Result<()> {
        let result = command.output()?;
        assert_eq!(
            result.status.code(),
            Some(if success { 0 } else { 1 }),
            "{name}"
        );
        assert!(result.stdout.is_empty(), "{name}");
        assert_eq!(
            result.stderr,
            if success { &b""[..] } else { REFUSAL },
            "{name}"
        );
        assert!(
            !self.root.join("provider.sock").exists(),
            "validation must not serve"
        );
        if self.root.join("state/custody-profile.pin").exists() {
            assert_eq!(
                fs::read(self.root.join("state/custody-profile.pin"))?,
                self.pin(),
                "{name}: retained bytes"
            );
        }
        self.cases.push(name.to_owned());
        Ok(())
    }

    fn run(mut self) -> std::io::Result<Vec<String>> {
        assert_eq!(self.profile.len(), 223);
        let network = u32::from_be_bytes(self.profile[201..205].try_into().expect("network"));
        layerx_paxeer_client::validate_native_custody_profile(&self.profile, network)
            .expect("actual captured/owner native profile");
        let mut command = self.command();
        self.check("valid-profile", &mut command, true)?;
        let pin = self.root.join("state/custody-profile.pin");
        let metadata = fs::symlink_metadata(&pin)?;
        assert!(metadata.is_file());
        assert_eq!(metadata.uid(), rustix::process::geteuid().as_raw());
        assert_eq!(metadata.mode() & 0o777, 0o600);
        assert_eq!(metadata.nlink(), 1);
        let mut command = self.command();
        self.check("same-profile-restart", &mut command, true)?;
        assert_eq!(fs::metadata(&pin)?.ino(), metadata.ino());
        let mut command = self.command();
        command.env_remove("LAYERX_HUMAN_MOVEMENT_PROVIDER_CUSTODY_PROFILE");
        self.check("missing-path", &mut command, false)?;
        fs::remove_file(self.root.join("material/profile"))?;
        let mut command = self.command();
        self.check("absent-file", &mut command, false)?;
        self.write("material/profile", &self.profile)?;
        let mut command = self.command();
        command.env(
            "LAYERX_HUMAN_MOVEMENT_PROVIDER_NETWORK_ID",
            network
                .checked_add(1)
                .expect("network increment")
                .to_string(),
        );
        self.check("wrong-network", &mut command, false)?;
        let mut command = self.command();
        command.env(
            "LAYERX_HUMAN_MOVEMENT_PROVIDER_CUSTODY_PROFILE_SHA256",
            format!("0x{}", "00".repeat(32)),
        );
        self.check("wrong-policy-hash", &mut command, false)?;
        for (name, start, end) in [
            ("wrong-module", 33, 65),
            ("wrong-asset", 97, 129),
            ("wrong-reserve", 129, 161),
            ("wrong-light-authority", 65, 97),
            ("wrong-protocol", 205, 207),
        ] {
            let mut changed = self.profile.clone();
            changed[start..end].fill(0);
            self.write("material/profile", &changed)?;
            let mut command = self.command();
            command.env(
                "LAYERX_HUMAN_MOVEMENT_PROVIDER_CUSTODY_PROFILE_SHA256",
                format!("0x{:x}", Sha256::digest(&changed)),
            );
            self.check(name, &mut command, false)?;
        }
        for (name, length) in [("short-profile", 222), ("long-profile", 224)] {
            let mut changed = self.profile.clone();
            changed.resize(length, 0);
            self.write("material/profile", &changed)?;
            let mut command = self.command();
            self.check(name, &mut command, false)?;
        }
        self.write("material/profile", &self.profile)?;
        fs::set_permissions(
            self.root.join("material/profile"),
            fs::Permissions::from_mode(0o640),
        )?;
        let mut command = self.command();
        self.check("group-readable", &mut command, false)?;
        fs::set_permissions(
            self.root.join("material/profile"),
            fs::Permissions::from_mode(0o600),
        )?;
        fs::set_permissions(
            self.root.join("material"),
            fs::Permissions::from_mode(0o750),
        )?;
        let mut command = self.command();
        self.check("group-readable-directory", &mut command, false)?;
        fs::set_permissions(
            self.root.join("material"),
            fs::Permissions::from_mode(0o700),
        )?;
        self.write("material/link-target", &self.profile)?;
        fs::remove_file(self.root.join("material/profile"))?;
        symlink(
            self.root.join("material/link-target"),
            self.root.join("material/profile"),
        )?;
        let mut command = self.command();
        self.check("symlink-profile", &mut command, false)?;
        fs::remove_file(self.root.join("material/profile"))?;
        self.write("material/profile", &self.profile)?;
        assert_eq!(
            rustix::process::geteuid().as_raw(),
            0,
            "real non-owner refusal requires disposable root fixture"
        );
        rustix::fs::chown(
            self.root.join("material/profile"),
            Some(rustix::process::Uid::from_raw(65534)),
            None,
        )?;
        let mut command = self.command();
        self.check("non-owner", &mut command, false)?;
        rustix::fs::chown(
            self.root.join("material/profile"),
            Some(rustix::process::Uid::ROOT),
            None,
        )?;
        let mut changed = self.profile.clone();
        changed[65] ^= 1;
        layerx_paxeer_client::validate_native_custody_profile(&changed, network)
            .expect("changed structurally valid authority");
        self.write("material/profile", &changed)?;
        let mut command = self.command();
        command.env(
            "LAYERX_HUMAN_MOVEMENT_PROVIDER_CUSTODY_PROFILE_SHA256",
            format!("0x{:x}", Sha256::digest(&changed)),
        );
        self.check("changed-authority-retained-pin", &mut command, false)?;
        self.write("material/profile", &self.profile)?;
        for (case, suffix, fixture) in [
            (
                "changed-checkpoint-authority",
                "PAXEER_CHECKPOINT_AUTHORITY",
                "LAYERX_MOVEMENT_CUSTODY_CHECKPOINT_AUTHORITY",
            ),
            (
                "changed-custody-reference",
                "CUSTODY_REFERENCE",
                "LAYERX_MOVEMENT_CUSTODY_REFERENCE",
            ),
            (
                "changed-checkpoint-registry",
                "PAXEER_CHECKPOINT_REGISTRY",
                "LAYERX_MOVEMENT_CUSTODY_CHECKPOINT_REGISTRY",
            ),
        ] {
            let original = std::env::var(fixture).expect("actual owner authority");
            let mut bytes = original.into_bytes();
            bytes[2] = if bytes[2] == b'1' { b'2' } else { b'1' };
            let mut command = self.command();
            command.env(
                format!("LAYERX_HUMAN_MOVEMENT_PROVIDER_{suffix}"),
                String::from_utf8(bytes).expect("hex"),
            );
            self.check(case, &mut command, false)?;
        }
        let mut command = self.command();
        self.check("restored-profile-restart", &mut command, true)?;
        assert_eq!(fs::metadata(&pin)?.ino(), metadata.ino());
        assert_eq!(fs::read_dir(self.root.join("state"))?.count(), 1);
        Ok(self.cases)
    }
}

fn movement_role_startup(profile: &[u8]) -> std::io::Result<Vec<String>> {
    let mut nonce = [0_u8; 16];
    getrandom::fill(&mut nonce).expect("disposable fixture nonce");
    let parent = std::env::temp_dir().join(format!(
        "layerx-movement-custody-{:x}",
        Sha256::digest(nonce)
    ));
    DirBuilder::new().mode(0o755).create(&parent)?;
    fs::set_permissions(&parent, fs::Permissions::from_mode(0o755))?;
    let executable = PathBuf::from(
        std::env::var_os("LAYERX_MOVEMENT_CUSTODY_BINARY").expect("actual bound movement binary"),
    );
    let copied = parent.join("movement-provider");
    fs::copy(&executable, &copied)?;
    assert_eq!(
        Sha256::digest(fs::read(&executable)?),
        Sha256::digest(fs::read(&copied)?)
    );
    fs::set_permissions(&copied, fs::Permissions::from_mode(0o755))?;
    let mut fixture = CustodyStartup::new(&parent, "role", profile)?;
    let uid = Some(rustix::process::Uid::from_raw(4020));
    let gid = Some(rustix::process::Gid::from_raw(4020));
    for name in [
        "",
        "state",
        "evidence",
        "material",
        "material/profile",
        "material/ca.der",
    ] {
        rustix::fs::chown(fixture.root.join(name), uid, gid)?;
    }
    for case in ["movement-uid4020-startup", "movement-uid4020-restart"] {
        let configuration = fixture.command();
        let mut command = Command::new("/usr/bin/setpriv");
        command
            .args(["--reuid=4020", "--regid=4020", "--clear-groups", "--"])
            .arg(&copied)
            .arg("validate-config")
            .env_clear();
        for (key, value) in configuration.get_envs() {
            if let Some(value) = value {
                command.env(key, value);
            }
        }
        command
            .env("LAYERX_HUMAN_MOVEMENT_PROVIDER_ALLOWED_UID", "4020")
            .env("LAYERX_HUMAN_MOVEMENT_PROVIDER_ALLOWED_GID", "4020");
        fixture.check(case, &mut command, true)?;
        let pin = fs::symlink_metadata(fixture.root.join("state/custody-profile.pin"))?;
        assert_eq!(pin.uid(), 4020);
        assert_eq!(pin.gid(), 4020);
        assert_eq!(pin.mode() & 0o777, 0o600);
        assert_eq!(pin.nlink(), 1);
        assert_eq!(pin.len(), 313);
    }
    let cases = fixture.cases;
    fs::remove_dir_all(parent)?;
    Ok(cases)
}

#[test]
fn custody_profile_startup_enforces_owner_binding_and_retained_pin() -> std::io::Result<()> {
    let evidence = PathBuf::from(
        std::env::var_os("LAYERX_MOVEMENT_CUSTODY_TEST_EVIDENCE")
            .expect("protected qualification evidence directory required"),
    );
    let owner_path = PathBuf::from(
        std::env::var_os("LAYERX_MOVEMENT_CUSTODY_OWNER_PROFILE")
            .expect("actual owner profile required"),
    );
    let owner = fs::read(owner_path)?;
    let captured = CustodyStartup::new(&evidence, "captured", CAPTURED_PROFILE)?.run()?;
    let actual = CustodyStartup::new(&evidence, "owner", &owner)?.run()?;
    assert_eq!(captured, actual);
    let role_cases = movement_role_startup(&owner)?;
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(evidence.join("startup-cases.json"))?;
    file.write_all(&serde_json::to_vec(&serde_json::json!({"version":1,"captured":captured,"owner":actual,"role_cases":role_cases,"owner_profile_sha256":format!("{:x}",Sha256::digest(&owner))}))?)?;
    file.sync_all()
}
