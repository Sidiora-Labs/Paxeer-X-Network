use super::class::{DetailLevel, NotificationClass, CLASS_COUNT};
use super::NotifyError;

const MAGIC: &[u8; 4] = b"LXNP";
const VERSION: u8 = 1;

/// One notification delivery channel.
#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub enum Channel {
    Push,
    Email,
    InApp,
}

impl Channel {
    /// Every supported channel in stable contract order.
    pub const ALL: [Self; 3] = [Self::Push, Self::Email, Self::InApp];

    /// Returns the stable channel name.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Push => "push",
            Self::Email => "email",
            Self::InApp => "in-app",
        }
    }

    pub(crate) const fn code(self) -> u8 {
        match self {
            Self::Push => 1,
            Self::Email => 2,
            Self::InApp => 3,
        }
    }

    pub(crate) const fn from_code(value: u8) -> Option<Self> {
        match value {
            1 => Some(Self::Push),
            2 => Some(Self::Email),
            3 => Some(Self::InApp),
            _ => None,
        }
    }

    const fn index(self) -> usize {
        match self {
            Self::Push => 0,
            Self::Email => 1,
            Self::InApp => 2,
        }
    }
}

/// One channel toggle with its nested per-event-class choices.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ChannelPreferences {
    enabled: bool,
    classes: [bool; CLASS_COUNT],
}

impl ChannelPreferences {
    /// Returns whether the channel-level toggle is on.
    #[must_use]
    pub const fn enabled(&self) -> bool {
        self.enabled
    }

    /// Returns whether this event class is selected inside the channel.
    #[must_use]
    pub const fn class_enabled(&self, class: NotificationClass) -> bool {
        self.classes[class.index()]
    }
}

/// Principal-owned notification preferences, applied from durable storage on
/// every dispatch.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Preferences {
    detail: DetailLevel,
    channels: [ChannelPreferences; 3],
}

impl Default for Preferences {
    fn default() -> Self {
        Self {
            detail: DetailLevel::Summary,
            channels: std::array::from_fn(|_| ChannelPreferences {
                enabled: true,
                classes: [true; CLASS_COUNT],
            }),
        }
    }
}

impl Preferences {
    /// Returns the notification detail level.
    #[must_use]
    pub const fn detail(&self) -> DetailLevel {
        self.detail
    }

    /// Changes the notification detail level.
    pub const fn set_detail(&mut self, detail: DetailLevel) {
        self.detail = detail;
    }

    /// Returns one channel's nested preferences.
    #[must_use]
    pub const fn channel(&self, channel: Channel) -> &ChannelPreferences {
        &self.channels[channel.index()]
    }

    /// Applies the channel-level toggle.
    pub const fn set_channel(&mut self, channel: Channel, enabled: bool) {
        self.channels[channel.index()].enabled = enabled;
    }

    /// Applies one event-class choice beneath a channel toggle.
    pub const fn set_class(&mut self, channel: Channel, class: NotificationClass, enabled: bool) {
        self.channels[channel.index()].classes[class.index()] = enabled;
    }

    pub(crate) fn selected(&self, class: NotificationClass) -> Vec<Channel> {
        let mut selected = Channel::ALL
            .into_iter()
            .filter(|channel| {
                let preference = self.channel(*channel);
                preference.enabled() && preference.class_enabled(class)
            })
            .collect::<Vec<_>>();
        if selected.is_empty() && class.security_critical() {
            selected.push(Channel::InApp);
        }
        selected
    }

    pub(crate) fn encode(&self) -> Vec<u8> {
        let mut bytes = Vec::with_capacity(4 + 2 + 3 * (1 + CLASS_COUNT));
        bytes.extend_from_slice(MAGIC);
        bytes.push(VERSION);
        bytes.push(self.detail.code());
        for channel in &self.channels {
            bytes.push(u8::from(channel.enabled));
            bytes.extend(channel.classes.iter().copied().map(u8::from));
        }
        bytes
    }

    pub(crate) fn decode(bytes: &[u8]) -> Result<Self, NotifyError> {
        let expected = 4 + 2 + 3 * (1 + CLASS_COUNT);
        if bytes.len() != expected || bytes.get(..4) != Some(MAGIC.as_slice()) {
            return Err(NotifyError::Corrupt("invalid preference record"));
        }
        if bytes[4] != VERSION {
            return Err(NotifyError::Corrupt("unknown preference version"));
        }
        let detail =
            DetailLevel::from_code(bytes[5]).ok_or(NotifyError::Corrupt("unknown detail level"))?;
        let mut offset = 6;
        let mut channels = std::array::from_fn(|_| ChannelPreferences {
            enabled: false,
            classes: [false; CLASS_COUNT],
        });
        for channel in &mut channels {
            channel.enabled = decode_bool(bytes[offset])?;
            offset += 1;
            for enabled in &mut channel.classes {
                *enabled = decode_bool(bytes[offset])?;
                offset += 1;
            }
        }
        Ok(Self { detail, channels })
    }
}

fn decode_bool(value: u8) -> Result<bool, NotifyError> {
    match value {
        0 => Ok(false),
        1 => Ok(true),
        _ => Err(NotifyError::Corrupt("invalid preference boolean")),
    }
}
