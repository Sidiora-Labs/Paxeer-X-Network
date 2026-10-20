use crate::core::types::{CandleRecord, PriceTick};

/// Index of the first tick with `timestamp_ns >= target`.
/// O(log n) via binary search on the sorted mmap slice.
pub fn lower_bound_ticks(ticks: &[PriceTick], target_ns: u64) -> usize {
    ticks.partition_point(|t| t.timestamp_ns < target_ns)
}

/// Index of the first tick with `timestamp_ns > target`.
pub fn upper_bound_ticks(ticks: &[PriceTick], target_ns: u64) -> usize {
    ticks.partition_point(|t| t.timestamp_ns <= target_ns)
}

/// Zero-copy slice of ticks in `[start_ns, end_ns)`.
pub fn query_tick_range(ticks: &[PriceTick], start_ns: u64, end_ns: u64) -> &[PriceTick] {
    let lo = lower_bound_ticks(ticks, start_ns);
    let hi = lower_bound_ticks(ticks, end_ns);
    &ticks[lo..hi]
}

/// Zero-copy slice of ticks in `[start_ns, end_ns]` (inclusive both ends).
pub fn query_tick_range_inclusive(ticks: &[PriceTick], start_ns: u64, end_ns: u64) -> &[PriceTick] {
    let lo = lower_bound_ticks(ticks, start_ns);
    let hi = upper_bound_ticks(ticks, end_ns);
    &ticks[lo..hi]
}

/// Last `count` ticks. Returns fewer if not enough data.
pub fn query_last_ticks(ticks: &[PriceTick], count: usize) -> &[PriceTick] {
    let start = ticks.len().saturating_sub(count);
    &ticks[start..]
}

/// Index of the first candle with `timestamp_ns >= target`.
pub fn lower_bound_candles(candles: &[CandleRecord], target_ns: u64) -> usize {
    candles.partition_point(|c| c.timestamp_ns < target_ns)
}

/// Index of the first candle with `timestamp_ns > target`.
pub fn upper_bound_candles(candles: &[CandleRecord], target_ns: u64) -> usize {
    candles.partition_point(|c| c.timestamp_ns <= target_ns)
}

/// Zero-copy slice of candles in `[start_ns, end_ns)`.
pub fn query_candle_range(candles: &[CandleRecord], start_ns: u64, end_ns: u64) -> &[CandleRecord] {
    let lo = lower_bound_candles(candles, start_ns);
    let hi = lower_bound_candles(candles, end_ns);
    &candles[lo..hi]
}

/// Zero-copy slice of candles in `[start_ns, end_ns]` (inclusive both ends).
pub fn query_candle_range_inclusive(
    candles: &[CandleRecord],
    start_ns: u64,
    end_ns: u64,
) -> &[CandleRecord] {
    let lo = lower_bound_candles(candles, start_ns);
    let hi = upper_bound_candles(candles, end_ns);
    &candles[lo..hi]
}

/// Last `count` candles. Returns fewer if not enough data.
pub fn query_last_candles(candles: &[CandleRecord], count: usize) -> &[CandleRecord] {
    let start = candles.len().saturating_sub(count);
    &candles[start..]
}

/// Find the candle whose bucket contains the given timestamp.
/// Returns `None` if no candle matches.
pub fn find_candle_at(candles: &[CandleRecord], timestamp_ns: u64) -> Option<&CandleRecord> {
    if candles.is_empty() {
        return None;
    }
    let idx = upper_bound_candles(candles, timestamp_ns);
    if idx == 0 {
        if candles[0].timestamp_ns <= timestamp_ns {
            return Some(&candles[0]);
        }
        return None;
    }
    Some(&candles[idx - 1])
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::core::timeframe::Timeframe;

    fn make_ticks(count: usize, interval_ns: u64) -> Vec<PriceTick> {
        (0..count)
            .map(|i| PriceTick {
                timestamp_ns: i as u64 * interval_ns,
                market_id: 0,
                _pad: [0; 6],
                price: 60_000.0 + i as f64,
            })
            .collect()
    }

    fn make_candles(count: usize, interval_ns: u64) -> Vec<CandleRecord> {
        (0..count)
            .map(|i| CandleRecord {
                timestamp_ns: i as u64 * interval_ns,
                open: 60_000.0 + i as f64,
                high: 60_001.0 + i as f64,
                low: 59_999.0 + i as f64,
                close: 60_000.5 + i as f64,
                volume: 100.0,
                market_id: 0,
                timeframe: Timeframe::Min1 as u8,
                _pad: 0,
            })
            .collect()
    }

    #[test]
    fn tick_range_empty() {
        let ticks: Vec<PriceTick> = vec![];
        assert!(query_tick_range(&ticks, 0, 1000).is_empty());
    }

    #[test]
    fn tick_range_exact_boundaries() {
        let ticks = make_ticks(10, 1_000_000_000);

        let range = query_tick_range(&ticks, 2_000_000_000, 5_000_000_000);
        assert_eq!(range.len(), 3);
        assert_eq!(range[0].timestamp_ns, 2_000_000_000);
        assert_eq!(range[2].timestamp_ns, 4_000_000_000);
    }

    #[test]
    fn tick_range_inclusive() {
        let ticks = make_ticks(10, 1_000_000_000);

        let range = query_tick_range_inclusive(&ticks, 2_000_000_000, 5_000_000_000);
        assert_eq!(range.len(), 4);
    }

    #[test]
    fn tick_range_no_match() {
        let ticks = make_ticks(10, 1_000_000_000);

        assert!(query_tick_range(&ticks, 100_000_000_000, 200_000_000_000).is_empty());
    }

    #[test]
    fn tick_lower_bound() {
        let ticks = make_ticks(5, 1_000_000_000);

        assert_eq!(lower_bound_ticks(&ticks, 0), 0);
        assert_eq!(lower_bound_ticks(&ticks, 500_000_000), 1);
        assert_eq!(lower_bound_ticks(&ticks, 1_000_000_000), 1);
        assert_eq!(lower_bound_ticks(&ticks, 99_000_000_000), 5);
    }

    #[test]
    fn tick_upper_bound() {
        let ticks = make_ticks(5, 1_000_000_000);

        assert_eq!(upper_bound_ticks(&ticks, 0), 1);
        assert_eq!(upper_bound_ticks(&ticks, 1_000_000_000), 2);
    }

    #[test]
    fn last_ticks() {
        let ticks = make_ticks(100, 1_000);

        let last5 = query_last_ticks(&ticks, 5);
        assert_eq!(last5.len(), 5);
        assert_eq!(last5[0].timestamp_ns, 95_000);

        let all = query_last_ticks(&ticks, 200);
        assert_eq!(all.len(), 100);
    }

    #[test]
    fn candle_range_empty() {
        let candles: Vec<CandleRecord> = vec![];
        assert!(query_candle_range(&candles, 0, 1000).is_empty());
    }

    #[test]
    fn candle_range_exact() {
        let candles = make_candles(10, 60_000_000_000);

        let range = query_candle_range(&candles, 120_000_000_000, 300_000_000_000);
        assert_eq!(range.len(), 3);
    }

    #[test]
    fn candle_range_inclusive() {
        let candles = make_candles(10, 60_000_000_000);

        let range = query_candle_range_inclusive(&candles, 120_000_000_000, 300_000_000_000);
        assert_eq!(range.len(), 4);
    }

    #[test]
    fn last_candles() {
        let candles = make_candles(50, 60_000_000_000);

        let last10 = query_last_candles(&candles, 10);
        assert_eq!(last10.len(), 10);
        assert_eq!(last10[0].timestamp_ns, 40 * 60_000_000_000);
    }

    #[test]
    fn find_candle_at_timestamp() {
        let candles = make_candles(5, 60_000_000_000);

        let c = find_candle_at(&candles, 120_000_000_000).unwrap();
        assert_eq!(c.timestamp_ns, 120_000_000_000);

        let c = find_candle_at(&candles, 150_000_000_000).unwrap();
        assert_eq!(c.timestamp_ns, 120_000_000_000);

        assert!(find_candle_at(&candles, 0).is_some());

        let c = find_candle_at(&candles, 999_000_000_000).unwrap();
        assert_eq!(c.timestamp_ns, 240_000_000_000);
    }

    #[test]
    fn find_candle_at_empty() {
        let candles: Vec<CandleRecord> = vec![];
        assert!(find_candle_at(&candles, 1_000).is_none());
    }

    #[test]
    fn binary_search_large_dataset() {
        let candles = make_candles(100_000, 60_000_000_000);

        let start_ns = 30_000u64 * 60_000_000_000;
        let end_ns = 30_060u64 * 60_000_000_000;

        let range = query_candle_range(&candles, start_ns, end_ns);
        assert_eq!(range.len(), 60);
        assert_eq!(range[0].timestamp_ns, start_ns);
    }
}
