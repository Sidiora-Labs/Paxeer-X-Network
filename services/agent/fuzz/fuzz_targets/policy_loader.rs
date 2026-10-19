#![no_main]

use layerx_agentd::policy::load_policy_source;
use libfuzzer_sys::fuzz_target;

fuzz_target!(|data: &[u8]| {
    let _ = load_policy_source(data);
});
