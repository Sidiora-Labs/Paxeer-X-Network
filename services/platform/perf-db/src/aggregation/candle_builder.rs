use crate::core::market::{MarketId, MARKET_COUNT};
use crate::core::timeframe::Timeframe;
use crate::core::types::{CandleRecord, PriceTick};

const TF_COUNT: usize = 16;

/// In-memory OHLCV aggregation state for one (market, timeframe) pair.
#[derive(Debug, Clone)]
struct CandleBuffer {
    bucket_start_ns: u64,
    open: f64,
    high: f64,
    low: f64,
    close: f64,
    volume: f32,
    tick_count: u32,
}

impl CandleBuffer {
    fn new(bucket_start_ns: u64, price: f64) -> Self {
        Self {
            bucket_start_ns,
            open: price,
            high: price,
            low: price,
            close: price,
            volume: 0.0,
            tick_count: 1,
        }
    }

    fn update(&mut self, price: f64) {
        if price > self.high {
            self.high = price;
        }
        if price < self.low {
            self.low = price;
        }
        self.close = price;
        self.tick_count += 1;
    }

    fn to_record(&self, market_id: MarketId, tf: Timeframe) -> CandleRecord {
        CandleRecord {
            timestamp_ns: self.bucket_start_ns,
            open: self.open,
            high: self.high,
            low: self.low,
            close: self.close,
            volume: self.volume,
            market_id,
            timeframe: tf as u8,
            _pad: 0,
        }
    }
}

/// Output from the candle builder when processing a tick.
#[derive(Debug, Clone)]
pub enum CandleOutput {
    /// A candle bucket was finalized (boundary crossed). This is the final OHLCV.
    /// The caller should persist this via `CandleStore::append`.
    Closed(CandleRecord),

    /// A live candle was updated or created. This is the current in-progress state.
    /// Not persisted to disk — held in memory only. Used for real-time API reads
    /// and pub/sub notifications.
    Live(CandleRecord),
}

/// Real-time candle builder for all markets across all 16 timeframes.
///
/// For each incoming tick:
/// 1. Compute the bucket start for all 16 timeframes
/// 2. If same bucket → update OHLCV, emit `Live`
/// 3. If new bucket → emit `Closed` for old bucket, start fresh, emit `Live` for new
///
/// Persistence strategy:
/// - `Closed` candles → `CandleStore::append` (finalized, immutable)
/// - `Live` candles → in memory only (reconstructible from ticks after crash)
pub struct CandleBuilder {
    /// `buffers[market_id * TF_COUNT + tf_index]`
    buffers: Vec<Option<CandleBuffer>>,
}

impl CandleBuilder {
    pub fn new() -> Self {
        let total = MARKET_COUNT * TF_COUNT;
        Self {
            buffers: (0..total).map(|_| None).collect(),
        }
    }

    /// Process a price tick. Returns candle outputs for all 16 timeframes.
    ///
    /// Typical output per tick: 16 `Live` outputs, plus 0–16 `Closed` outputs
    /// (one per timeframe where a bucket boundary was crossed).
    pub fn on_tick(&mut self, tick: &PriceTick) -> Vec<CandleOutput> {
        let mid = tick.market_id;
        if mid as usize >= MARKET_COUNT {
            return Vec::new();
        }

        let mut output = Vec::with_capacity(TF_COUNT * 2);

        for tf in Timeframe::ALL {
            let bucket_ns = tf.bucket_start_ns(tick.timestamp_ns);
            let idx = mid as usize * TF_COUNT + tf as usize;

            match &mut self.buffers[idx] {
                Some(buf) if buf.bucket_start_ns == bucket_ns => {
                    buf.update(tick.price);
                    output.push(CandleOutput::Live(buf.to_record(mid, tf)));
                }
                Some(buf) => {
                    output.push(CandleOutput::Closed(buf.to_record(mid, tf)));
                    self.buffers[idx] = Some(CandleBuffer::new(bucket_ns, tick.price));
                    output.push(CandleOutput::Live(
                        self.buffers[idx].as_ref().unwrap().to_record(mid, tf),
                    ));
                }
                None => {
                    self.buffers[idx] = Some(CandleBuffer::new(bucket_ns, tick.price));
                    output.push(CandleOutput::Live(
                        self.buffers[idx].as_ref().unwrap().to_record(mid, tf),
                    ));
                }
            }
        }

        output
    }

    /// Current live candle for a (market, timeframe) pair.
    /// Returns `None` if no ticks have been processed.
    pub fn current(&self, market_id: MarketId, tf: Timeframe) -> Option<CandleRecord> {
        let idx = market_id as usize * TF_COUNT + tf as usize;
        self.buffers.get(idx)?.as_ref().map(|b| b.to_record(market_id, tf))
    }

    /// Flush all current buffers as finalized candles (for graceful shutdown).
    pub fn flush_all(&self) -> Vec<CandleRecord> {
        let mut out = Vec::new();
        for mid in 0..MARKET_COUNT as MarketId {
            for tf in Timeframe::ALL {
                let idx = mid as usize * TF_COUNT + tf as usize;
                if let Some(buf) = &self.buffers[idx] {
                    out.push(buf.to_record(mid, tf));
                }
            }
        }
        out
    }

    /// Seed a buffer from a previously stored candle (used during recovery).
    ///
    /// After loading completed candles from CandleStore, call this to restore
    /// the last known state before replaying ticks.
    pub fn seed(&mut self, market_id: MarketId, tf: Timeframe, candle: &CandleRecord) {
        let idx = market_id as usize * TF_COUNT + tf as usize;
        if idx < self.buffers.len() {
            self.buffers[idx] = Some(CandleBuffer {
                bucket_start_ns: candle.timestamp_ns,
                open: candle.open,
                high: candle.high,
                low: candle.low,
                close: candle.close,
                volume: candle.volume,
                tick_count: 0,
            });
        }
    }
}

impl Default for CandleBuilder {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_tick(ts_ns: u64, market_id: u16, price: f64) -> PriceTick {
        PriceTick {
            timestamp_ns: ts_ns,
            market_id,
            _pad: [0; 6],
            price,
        }
    }

    #[test]
    fn first_tick_produces_live_outputs() {
        let mut builder = CandleBuilder::new();
        let output = builder.on_tick(&make_tick(1_000_000_000, 0, 67_000.0));

        assert_eq!(output.len(), 16);
        for o in &output {
            match o {
                CandleOutput::Live(c) => {
                    assert_eq!(c.open, 67_000.0);
                    assert_eq!(c.close, 67_000.0);
                    assert_eq!(c.market_id, 0);
                }
                CandleOutput::Closed(_) => panic!("no closed candle expected on first tick"),
            }
        }
    }

    #[test]
    fn same_bucket_updates_ohlcv() {
        let mut builder = CandleBuilder::new();

        builder.on_tick(&make_tick(100_000_000, 0, 67_000.0));
        builder.on_tick(&make_tick(200_000_000, 0, 67_500.0));
        builder.on_tick(&make_tick(300_000_000, 0, 66_800.0));
        let output = builder.on_tick(&make_tick(400_000_000, 0, 67_200.0));

        let sec1 = output.iter().find_map(|o| match o {
            CandleOutput::Live(c) if c.timeframe == Timeframe::Sec1 as u8 => Some(c),
            _ => None,
        }).unwrap();

        assert_eq!(sec1.open, 67_000.0);
        assert_eq!(sec1.high, 67_500.0);
        assert_eq!(sec1.low, 66_800.0);
        assert_eq!(sec1.close, 67_200.0);
    }

    #[test]
    fn bucket_boundary_emits_closed() {
        let mut builder = CandleBuilder::new();

        builder.on_tick(&make_tick(500_000_000, 0, 67_000.0));

        let output = builder.on_tick(&make_tick(1_500_000_000, 0, 67_100.0));

        let closed_1s: Vec<_> = output.iter().filter_map(|o| match o {
            CandleOutput::Closed(c) if c.timeframe == Timeframe::Sec1 as u8 => Some(c),
            _ => None,
        }).collect();

        let live_1s: Vec<_> = output.iter().filter_map(|o| match o {
            CandleOutput::Live(c) if c.timeframe == Timeframe::Sec1 as u8 => Some(c),
            _ => None,
        }).collect();

        assert_eq!(closed_1s.len(), 1);
        assert_eq!(live_1s.len(), 1);

        assert_eq!(closed_1s[0].close, 67_000.0);
        assert_eq!(closed_1s[0].timestamp_ns, 0);

        assert_eq!(live_1s[0].close, 67_100.0);
        assert_eq!(live_1s[0].timestamp_ns, 1_000_000_000);
    }

    #[test]
    fn multi_market_independent() {
        let mut builder = CandleBuilder::new();

        builder.on_tick(&make_tick(100_000_000, 0, 67_000.0));
        builder.on_tick(&make_tick(100_000_000, 1, 3_500.0));

        let btc = builder.current(0, Timeframe::Sec1).unwrap();
        let eth = builder.current(1, Timeframe::Sec1).unwrap();

        assert_eq!(btc.close, 67_000.0);
        assert_eq!(eth.close, 3_500.0);
    }

    #[test]
    fn all_timeframes_tracked() {
        let mut builder = CandleBuilder::new();
        builder.on_tick(&make_tick(100_000_000, 0, 67_000.0));

        for tf in Timeframe::ALL {
            assert!(builder.current(0, tf).is_some(), "missing buffer for {tf}");
        }
    }

    #[test]
    fn flush_all_returns_all_active() {
        let mut builder = CandleBuilder::new();
        builder.on_tick(&make_tick(100_000_000, 0, 67_000.0));
        builder.on_tick(&make_tick(100_000_000, 1, 3_500.0));

        let flushed = builder.flush_all();
        assert_eq!(flushed.len(), 32);
    }

    #[test]
    fn seed_restores_state() {
        let mut builder = CandleBuilder::new();

        let candle = CandleRecord {
            timestamp_ns: 60_000_000_000,
            open: 67_000.0,
            high: 67_500.0,
            low: 66_800.0,
            close: 67_200.0,
            volume: 100.0,
            market_id: 0,
            timeframe: Timeframe::Min1 as u8,
            _pad: 0,
        };

        builder.seed(0, Timeframe::Min1, &candle);

        let current = builder.current(0, Timeframe::Min1).unwrap();
        assert_eq!(current.open, 67_000.0);
        assert_eq!(current.high, 67_500.0);
        assert_eq!(current.close, 67_200.0);
    }

    #[test]
    fn minute_boundary_crossing() {
        let mut builder = CandleBuilder::new();

        builder.on_tick(&make_tick(30_000_000_000, 0, 67_000.0));
        let output = builder.on_tick(&make_tick(90_000_000_000, 0, 67_100.0));

        let closed_1m: Vec<_> = output.iter().filter_map(|o| match o {
            CandleOutput::Closed(c) if c.timeframe == Timeframe::Min1 as u8 => Some(c),
            _ => None,
        }).collect();

        assert_eq!(closed_1m.len(), 1);
        assert_eq!(closed_1m[0].timestamp_ns, 0);
        assert_eq!(closed_1m[0].close, 67_000.0);
    }

    #[test]
    fn invalid_market_ignored() {
        let mut builder = CandleBuilder::new();
        let output = builder.on_tick(&make_tick(1_000, 99, 1.0));
        assert!(output.is_empty());
    }
}
