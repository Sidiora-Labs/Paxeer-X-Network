use super::AuditError;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(super) enum WireError {
    Truncated,
    Overflow,
}

impl From<WireError> for AuditError {
    fn from(value: WireError) -> Self {
        match value {
            WireError::Truncated => Self::Corrupt("truncated audit bytes"),
            WireError::Overflow => Self::SizeOverflow,
        }
    }
}

pub(super) fn push_length(output: &mut Vec<u8>, value: usize) -> Result<(), WireError> {
    let value = u32::try_from(value).map_err(|_| WireError::Overflow)?;
    output.extend_from_slice(&value.to_be_bytes());
    Ok(())
}

pub(super) fn push_bytes(output: &mut Vec<u8>, bytes: &[u8]) -> Result<(), WireError> {
    push_length(output, bytes.len())?;
    output.extend_from_slice(bytes);
    Ok(())
}

pub(super) struct Reader<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Reader<'a> {
    pub(super) const fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, offset: 0 }
    }

    pub(super) fn take(&mut self, length: usize) -> Result<&'a [u8], WireError> {
        let end = self.offset.checked_add(length).ok_or(WireError::Overflow)?;
        let value = self
            .bytes
            .get(self.offset..end)
            .ok_or(WireError::Truncated)?;
        self.offset = end;
        Ok(value)
    }

    pub(super) fn byte(&mut self) -> Result<u8, WireError> {
        self.take(1)?.first().copied().ok_or(WireError::Truncated)
    }

    pub(super) fn u64(&mut self) -> Result<u64, WireError> {
        let mut encoded = [0_u8; 8];
        encoded.copy_from_slice(self.take(8)?);
        Ok(u64::from_be_bytes(encoded))
    }

    pub(super) fn length(&mut self) -> Result<usize, WireError> {
        let mut encoded = [0_u8; 4];
        encoded.copy_from_slice(self.take(4)?);
        usize::try_from(u32::from_be_bytes(encoded)).map_err(|_| WireError::Overflow)
    }

    pub(super) fn bytes(&mut self) -> Result<&'a [u8], WireError> {
        let length = self.length()?;
        self.take(length)
    }

    pub(super) fn array(&mut self) -> Result<[u8; 32], WireError> {
        let mut value = [0_u8; 32];
        value.copy_from_slice(self.take(32)?);
        Ok(value)
    }

    pub(super) const fn is_empty(&self) -> bool {
        self.offset == self.bytes.len()
    }
}
