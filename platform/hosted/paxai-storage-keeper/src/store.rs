use std::collections::BTreeMap;
use std::fs::{File, OpenOptions};
use std::io::{Read, Seek, SeekFrom, Write};
use std::os::unix::fs::{MetadataExt, OpenOptionsExt};
use std::path::{Path, PathBuf};

use layerx_platform_authority::ai_storage_admission::{
    decode_reserve_answer, request_digest, verify_answer, AdmissionError, Observation, Profile,
    Request, Reservation, CANCEL_RESPONSE, INSTALL_RESPONSE, OBSERVATION_BYTES, OBSERVE_RESPONSE,
    PROFILE_BYTES, PROOF_BYTES, RECONCILE_RESPONSE, RECORD_BYTES, REQUEST_BYTES, RESERVE_RESPONSE,
};
use sha2::{Digest, Sha256};

const LOG_NAME: &str = "capacity-keeper.log";
const MAGIC: &[u8; 8] = b"LXKEEPR1";
const HEADER_BYTES: usize = 4 + 1;
const CHECKSUM_BYTES: usize = 32;
const MAX_BODY_BYTES: usize = REQUEST_BYTES + OBSERVATION_BYTES + PROOF_BYTES;

const KIND_PROFILE: u8 = 1;
const KIND_STAGED: u8 = 2;
const KIND_ANSWER: u8 = 3;
const KIND_ABANDONED: u8 = 4;

#[derive(Debug)]
pub enum StoreError {
    Io(std::io::Error),
    Corrupt(&'static str),
    Answer(AdmissionError),
}

impl core::fmt::Display for StoreError {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Io(error) => write!(formatter, "store io: {error}"),
            Self::Corrupt(what) => write!(formatter, "store corrupt: {what}"),
            Self::Answer(error) => write!(formatter, "store answer: {error}"),
        }
    }
}

impl From<std::io::Error> for StoreError {
    fn from(error: std::io::Error) -> Self {
        Self::Io(error)
    }
}

impl From<AdmissionError> for StoreError {
    fn from(error: AdmissionError) -> Self {
        Self::Answer(error)
    }
}

/// An exact request persisted before it is sent, with the signed observation
/// whose head it binds.
pub struct Staged {
    pub digest: [u8; 32],
    pub bytes: Vec<u8>,
    pub request: Request,
    pub observation: Observation,
}

/// A request the node refused for good; it never holds capacity.
pub struct Abandoned {
    pub class: u8,
    pub result: i32,
}

/// Durable keeper state: an append-only, checksummed, fsync'd log of signed
/// node answers and the exact requests they answer.
pub struct Store {
    file: File,
    pub profile: Option<Profile>,
    pub staged: Vec<Staged>,
    pub records: BTreeMap<u64, Reservation>,
    pub abandoned: BTreeMap<[u8; 32], Abandoned>,
    pub truncated_tail: bool,
    network_id: u32,
    sequencer: [u8; 32],
}

impl Store {
    /// Opens or creates the log and replays every record, verifying each
    /// checksum and each node signature against the pinned sequencer.
    ///
    /// # Errors
    ///
    /// Fails closed on a foreign owner, loose permissions, a wrong magic, a
    /// corrupt interior record or any answer that does not verify. A torn
    /// final record left by a crash is cut off, since nothing was sent or
    /// acknowledged on its strength.
    pub fn open(
        directory: &Path,
        network_id: u32,
        sequencer: [u8; 32],
    ) -> Result<Self, StoreError> {
        let path: PathBuf = directory.join(LOG_NAME);
        let created = !path.exists();
        let mut file = OpenOptions::new()
            .read(true)
            .append(true)
            .create(true)
            .mode(0o600)
            .open(&path)?;
        let metadata = file.metadata()?;
        let named = std::fs::symlink_metadata(&path)?;
        let directory_metadata = std::fs::metadata(directory)?;
        if !named.file_type().is_file()
            || named.dev() != metadata.dev()
            || named.ino() != metadata.ino()
            || !metadata.is_file()
            || metadata.nlink() != 1
            || metadata.mode() & 0o077 != 0
            || metadata.uid() != directory_metadata.uid()
            || directory_metadata.mode() & 0o022 != 0
        {
            return Err(StoreError::Corrupt("log ownership or permissions"));
        }
        if created || metadata.len() == 0 {
            file.write_all(MAGIC)?;
            file.sync_all()?;
            File::open(directory)?.sync_all()?;
        }
        let mut bytes = Vec::new();
        file.seek(SeekFrom::Start(0))?;
        file.read_to_end(&mut bytes)?;
        let mut store = Self {
            file,
            profile: None,
            staged: Vec::new(),
            records: BTreeMap::new(),
            abandoned: BTreeMap::new(),
            truncated_tail: false,
            network_id,
            sequencer,
        };
        store.replay(&bytes)?;
        Ok(store)
    }

    fn replay(&mut self, bytes: &[u8]) -> Result<(), StoreError> {
        if bytes.get(..MAGIC.len()) != Some(MAGIC.as_slice()) {
            return Err(StoreError::Corrupt("magic"));
        }
        let mut cursor = MAGIC.len();
        while cursor < bytes.len() {
            match frame_at(bytes, cursor) {
                Frame::Complete { kind, body, next } => {
                    self.apply(kind, body)?;
                    cursor = next;
                }
                Frame::Torn => {
                    let keep = u64::try_from(cursor).map_err(|_| StoreError::Corrupt("length"))?;
                    self.file.set_len(keep)?;
                    self.file.sync_all()?;
                    self.truncated_tail = true;
                    return Ok(());
                }
                Frame::Corrupt => return Err(StoreError::Corrupt("record checksum")),
            }
        }
        Ok(())
    }

    fn apply(&mut self, kind: u8, body: &[u8]) -> Result<(), StoreError> {
        match kind {
            KIND_PROFILE => {
                let (payload, proof) = split(body, PROFILE_BYTES)?;
                verify_answer(
                    self.network_id,
                    INSTALL_RESPONSE,
                    payload,
                    proof,
                    &self.sequencer,
                )?;
                self.profile = Some(Profile::decode(payload)?);
            }
            KIND_STAGED => {
                let (request_bytes, rest) = split_prefix(body, REQUEST_BYTES)?;
                let (observation_bytes, proof) = split(rest, OBSERVATION_BYTES)?;
                verify_answer(
                    self.network_id,
                    OBSERVE_RESPONSE,
                    observation_bytes,
                    proof,
                    &self.sequencer,
                )?;
                let request = Request::decode(request_bytes)?;
                let observation = Observation::decode(observation_bytes)?;
                observation.require_head(request.expected_sequence, &request.expected_root)?;
                let digest = request_digest(request_bytes);
                if self.staged_by_digest(&digest).is_some() {
                    return Err(StoreError::Corrupt("duplicate staged request"));
                }
                self.staged.push(Staged {
                    digest,
                    bytes: request_bytes.to_vec(),
                    request,
                    observation,
                });
            }
            KIND_ANSWER => {
                let (tag_bytes, rest) = split_prefix(body, 2)?;
                let tag = u16::from_be_bytes([tag_bytes[0], tag_bytes[1]]);
                let payload_length = match tag {
                    RESERVE_RESPONSE => 1 + RECORD_BYTES,
                    RECONCILE_RESPONSE | CANCEL_RESPONSE => RECORD_BYTES,
                    _ => return Err(StoreError::Corrupt("answer tag")),
                };
                let (payload, proof) = split(rest, payload_length)?;
                verify_answer(self.network_id, tag, payload, proof, &self.sequencer)?;
                let record = if tag == RESERVE_RESPONSE {
                    decode_reserve_answer(payload)?.1
                } else {
                    Reservation::decode(payload)?
                };
                self.accept(record)?;
            }
            KIND_ABANDONED => {
                if body.len() != 32 + 1 + 4 {
                    return Err(StoreError::Corrupt("record length"));
                }
                let mut key = [0_u8; 32];
                key.copy_from_slice(&body[..32]);
                if self.staged_by_digest(&key).is_none() {
                    return Err(StoreError::Corrupt("abandoned unknown request"));
                }
                self.abandoned.insert(
                    key,
                    Abandoned {
                        class: body[32],
                        result: i32::from_be_bytes([body[33], body[34], body[35], body[36]]),
                    },
                );
            }
            _ => return Err(StoreError::Corrupt("record kind")),
        }
        Ok(())
    }

    fn accept(&mut self, record: Reservation) -> Result<(), StoreError> {
        let staged = self
            .staged_by_digest(&record.request_digest)
            .ok_or(StoreError::Corrupt("answer without staged request"))?;
        record.require_answers(&staged.request, &staged.digest)?;
        if let Some(known) = self.records.get(&record.request_id) {
            if known.request_digest != record.request_digest {
                return Err(StoreError::Corrupt("request id rebound"));
            }
        } else if self
            .records
            .values()
            .any(|known| known.request_digest == record.request_digest)
        {
            return Err(StoreError::Corrupt("request digest rebound"));
        }
        self.records.insert(record.request_id, record);
        Ok(())
    }

    #[must_use]
    pub fn staged_by_digest(&self, digest: &[u8; 32]) -> Option<&Staged> {
        self.staged.iter().find(|staged| &staged.digest == digest)
    }

    #[must_use]
    pub fn record_for(&self, digest: &[u8; 32]) -> Option<&Reservation> {
        self.records
            .values()
            .find(|record| &record.request_digest == digest)
    }

    /// Persists an install answer.
    ///
    /// # Errors
    ///
    /// Returns an error when the answer does not verify or the write fails.
    pub fn record_profile(&mut self, payload: &[u8], proof: &[u8]) -> Result<Profile, StoreError> {
        let body = [payload, proof].concat();
        self.apply(KIND_PROFILE, &body)?;
        self.append(KIND_PROFILE, &body)?;
        self.profile.ok_or(StoreError::Corrupt("profile"))
    }

    /// Persists an exact request and the signed observation it binds before
    /// anything is sent.
    ///
    /// # Errors
    ///
    /// Returns an error for a request already staged, an observation that
    /// does not verify or bind the request's head, or a failed write.
    pub fn stage(
        &mut self,
        request_bytes: &[u8],
        observation_payload: &[u8],
        observation_proof: &[u8],
    ) -> Result<[u8; 32], StoreError> {
        let body = [request_bytes, observation_payload, observation_proof].concat();
        self.apply(KIND_STAGED, &body)?;
        self.append(KIND_STAGED, &body)?;
        Ok(request_digest(request_bytes))
    }

    /// Persists a signed reserve, reconcile or cancel answer.
    ///
    /// # Errors
    ///
    /// Returns an error when the answer does not verify, does not answer a
    /// staged request, rebinds an identity, or the write fails.
    pub fn record_answer(
        &mut self,
        tag: u16,
        payload: &[u8],
        proof: &[u8],
    ) -> Result<Reservation, StoreError> {
        let body = [&tag.to_be_bytes()[..], payload, proof].concat();
        let id = if tag == RESERVE_RESPONSE {
            decode_reserve_answer(payload)?.1.request_id
        } else {
            Reservation::decode(payload)?.request_id
        };
        self.apply(KIND_ANSWER, &body)?;
        self.append(KIND_ANSWER, &body)?;
        self.records
            .get(&id)
            .cloned()
            .ok_or(StoreError::Corrupt("answer"))
    }

    /// Persists a definitive node refusal of a staged request.
    ///
    /// # Errors
    ///
    /// Returns an error for an unknown request or a failed write.
    pub fn record_abandoned(
        &mut self,
        digest: &[u8; 32],
        class: u8,
        result: i32,
    ) -> Result<(), StoreError> {
        let body = [&digest[..], &[class], &result.to_be_bytes()[..]].concat();
        self.apply(KIND_ABANDONED, &body)?;
        self.append(KIND_ABANDONED, &body)
    }

    fn append(&mut self, kind: u8, body: &[u8]) -> Result<(), StoreError> {
        let length = u32::try_from(body.len()).map_err(|_| StoreError::Corrupt("length"))?;
        let mut frame = Vec::with_capacity(HEADER_BYTES + body.len() + CHECKSUM_BYTES);
        frame.extend_from_slice(&length.to_be_bytes());
        frame.push(kind);
        frame.extend_from_slice(body);
        frame.extend_from_slice(&checksum(kind, body));
        self.file.write_all(&frame)?;
        self.file.sync_data()?;
        Ok(())
    }
}

enum Frame<'a> {
    Complete {
        kind: u8,
        body: &'a [u8],
        next: usize,
    },
    Torn,
    Corrupt,
}

fn frame_at(bytes: &[u8], cursor: usize) -> Frame<'_> {
    let Some(header) = bytes.get(cursor..cursor.saturating_add(HEADER_BYTES)) else {
        return Frame::Torn;
    };
    let length = u32::from_be_bytes([header[0], header[1], header[2], header[3]]);
    let Ok(length) = usize::try_from(length) else {
        return Frame::Corrupt;
    };
    if length > MAX_BODY_BYTES {
        return Frame::Corrupt;
    }
    let body_start = cursor + HEADER_BYTES;
    let body_end = body_start + length;
    let next = body_end + CHECKSUM_BYTES;
    let (Some(body), Some(sum)) = (bytes.get(body_start..body_end), bytes.get(body_end..next))
    else {
        return Frame::Torn;
    };
    if checksum(header[4], body) == sum {
        Frame::Complete {
            kind: header[4],
            body,
            next,
        }
    } else if next == bytes.len() {
        Frame::Torn
    } else {
        Frame::Corrupt
    }
}

fn checksum(kind: u8, body: &[u8]) -> [u8; 32] {
    let mut hash = Sha256::new();
    hash.update(MAGIC);
    hash.update([kind]);
    hash.update(body);
    hash.finalize().into()
}

fn split(body: &[u8], first: usize) -> Result<(&[u8], &[u8]), StoreError> {
    let (head, tail) = split_prefix(body, first)?;
    if tail.len() == PROOF_BYTES {
        Ok((head, tail))
    } else {
        Err(StoreError::Corrupt("record length"))
    }
}

fn split_prefix(body: &[u8], first: usize) -> Result<(&[u8], &[u8]), StoreError> {
    if body.len() < first {
        return Err(StoreError::Corrupt("record length"));
    }
    Ok(body.split_at(first))
}
