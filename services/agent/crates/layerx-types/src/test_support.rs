//! Deterministic facilities shared by unit, property, fault, and fuzz suites.

/// Error returned when a deterministic clock would overflow.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ClockOverflow;

/// An explicit test clock with no ambient time source.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct DeterministicClock {
    tick: u64,
}

impl DeterministicClock {
    /// Creates a clock at an explicit protocol tick.
    #[must_use]
    pub const fn new(tick: u64) -> Self {
        Self { tick }
    }

    /// Returns the current deterministic tick.
    #[must_use]
    pub const fn now(self) -> u64 {
        self.tick
    }

    /// Advances by an explicit delta without wrapping.
    ///
    /// # Errors
    ///
    /// Returns [`ClockOverflow`] when the new tick cannot fit in `u64`.
    pub fn advance(&mut self, delta: u64) -> Result<u64, ClockOverflow> {
        self.tick = self.tick.checked_add(delta).ok_or(ClockOverflow)?;
        Ok(self.tick)
    }
}

/// A reproducible xorshift source seeded only by recorded test input.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct DeterministicRng {
    state: u64,
}

impl DeterministicRng {
    /// Creates a source from a recorded seed. Zero is mapped to a fixed,
    /// non-zero state required by xorshift.
    #[must_use]
    pub const fn from_seed(seed: u64) -> Self {
        let state = if seed == 0 {
            0x9e37_79b9_7f4a_7c15
        } else {
            seed
        };
        Self { state }
    }

    /// Returns the next deterministic word.
    pub const fn next_u64(&mut self) -> u64 {
        let mut value = self.state;
        value ^= value << 13;
        value ^= value >> 7;
        value ^= value << 17;
        self.state = value;
        value
    }
}

/// Names the make target for each test-suite family.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SuiteKind {
    /// Crate-local unit tests.
    Unit,
    /// Cross-crate interaction tests.
    Integration,
    /// Generated invariant checks.
    Property,
    /// Rust-versus-core conformance checks.
    Differential,
    /// Deterministic injected-failure checks.
    FaultInjection,
    /// Published-vector conformance checks.
    Conformance,
}

impl SuiteKind {
    /// Returns the naming-convention target that owns this suite.
    #[must_use]
    pub const fn make_target(self) -> &'static str {
        match self {
            Self::Unit | Self::Integration => "agent-test",
            Self::Property => "agent-test-property",
            Self::Differential => "agent-test-differential",
            Self::FaultInjection => "agent-test-faults",
            Self::Conformance => "agent-test-vectors",
        }
    }
}

/// The deterministic seed policy used by property and fuzz suites.
#[must_use]
pub const fn agent_fuzz_corpus_policy() -> &'static str {
    "seed-v1: persist failing bytes and print the explicit u64 seed"
}
