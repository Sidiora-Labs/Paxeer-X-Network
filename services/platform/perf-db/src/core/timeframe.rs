use serde::{Deserialize, Serialize};

/// 16 supported candle timeframes.
///
/// Index values (0–15) are stored as `u8` in `CandleRecord.timeframe`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[repr(u8)]
pub enum Timeframe {
    Sec1 = 0,
    Sec5 = 1,
    Sec15 = 2,
    Sec30 = 3,
    Min1 = 4,
    Min2 = 5,
    Min3 = 6,
    Min5 = 7,
    Min15 = 8,
    Min30 = 9,
    Hour1 = 10,
    Hour2 = 11,
    Hour4 = 12,
    Hour8 = 13,
    Day1 = 14,
    Week1 = 15,
}

impl Timeframe {
    /// All 16 variants in index order.
    pub const ALL: [Timeframe; 16] = [
        Self::Sec1,
        Self::Sec5,
        Self::Sec15,
        Self::Sec30,
        Self::Min1,
        Self::Min2,
        Self::Min3,
        Self::Min5,
        Self::Min15,
        Self::Min30,
        Self::Hour1,
        Self::Hour2,
        Self::Hour4,
        Self::Hour8,
        Self::Day1,
        Self::Week1,
    ];

    /// Duration of one candle in milliseconds.
    pub const fn duration_ms(&self) -> i64 {
        match self {
            Self::Sec1 => 1_000,
            Self::Sec5 => 5_000,
            Self::Sec15 => 15_000,
            Self::Sec30 => 30_000,
            Self::Min1 => 60_000,
            Self::Min2 => 120_000,
            Self::Min3 => 180_000,
            Self::Min5 => 300_000,
            Self::Min15 => 900_000,
            Self::Min30 => 1_800_000,
            Self::Hour1 => 3_600_000,
            Self::Hour2 => 7_200_000,
            Self::Hour4 => 14_400_000,
            Self::Hour8 => 28_800_000,
            Self::Day1 => 86_400_000,
            Self::Week1 => 604_800_000,
        }
    }

    /// Duration in seconds (convenience).
    pub const fn duration_secs(&self) -> u64 {
        self.duration_ms() as u64 / 1000
    }

    /// Round a millisecond timestamp down to the start of the containing bucket.
    pub const fn bucket_start_ms(&self, timestamp_ms: i64) -> i64 {
        let d = self.duration_ms();
        // Week alignment: align to Thursday 00:00 UTC (Unix epoch was Thursday)
        timestamp_ms - (timestamp_ms.rem_euclid(d))
    }

    /// Round a nanosecond timestamp down to the start of the containing bucket,
    /// returned in nanoseconds.
    pub const fn bucket_start_ns(&self, timestamp_ns: u64) -> u64 {
        let d_ns = self.duration_ms() as u64 * 1_000_000;
        timestamp_ns - (timestamp_ns % d_ns)
    }

    /// Parse a human string like "1m", "4h", "1D", "1W".
    /// Case-insensitive for letters, exact match for numbers.
    pub fn from_str(s: &str) -> Option<Self> {
        match s {
            "1s" => Some(Self::Sec1),
            "5s" => Some(Self::Sec5),
            "15s" => Some(Self::Sec15),
            "30s" => Some(Self::Sec30),
            "1m" => Some(Self::Min1),
            "2m" => Some(Self::Min2),
            "3m" => Some(Self::Min3),
            "5m" => Some(Self::Min5),
            "15m" => Some(Self::Min15),
            "30m" => Some(Self::Min30),
            "1h" => Some(Self::Hour1),
            "2h" => Some(Self::Hour2),
            "4h" => Some(Self::Hour4),
            "8h" => Some(Self::Hour8),
            "1D" | "1d" => Some(Self::Day1),
            "1W" | "1w" => Some(Self::Week1),
            _ => None,
        }
    }

    /// Canonical display string.
    pub const fn as_str(&self) -> &'static str {
        match self {
            Self::Sec1 => "1s",
            Self::Sec5 => "5s",
            Self::Sec15 => "15s",
            Self::Sec30 => "30s",
            Self::Min1 => "1m",
            Self::Min2 => "2m",
            Self::Min3 => "3m",
            Self::Min5 => "5m",
            Self::Min15 => "15m",
            Self::Min30 => "30m",
            Self::Hour1 => "1h",
            Self::Hour2 => "2h",
            Self::Hour4 => "4h",
            Self::Hour8 => "8h",
            Self::Day1 => "1D",
            Self::Week1 => "1W",
        }
    }

    /// Convert `u8` index back to enum.
    pub const fn from_u8(v: u8) -> Option<Self> {
        match v {
            0 => Some(Self::Sec1),
            1 => Some(Self::Sec5),
            2 => Some(Self::Sec15),
            3 => Some(Self::Sec30),
            4 => Some(Self::Min1),
            5 => Some(Self::Min2),
            6 => Some(Self::Min3),
            7 => Some(Self::Min5),
            8 => Some(Self::Min15),
            9 => Some(Self::Min30),
            10 => Some(Self::Hour1),
            11 => Some(Self::Hour2),
            12 => Some(Self::Hour4),
            13 => Some(Self::Hour8),
            14 => Some(Self::Day1),
            15 => Some(Self::Week1),
            _ => None,
        }
    }
}

impl std::fmt::Display for Timeframe {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn roundtrip_u8() {
        for tf in Timeframe::ALL {
            let idx = tf as u8;
            assert_eq!(Timeframe::from_u8(idx), Some(tf));
        }
    }

    #[test]
    fn roundtrip_str() {
        for tf in Timeframe::ALL {
            let s = tf.as_str();
            assert_eq!(Timeframe::from_str(s), Some(tf), "failed for {s}");
        }
    }

    #[test]
    fn bucket_alignment() {
        // 1m bucket: 90_000ms → bucket at 60_000
        assert_eq!(Timeframe::Min1.bucket_start_ms(90_000), 60_000);

        // 1h bucket: 3_700_000ms → bucket at 3_600_000
        assert_eq!(Timeframe::Hour1.bucket_start_ms(3_700_000), 3_600_000);

        // 1D bucket: exactly on boundary stays
        assert_eq!(Timeframe::Day1.bucket_start_ms(86_400_000), 86_400_000);
    }

    #[test]
    fn durations_consistent() {
        assert_eq!(Timeframe::Min1.duration_ms(), 60_000);
        assert_eq!(Timeframe::Hour1.duration_ms(), 3_600_000);
        assert_eq!(Timeframe::Day1.duration_ms(), 86_400_000);
        assert_eq!(Timeframe::Week1.duration_ms(), 604_800_000);
    }
}
