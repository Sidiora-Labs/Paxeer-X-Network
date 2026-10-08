//! V1 private-object profile: verification of XChaCha20-Poly1305 frames over
//! u64BE(length)||bytes, and a key provider that wraps per-object keys for the
//! service KMS role.
use chacha20poly1305::aead::{Aead, KeyInit, Payload};
use chacha20poly1305::{XChaCha20Poly1305, XNonce};
use layerx_programs_ai_market::evidence::ArtifactError;
use sha2::{Digest, Sha256};
use std::io::Read;
use std::path::Path;

pub const FRAME_AAD_PREFIX: &[u8] = b"PAXAI/private-frame/v1\0";
pub const KEY_WRAP_DOMAIN: &[u8] = b"PAXAI/object-key-wrap/v1\0";
pub const FRAME_VERSION: u16 = 1;
pub const MAX_FRAME_PLAINTEXT: usize = 262_144;
pub const FRAME_HEADER_BYTES: usize = 2 + 4 + 24 + 4;
pub const TAG_BYTES: usize = 16;

/// Frame associated data: C (chain||program||market||policy), subject and kind.
#[derive(Clone, Copy, Debug)]
pub struct FrameContext {
    pub context: [u8; 128],
    pub subject: [u8; 32],
    pub kind: u8,
}

fn frame_aad(ctx: &FrameContext, index: u32) -> Vec<u8> {
    let mut aad = Vec::with_capacity(FRAME_AAD_PREFIX.len() + 128 + 32 + 1 + 4);
    aad.extend_from_slice(FRAME_AAD_PREFIX);
    aad.extend_from_slice(&ctx.context);
    aad.extend_from_slice(&ctx.subject);
    aad.push(ctx.kind);
    aad.extend_from_slice(&index.to_be_bytes());
    aad
}

pub fn random<const N: usize>() -> Result<[u8; N], ArtifactError> {
    let mut out = [0u8; N];
    getrandom::getrandom(&mut out).map_err(|_| ArtifactError::StorageFailure)?;
    Ok(out)
}

/// A newly generated 256-bit object key; the service issues one per staging
/// session, so a key is never reused across objects.
pub fn new_object_key() -> Result<[u8; 32], ArtifactError> {
    random::<32>()
}

fn read_full(reader: &mut impl Read, buf: &mut [u8]) -> Result<usize, ArtifactError> {
    let mut filled = 0;
    while filled < buf.len() {
        match reader.read(&mut buf[filled..]) {
            Ok(0) => break,
            Ok(n) => filled += n,
            Err(e) if e.kind() == std::io::ErrorKind::Interrupted => {}
            Err(_) => return Err(ArtifactError::StorageFailure),
        }
    }
    Ok(filled)
}

/// Opens an encoded frame stream. Every frame is authenticated before its
/// plaintext reaches `sink`; exact sequential indices, full non-final frames,
/// the declared plaintext length and the absence of trailing bytes are checked,
/// and only `Ok` establishes a complete object. Returns the plaintext length.
pub fn open_stream(
    key: &[u8; 32],
    ctx: &FrameContext,
    mut reader: impl Read,
    mut sink: impl FnMut(&[u8]),
) -> Result<u64, ArtifactError> {
    let cipher = XChaCha20Poly1305::new(key.into());
    let mut expected = 0u32;
    let mut declared: Option<u64> = None;
    let mut total = 0u64;
    let mut short_frame_seen = false;
    loop {
        let mut header = [0u8; FRAME_HEADER_BYTES];
        match read_full(&mut reader, &mut header)? {
            0 => break,
            FRAME_HEADER_BYTES => {}
            _ => return Err(ArtifactError::IntegrityConflict),
        }
        if short_frame_seen {
            return Err(ArtifactError::IntegrityConflict);
        }
        if u16::from_be_bytes([header[0], header[1]]) != FRAME_VERSION {
            return Err(ArtifactError::UnsupportedVersion);
        }
        let index = u32::from_be_bytes([header[2], header[3], header[4], header[5]]);
        if index != expected {
            return Err(ArtifactError::IntegrityConflict);
        }
        let length = u32::from_be_bytes([header[30], header[31], header[32], header[33]]) as usize;
        if !(TAG_BYTES + 1..=MAX_FRAME_PLAINTEXT + TAG_BYTES).contains(&length) {
            return Err(ArtifactError::IntegrityConflict);
        }
        let mut sealed = vec![0u8; length];
        if read_full(&mut reader, &mut sealed)? != length {
            return Err(ArtifactError::IntegrityConflict);
        }
        let aad = frame_aad(ctx, index);
        let opened = cipher
            .decrypt(
                XNonce::from_slice(&header[6..30]),
                Payload {
                    msg: &sealed,
                    aad: &aad,
                },
            )
            .map_err(|_| ArtifactError::IntegrityConflict)?;
        short_frame_seen = opened.len() < MAX_FRAME_PLAINTEXT;
        let part = match declared {
            Some(_) => opened.as_slice(),
            None => {
                let (prefix, rest) = opened
                    .split_first_chunk::<8>()
                    .ok_or(ArtifactError::IntegrityConflict)?;
                declared = Some(u64::from_be_bytes(*prefix));
                rest
            }
        };
        total = total
            .checked_add(part.len() as u64)
            .ok_or(ArtifactError::LengthMismatch)?;
        if declared.is_some_and(|d| total > d) {
            return Err(ArtifactError::LengthMismatch);
        }
        sink(part);
        expected = expected
            .checked_add(1)
            .ok_or(ArtifactError::IntegrityConflict)?;
    }
    match declared {
        Some(d) if d == total => Ok(total),
        Some(_) => Err(ArtifactError::LengthMismatch),
        None => Err(ArtifactError::IntegrityConflict),
    }
}

/// Service KMS role: wraps and releases per-object keys under a key-encryption key.
pub trait KeyProvider: Send + Sync {
    fn key_id(&self) -> &str;
    fn wrap(&self, aad: &[u8], key: &[u8; 32]) -> Result<Vec<u8>, ArtifactError>;
    fn unwrap(&self, aad: &[u8], wrapped: &[u8]) -> Result<[u8; 32], ArtifactError>;
}

/// Local key provider: a 32-byte key-encryption key read from an operator file.
pub struct LocalFileKeyProvider {
    kek: [u8; 32],
    id: String,
}

impl LocalFileKeyProvider {
    pub fn load(path: &Path) -> Result<Self, ArtifactError> {
        let bytes = std::fs::read(path).map_err(|_| ArtifactError::StorageFailure)?;
        let kek: [u8; 32] = bytes
            .as_slice()
            .try_into()
            .map_err(|_| ArtifactError::Malformed)?;
        let mut h = Sha256::new();
        h.update(b"PAXAI/kek-id/v1\0");
        h.update(kek);
        let id = hex::encode(&h.finalize()[..8]);
        Ok(Self { kek, id })
    }
}

impl KeyProvider for LocalFileKeyProvider {
    fn key_id(&self) -> &str {
        &self.id
    }
    fn wrap(&self, aad: &[u8], key: &[u8; 32]) -> Result<Vec<u8>, ArtifactError> {
        let cipher = XChaCha20Poly1305::new((&self.kek).into());
        let nonce = random::<24>()?;
        let full = [KEY_WRAP_DOMAIN, aad].concat();
        let sealed = cipher
            .encrypt(
                XNonce::from_slice(&nonce),
                Payload {
                    msg: key,
                    aad: &full,
                },
            )
            .map_err(|_| ArtifactError::StorageFailure)?;
        Ok([nonce.as_slice(), &sealed].concat())
    }
    fn unwrap(&self, aad: &[u8], wrapped: &[u8]) -> Result<[u8; 32], ArtifactError> {
        if wrapped.len() != 24 + 32 + TAG_BYTES {
            return Err(ArtifactError::IntegrityConflict);
        }
        let cipher = XChaCha20Poly1305::new((&self.kek).into());
        let full = [KEY_WRAP_DOMAIN, aad].concat();
        let opened = cipher
            .decrypt(
                XNonce::from_slice(&wrapped[..24]),
                Payload {
                    msg: &wrapped[24..],
                    aad: &full,
                },
            )
            .map_err(|_| ArtifactError::IntegrityConflict)?;
        opened
            .as_slice()
            .try_into()
            .map_err(|_| ArtifactError::IntegrityConflict)
    }
}
