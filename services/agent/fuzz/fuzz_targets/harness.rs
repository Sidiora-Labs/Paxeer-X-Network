#![no_main]

use layerx_types::test_support::{DeterministicClock, DeterministicRng};
use libfuzzer_sys::fuzz_target;

fuzz_target!(|data: &[u8]| {
    let mut seed_bytes = [0_u8; 8];
    let copied = data.len().min(seed_bytes.len());
    seed_bytes[..copied].copy_from_slice(&data[..copied]);
    let mut rng = DeterministicRng::from_seed(u64::from_le_bytes(seed_bytes));
    let mut clock = DeterministicClock::new(0);
    for byte in data {
        let delta = (rng.next_u64() ^ u64::from(*byte)) & 7;
        if clock.advance(delta).is_err() {
            break;
        }
    }
});
