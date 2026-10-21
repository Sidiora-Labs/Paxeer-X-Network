use std::fs::{self, File, OpenOptions};
use std::io::{self, Write};
use std::path::Path;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(crate) enum CodecError {
    Truncated,
    Overflow,
}

pub(crate) fn push_length(output: &mut Vec<u8>, value: usize) -> Result<(), CodecError> {
    let value = u32::try_from(value).map_err(|_| CodecError::Overflow)?;
    output.extend_from_slice(&value.to_be_bytes());
    Ok(())
}

pub(crate) fn push_bytes(output: &mut Vec<u8>, bytes: &[u8]) -> Result<(), CodecError> {
    push_length(output, bytes.len())?;
    output.extend_from_slice(bytes);
    Ok(())
}

pub(crate) struct Decoder<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Decoder<'a> {
    pub(crate) const fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, offset: 0 }
    }

    pub(crate) fn take(&mut self, length: usize) -> Result<&'a [u8], CodecError> {
        let end = self
            .offset
            .checked_add(length)
            .ok_or(CodecError::Overflow)?;
        let value = self
            .bytes
            .get(self.offset..end)
            .ok_or(CodecError::Truncated)?;
        self.offset = end;
        Ok(value)
    }

    pub(crate) fn byte(&mut self) -> Result<u8, CodecError> {
        self.take(1)?.first().copied().ok_or(CodecError::Truncated)
    }

    pub(crate) fn u32(&mut self) -> Result<u32, CodecError> {
        let mut encoded = [0_u8; 4];
        encoded.copy_from_slice(self.take(4)?);
        Ok(u32::from_be_bytes(encoded))
    }

    pub(crate) fn u64(&mut self) -> Result<u64, CodecError> {
        let mut encoded = [0_u8; 8];
        encoded.copy_from_slice(self.take(8)?);
        Ok(u64::from_be_bytes(encoded))
    }

    pub(crate) fn length(&mut self) -> Result<usize, CodecError> {
        usize::try_from(self.u32()?).map_err(|_| CodecError::Overflow)
    }

    pub(crate) fn bytes(&mut self) -> Result<&'a [u8], CodecError> {
        let length = self.length()?;
        self.take(length)
    }

    pub(crate) const fn is_empty(&self) -> bool {
        self.offset == self.bytes.len()
    }
}

pub(crate) fn write_atomic(
    directory: &Path,
    final_name: &str,
    temp_name: &str,
    bytes: &[u8],
) -> io::Result<()> {
    fs::create_dir_all(directory)?;
    let temp_path = directory.join(temp_name);
    let mut file = OpenOptions::new()
        .create(true)
        .truncate(true)
        .write(true)
        .open(&temp_path)?;
    file.write_all(bytes)?;
    file.sync_all()?;
    fs::rename(temp_path, directory.join(final_name))?;
    File::open(directory)?.sync_all()?;
    Ok(())
}
