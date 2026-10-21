use super::class::NotificationClass;
use super::content::{email_payload, in_app_payload, push_payload, Content};
use super::event::NotificationId;
use super::preferences::Channel;
use super::NotifyError;

const MAGIC: &[u8; 4] = b"LXND";
const VERSION: u8 = 1;

/// One immutable, principal-scoped channel delivery.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Delivery {
    notification_id: NotificationId,
    class: NotificationClass,
    channel: Channel,
    created_at: u64,
    deep_link: String,
    action_copy_key: Option<String>,
    payload: String,
}

impl Delivery {
    pub(crate) fn build(
        content: &Content,
        id: NotificationId,
        channel: Channel,
        created_at: u64,
    ) -> Self {
        let payload = match channel {
            Channel::Push => push_payload(content, &id, created_at),
            Channel::Email => email_payload(content, &id, created_at),
            Channel::InApp => in_app_payload(content, &id, created_at),
        };
        Self {
            notification_id: id,
            class: content.class,
            channel,
            created_at,
            deep_link: content.deep_link.clone(),
            action_copy_key: content.action_copy_key.clone(),
            payload,
        }
    }

    /// Returns the stable notification identifier shared across channels.
    #[must_use]
    pub const fn notification_id(&self) -> &NotificationId {
        &self.notification_id
    }

    /// Returns the event class.
    #[must_use]
    pub const fn class(&self) -> NotificationClass {
        self.class
    }

    /// Returns the delivery channel.
    #[must_use]
    pub const fn channel(&self) -> Channel {
        self.channel
    }

    /// Returns the caller-injected dispatch time.
    #[must_use]
    pub const fn created_at(&self) -> u64 {
        self.created_at
    }

    /// Returns the authenticated application deep link.
    #[must_use]
    pub fn deep_link(&self) -> &str {
        &self.deep_link
    }

    /// Returns the optional action copy key.
    #[must_use]
    pub fn action_copy_key(&self) -> Option<&str> {
        self.action_copy_key.as_deref()
    }

    /// Returns the exact channel payload.
    #[must_use]
    pub fn payload(&self) -> &str {
        &self.payload
    }

    pub(crate) fn encode(&self) -> Result<Vec<u8>, NotifyError> {
        let mut bytes = Vec::new();
        bytes.extend_from_slice(MAGIC);
        bytes.push(VERSION);
        bytes.push(self.class.code());
        bytes.push(self.channel.code());
        bytes.extend_from_slice(&self.created_at.to_be_bytes());
        push_string(&mut bytes, self.notification_id.as_str())?;
        push_string(&mut bytes, &self.deep_link)?;
        match &self.action_copy_key {
            Some(action) => {
                bytes.push(1);
                push_string(&mut bytes, action)?;
            }
            None => bytes.push(0),
        }
        push_string(&mut bytes, &self.payload)?;
        Ok(bytes)
    }

    pub(crate) fn decode(bytes: &[u8]) -> Result<Self, NotifyError> {
        let mut reader = Reader::new(bytes);
        if reader.take(4)? != MAGIC || reader.byte()? != VERSION {
            return Err(NotifyError::Corrupt("invalid delivery record"));
        }
        let class = NotificationClass::from_code(reader.byte()?)
            .ok_or(NotifyError::Corrupt("unknown notification class"))?;
        let channel =
            Channel::from_code(reader.byte()?).ok_or(NotifyError::Corrupt("unknown channel"))?;
        let created_at = reader.u64()?;
        let notification_id = NotificationId::new(reader.string()?)?;
        let deep_link = reader.string()?;
        let action_copy_key = match reader.byte()? {
            0 => None,
            1 => Some(reader.string()?),
            _ => return Err(NotifyError::Corrupt("invalid optional action")),
        };
        let payload = reader.string()?;
        if !reader.is_empty() {
            return Err(NotifyError::Corrupt("trailing delivery bytes"));
        }
        Ok(Self {
            notification_id,
            class,
            channel,
            created_at,
            deep_link,
            action_copy_key,
            payload,
        })
    }
}

fn push_string(output: &mut Vec<u8>, value: &str) -> Result<(), NotifyError> {
    let length = u32::try_from(value.len()).map_err(|_| NotifyError::SizeOverflow)?;
    output.extend_from_slice(&length.to_be_bytes());
    output.extend_from_slice(value.as_bytes());
    Ok(())
}

struct Reader<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Reader<'a> {
    const fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, offset: 0 }
    }

    fn take(&mut self, length: usize) -> Result<&'a [u8], NotifyError> {
        let end = self
            .offset
            .checked_add(length)
            .ok_or(NotifyError::Corrupt("delivery length overflow"))?;
        let value = self
            .bytes
            .get(self.offset..end)
            .ok_or(NotifyError::Corrupt("truncated delivery"))?;
        self.offset = end;
        Ok(value)
    }

    fn byte(&mut self) -> Result<u8, NotifyError> {
        Ok(self.take(1)?[0])
    }

    fn u64(&mut self) -> Result<u64, NotifyError> {
        let bytes: [u8; 8] = self
            .take(8)?
            .try_into()
            .map_err(|_| NotifyError::Corrupt("invalid delivery timestamp"))?;
        Ok(u64::from_be_bytes(bytes))
    }

    fn string(&mut self) -> Result<String, NotifyError> {
        let length: [u8; 4] = self
            .take(4)?
            .try_into()
            .map_err(|_| NotifyError::Corrupt("invalid delivery string length"))?;
        let length =
            usize::try_from(u32::from_be_bytes(length)).map_err(|_| NotifyError::SizeOverflow)?;
        let text = std::str::from_utf8(self.take(length)?)
            .map_err(|_| NotifyError::Corrupt("delivery text is not UTF-8"))?;
        Ok(text.to_owned())
    }

    const fn is_empty(&self) -> bool {
        self.offset == self.bytes.len()
    }
}
