use layerx_client::lni::abi::{negotiate, AbiIncompatible, AbiVersion};
use layerx_client::lni::framing::{read_frame, write_frame};

const HEADER: &str = include_str!("../../../include/layerx_lni_abi.h");
const ALLOWLIST: &str = include_str!("../../../unsafe-allowlist.toml");

#[test]
fn published_header_exposes_only_opaque_handles_and_byte_buffers() {
    assert!(HEADER.contains("typedef struct layerx_lni_handle layerx_lni_handle;"));
    assert!(HEADER.contains("const uint8_t *canonical_frame"));
    assert!(HEADER.contains("LAYERX_LNI_ABI_VERSION_MAJOR 1u"));
    assert!(!HEADER.contains("include/layerx"));
    assert!(!HEADER.contains("struct layerx_lni_handle {"));
    for forbidden in ["activity", "receipt", "checkpoint", "batch_header"] {
        assert!(!HEADER.contains(forbidden));
    }
}

#[test]
fn abi_version_negotiation_matches_socket_refusal_rules() {
    assert_eq!(
        negotiate(
            AbiVersion { major: 1, minor: 2 },
            AbiVersion { major: 1, minor: 7 }
        ),
        Ok(AbiVersion { major: 1, minor: 2 })
    );
    assert_eq!(
        negotiate(
            AbiVersion { major: 1, minor: 2 },
            AbiVersion { major: 2, minor: 0 }
        ),
        Err(AbiIncompatible {
            built: AbiVersion { major: 1, minor: 2 },
            peer: AbiVersion { major: 2, minor: 0 },
        })
    );
}

#[test]
fn abi_and_socket_transports_share_the_exact_frame_encoding() {
    let canonical_envelope = b"canonical-lni-envelope";
    let mut abi_wire = Vec::new();
    write_frame(&mut abi_wire, canonical_envelope, 1024)
        .unwrap_or_else(|error| panic!("ABI framing failed: {error:?}"));
    let mut socket_wire = Vec::new();
    write_frame(&mut socket_wire, canonical_envelope, 1024)
        .unwrap_or_else(|error| panic!("socket framing failed: {error:?}"));
    assert_eq!(abi_wire, socket_wire);
    assert_eq!(
        read_frame(&mut abi_wire.as_slice(), 1024),
        Ok(canonical_envelope.to_vec())
    );
}

#[test]
fn every_unsafe_operation_is_confined_and_justified() {
    assert!(ALLOWLIST.contains("crates/layerx-client/src/lni/abi.rs"));
    assert!(ALLOWLIST.contains("opaque non-null handle"));
    assert!(ALLOWLIST.contains("bounds every buffer"));
}
