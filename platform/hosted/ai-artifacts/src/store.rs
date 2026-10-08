//! Durable local store: one fsynced metadata snapshot written by atomic rename,
//! fsynced chunk files, and an append-only event log holding identifiers only.
use layerx_programs_ai_market::evidence::{ArtifactError, ArtifactManifest, ContentAssembler};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::fs::{self, File, OpenOptions};
use std::io::{Cursor, Read, Write};
use std::path::{Path, PathBuf};

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum SessionState {
    Staging,
    Published,
    Quarantined,
    Failed,
    Expired,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum ObjectState {
    Publishing,
    Available,
    Tombstoned,
    Quarantined,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Disposition {
    pub digest: String,
    pub status: u16,
    pub body: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Session {
    pub tenant: String,
    pub request_id: String,
    pub kind: u8,
    pub privacy: u8,
    pub context: String,
    pub byte_length: u64,
    pub chunk_count: u32,
    pub expected_root: Option<String>,
    pub created_at: u64,
    pub state: SessionState,
    pub root: Option<String>,
    pub wrapped_key: Option<String>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ObjectRecord {
    pub tenant: String,
    pub session: String,
    pub manifest: String,
    pub envelope: String,
    pub declaration: Option<String>,
    pub declared_purposes: Option<u16>,
    pub rights: Option<u8>,
    pub record_digest: String,
    pub state: ObjectState,
    pub privacy: u8,
    pub byte_length: u64,
    pub chunk_count: u32,
    pub publisher: String,
    pub generation: u64,
    pub wrapped_key: Option<String>,
    pub access_generation: u64,
    pub published_at: u64,
    pub tombstone_reason: Option<u8>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Grant {
    pub issuer: String,
    pub grantee: String,
    pub root: String,
    pub task: String,
    pub purpose_mask: u16,
    pub generation: u64,
    pub expires_at: u64,
    pub revoked: bool,
    pub revocation_sequence: u64,
    pub revocation_digest: Option<String>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct KeyRelease {
    pub grant: String,
    pub root: String,
    pub generation: u64,
    pub at: u64,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct State {
    pub requests: BTreeMap<String, Disposition>,
    pub sessions: BTreeMap<String, Session>,
    pub objects: BTreeMap<String, ObjectRecord>,
    pub quota_used: BTreeMap<String, u64>,
    pub grants: BTreeMap<String, Grant>,
    pub publisher_revocations: BTreeMap<String, u64>,
    pub key_releases: Vec<KeyRelease>,
}

pub struct Store {
    dir: PathBuf,
    pub state: State,
}

const FAIL: ArtifactError = ArtifactError::StorageFailure;

fn sync_dir(path: &Path) -> Result<(), ArtifactError> {
    File::open(path)
        .and_then(|d| d.sync_all())
        .map_err(|_| FAIL)
}

fn write_durable(path: &Path, bytes: &[u8]) -> Result<(), ArtifactError> {
    let tmp = path.with_extension("tmp");
    let mut file = File::create(&tmp).map_err(|_| FAIL)?;
    file.write_all(bytes).map_err(|_| FAIL)?;
    file.sync_all().map_err(|_| FAIL)?;
    fs::rename(&tmp, path).map_err(|_| FAIL)?;
    sync_dir(path.parent().ok_or(FAIL)?)
}

impl Store {
    pub fn open(dir: &Path) -> Result<Self, ArtifactError> {
        fs::create_dir_all(dir.join("staging")).map_err(|_| FAIL)?;
        sync_dir(dir)?;
        let mut store = Self {
            dir: dir.to_path_buf(),
            state: State::default(),
        };
        store.reload()?;
        Ok(store)
    }

    pub fn reload(&mut self) -> Result<(), ArtifactError> {
        let path = self.dir.join("state.json");
        self.state = match fs::read(&path) {
            Ok(bytes) => serde_json::from_slice(&bytes).map_err(|_| FAIL)?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => State::default(),
            Err(_) => return Err(FAIL),
        };
        Ok(())
    }

    // ponytail: whole-snapshot rewrite per acknowledged operation; move to an
    // append-only journal or database when metadata volume makes this slow.
    pub fn persist(&self) -> Result<(), ArtifactError> {
        let bytes = serde_json::to_vec(&self.state).map_err(|_| FAIL)?;
        write_durable(&self.dir.join("state.json"), &bytes)
    }

    pub fn event(&self, line: &str) -> Result<(), ArtifactError> {
        let mut file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(self.dir.join("events.log"))
            .map_err(|_| FAIL)?;
        writeln!(file, "{line}").map_err(|_| FAIL)?;
        file.sync_data().map_err(|_| FAIL)
    }

    fn session_dir(&self, session: &str) -> PathBuf {
        self.dir.join("staging").join(session)
    }

    pub fn create_session_dir(&self, session: &str) -> Result<(), ArtifactError> {
        fs::create_dir_all(self.session_dir(session)).map_err(|_| FAIL)?;
        sync_dir(&self.dir.join("staging"))
    }

    pub fn read_chunk(&self, session: &str, index: u32) -> Result<Option<Vec<u8>>, ArtifactError> {
        match fs::read(self.session_dir(session).join(index.to_string())) {
            Ok(bytes) => Ok(Some(bytes)),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(None),
            Err(_) => Err(FAIL),
        }
    }

    /// An already stored chunk with different bytes is an integrity conflict.
    pub fn write_chunk(
        &self,
        session: &str,
        index: u32,
        bytes: &[u8],
    ) -> Result<(), ArtifactError> {
        if let Some(existing) = self.read_chunk(session, index)? {
            return if existing == bytes {
                Ok(())
            } else {
                Err(ArtifactError::IntegrityConflict)
            };
        }
        write_durable(&self.session_dir(session).join(index.to_string()), bytes)
    }

    /// Re-reads every durable chunk and recomputes the manifest content root;
    /// a file's existence alone never counts as a completed upload.
    pub fn verify_content(
        &self,
        session: &str,
        manifest: &ArtifactManifest<'_>,
    ) -> Result<(), ArtifactError> {
        let mut scratch = vec![[0u8; 32]; manifest.chunk_count as usize];
        let mut assembler = ContentAssembler::new(manifest, &mut scratch)?;
        for index in 0..manifest.chunk_count {
            let chunk = self
                .read_chunk(session, index)?
                .ok_or(ArtifactError::MissingChunk)?;
            assembler.deliver(index, &chunk)?;
        }
        assembler.finish()
    }
}

/// Sequential reader over a session's durable chunk files; a missing chunk is
/// an error, never an early end of stream.
pub struct ChunkReader<'a> {
    store: &'a Store,
    session: &'a str,
    count: u32,
    next: u32,
    current: Cursor<Vec<u8>>,
}

impl Store {
    pub fn chunk_reader<'a>(&'a self, session: &'a str, count: u32) -> ChunkReader<'a> {
        ChunkReader {
            store: self,
            session,
            count,
            next: 0,
            current: Cursor::new(Vec::new()),
        }
    }
}

impl Read for ChunkReader<'_> {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        loop {
            let n = self.current.read(buf)?;
            if n > 0 || buf.is_empty() || self.next >= self.count {
                return Ok(n);
            }
            let chunk = self
                .store
                .read_chunk(self.session, self.next)
                .map_err(|_| std::io::Error::other("chunk read failed"))?
                .ok_or_else(|| std::io::Error::other("chunk missing"))?;
            self.current = Cursor::new(chunk);
            self.next += 1;
        }
    }
}
