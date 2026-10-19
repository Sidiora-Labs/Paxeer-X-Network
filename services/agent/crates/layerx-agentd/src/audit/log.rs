use std::fmt::{Display, Formatter};
use std::fs::{self, File, OpenOptions};
use std::io::{self, BufReader, Read, Write};
use std::path::{Path, PathBuf};

use sha2::{Digest, Sha256};

use crate::store::TenantId;

const LOG_MAGIC: &[u8; 8] = b"LXAUD001";
const ANCHOR_MAGIC: &[u8; 8] = b"LXANC001";
const ENTRY_MAGIC: &[u8; 4] = b"LXAE";
const HASH_DOMAIN: &[u8] = b"layerx-agent-audit-v1";
const LOG_FILE: &str = "audit.chain";
const ANCHOR_FILE: &str = "audit.anchor";
const ANCHOR_TEMP_FILE: &str = "audit.anchor.tmp";
const HEADER_BYTES: usize = 40;
const ANCHOR_BYTES: usize = 80;
const MAX_PAYLOAD_BYTES: usize = 1_048_576;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ChainIssue {
    InvalidHeader,
    TruncatedEntry,
    InvalidEntryMarker,
    EntryTooLarge,
    SequenceMismatch,
    PreviousHashMismatch,
    EntryHashMismatch,
    MissingAnchor,
    InvalidAnchor,
    AnchorMismatch,
}

impl Display for ChainIssue {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        let text = match self {
            Self::InvalidHeader => "invalid log header",
            Self::TruncatedEntry => "truncated entry",
            Self::InvalidEntryMarker => "invalid entry marker",
            Self::EntryTooLarge => "entry exceeds the bounded payload size",
            Self::SequenceMismatch => "entry sequence mismatch",
            Self::PreviousHashMismatch => "previous hash mismatch",
            Self::EntryHashMismatch => "entry hash mismatch",
            Self::MissingAnchor => "trusted tail anchor is missing",
            Self::InvalidAnchor => "trusted tail anchor is invalid",
            Self::AnchorMismatch => "trusted tail anchor does not match the chain",
        };
        formatter.write_str(text)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ChainFailure {
    pub entry: u64,
    pub issue: ChainIssue,
}

impl Display for ChainFailure {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        write!(formatter, "entry {}: {}", self.entry, self.issue)
    }
}

#[derive(Debug)]
pub enum AuditError {
    Io(io::Error),
    Invalid(ChainFailure),
    TenantMismatch,
    PayloadTooLarge,
    SequenceOverflow,
    StaleWriter,
}

impl Display for AuditError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Io(error) => write!(formatter, "audit I/O failure: {error}"),
            Self::Invalid(failure) => write!(formatter, "audit chain invalid at {failure}"),
            Self::TenantMismatch => formatter.write_str("audit tenant does not match log header"),
            Self::PayloadTooLarge => formatter.write_str("audit payload exceeds its bound"),
            Self::SequenceOverflow => formatter.write_str("audit sequence exhausted"),
            Self::StaleWriter => formatter.write_str("audit log changed behind this writer"),
        }
    }
}

impl std::error::Error for AuditError {}

impl From<io::Error> for AuditError {
    fn from(value: io::Error) -> Self {
        Self::Io(value)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Verification {
    pub entries: u64,
    pub tail_hash: [u8; 32],
    pub tenant_hash: [u8; 32],
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AppendReceipt {
    pub sequence: u64,
    pub entry_hash: [u8; 32],
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub(crate) struct VerifiedFrame {
    pub sequence: u64,
    pub previous_hash: [u8; 32],
    pub payload: Vec<u8>,
    pub entry_hash: [u8; 32],
}

#[derive(Debug)]
pub struct Log {
    path: PathBuf,
    anchor_path: PathBuf,
    directory: PathBuf,
    tenant_hash: [u8; 32],
    entries: u64,
    tail_hash: [u8; 32],
}

impl Log {
    /// Opens or creates the audit chain assigned to one tenant.
    ///
    /// # Errors
    ///
    /// Returns an error when the tenant chain cannot be created, read, or verified.
    pub fn open(root: impl AsRef<Path>, tenant: &TenantId) -> Result<Self, AuditError> {
        let tenant_hash: [u8; 32] = Sha256::digest(tenant.as_str().as_bytes()).into();
        let directory = root.as_ref().join("audit").join(hex(&tenant_hash));
        fs::create_dir_all(&directory)?;
        let path = directory.join(LOG_FILE);
        let anchor_path = directory.join(ANCHOR_FILE);
        match OpenOptions::new().create_new(true).write(true).open(&path) {
            Ok(mut file) => {
                file.write_all(LOG_MAGIC)?;
                file.write_all(&tenant_hash)?;
                file.sync_all()?;
                if anchor_path.exists() {
                    return Err(invalid(0, ChainIssue::AnchorMismatch));
                }
                write_anchor(&directory, &anchor_path, tenant_hash, 0, [0; 32])?;
            }
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => {}
            Err(error) => return Err(AuditError::Io(error)),
        }
        let verified = verify_chain(&path)?;
        if verified.tenant_hash != tenant_hash {
            return Err(AuditError::TenantMismatch);
        }
        Ok(Self {
            path,
            anchor_path,
            directory,
            tenant_hash,
            entries: verified.entries,
            tail_hash: verified.tail_hash,
        })
    }

    #[must_use]
    pub fn path(&self) -> &Path {
        &self.path
    }

    #[must_use]
    pub const fn entries(&self) -> u64 {
        self.entries
    }

    /// Returns whether this chain is cryptographically bound to `tenant`.
    #[must_use]
    pub fn owns_tenant(&self, tenant: &TenantId) -> bool {
        let tenant_hash: [u8; 32] = Sha256::digest(tenant.as_str().as_bytes()).into();
        self.tenant_hash == tenant_hash
    }

    /// Durably appends an entry before invoking the supplied operation.
    ///
    /// # Errors
    ///
    /// Returns an error without invoking `operation` when the chain cannot be verified,
    /// appended, synced, and anchored durably.
    pub fn before_operation<T>(
        &mut self,
        payload: &[u8],
        operation: impl FnOnce() -> T,
    ) -> Result<(AppendReceipt, T), AuditError> {
        let receipt = self.append_durable(payload)?;
        Ok((receipt, operation()))
    }

    fn append_durable(&mut self, payload: &[u8]) -> Result<AppendReceipt, AuditError> {
        if payload.len() > MAX_PAYLOAD_BYTES {
            return Err(AuditError::PayloadTooLarge);
        }
        let mut file = OpenOptions::new().append(true).open(&self.path)?;
        let current = verify_chain(&self.path)?;
        if current.tenant_hash != self.tenant_hash
            || current.entries != self.entries
            || current.tail_hash != self.tail_hash
        {
            return Err(AuditError::StaleWriter);
        }
        let sequence = self.entries;
        let next_entries = sequence
            .checked_add(1)
            .ok_or(AuditError::SequenceOverflow)?;
        let payload_length =
            u32::try_from(payload.len()).map_err(|_| AuditError::PayloadTooLarge)?;
        let entry_hash = entry_hash(
            self.tenant_hash,
            sequence,
            self.tail_hash,
            payload_length,
            payload,
        );
        let mut frame = Vec::with_capacity(80 + payload.len());
        frame.extend_from_slice(ENTRY_MAGIC);
        frame.extend_from_slice(&sequence.to_be_bytes());
        frame.extend_from_slice(&self.tail_hash);
        frame.extend_from_slice(&payload_length.to_be_bytes());
        frame.extend_from_slice(payload);
        frame.extend_from_slice(&entry_hash);

        file.write_all(&frame)?;
        file.sync_all()?;
        write_anchor(
            &self.directory,
            &self.anchor_path,
            self.tenant_hash,
            next_entries,
            entry_hash,
        )?;
        self.entries = next_entries;
        self.tail_hash = entry_hash;
        Ok(AppendReceipt {
            sequence,
            entry_hash,
        })
    }
}

/// Verifies every entry and the separately persisted tail anchor.
///
/// # Errors
///
/// Returns the first inconsistent entry or an I/O failure that prevented verification.
pub fn verify_chain(path: impl AsRef<Path>) -> Result<Verification, AuditError> {
    let path = path.as_ref();
    let file = File::open(path)?;
    let mut reader = BufReader::new(file);
    let mut header = [0_u8; HEADER_BYTES];
    reader
        .read_exact(&mut header)
        .map_err(|_| invalid(0, ChainIssue::InvalidHeader))?;
    if &header[..8] != LOG_MAGIC {
        return Err(invalid(0, ChainIssue::InvalidHeader));
    }
    let mut tenant_hash = [0_u8; 32];
    tenant_hash.copy_from_slice(&header[8..]);

    let mut entries = 0_u64;
    let mut tail_hash = [0_u8; 32];
    loop {
        let mut marker = [0_u8; 4];
        let marker_bytes = read_until_eof(&mut reader, &mut marker)?;
        if marker_bytes == 0 {
            break;
        }
        if marker_bytes != marker.len() {
            return Err(invalid(entries, ChainIssue::TruncatedEntry));
        }
        if &marker != ENTRY_MAGIC {
            return Err(invalid(entries, ChainIssue::InvalidEntryMarker));
        }
        let sequence = read_u64(&mut reader, entries)?;
        let previous_hash = read_array::<32>(&mut reader, entries)?;
        let payload_length = read_u32(&mut reader, entries)?;
        let payload_length_usize = usize::try_from(payload_length)
            .map_err(|_| invalid(entries, ChainIssue::EntryTooLarge))?;
        if payload_length_usize > MAX_PAYLOAD_BYTES {
            return Err(invalid(entries, ChainIssue::EntryTooLarge));
        }
        let mut payload = vec![0_u8; payload_length_usize];
        reader
            .read_exact(&mut payload)
            .map_err(|_| invalid(entries, ChainIssue::TruncatedEntry))?;
        let stored_hash = read_array::<32>(&mut reader, entries)?;
        if sequence != entries {
            return Err(invalid(entries, ChainIssue::SequenceMismatch));
        }
        if previous_hash != tail_hash {
            return Err(invalid(entries, ChainIssue::PreviousHashMismatch));
        }
        let expected_hash = entry_hash(
            tenant_hash,
            sequence,
            previous_hash,
            payload_length,
            &payload,
        );
        if stored_hash != expected_hash {
            return Err(invalid(entries, ChainIssue::EntryHashMismatch));
        }
        tail_hash = stored_hash;
        entries = entries.checked_add(1).ok_or(AuditError::SequenceOverflow)?;
    }

    let anchor_path = path.with_file_name(ANCHOR_FILE);
    let anchor = match fs::read(&anchor_path) {
        Ok(value) => value,
        Err(error) if error.kind() == io::ErrorKind::NotFound => {
            return Err(invalid(entries, ChainIssue::MissingAnchor));
        }
        Err(error) => return Err(AuditError::Io(error)),
    };
    if anchor.len() != ANCHOR_BYTES || &anchor[..8] != ANCHOR_MAGIC {
        return Err(invalid(entries, ChainIssue::InvalidAnchor));
    }
    if anchor[8..40] != tenant_hash {
        return Err(invalid(entries, ChainIssue::InvalidAnchor));
    }
    let anchor_entries = u64::from_be_bytes(
        anchor[40..48]
            .try_into()
            .map_err(|_| invalid(entries, ChainIssue::InvalidAnchor))?,
    );
    if anchor_entries != entries || anchor[48..80] != tail_hash {
        return Err(invalid(entries, ChainIssue::AnchorMismatch));
    }
    Ok(Verification {
        entries,
        tail_hash,
        tenant_hash,
    })
}

pub(crate) fn read_payloads(path: impl AsRef<Path>) -> Result<Vec<Vec<u8>>, AuditError> {
    Ok(read_frames(path)?
        .into_iter()
        .map(|frame| frame.payload)
        .collect())
}

pub(crate) fn read_frames(path: impl AsRef<Path>) -> Result<Vec<VerifiedFrame>, AuditError> {
    let path = path.as_ref();
    let _verified = verify_chain(path)?;
    let bytes = fs::read(path)?;
    let mut offset = HEADER_BYTES;
    let mut frames = Vec::new();
    while offset < bytes.len() {
        let sequence_offset = offset.checked_add(4).ok_or(AuditError::SequenceOverflow)?;
        let previous_hash_offset = sequence_offset
            .checked_add(8)
            .ok_or(AuditError::SequenceOverflow)?;
        let length_offset = offset.checked_add(44).ok_or(AuditError::SequenceOverflow)?;
        let payload_offset = length_offset
            .checked_add(4)
            .ok_or(AuditError::SequenceOverflow)?;
        let length_end = payload_offset;
        let payload_length = u32::from_be_bytes(
            bytes
                .get(length_offset..length_end)
                .ok_or_else(|| invalid(frames.len() as u64, ChainIssue::TruncatedEntry))?
                .try_into()
                .map_err(|_| invalid(frames.len() as u64, ChainIssue::TruncatedEntry))?,
        );
        let payload_end = payload_offset
            .checked_add(payload_length as usize)
            .ok_or(AuditError::SequenceOverflow)?;
        let frame_end = payload_end
            .checked_add(32)
            .ok_or(AuditError::SequenceOverflow)?;
        let payload = bytes
            .get(payload_offset..payload_end)
            .ok_or_else(|| invalid(frames.len() as u64, ChainIssue::TruncatedEntry))?;
        if frame_end > bytes.len() {
            return Err(invalid(frames.len() as u64, ChainIssue::TruncatedEntry));
        }
        let sequence = u64::from_be_bytes(
            bytes
                .get(sequence_offset..previous_hash_offset)
                .ok_or_else(|| invalid(frames.len() as u64, ChainIssue::TruncatedEntry))?
                .try_into()
                .map_err(|_| invalid(frames.len() as u64, ChainIssue::TruncatedEntry))?,
        );
        let previous_hash: [u8; 32] = bytes
            .get(previous_hash_offset..length_offset)
            .ok_or_else(|| invalid(frames.len() as u64, ChainIssue::TruncatedEntry))?
            .try_into()
            .map_err(|_| invalid(frames.len() as u64, ChainIssue::TruncatedEntry))?;
        let entry_hash = bytes
            .get(payload_end..frame_end)
            .ok_or_else(|| invalid(frames.len() as u64, ChainIssue::TruncatedEntry))?
            .try_into()
            .map_err(|_| invalid(frames.len() as u64, ChainIssue::TruncatedEntry))?;
        frames.push(VerifiedFrame {
            sequence,
            previous_hash,
            payload: payload.to_vec(),
            entry_hash,
        });
        offset = frame_end;
    }
    Ok(frames)
}

fn read_until_eof(reader: &mut impl Read, output: &mut [u8]) -> Result<usize, AuditError> {
    let mut read = 0;
    while read < output.len() {
        match reader.read(&mut output[read..]) {
            Ok(0) => break,
            Ok(count) => read += count,
            Err(error) if error.kind() == io::ErrorKind::Interrupted => {}
            Err(error) => return Err(AuditError::Io(error)),
        }
    }
    Ok(read)
}

fn read_u64(reader: &mut impl Read, entry: u64) -> Result<u64, AuditError> {
    Ok(u64::from_be_bytes(read_array::<8>(reader, entry)?))
}

fn read_u32(reader: &mut impl Read, entry: u64) -> Result<u32, AuditError> {
    Ok(u32::from_be_bytes(read_array::<4>(reader, entry)?))
}

fn read_array<const N: usize>(reader: &mut impl Read, entry: u64) -> Result<[u8; N], AuditError> {
    let mut output = [0_u8; N];
    reader
        .read_exact(&mut output)
        .map_err(|_| invalid(entry, ChainIssue::TruncatedEntry))?;
    Ok(output)
}

pub(crate) fn entry_hash(
    tenant_hash: [u8; 32],
    sequence: u64,
    previous_hash: [u8; 32],
    payload_length: u32,
    payload: &[u8],
) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(HASH_DOMAIN);
    digest.update(tenant_hash);
    digest.update(sequence.to_be_bytes());
    digest.update(previous_hash);
    digest.update(payload_length.to_be_bytes());
    digest.update(payload);
    digest.finalize().into()
}

fn write_anchor(
    directory: &Path,
    anchor_path: &Path,
    tenant_hash: [u8; 32],
    entries: u64,
    tail_hash: [u8; 32],
) -> Result<(), AuditError> {
    let temporary = directory.join(ANCHOR_TEMP_FILE);
    let mut file = OpenOptions::new()
        .create(true)
        .truncate(true)
        .write(true)
        .open(&temporary)?;
    file.write_all(ANCHOR_MAGIC)?;
    file.write_all(&tenant_hash)?;
    file.write_all(&entries.to_be_bytes())?;
    file.write_all(&tail_hash)?;
    file.sync_all()?;
    fs::rename(temporary, anchor_path)?;
    File::open(directory)?.sync_all()?;
    Ok(())
}

fn invalid(entry: u64, issue: ChainIssue) -> AuditError {
    AuditError::Invalid(ChainFailure { entry, issue })
}

fn hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut output = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        output.push(char::from(DIGITS[usize::from(byte >> 4)]));
        output.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    output
}
