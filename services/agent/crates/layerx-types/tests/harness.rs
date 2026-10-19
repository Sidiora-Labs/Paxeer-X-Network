use layerx_types::test_support::{
    agent_fuzz_corpus_policy, DeterministicClock, DeterministicRng, SuiteKind,
};

fn agent_test_harness(seed: u64, steps: usize) -> (u64, u64) {
    let mut clock = DeterministicClock::new(0);
    let mut rng = DeterministicRng::from_seed(seed);
    let mut digest = 0_u64;
    for _ in 0..steps {
        let word = rng.next_u64();
        digest ^= word;
        assert!(clock.advance(word & 7).is_ok());
    }
    (clock.now(), digest)
}

#[test]
fn suites_map_to_stable_make_targets() {
    let cases = [
        (SuiteKind::Unit, "agent-test"),
        (SuiteKind::Integration, "agent-test"),
        (SuiteKind::Property, "agent-test-property"),
        (SuiteKind::Differential, "agent-test-differential"),
        (SuiteKind::FaultInjection, "agent-test-faults"),
        (SuiteKind::Conformance, "agent-test-vectors"),
    ];
    for (suite, target) in cases {
        assert_eq!(suite.make_target(), target);
    }
}

#[test]
fn clock_and_randomness_replay_from_seed() {
    for seed in [0, 1, 17, u64::MAX] {
        assert_eq!(agent_test_harness(seed, 128), agent_test_harness(seed, 128));
    }
    assert_ne!(agent_test_harness(1, 128), agent_test_harness(2, 128));
    assert!(agent_fuzz_corpus_policy().contains("explicit u64 seed"));
}

#[test]
fn clock_overflow_is_an_error() {
    let mut clock = DeterministicClock::new(u64::MAX);
    assert!(clock.advance(1).is_err());
    assert_eq!(clock.now(), u64::MAX);
}
