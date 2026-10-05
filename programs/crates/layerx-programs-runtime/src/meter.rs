//! Deterministic resource accounting for guest execution.

pub mod inject;

use core::fmt::{self, Display};

use wasmi::errors::{MemoryError, TableError};
use wasmi::ResourceLimiter;

/// Default instruction fuel admitted for one execution.
pub const DEFAULT_CPU_FUEL: u64 = 1_000_000;
/// Default peak linear-memory budget in bytes.
pub const DEFAULT_MEMORY_BYTES: u64 = 16 * 1_024 * 1_024;
/// Default storage-read budget in bytes.
pub const DEFAULT_STORAGE_READ_BYTES: u64 = 1_048_576;
/// Default storage-write budget in bytes.
pub const DEFAULT_STORAGE_WRITE_BYTES: u64 = 1_048_576;
/// Default result-value count admitted at the guest boundary.
pub const DEFAULT_OUTPUT_VALUES: u32 = 64;
/// Default successful-response byte budget.
pub const DEFAULT_OUTPUT_BYTES: u64 = 1_048_576;
/// Default table-element limit per execution.
pub const DEFAULT_TABLE_ELEMENTS: u32 = 4_096;
/// Version of the schedule installed into protocol state at genesis.
pub const GENESIS_FEE_SCHEDULE_VERSION: u32 = 1;
/// Genesis fee units charged per interpreter CPU fuel unit.
pub const GENESIS_CPU_FUEL_PRICE: u64 = 1;
/// Genesis fee units charged per peak linear-memory byte.
pub const GENESIS_MEMORY_BYTE_PRICE: u64 = 1;
/// Genesis fee units charged per storage byte read.
pub const GENESIS_STORAGE_READ_BYTE_PRICE: u64 = 2;
/// Genesis fee units charged per storage byte written.
pub const GENESIS_STORAGE_WRITE_BYTE_PRICE: u64 = 4;
/// Genesis fee units charged per integer result value.
pub const GENESIS_OUTPUT_VALUE_PRICE: u64 = 1;
/// Genesis fee units charged per response byte.
pub const GENESIS_OUTPUT_BYTE_PRICE: u64 = 1;
/// Genesis fee units charged per namespace byte held for one protocol batch.
pub const GENESIS_OCCUPANCY_BYTE_BATCH_PRICE: u64 = 1;
/// Legacy name for the genesis occupancy price.
pub const DEFAULT_OCCUPANCY_BYTE_BATCH_PRICE: u64 = GENESIS_OCCUPANCY_BYTE_BATCH_PRICE;

/// One independently enforced deterministic resource class.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ResourceKind {
    /// Interpreter instruction fuel.
    Cpu,
    /// Peak guest linear memory.
    Memory,
    /// Bytes read through the storage ABI.
    StorageRead,
    /// Bytes written through the storage ABI.
    StorageWrite,
    /// One namespace byte held across one protocol batch.
    StorageOccupancy,
    /// Integer values returned across the guest boundary.
    Output,
    /// Successful response bytes copied across a call boundary.
    OutputBytes,
}

impl Display for ResourceKind {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Cpu => write!(formatter, "cpu fuel"),
            Self::Memory => write!(formatter, "memory bytes"),
            Self::StorageRead => write!(formatter, "storage read bytes"),
            Self::StorageWrite => write!(formatter, "storage write bytes"),
            Self::StorageOccupancy => write!(formatter, "storage occupancy byte-batches"),
            Self::Output => write!(formatter, "output values"),
            Self::OutputBytes => write!(formatter, "output bytes"),
        }
    }
}

/// Resource classes available only to caller-declared activity budgets.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BudgetResourceKind {
    Cpu,
    Memory,
    StorageRead,
    StorageWrite,
    Output,
    OutputBytes,
    Table,
}

/// Receipt-carriable resource refusal for an admitted activity budget.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BudgetMeterRefusal {
    BudgetExceeded {
        resource: BudgetResourceKind,
        limit: u64,
        attempted: u64,
    },
    CounterOverflow {
        resource: BudgetResourceKind,
    },
}

impl TryFrom<MeterRefusal> for BudgetMeterRefusal {
    type Error = MeterRefusal;

    fn try_from(refusal: MeterRefusal) -> Result<Self, Self::Error> {
        budget_refusal(refusal).ok_or(refusal)
    }
}

/// Exact resource budget applied to one fresh execution.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ResourceBudget {
    cpu_fuel: u64,
    memory_bytes: u64,
    storage_read_bytes: u64,
    storage_write_bytes: u64,
    output_values: u32,
    output_bytes: u64,
    table_elements: u32,
}

impl ResourceBudget {
    /// Constructs an explicit integer-only execution budget.
    #[must_use]
    pub const fn new(
        cpu_fuel: u64,
        memory_bytes: u64,
        storage_read_bytes: u64,
        storage_write_bytes: u64,
        output_values: u32,
        table_elements: u32,
    ) -> Self {
        Self {
            cpu_fuel,
            memory_bytes,
            storage_read_bytes,
            storage_write_bytes,
            output_values,
            output_bytes: DEFAULT_OUTPUT_BYTES,
            table_elements,
        }
    }

    /// Constructs all seven resource limits without defaulting response bytes.
    #[must_use]
    pub const fn new_complete(
        cpu_fuel: u64,
        memory_bytes: u64,
        storage_read_bytes: u64,
        storage_write_bytes: u64,
        output_values: u32,
        output_bytes: u64,
        table_elements: u32,
    ) -> Self {
        Self {
            cpu_fuel,
            memory_bytes,
            storage_read_bytes,
            storage_write_bytes,
            output_values,
            output_bytes,
            table_elements,
        }
    }

    /// Returns the declared production budget.
    #[must_use]
    pub const fn declared() -> Self {
        Self::new(
            DEFAULT_CPU_FUEL,
            DEFAULT_MEMORY_BYTES,
            DEFAULT_STORAGE_READ_BYTES,
            DEFAULT_STORAGE_WRITE_BYTES,
            DEFAULT_OUTPUT_VALUES,
            DEFAULT_TABLE_ELEMENTS,
        )
    }

    /// Returns the instruction-fuel limit.
    #[must_use]
    pub const fn cpu_fuel(self) -> u64 {
        self.cpu_fuel
    }

    /// Returns the peak linear-memory limit.
    #[must_use]
    pub const fn memory_bytes(self) -> u64 {
        self.memory_bytes
    }

    /// Returns the cumulative storage-read limit.
    #[must_use]
    pub const fn storage_read_bytes(self) -> u64 {
        self.storage_read_bytes
    }

    /// Returns the cumulative storage-write limit.
    #[must_use]
    pub const fn storage_write_bytes(self) -> u64 {
        self.storage_write_bytes
    }

    /// Returns the maximum result-value count.
    #[must_use]
    pub const fn output_values(self) -> u32 {
        self.output_values
    }

    #[must_use]
    pub const fn with_output_bytes(mut self, output_bytes: u64) -> Self {
        self.output_bytes = output_bytes;
        self
    }

    #[must_use]
    pub const fn output_bytes(self) -> u64 {
        self.output_bytes
    }

    /// Returns the peak table-element limit.
    #[must_use]
    pub const fn table_elements(self) -> u32 {
        self.table_elements
    }
}

impl Default for ResourceBudget {
    fn default() -> Self {
        Self::declared()
    }
}

/// Integer fee-unit prices for each metered resource.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FeeSchedule {
    version: u32,
    fee_units_per_cpu_fuel: u64,
    fee_units_per_memory_byte: u64,
    fee_units_per_storage_read_byte: u64,
    fee_units_per_storage_write_byte: u64,
    fee_units_per_output_value: u64,
    fee_units_per_output_byte: u64,
    fee_units_per_occupancy_byte_batch: u64,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct FeeScheduleParameters {
    pub version: u32,
    pub fee_units_per_cpu_fuel: u64,
    pub fee_units_per_memory_byte: u64,
    pub fee_units_per_storage_read_byte: u64,
    pub fee_units_per_storage_write_byte: u64,
    pub fee_units_per_output_value: u64,
    pub fee_units_per_output_byte: u64,
    pub fee_units_per_occupancy_byte_batch: u64,
}

impl FeeSchedule {
    /// Returns whether the schedule has a version and a nonzero coefficient
    /// for every priced resource.
    #[must_use]
    pub const fn is_valid(self) -> bool {
        self.version != 0
            && self.fee_units_per_cpu_fuel != 0
            && self.fee_units_per_memory_byte != 0
            && self.fee_units_per_storage_read_byte != 0
            && self.fee_units_per_storage_write_byte != 0
            && self.fee_units_per_output_value != 0
            && self.fee_units_per_output_byte != 0
            && self.fee_units_per_occupancy_byte_batch != 0
    }

    /// Constructs an explicit integer fee schedule.
    #[must_use]
    pub const fn new(
        fee_units_per_cpu_fuel: u64,
        fee_units_per_memory_byte: u64,
        fee_units_per_storage_read_byte: u64,
        fee_units_per_storage_write_byte: u64,
        fee_units_per_output_value: u64,
    ) -> Self {
        Self {
            version: GENESIS_FEE_SCHEDULE_VERSION,
            fee_units_per_cpu_fuel,
            fee_units_per_memory_byte,
            fee_units_per_storage_read_byte,
            fee_units_per_storage_write_byte,
            fee_units_per_output_value,
            fee_units_per_output_byte: GENESIS_OUTPUT_BYTE_PRICE,
            fee_units_per_occupancy_byte_batch: GENESIS_OCCUPANCY_BYTE_BATCH_PRICE,
        }
    }

    /// Constructs the complete governed schedule recorded by the protocol.
    #[must_use]
    pub const fn new_complete(parameters: FeeScheduleParameters) -> Self {
        let FeeScheduleParameters {
            version,
            fee_units_per_cpu_fuel,
            fee_units_per_memory_byte,
            fee_units_per_storage_read_byte,
            fee_units_per_storage_write_byte,
            fee_units_per_output_value,
            fee_units_per_output_byte,
            fee_units_per_occupancy_byte_batch,
        } = parameters;
        Self {
            version,
            fee_units_per_cpu_fuel,
            fee_units_per_memory_byte,
            fee_units_per_storage_read_byte,
            fee_units_per_storage_write_byte,
            fee_units_per_output_value,
            fee_units_per_output_byte,
            fee_units_per_occupancy_byte_batch,
        }
    }

    /// Returns the governed protocol-state version of this schedule.
    #[must_use]
    pub const fn version(self) -> u32 {
        self.version
    }

    /// Replaces the response-byte coefficient without changing the version.
    #[must_use]
    pub const fn with_output_byte_price(mut self, fee_units_per_output_byte: u64) -> Self {
        self.fee_units_per_output_byte = fee_units_per_output_byte;
        self
    }

    /// Replaces the occupancy coefficient without changing the version.
    #[must_use]
    pub const fn with_occupancy_byte_batch_price(
        mut self,
        fee_units_per_occupancy_byte_batch: u64,
    ) -> Self {
        self.fee_units_per_occupancy_byte_batch = fee_units_per_occupancy_byte_batch;
        self
    }

    /// Returns the schedule installed into protocol state at genesis.
    ///
    /// Protocol execution and replay must resolve a schedule from governed
    /// state by recorded version; this value is only the genesis input.
    #[must_use]
    pub const fn declared() -> Self {
        Self::new_complete(FeeScheduleParameters {
            version: GENESIS_FEE_SCHEDULE_VERSION,
            fee_units_per_cpu_fuel: GENESIS_CPU_FUEL_PRICE,
            fee_units_per_memory_byte: GENESIS_MEMORY_BYTE_PRICE,
            fee_units_per_storage_read_byte: GENESIS_STORAGE_READ_BYTE_PRICE,
            fee_units_per_storage_write_byte: GENESIS_STORAGE_WRITE_BYTE_PRICE,
            fee_units_per_output_value: GENESIS_OUTPUT_VALUE_PRICE,
            fee_units_per_output_byte: GENESIS_OUTPUT_BYTE_PRICE,
            fee_units_per_occupancy_byte_batch: GENESIS_OCCUPANCY_BYTE_BATCH_PRICE,
        })
    }

    /// Returns fee units per interpreter CPU fuel unit.
    #[must_use]
    pub const fn cpu_price(self) -> u64 {
        self.fee_units_per_cpu_fuel
    }

    /// Returns fee units per peak linear-memory byte.
    #[must_use]
    pub const fn memory_byte_price(self) -> u64 {
        self.fee_units_per_memory_byte
    }

    /// Returns fee units per storage byte read.
    #[must_use]
    pub const fn storage_read_byte_price(self) -> u64 {
        self.fee_units_per_storage_read_byte
    }

    /// Returns fee units per storage byte written.
    #[must_use]
    pub const fn storage_write_byte_price(self) -> u64 {
        self.fee_units_per_storage_write_byte
    }

    /// Returns fee units per integer result value.
    #[must_use]
    pub const fn output_value_price(self) -> u64 {
        self.fee_units_per_output_value
    }

    /// Returns fee units per response byte.
    #[must_use]
    pub const fn output_byte_price(self) -> u64 {
        self.fee_units_per_output_byte
    }

    /// Returns fee units per namespace byte held for one protocol batch.
    #[must_use]
    pub const fn occupancy_byte_batch_price(self) -> u64 {
        self.fee_units_per_occupancy_byte_batch
    }

    /// Derives the next effective scarce-resource price from canonical batch
    /// occupancy, bounded by the governed per-batch fraction.
    ///
    /// The returned schedule advances to `next_version` only when its price
    /// changes. Callers persist that version before admitting another batch.
    ///
    /// # Errors
    ///
    /// Refuses invalid policy, non-consecutive versions, or arithmetic that
    /// cannot be represented exactly. It never substitutes a current price.
    pub fn adjust_occupancy_base_price(
        self,
        observed_occupancy_byte_batches: u128,
        policy: DemandPricePolicy,
        next_version: u32,
    ) -> Result<DemandPriceAdjustment, FeeScheduleError> {
        policy.validate()?;
        if !self.is_valid()
            || self.fee_units_per_occupancy_byte_batch < policy.minimum_price
            || self.fee_units_per_occupancy_byte_batch > policy.maximum_price
        {
            return Err(FeeScheduleError::InvalidSchedule);
        }
        if self.version.checked_add(1) != Some(next_version) {
            return Err(FeeScheduleError::NonConsecutiveVersion {
                previous: self.version,
                attempted: next_version,
            });
        }
        let target = u128::from(policy.target_occupancy_byte_batches);
        let deviation = observed_occupancy_byte_batches.abs_diff(target);
        let current = u128::from(self.fee_units_per_occupancy_byte_batch);
        let response_divisor = target
            .checked_mul(u128::from(policy.response_denominator))
            .ok_or(FeeScheduleError::ArithmeticOverflow)?;
        let maximum_change = current
            .checked_mul(u128::from(policy.maximum_change_numerator))
            .ok_or(FeeScheduleError::ArithmeticOverflow)?
            / u128::from(policy.maximum_change_denominator);
        let maximum_change =
            u64::try_from(maximum_change).map_err(|_| FeeScheduleError::ArithmeticOverflow)?;
        let bounded_change = u128::from(capped_proportional_change(
            self.fee_units_per_occupancy_byte_batch,
            deviation,
            response_divisor,
            maximum_change,
        )?);
        let proposed = match observed_occupancy_byte_batches.cmp(&target) {
            core::cmp::Ordering::Greater => current
                .checked_add(bounded_change)
                .ok_or(FeeScheduleError::ArithmeticOverflow)?
                .min(u128::from(policy.maximum_price)),
            core::cmp::Ordering::Less => current
                .checked_sub(bounded_change)
                .ok_or(FeeScheduleError::ArithmeticOverflow)?
                .max(u128::from(policy.minimum_price)),
            core::cmp::Ordering::Equal => current,
        };
        let applied_change = current.abs_diff(proposed);
        if applied_change > u128::from(maximum_change) {
            return Err(FeeScheduleError::AdjustmentBoundExceeded);
        }
        let price = u64::try_from(proposed).map_err(|_| FeeScheduleError::ArithmeticOverflow)?;
        let applied_change =
            u64::try_from(applied_change).map_err(|_| FeeScheduleError::ArithmeticOverflow)?;
        let mut resulting_schedule = self;
        if price != self.fee_units_per_occupancy_byte_batch {
            resulting_schedule.version = next_version;
            resulting_schedule.fee_units_per_occupancy_byte_batch = price;
        }
        Ok(DemandPriceAdjustment {
            observed_occupancy_byte_batches,
            target_occupancy_byte_batches: policy.target_occupancy_byte_batches,
            maximum_change,
            applied_change,
            resulting_schedule,
        })
    }
}

/// Governed integer coefficients for demand-responsive occupancy pricing.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct DemandPricePolicy {
    target_occupancy_byte_batches: u64,
    response_denominator: u64,
    maximum_change_numerator: u64,
    maximum_change_denominator: u64,
    minimum_price: u64,
    maximum_price: u64,
}

impl DemandPricePolicy {
    /// Constructs the complete policy. Prices are fee units per occupancy
    /// byte-batch; the maximum per-batch movement is `numerator / denominator`.
    ///
    /// # Errors
    ///
    /// Refuses zero divisors, an improper movement fraction, or invalid bounds.
    pub const fn new(
        target_occupancy_byte_batches: u64,
        response_denominator: u64,
        maximum_change_numerator: u64,
        maximum_change_denominator: u64,
        minimum_price: u64,
        maximum_price: u64,
    ) -> Result<Self, FeeScheduleError> {
        let policy = Self {
            target_occupancy_byte_batches,
            response_denominator,
            maximum_change_numerator,
            maximum_change_denominator,
            minimum_price,
            maximum_price,
        };
        match policy.validate() {
            Ok(()) => Ok(policy),
            Err(error) => Err(error),
        }
    }

    const fn validate(self) -> Result<(), FeeScheduleError> {
        if self.target_occupancy_byte_batches == 0
            || self.response_denominator == 0
            || self.maximum_change_numerator == 0
            || self.maximum_change_denominator == 0
            || self.maximum_change_numerator > self.maximum_change_denominator
            || self.minimum_price == 0
            || self.minimum_price > self.maximum_price
        {
            Err(FeeScheduleError::InvalidDemandPolicy)
        } else {
            Ok(())
        }
    }

    /// Returns the governed occupancy target in byte-batches.
    #[must_use]
    pub const fn target_occupancy_byte_batches(self) -> u64 {
        self.target_occupancy_byte_batches
    }

    /// Returns the divisor damping deviation-driven price movement.
    #[must_use]
    pub const fn response_denominator(self) -> u64 {
        self.response_denominator
    }

    /// Returns the numerator of the maximum per-batch movement fraction.
    #[must_use]
    pub const fn maximum_change_numerator(self) -> u64 {
        self.maximum_change_numerator
    }

    /// Returns the denominator of the maximum per-batch movement fraction.
    #[must_use]
    pub const fn maximum_change_denominator(self) -> u64 {
        self.maximum_change_denominator
    }

    /// Returns the inclusive occupancy-price floor in fee units.
    #[must_use]
    pub const fn minimum_price(self) -> u64 {
        self.minimum_price
    }

    /// Returns the inclusive occupancy-price ceiling in fee units.
    #[must_use]
    pub const fn maximum_price(self) -> u64 {
        self.maximum_price
    }
}

/// Receipt-ready evidence of one deterministic occupancy-price transition.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct DemandPriceAdjustment {
    observed_occupancy_byte_batches: u128,
    target_occupancy_byte_batches: u64,
    maximum_change: u64,
    applied_change: u64,
    resulting_schedule: FeeSchedule,
}

impl DemandPriceAdjustment {
    /// Returns the canonical completed-batch occupancy used by the transition.
    #[must_use]
    pub const fn observed_occupancy_byte_batches(self) -> u128 {
        self.observed_occupancy_byte_batches
    }

    /// Returns the governed occupancy target used by the transition.
    #[must_use]
    pub const fn target_occupancy_byte_batches(self) -> u64 {
        self.target_occupancy_byte_batches
    }

    /// Returns the maximum fee-unit movement admitted for this batch.
    #[must_use]
    pub const fn maximum_change(self) -> u64 {
        self.maximum_change
    }

    /// Returns the actual absolute fee-unit movement for this batch.
    #[must_use]
    pub const fn applied_change(self) -> u64 {
        self.applied_change
    }

    /// Returns the effective schedule after this transition.
    #[must_use]
    pub const fn resulting_schedule(self) -> FeeSchedule {
        self.resulting_schedule
    }
}

/// Fail-closed schedule selection or demand-transition refusal.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FeeScheduleError {
    /// The schedule is unversioned or lies outside its governed price bounds.
    InvalidSchedule,
    /// A demand coefficient, divisor, fraction, or price bound is invalid.
    InvalidDemandPolicy,
    /// An effective version did not immediately follow the previous version.
    NonConsecutiveVersion {
        /// Last effective version retained in protocol state.
        previous: u32,
        /// Version proposed for the next effective schedule.
        attempted: u32,
    },
    /// The receipt-recorded version is absent from governed history.
    UnknownVersion {
        /// Exact version requested by replay.
        version: u32,
    },
    /// Exact integer arithmetic exceeded its declared representation.
    ArithmeticOverflow,
    /// A postcondition found movement above the governed per-batch fraction.
    AdjustmentBoundExceeded,
}

impl Display for FeeScheduleError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidSchedule => formatter.write_str("invalid fee schedule"),
            Self::InvalidDemandPolicy => formatter.write_str("invalid demand-price policy"),
            Self::NonConsecutiveVersion {
                previous,
                attempted,
            } => write!(
                formatter,
                "fee schedule version {attempted} does not follow {previous}"
            ),
            Self::UnknownVersion { version } => {
                write!(formatter, "unknown fee schedule version {version}")
            }
            Self::ArithmeticOverflow => formatter.write_str("fee schedule arithmetic overflowed"),
            Self::AdjustmentBoundExceeded => {
                formatter.write_str("fee schedule adjustment exceeded its per-batch bound")
            }
        }
    }
}

impl std::error::Error for FeeScheduleError {}

/// Append-only governed schedule history used for canonical receipt replay.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FeeScheduleHistory {
    schedules: Vec<FeeSchedule>,
}

impl FeeScheduleHistory {
    /// Starts history from a protocol-state schedule.
    ///
    /// # Errors
    ///
    /// Refuses an unversioned schedule or a zero coefficient.
    pub fn new(initial: FeeSchedule) -> Result<Self, FeeScheduleError> {
        if !initial.is_valid() {
            return Err(FeeScheduleError::InvalidSchedule);
        }
        Ok(Self {
            schedules: vec![initial],
        })
    }

    /// Appends the next governed or demand-derived effective schedule without
    /// replacing any historical version.
    ///
    /// # Errors
    ///
    /// Refuses a gap, duplicate, regression, or unversioned schedule.
    pub fn record(&mut self, schedule: FeeSchedule) -> Result<(), FeeScheduleError> {
        let previous = self
            .schedules
            .last()
            .copied()
            .ok_or(FeeScheduleError::InvalidSchedule)?;
        if !schedule.is_valid() {
            return Err(FeeScheduleError::InvalidSchedule);
        }
        if previous.version.checked_add(1) != Some(schedule.version) {
            return Err(FeeScheduleError::NonConsecutiveVersion {
                previous: previous.version,
                attempted: schedule.version,
            });
        }
        self.schedules.push(schedule);
        Ok(())
    }

    /// Selects exactly the schedule version recorded by a receipt.
    ///
    /// # Errors
    ///
    /// Unknown versions are refused; node-current or latest schedules are never
    /// considered as a fallback.
    pub fn select_recorded(&self, version: u32) -> Result<FeeSchedule, FeeScheduleError> {
        self.schedules
            .binary_search_by_key(&version, |schedule| schedule.version())
            .map(|index| self.schedules[index])
            .map_err(|_| FeeScheduleError::UnknownVersion { version })
    }

    /// Returns append-only schedule history in effective-version order.
    #[must_use]
    pub fn schedules(&self) -> &[FeeSchedule] {
        &self.schedules
    }
}

/// Deterministic fee-governance projection used by replay tooling and the
/// scalar runtime boundary. Protocol authority and receipt verification remain
/// owned by the C Programs transition; this type applies only schedules that
/// transition has admitted into append-only history.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FeeGovernance {
    history: FeeScheduleHistory,
    demand_policy: DemandPricePolicy,
}

impl FeeGovernance {
    /// Constructs the runtime projection from authenticated protocol state.
    ///
    /// # Errors
    ///
    /// Returns a refusal for an invalid demand policy or initial fee schedule.
    pub fn new(
        initial: FeeSchedule,
        demand_policy: DemandPricePolicy,
    ) -> Result<Self, FeeScheduleError> {
        demand_policy.validate()?;
        Ok(Self {
            history: FeeScheduleHistory::new(initial)?,
            demand_policy,
        })
    }

    /// Appends one receipt-authorized schedule already made effective by the
    /// protocol transition.
    ///
    /// # Errors
    ///
    /// Returns a refusal when the schedule is invalid or violates history ordering.
    pub fn record_governed(&mut self, schedule: FeeSchedule) -> Result<(), FeeScheduleError> {
        self.history.record(schedule)
    }

    /// Applies one canonical completed-batch occupancy observation and records
    /// the resulting schedule only when its bounded price changes.
    ///
    /// # Errors
    ///
    /// Returns a refusal for invalid demand adjustment, arithmetic overflow, or history insertion.
    pub fn observe_batch(
        &mut self,
        observed_occupancy_byte_batches: u128,
    ) -> Result<DemandPriceAdjustment, FeeScheduleError> {
        let current = self.current()?;
        let next_version = current
            .version()
            .checked_add(1)
            .ok_or(FeeScheduleError::ArithmeticOverflow)?;
        let adjustment = current.adjust_occupancy_base_price(
            observed_occupancy_byte_batches,
            self.demand_policy,
            next_version,
        )?;
        if adjustment.resulting_schedule().version() != current.version() {
            self.history.record(adjustment.resulting_schedule())?;
        }
        Ok(adjustment)
    }

    /// Returns the effective protocol-state schedule.
    ///
    /// # Errors
    ///
    /// Returns `InvalidSchedule` when no effective schedule exists.
    pub fn current(&self) -> Result<FeeSchedule, FeeScheduleError> {
        self.history
            .schedules()
            .last()
            .copied()
            .ok_or(FeeScheduleError::InvalidSchedule)
    }

    /// Returns append-only history for receipt-recorded replay selection.
    #[must_use]
    pub const fn history(&self) -> &FeeScheduleHistory {
        &self.history
    }
}

fn ceil_scaled_fraction(
    multiplier: u64,
    value: u128,
    divisor: u64,
) -> Result<u128, FeeScheduleError> {
    if divisor == 0 || multiplier > divisor {
        return Err(FeeScheduleError::ArithmeticOverflow);
    }
    let divisor = u128::from(divisor);
    let multiplier = u128::from(multiplier);
    let whole = value / divisor;
    let remainder = value % divisor;
    let scaled_whole = multiplier
        .checked_mul(whole)
        .ok_or(FeeScheduleError::ArithmeticOverflow)?;
    let scaled_remainder = multiplier
        .checked_mul(remainder)
        .ok_or(FeeScheduleError::ArithmeticOverflow)?
        .div_ceil(divisor);
    scaled_whole
        .checked_add(scaled_remainder)
        .ok_or(FeeScheduleError::ArithmeticOverflow)
}

fn capped_proportional_change(
    current_price: u64,
    deviation: u128,
    response_divisor: u128,
    maximum_change: u64,
) -> Result<u64, FeeScheduleError> {
    // Search the governed cap using division thresholds so full-range u128
    // occupancy never requires a potentially overflowing u192 product.
    let mut lower = 0;
    let mut upper = maximum_change;
    while lower < upper {
        let distance = upper - lower;
        let candidate = lower + distance.div_ceil(2);
        let required_deviation = ceil_scaled_fraction(candidate, response_divisor, current_price)?;
        if deviation >= required_deviation {
            lower = candidate;
        } else {
            upper = candidate - 1;
        }
    }
    Ok(lower)
}

impl Default for FeeSchedule {
    fn default() -> Self {
        Self::declared()
    }
}

/// Exact deterministic usage and fee units produced by one execution.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct MeteredUsage {
    /// Interpreter fuel consumed.
    pub cpu_fuel: u64,
    /// Peak guest memory admitted in bytes.
    pub memory_bytes: u64,
    /// Storage bytes read.
    pub storage_read_bytes: u64,
    /// Storage bytes written.
    pub storage_write_bytes: u64,
    /// Integer result values returned.
    pub output_values: u32,
    /// Successful response bytes copied across execution boundaries.
    pub output_bytes: u64,
    /// Persistent storage occupied across canonical protocol batch intervals.
    pub occupancy_byte_batches: u128,
    /// Exact occupancy fee, kept distinct from one-off execution fees.
    pub occupancy_fee_units: u128,
    /// Exact units handed to the existing fee mechanism.
    pub fee_units: u128,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) struct QualificationMeterSnapshot {
    pub(crate) cpu_fuel: u64,
    pub(crate) memory_bytes: u64,
    pub(crate) storage_read_bytes: u64,
    pub(crate) storage_write_bytes: u64,
    pub(crate) output_values: u32,
    pub(crate) output_bytes: u64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) struct OutputReservation(u32);

/// Typed resource refusal with exact limit and attempted use.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MeterRefusal {
    /// One resource budget was exceeded.
    BudgetExceeded {
        /// Resource whose budget was exceeded.
        resource: ResourceKind,
        /// Configured resource limit.
        limit: u64,
        /// Attempted cumulative or peak use.
        attempted: u64,
    },
    /// Cumulative accounting could not be represented exactly.
    CounterOverflow {
        /// Resource whose cumulative counter overflowed.
        resource: ResourceKind,
    },
    /// Fee-unit multiplication or accumulation exceeded `u128`.
    FeeOverflow,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum MeterMode {
    Legacy,
    Activity,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum MeterExhaustion {
    Legacy(MeterRefusal),
    Budget(BudgetMeterRefusal),
}

impl Display for MeterRefusal {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::BudgetExceeded {
                resource,
                limit,
                attempted,
            } => write!(
                formatter,
                "{resource} budget {limit} exceeded by attempted use {attempted}"
            ),
            Self::CounterOverflow { resource } => {
                write!(formatter, "{resource} cumulative accounting overflowed")
            }
            Self::FeeOverflow => write!(formatter, "metered fee units overflowed"),
        }
    }
}

impl std::error::Error for MeterRefusal {}

const fn budget_resource(resource: ResourceKind) -> Option<BudgetResourceKind> {
    match resource {
        ResourceKind::Cpu => Some(BudgetResourceKind::Cpu),
        ResourceKind::Memory => Some(BudgetResourceKind::Memory),
        ResourceKind::StorageRead => Some(BudgetResourceKind::StorageRead),
        ResourceKind::StorageWrite => Some(BudgetResourceKind::StorageWrite),
        ResourceKind::Output => Some(BudgetResourceKind::Output),
        ResourceKind::OutputBytes => Some(BudgetResourceKind::OutputBytes),
        ResourceKind::StorageOccupancy => None,
    }
}

const fn budget_refusal(refusal: MeterRefusal) -> Option<BudgetMeterRefusal> {
    match refusal {
        MeterRefusal::BudgetExceeded {
            resource,
            limit,
            attempted,
        } => match budget_resource(resource) {
            Some(resource) => Some(BudgetMeterRefusal::BudgetExceeded {
                resource,
                limit,
                attempted,
            }),
            None => None,
        },
        MeterRefusal::CounterOverflow { resource } => match budget_resource(resource) {
            Some(resource) => Some(BudgetMeterRefusal::CounterOverflow { resource }),
            None => None,
        },
        MeterRefusal::FeeOverflow => None,
    }
}

const fn project_budget_resource(resource: BudgetResourceKind) -> ResourceKind {
    match resource {
        BudgetResourceKind::Cpu => ResourceKind::Cpu,
        BudgetResourceKind::Memory | BudgetResourceKind::Table => ResourceKind::Memory,
        BudgetResourceKind::StorageRead => ResourceKind::StorageRead,
        BudgetResourceKind::StorageWrite => ResourceKind::StorageWrite,
        BudgetResourceKind::Output => ResourceKind::Output,
        BudgetResourceKind::OutputBytes => ResourceKind::OutputBytes,
    }
}

const fn project_budget_refusal(refusal: BudgetMeterRefusal) -> MeterRefusal {
    match refusal {
        BudgetMeterRefusal::BudgetExceeded {
            resource,
            limit,
            attempted,
        } => MeterRefusal::BudgetExceeded {
            resource: project_budget_resource(resource),
            limit,
            attempted,
        },
        BudgetMeterRefusal::CounterOverflow { resource } => MeterRefusal::CounterOverflow {
            resource: project_budget_resource(resource),
        },
    }
}

/// Per-execution deterministic meter and guest resource limiter.
#[derive(Debug, Clone)]
pub struct Meter {
    budget: ResourceBudget,
    prices: FeeSchedule,
    cpu_fuel: u64,
    cpu_carried: u64,
    memory_bytes: u64,
    active_memory_bytes: u64,
    active_table_elements: u64,
    storage_read_bytes: u64,
    storage_write_bytes: u64,
    output_values: u32,
    output_bytes: u64,
    mode: MeterMode,
    exhausted: Option<MeterExhaustion>,
}

impl Meter {
    /// Creates a fresh meter for one execution.
    #[must_use]
    pub const fn new(budget: ResourceBudget, prices: FeeSchedule) -> Self {
        Self {
            budget,
            prices,
            cpu_fuel: 0,
            cpu_carried: 0,
            memory_bytes: 0,
            active_memory_bytes: 0,
            active_table_elements: 0,
            storage_read_bytes: 0,
            storage_write_bytes: 0,
            output_values: 0,
            output_bytes: 0,
            mode: MeterMode::Legacy,
            exhausted: None,
        }
    }

    /// Creates a meter for an admitted activity with distinct table taxonomy.
    #[must_use]
    pub(crate) const fn new_activity(budget: ResourceBudget, prices: FeeSchedule) -> Self {
        let mut meter = Self::new(budget, prices);
        meter.mode = MeterMode::Activity;
        meter
    }

    pub(crate) const fn is_activity(&self) -> bool {
        matches!(self.mode, MeterMode::Activity)
    }

    /// Creates a fresh meter under the declared production budget and prices.
    #[must_use]
    pub const fn declared() -> Self {
        Self::new(ResourceBudget::declared(), FeeSchedule::declared())
    }

    /// Returns the declared instruction-fuel budget of the whole call graph.
    #[must_use]
    pub(crate) const fn fee_schedule_version(&self) -> u32 {
        self.prices.version()
    }

    pub const fn cpu_budget(&self) -> u64 {
        self.budget.cpu_fuel
    }

    /// Returns the fuel already consumed by frames outside the store this
    /// meter is about to drive.
    #[must_use]
    pub const fn cpu_carried(&self) -> u64 {
        self.cpu_carried
    }

    /// Returns the unconsumed CPU budget of the whole call graph.
    #[must_use]
    pub const fn cpu_remaining(&self) -> u64 {
        self.budget.cpu_fuel.saturating_sub(self.cpu_fuel)
    }

    /// Returns the fuel attributed to this meter by the last recorded frame.
    #[must_use]
    pub const fn cpu_total(&self) -> u64 {
        self.cpu_fuel
    }

    pub(crate) const fn qualification_snapshot(&self) -> QualificationMeterSnapshot {
        QualificationMeterSnapshot {
            cpu_fuel: self.cpu_fuel,
            memory_bytes: self.memory_bytes,
            storage_read_bytes: self.storage_read_bytes,
            storage_write_bytes: self.storage_write_bytes,
            output_values: self.output_values,
            output_bytes: self.output_bytes,
        }
    }

    pub(crate) fn execution_trace_usage(&self) -> Result<MeteredUsage, MeterRefusal> {
        self.finish_bounded_usage(self.cpu_fuel)
    }

    pub(crate) fn carry_cpu(&mut self, consumed: u64) -> Result<(), MeterRefusal> {
        let attempted = self.counter_add(ResourceKind::Cpu, self.cpu_carried, consumed)?;
        self.admit(ResourceKind::Cpu, self.budget.cpu_fuel, attempted)?;
        self.cpu_carried = attempted;
        Ok(())
    }

    pub(crate) fn restore_cpu_carry(&mut self, carried: u64) {
        self.cpu_carried = carried;
    }

    pub(crate) const fn active_frame_resources(&self) -> (u64, u64) {
        (self.active_memory_bytes, self.active_table_elements)
    }

    pub(crate) fn restore_active_frame_resources(
        &mut self,
        memory_bytes: u64,
        table_elements: u64,
    ) {
        self.active_memory_bytes = memory_bytes;
        self.active_table_elements = table_elements;
    }

    /// Charges bytes read through the future versioned storage ABI.
    ///
    /// # Errors
    ///
    /// Returns a typed refusal before the read when its cumulative budget is exceeded.
    pub fn charge_storage_read(&mut self, bytes: u64) -> Result<(), MeterRefusal> {
        let attempted =
            self.counter_add(ResourceKind::StorageRead, self.storage_read_bytes, bytes)?;
        self.admit(
            ResourceKind::StorageRead,
            self.budget.storage_read_bytes,
            attempted,
        )?;
        self.storage_read_bytes = attempted;
        Ok(())
    }

    /// Charges bytes written through the future versioned storage ABI.
    ///
    /// # Errors
    ///
    /// Returns a typed refusal before the write when its cumulative budget is exceeded.
    pub fn charge_storage_write(&mut self, bytes: u64) -> Result<(), MeterRefusal> {
        let attempted =
            self.counter_add(ResourceKind::StorageWrite, self.storage_write_bytes, bytes)?;
        self.admit(
            ResourceKind::StorageWrite,
            self.budget.storage_write_bytes,
            attempted,
        )?;
        self.storage_write_bytes = attempted;
        Ok(())
    }

    /// Charges CPU fuel for computational operations like wide-integer arithmetic.
    ///
    /// # Errors
    ///
    /// Returns a typed refusal when the cumulative CPU budget is exceeded.
    pub fn charge_cpu(&mut self, fuel: u64) -> Result<(), MeterRefusal> {
        let attempted = self.counter_add(ResourceKind::Cpu, self.cpu_fuel, fuel)?;
        self.admit(ResourceKind::Cpu, self.budget.cpu_fuel, attempted)?;
        self.cpu_fuel = attempted;
        Ok(())
    }

    pub(crate) fn mark_cpu_exhausted(&mut self) {
        let attempted = if self.is_activity() {
            self.budget.cpu_fuel.checked_add(1)
        } else {
            self.cpu_fuel.checked_add(1)
        };
        self.record_exhaustion(match attempted {
            Some(attempted) => MeterRefusal::BudgetExceeded {
                resource: ResourceKind::Cpu,
                limit: self.budget.cpu_fuel,
                attempted,
            },
            None => MeterRefusal::CounterOverflow {
                resource: ResourceKind::Cpu,
            },
        });
    }

    pub(crate) fn charge_output(
        &mut self,
        values: usize,
    ) -> Result<OutputReservation, MeterRefusal> {
        let reserved = u32::try_from(values).map_err(|_| {
            let refusal = MeterRefusal::CounterOverflow {
                resource: ResourceKind::Output,
            };
            self.record_exhaustion(refusal);
            refusal
        })?;
        let requested = u64::from(reserved);
        let attempted = self.counter_add(
            ResourceKind::Output,
            u64::from(self.output_values),
            requested,
        )?;
        self.admit(
            ResourceKind::Output,
            u64::from(self.budget.output_values),
            attempted,
        )?;
        self.output_values =
            u32::try_from(attempted).map_err(|_| MeterRefusal::BudgetExceeded {
                resource: ResourceKind::Output,
                limit: u64::from(self.budget.output_values),
                attempted,
            })?;
        Ok(OutputReservation(reserved))
    }

    pub(crate) fn rollback_output(&mut self, reservation: OutputReservation) {
        self.output_values = self
            .output_values
            .checked_sub(reservation.0)
            .unwrap_or_else(|| unreachable!());
    }

    pub(crate) fn charge_output_bytes(&mut self, bytes: usize) -> Result<(), MeterRefusal> {
        let requested = u64::try_from(bytes).unwrap_or(u64::MAX);
        let attempted =
            self.counter_add(ResourceKind::OutputBytes, self.output_bytes, requested)?;
        self.admit(
            ResourceKind::OutputBytes,
            self.budget.output_bytes,
            attempted,
        )?;
        self.output_bytes = attempted;
        Ok(())
    }

    pub(crate) const fn exhaustion(&self) -> Option<MeterRefusal> {
        match self.exhausted {
            Some(MeterExhaustion::Legacy(refusal)) => Some(refusal),
            Some(MeterExhaustion::Budget(refusal)) => Some(project_budget_refusal(refusal)),
            None => None,
        }
    }

    pub(crate) const fn budget_exhaustion(&self) -> Option<BudgetMeterRefusal> {
        match self.exhausted {
            Some(MeterExhaustion::Budget(refusal)) => Some(refusal),
            Some(MeterExhaustion::Legacy(refusal)) => budget_refusal(refusal),
            None => None,
        }
    }

    /// Finalises exact usage and fee units after guest execution.
    ///
    /// # Errors
    ///
    /// Returns a prior resource refusal or fee overflow.
    pub fn finish(&self) -> Result<MeteredUsage, MeterRefusal> {
        if let Some(refusal) = self.exhaustion() {
            return Err(refusal);
        }
        self.finish_bounded_usage(self.cpu_fuel)
    }

    /// Finalises a published ABI-v2 failure after that same guest frame
    /// consumed its complete CPU allowance.
    pub(crate) fn finish_published_failure(&self) -> Result<MeteredUsage, MeterRefusal> {
        match self.exhaustion() {
            None => self.finish_bounded_usage(self.cpu_fuel),
            Some(MeterRefusal::BudgetExceeded {
                resource: ResourceKind::Cpu,
                limit,
                attempted,
            }) if limit == self.budget.cpu_fuel
                && self.cpu_fuel.checked_add(1) == Some(attempted)
                && (attempted == limit || limit.checked_add(1) == Some(attempted)) =>
            {
                self.finish_bounded_usage(limit)
            }
            Some(refusal) => Err(refusal),
        }
    }

    /// Prices only counters admitted before a resource increment was refused.
    pub(crate) fn finish_resource_failure(&self) -> Result<MeteredUsage, MeterRefusal> {
        self.finish_bounded_usage(self.cpu_fuel)
    }

    fn finish_bounded_usage(&self, cpu_fuel: u64) -> Result<MeteredUsage, MeterRefusal> {
        let priced = [
            (u128::from(cpu_fuel), self.prices.fee_units_per_cpu_fuel),
            (
                u128::from(self.memory_bytes),
                self.prices.fee_units_per_memory_byte,
            ),
            (
                u128::from(self.storage_read_bytes),
                self.prices.fee_units_per_storage_read_byte,
            ),
            (
                u128::from(self.storage_write_bytes),
                self.prices.fee_units_per_storage_write_byte,
            ),
            (
                u128::from(self.output_values),
                self.prices.fee_units_per_output_value,
            ),
            (
                u128::from(self.output_bytes),
                self.prices.fee_units_per_output_byte,
            ),
        ];
        let mut fee_units = 0u128;
        for (use_units, price) in priced {
            fee_units = fee_units
                .checked_add(
                    use_units
                        .checked_mul(u128::from(price))
                        .ok_or(MeterRefusal::FeeOverflow)?,
                )
                .ok_or(MeterRefusal::FeeOverflow)?;
        }
        Ok(MeteredUsage {
            cpu_fuel,
            memory_bytes: self.memory_bytes,
            storage_read_bytes: self.storage_read_bytes,
            storage_write_bytes: self.storage_write_bytes,
            output_values: self.output_values,
            output_bytes: self.output_bytes,
            occupancy_byte_batches: 0,
            occupancy_fee_units: 0,
            fee_units,
        })
    }

    fn admit(
        &mut self,
        resource: ResourceKind,
        limit: u64,
        attempted: u64,
    ) -> Result<(), MeterRefusal> {
        if attempted <= limit {
            return Ok(());
        }
        let refusal = MeterRefusal::BudgetExceeded {
            resource,
            limit,
            attempted,
        };
        self.record_exhaustion(refusal);
        Err(refusal)
    }

    fn counter_add(
        &mut self,
        resource: ResourceKind,
        current: u64,
        increment: u64,
    ) -> Result<u64, MeterRefusal> {
        current.checked_add(increment).ok_or_else(|| {
            let refusal = MeterRefusal::CounterOverflow { resource };
            self.record_exhaustion(refusal);
            refusal
        })
    }

    fn record_exhaustion(&mut self, refusal: MeterRefusal) {
        if self.is_activity() && self.exhausted.is_some() {
            return;
        }
        self.exhausted = Some(if self.is_activity() {
            match budget_refusal(refusal) {
                Some(refusal) => MeterExhaustion::Budget(refusal),
                None => MeterExhaustion::Legacy(refusal),
            }
        } else {
            MeterExhaustion::Legacy(refusal)
        });
    }
}

impl Default for Meter {
    fn default() -> Self {
        Self::declared()
    }
}

impl ResourceLimiter for Meter {
    fn memory_growing(
        &mut self,
        current: usize,
        desired: usize,
        maximum: Option<usize>,
    ) -> Result<bool, MemoryError> {
        if maximum.is_some_and(|maximum| desired > maximum) {
            return Ok(false);
        }
        let desired = u64::try_from(desired).unwrap_or(u64::MAX);
        let attempted = if self.is_activity() {
            let current = u64::try_from(current).unwrap_or(u64::MAX);
            let increment = desired
                .checked_sub(current)
                .ok_or(MemoryError::OutOfBoundsGrowth)?;
            self.counter_add(ResourceKind::Memory, self.active_memory_bytes, increment)
                .map_err(|_| MemoryError::OutOfBoundsGrowth)?
        } else {
            desired
        };
        if self
            .admit(ResourceKind::Memory, self.budget.memory_bytes, attempted)
            .is_err()
        {
            return Err(MemoryError::OutOfBoundsGrowth);
        }
        self.memory_bytes = self.memory_bytes.max(attempted);
        if self.is_activity() {
            self.active_memory_bytes = attempted;
        }
        Ok(true)
    }

    fn table_growing(
        &mut self,
        current: u32,
        desired: u32,
        maximum: Option<u32>,
    ) -> Result<bool, TableError> {
        if self.is_activity() {
            if maximum.is_some_and(|maximum| desired > maximum) {
                return Ok(false);
            }
            let increment = u64::from(desired.checked_sub(current).ok_or(
                TableError::GrowOutOfBounds {
                    maximum: self.budget.table_elements,
                    current,
                    delta: 0,
                },
            )?);
            let Some(attempted) = self.active_table_elements.checked_add(increment) else {
                if self.exhausted.is_none() {
                    self.exhausted = Some(MeterExhaustion::Budget(
                        BudgetMeterRefusal::CounterOverflow {
                            resource: BudgetResourceKind::Table,
                        },
                    ));
                }
                return Err(TableError::GrowOutOfBounds {
                    maximum: self.budget.table_elements,
                    current,
                    delta: desired.saturating_sub(current),
                });
            };
            if attempted > u64::from(self.budget.table_elements) {
                if self.exhausted.is_none() {
                    self.exhausted = Some(MeterExhaustion::Budget(
                        BudgetMeterRefusal::BudgetExceeded {
                            resource: BudgetResourceKind::Table,
                            limit: u64::from(self.budget.table_elements),
                            attempted,
                        },
                    ));
                }
                return Err(TableError::GrowOutOfBounds {
                    maximum: self.budget.table_elements,
                    current,
                    delta: desired.saturating_sub(current),
                });
            }
            self.active_table_elements = attempted;
            return Ok(true);
        }
        let limit = maximum
            .unwrap_or(self.budget.table_elements)
            .min(self.budget.table_elements);
        if desired <= limit {
            return Ok(true);
        }
        self.record_exhaustion(MeterRefusal::BudgetExceeded {
            resource: ResourceKind::Memory,
            limit: u64::from(limit),
            attempted: u64::from(desired),
        });
        Err(TableError::GrowOutOfBounds {
            maximum: limit,
            current,
            delta: desired.saturating_sub(current),
        })
    }

    fn instances(&self) -> usize {
        1
    }

    fn tables(&self) -> usize {
        1
    }

    fn memories(&self) -> usize {
        1
    }
}

#[cfg(test)]
mod response_tests {
    use super::{FeeSchedule, Meter, MeterRefusal, ResourceBudget, ResourceKind};

    #[test]
    fn response_byte_counter_overflow_is_typed() {
        let mut meter = Meter::new(
            ResourceBudget::declared().with_output_bytes(u64::MAX),
            FeeSchedule::declared(),
        );
        meter.output_bytes = u64::MAX;
        assert_eq!(
            meter.charge_output_bytes(1),
            Err(MeterRefusal::CounterOverflow {
                resource: ResourceKind::OutputBytes,
            })
        );
    }

    #[test]
    fn failed_parent_output_rollback_preserves_nested_output_usage() {
        let mut meter = Meter::new_activity(ResourceBudget::declared(), FeeSchedule::declared());
        let parent = meter
            .charge_output(1)
            .unwrap_or_else(|error| panic!("parent reservation: {error}"));
        let _child = meter
            .charge_output(1)
            .unwrap_or_else(|error| panic!("child reservation: {error}"));
        meter.rollback_output(parent);
        let _sibling = meter
            .charge_output(1)
            .unwrap_or_else(|error| panic!("sibling reservation: {error}"));
        let usage = meter
            .finish_resource_failure()
            .unwrap_or_else(|error| panic!("usage: {error}"));
        assert_eq!(usage.output_values, 2);
    }

    #[test]
    fn oversized_output_reservation_is_typed_and_unbilled() {
        let mut meter = Meter::new_activity(ResourceBudget::declared(), FeeSchedule::declared());
        assert_eq!(
            meter.charge_output(usize::MAX),
            Err(MeterRefusal::CounterOverflow {
                resource: ResourceKind::Output,
            })
        );
        let usage = meter
            .finish_resource_failure()
            .unwrap_or_else(|error| panic!("usage: {error}"));
        assert_eq!(usage.output_values, 0);
    }
}

#[cfg(test)]
mod declared_budget_tests {
    use super::{
        FeeSchedule, Meter, MeterRefusal, MeteredUsage, ResourceBudget, ResourceKind,
        ResourceLimiter,
    };
    use crate::budget::{maximum_fee_units, DeclaredBudget, MIN_ACTIVITY_CPU_FUEL};

    const PAGE: u64 = 65_536;

    #[derive(Debug, Clone, Copy)]
    enum Charge {
        Cpu(u64),
        MemoryPages(u64),
        StorageRead(u64),
        StorageWrite(u64),
        OutputValues(usize),
        OutputBytes(usize),
    }

    struct Sequence {
        state: u64,
    }

    impl Sequence {
        fn next(&mut self, bound: u64) -> u64 {
            self.state = self
                .state
                .wrapping_mul(6_364_136_223_846_793_005)
                .wrapping_add(1_442_695_040_888_963_407);
            (self.state >> 33) % bound
        }
    }

    fn charges(seed: u64) -> Vec<Charge> {
        let mut sequence = Sequence { state: seed };
        let length = 1 + sequence.next(24);
        (0..length)
            .map(|_| match sequence.next(6) {
                0 => Charge::Cpu(1 + sequence.next(2_000)),
                1 => Charge::MemoryPages(1 + sequence.next(4)),
                2 => Charge::StorageRead(sequence.next(4_096)),
                3 => Charge::StorageWrite(sequence.next(4_096)),
                4 => Charge::OutputValues(
                    usize::try_from(1 + sequence.next(2))
                        .unwrap_or_else(|error| panic!("values: {error}")),
                ),
                _ => Charge::OutputBytes(
                    usize::try_from(sequence.next(4_096))
                        .unwrap_or_else(|error| panic!("bytes: {error}")),
                ),
            })
            .collect()
    }

    fn execute(budget: ResourceBudget, charges: &[Charge]) -> Result<MeteredUsage, MeterRefusal> {
        let mut meter = Meter::new_activity(budget, FeeSchedule::declared());
        let mut memory = 0_usize;
        for charge in charges {
            match *charge {
                Charge::Cpu(fuel) => meter.charge_cpu(fuel)?,
                Charge::MemoryPages(pages) => {
                    let desired = memory
                        + usize::try_from(pages * PAGE)
                            .unwrap_or_else(|error| panic!("pages: {error}"));
                    if meter.memory_growing(memory, desired, None).is_err() {
                        return meter.finish();
                    }
                    memory = desired;
                }
                Charge::StorageRead(bytes) => meter.charge_storage_read(bytes)?,
                Charge::StorageWrite(bytes) => meter.charge_storage_write(bytes)?,
                Charge::OutputValues(values) => {
                    meter.charge_output(values)?;
                }
                Charge::OutputBytes(bytes) => meter.charge_output_bytes(bytes)?,
            }
        }
        meter.finish()
    }

    fn exact_fit(charges: &[Charge]) -> DeclaredBudget {
        let mut totals = [0_u64; 6];
        for charge in charges {
            let (index, amount) = match *charge {
                Charge::Cpu(fuel) => (0, fuel),
                Charge::MemoryPages(pages) => (1, pages * PAGE),
                Charge::StorageRead(bytes) => (2, bytes),
                Charge::StorageWrite(bytes) => (3, bytes),
                Charge::OutputValues(values) => (
                    4,
                    u64::try_from(values).unwrap_or_else(|error| panic!("values: {error}")),
                ),
                Charge::OutputBytes(bytes) => (
                    5,
                    u64::try_from(bytes).unwrap_or_else(|error| panic!("bytes: {error}")),
                ),
            };
            totals[index] += amount;
        }
        DeclaredBudget::new(
            totals[0].max(MIN_ACTIVITY_CPU_FUEL),
            totals[1].max(PAGE),
            totals[2],
            totals[3],
            u32::try_from(totals[4].max(1)).unwrap_or_else(|error| panic!("values: {error}")),
            totals[5],
            0,
        )
        .unwrap_or_else(|error| panic!("exact-fit declaration: {error}"))
    }

    fn widened(exact: DeclaredBudget, sequence: &mut Sequence) -> DeclaredBudget {
        let maximum = DeclaredBudget::protocol_maximum();
        let mut widen =
            |declared: u64, ceiling: u64| declared + sequence.next(ceiling - declared + 1);
        let output_values = widen(
            u64::from(exact.output_values()),
            u64::from(maximum.output_values()),
        );
        let table_elements = widen(0, u64::from(maximum.table_elements()));
        DeclaredBudget::new(
            widen(exact.cpu_fuel(), maximum.cpu_fuel()),
            widen(exact.memory_bytes(), maximum.memory_bytes()),
            widen(exact.storage_read_bytes(), maximum.storage_read_bytes()),
            widen(exact.storage_write_bytes(), maximum.storage_write_bytes()),
            u32::try_from(output_values).unwrap_or_else(|error| panic!("values: {error}")),
            widen(exact.output_bytes(), maximum.output_bytes()),
            u32::try_from(table_elements).unwrap_or_else(|error| panic!("table: {error}")),
        )
        .unwrap_or_else(|error| panic!("widened declaration: {error}"))
    }

    fn priced(usage: MeteredUsage) -> u128 {
        let schedule = FeeSchedule::declared();
        u128::from(usage.cpu_fuel) * u128::from(schedule.cpu_price())
            + u128::from(usage.memory_bytes) * u128::from(schedule.memory_byte_price())
            + u128::from(usage.storage_read_bytes) * u128::from(schedule.storage_read_byte_price())
            + u128::from(usage.storage_write_bytes)
                * u128::from(schedule.storage_write_byte_price())
            + u128::from(usage.output_values) * u128::from(schedule.output_value_price())
            + u128::from(usage.output_bytes) * u128::from(schedule.output_byte_price())
    }

    #[test]
    fn declared_ceiling_never_changes_consumed_usage_or_billed_fee() {
        let maximum_fee = maximum_fee_units(
            DeclaredBudget::protocol_maximum().resource_budget(),
            FeeSchedule::declared(),
        )
        .unwrap_or_else(|error| panic!("maximum fee: {error}"));
        for seed in 0_u64..512 {
            let charges = charges(seed);
            let exact = exact_fit(&charges);
            let baseline = execute(exact.resource_budget(), &charges)
                .unwrap_or_else(|error| panic!("seed {seed} exact fit: {error}"));
            assert_eq!(baseline.fee_units, priced(baseline), "seed {seed}");
            assert!(baseline.fee_units < maximum_fee, "seed {seed}");
            let mut sequence = Sequence {
                state: seed ^ 0x5eed,
            };
            let mut ceilings = vec![DeclaredBudget::protocol_maximum()];
            ceilings.extend((0..8).map(|_| widened(exact, &mut sequence)));
            for ceiling in ceilings {
                let usage = execute(ceiling.resource_budget(), &charges)
                    .unwrap_or_else(|error| panic!("seed {seed} widened: {error}"));
                assert_eq!(usage, baseline, "seed {seed} ceiling {ceiling:?}");
                let ceiling_fee =
                    maximum_fee_units(ceiling.resource_budget(), FeeSchedule::declared())
                        .unwrap_or_else(|error| panic!("seed {seed} ceiling fee: {error}"));
                assert!(usage.fee_units <= ceiling_fee, "seed {seed}");
            }
        }
    }

    #[test]
    fn one_short_declared_ceiling_is_a_typed_refusal_at_the_exact_dimension() {
        for seed in 0_u64..512 {
            let charges = charges(seed);
            let budget = exact_fit(&charges).resource_budget();
            let baseline = execute(budget, &charges)
                .unwrap_or_else(|error| panic!("seed {seed} exact fit: {error}"));
            let shortened = [
                (
                    ResourceKind::Cpu,
                    baseline.cpu_fuel,
                    budget.cpu_fuel(),
                    ResourceBudget::new_complete(
                        baseline.cpu_fuel.saturating_sub(1),
                        budget.memory_bytes(),
                        budget.storage_read_bytes(),
                        budget.storage_write_bytes(),
                        budget.output_values(),
                        budget.output_bytes(),
                        budget.table_elements(),
                    ),
                ),
                (
                    ResourceKind::StorageRead,
                    baseline.storage_read_bytes,
                    budget.storage_read_bytes(),
                    ResourceBudget::new_complete(
                        budget.cpu_fuel(),
                        budget.memory_bytes(),
                        baseline.storage_read_bytes.saturating_sub(1),
                        budget.storage_write_bytes(),
                        budget.output_values(),
                        budget.output_bytes(),
                        budget.table_elements(),
                    ),
                ),
                (
                    ResourceKind::StorageWrite,
                    baseline.storage_write_bytes,
                    budget.storage_write_bytes(),
                    ResourceBudget::new_complete(
                        budget.cpu_fuel(),
                        budget.memory_bytes(),
                        budget.storage_read_bytes(),
                        baseline.storage_write_bytes.saturating_sub(1),
                        budget.output_values(),
                        budget.output_bytes(),
                        budget.table_elements(),
                    ),
                ),
                (
                    ResourceKind::OutputBytes,
                    baseline.output_bytes,
                    budget.output_bytes(),
                    ResourceBudget::new_complete(
                        budget.cpu_fuel(),
                        budget.memory_bytes(),
                        budget.storage_read_bytes(),
                        budget.storage_write_bytes(),
                        budget.output_values(),
                        baseline.output_bytes.saturating_sub(1),
                        budget.table_elements(),
                    ),
                ),
            ];
            for (resource, used, declared, short) in shortened {
                if used == 0 || used != declared {
                    continue;
                }
                match execute(short, &charges) {
                    Err(MeterRefusal::BudgetExceeded {
                        resource: refused,
                        limit,
                        attempted,
                    }) => {
                        assert_eq!(refused, resource, "seed {seed}");
                        assert_eq!(limit, used - 1, "seed {seed}");
                        assert!(attempted > limit, "seed {seed}");
                    }
                    other => panic!("seed {seed} {resource}: expected refusal, got {other:?}"),
                }
            }
        }
    }
}

const REPLAY_METER_DOMAIN: &[u8] = b"LayerX/programs/replay-meter/v1\0";
const MAX_REPLAY_METER_BYTES: usize = REPLAY_METER_DOMAIN.len() + 48 + 60 + 68 + 1 + 19;

fn replay_resource_code(resource: ResourceKind) -> u8 {
    match resource {
        ResourceKind::Cpu => 0,
        ResourceKind::Memory => 1,
        ResourceKind::StorageRead => 2,
        ResourceKind::StorageWrite => 3,
        ResourceKind::StorageOccupancy => 4,
        ResourceKind::Output => 5,
        ResourceKind::OutputBytes => 6,
    }
}
fn replay_budget_resource_code(resource: BudgetResourceKind) -> u8 {
    match resource {
        BudgetResourceKind::Cpu => 0,
        BudgetResourceKind::Memory => 1,
        BudgetResourceKind::StorageRead => 2,
        BudgetResourceKind::StorageWrite => 3,
        BudgetResourceKind::Output => 4,
        BudgetResourceKind::OutputBytes => 5,
        BudgetResourceKind::Table => 6,
    }
}
fn replay_resource(code: u8) -> Result<ResourceKind, crate::replay::ReplayWitnessError> {
    Ok(match code {
        0 => ResourceKind::Cpu,
        1 => ResourceKind::Memory,
        2 => ResourceKind::StorageRead,
        3 => ResourceKind::StorageWrite,
        4 => ResourceKind::StorageOccupancy,
        5 => ResourceKind::Output,
        6 => ResourceKind::OutputBytes,
        _ => return Err(crate::replay::ReplayWitnessError::Encoding),
    })
}
fn replay_budget_resource(
    code: u8,
) -> Result<BudgetResourceKind, crate::replay::ReplayWitnessError> {
    Ok(match code {
        0 => BudgetResourceKind::Cpu,
        1 => BudgetResourceKind::Memory,
        2 => BudgetResourceKind::StorageRead,
        3 => BudgetResourceKind::StorageWrite,
        4 => BudgetResourceKind::Output,
        5 => BudgetResourceKind::OutputBytes,
        6 => BudgetResourceKind::Table,
        _ => return Err(crate::replay::ReplayWitnessError::Encoding),
    })
}

impl Meter {
    pub(crate) fn replay_state_bytes(&self) -> Result<Vec<u8>, crate::replay::ReplayWitnessError> {
        let mut out = Vec::new();
        out.try_reserve_exact(MAX_REPLAY_METER_BYTES)
            .map_err(|_| crate::replay::ReplayWitnessError::Allocation)?;
        out.extend_from_slice(REPLAY_METER_DOMAIN);
        for value in [
            self.budget.cpu_fuel,
            self.budget.memory_bytes,
            self.budget.storage_read_bytes,
            self.budget.storage_write_bytes,
        ] {
            out.extend_from_slice(&value.to_be_bytes());
        }
        out.extend_from_slice(&self.budget.output_values.to_be_bytes());
        out.extend_from_slice(&self.budget.output_bytes.to_be_bytes());
        out.extend_from_slice(&self.budget.table_elements.to_be_bytes());
        out.extend_from_slice(&self.prices.version.to_be_bytes());
        for value in [
            self.prices.fee_units_per_cpu_fuel,
            self.prices.fee_units_per_memory_byte,
            self.prices.fee_units_per_storage_read_byte,
            self.prices.fee_units_per_storage_write_byte,
            self.prices.fee_units_per_output_value,
            self.prices.fee_units_per_output_byte,
            self.prices.fee_units_per_occupancy_byte_batch,
        ] {
            out.extend_from_slice(&value.to_be_bytes());
        }
        for value in [
            self.cpu_fuel,
            self.cpu_carried,
            self.memory_bytes,
            self.active_memory_bytes,
            self.active_table_elements,
            self.storage_read_bytes,
            self.storage_write_bytes,
        ] {
            out.extend_from_slice(&value.to_be_bytes());
        }
        out.extend_from_slice(&self.output_values.to_be_bytes());
        out.extend_from_slice(&self.output_bytes.to_be_bytes());
        out.push(match self.mode {
            MeterMode::Legacy => 0,
            MeterMode::Activity => 1,
        });
        match self.exhausted {
            None => out.push(0),
            Some(MeterExhaustion::Legacy(refusal)) => {
                out.push(1);
                match refusal {
                    MeterRefusal::BudgetExceeded {
                        resource,
                        limit,
                        attempted,
                    } => {
                        out.extend_from_slice(&[0, replay_resource_code(resource)]);
                        out.extend_from_slice(&limit.to_be_bytes());
                        out.extend_from_slice(&attempted.to_be_bytes());
                    }
                    MeterRefusal::CounterOverflow { resource } => {
                        out.extend_from_slice(&[1, replay_resource_code(resource)])
                    }
                    MeterRefusal::FeeOverflow => out.push(2),
                }
            }
            Some(MeterExhaustion::Budget(refusal)) => {
                out.push(2);
                match refusal {
                    BudgetMeterRefusal::BudgetExceeded {
                        resource,
                        limit,
                        attempted,
                    } => {
                        out.extend_from_slice(&[0, replay_budget_resource_code(resource)]);
                        out.extend_from_slice(&limit.to_be_bytes());
                        out.extend_from_slice(&attempted.to_be_bytes());
                    }
                    BudgetMeterRefusal::CounterOverflow { resource } => {
                        out.extend_from_slice(&[1, replay_budget_resource_code(resource)])
                    }
                }
            }
        }
        Ok(out)
    }

    pub(crate) fn from_replay_state_bytes(
        bytes: &[u8],
    ) -> Result<Self, crate::replay::ReplayWitnessError> {
        use crate::replay::{ReplayCursor, ReplayWitnessError as E};
        if bytes.len() > MAX_REPLAY_METER_BYTES {
            return Err(E::Bounds);
        }
        let mut cursor = ReplayCursor::new(bytes);
        if cursor.take(REPLAY_METER_DOMAIN.len())? != REPLAY_METER_DOMAIN {
            return Err(E::Encoding);
        }
        let budget = ResourceBudget {
            cpu_fuel: cursor.u64()?,
            memory_bytes: cursor.u64()?,
            storage_read_bytes: cursor.u64()?,
            storage_write_bytes: cursor.u64()?,
            output_values: cursor.u32()?,
            output_bytes: cursor.u64()?,
            table_elements: cursor.u32()?,
        };
        let prices = FeeSchedule {
            version: cursor.u32()?,
            fee_units_per_cpu_fuel: cursor.u64()?,
            fee_units_per_memory_byte: cursor.u64()?,
            fee_units_per_storage_read_byte: cursor.u64()?,
            fee_units_per_storage_write_byte: cursor.u64()?,
            fee_units_per_output_value: cursor.u64()?,
            fee_units_per_output_byte: cursor.u64()?,
            fee_units_per_occupancy_byte_batch: cursor.u64()?,
        };
        let cpu_fuel = cursor.u64()?;
        let cpu_carried = cursor.u64()?;
        let memory_bytes = cursor.u64()?;
        let active_memory_bytes = cursor.u64()?;
        let active_table_elements = cursor.u64()?;
        let storage_read_bytes = cursor.u64()?;
        let storage_write_bytes = cursor.u64()?;
        let output_values = cursor.u32()?;
        let output_bytes = cursor.u64()?;
        let mode = match cursor.u8()? {
            0 => MeterMode::Legacy,
            1 => MeterMode::Activity,
            _ => return Err(E::Encoding),
        };
        let exhausted = match cursor.u8()? {
            0 => None,
            1 => Some(MeterExhaustion::Legacy(match cursor.u8()? {
                0 => MeterRefusal::BudgetExceeded {
                    resource: replay_resource(cursor.u8()?)?,
                    limit: cursor.u64()?,
                    attempted: cursor.u64()?,
                },
                1 => MeterRefusal::CounterOverflow {
                    resource: replay_resource(cursor.u8()?)?,
                },
                2 => MeterRefusal::FeeOverflow,
                _ => return Err(E::Encoding),
            })),
            2 => Some(MeterExhaustion::Budget(match cursor.u8()? {
                0 => BudgetMeterRefusal::BudgetExceeded {
                    resource: replay_budget_resource(cursor.u8()?)?,
                    limit: cursor.u64()?,
                    attempted: cursor.u64()?,
                },
                1 => BudgetMeterRefusal::CounterOverflow {
                    resource: replay_budget_resource(cursor.u8()?)?,
                },
                _ => return Err(E::Encoding),
            })),
            _ => return Err(E::Encoding),
        };
        if !cursor.done() {
            return Err(E::Encoding);
        }
        let meter = Self {
            budget,
            prices,
            cpu_fuel,
            cpu_carried,
            memory_bytes,
            active_memory_bytes,
            active_table_elements,
            storage_read_bytes,
            storage_write_bytes,
            output_values,
            output_bytes,
            mode,
            exhausted,
        };
        if meter.replay_state_bytes()?.as_slice() != bytes {
            return Err(E::Encoding);
        }
        Ok(meter)
    }
}

#[cfg(test)]
mod replay_meter_tests {
    use super::*;
    #[test]
    fn exact_replay_codec_preserves_real_usage_and_exhaustion() {
        let budget = ResourceBudget::new_complete(100, 131072, 100, 100, 8, 100, 2);
        let mut legacy = Meter::new(budget, FeeSchedule::declared());
        legacy
            .charge_cpu(9)
            .unwrap_or_else(|error| panic!("cpu: {error}"));
        legacy
            .carry_cpu(3)
            .unwrap_or_else(|error| panic!("carry: {error}"));
        legacy
            .charge_storage_read(11)
            .unwrap_or_else(|error| panic!("read: {error}"));
        legacy
            .charge_storage_write(13)
            .unwrap_or_else(|error| panic!("write: {error}"));
        legacy
            .memory_growing(0, 65536, Some(131072))
            .unwrap_or_else(|error| panic!("memory: {error}"));
        let mut activity = Meter::new_activity(budget, FeeSchedule::declared());
        assert!(activity.table_growing(0, 3, Some(4)).is_err());
        let mut exhausted = legacy.clone();
        assert!(exhausted.charge_cpu(101).is_err());
        for meter in [legacy, activity, exhausted] {
            let encoded = meter
                .replay_state_bytes()
                .unwrap_or_else(|error| panic!("encode: {error:?}"));
            let restored = Meter::from_replay_state_bytes(&encoded)
                .unwrap_or_else(|error| panic!("decode: {error:?}"));
            assert_eq!(restored.budget, meter.budget);
            assert_eq!(restored.prices, meter.prices);
            assert_eq!(restored.cpu_carried, meter.cpu_carried);
            assert_eq!(
                restored.active_frame_resources(),
                meter.active_frame_resources()
            );
            assert_eq!(restored.exhausted, meter.exhausted);
            assert_eq!(
                restored.execution_trace_usage(),
                meter.execution_trace_usage()
            );
            assert_eq!(restored.replay_state_bytes(), Ok(encoded.clone()));
            let mut trailing = encoded.clone();
            trailing.push(0);
            assert!(Meter::from_replay_state_bytes(&trailing).is_err());
            for length in 0..encoded.len() {
                assert!(Meter::from_replay_state_bytes(&encoded[..length]).is_err());
            }
        }
    }
}

#[cfg(test)]
mod occupancy_class_tests {
    use super::*;

    #[test]
    fn occupancy_is_its_own_class_outside_one_off_execution_charges() {
        let kinds = [
            ResourceKind::Cpu,
            ResourceKind::Memory,
            ResourceKind::StorageRead,
            ResourceKind::StorageWrite,
            ResourceKind::StorageOccupancy,
            ResourceKind::Output,
            ResourceKind::OutputBytes,
        ];
        for (index, kind) in kinds.iter().enumerate() {
            assert_eq!(replay_resource(replay_resource_code(*kind)), Ok(*kind));
            for other in &kinds[index + 1..] {
                assert_ne!(replay_resource_code(*kind), replay_resource_code(*other));
                assert_ne!(kind.to_string(), other.to_string());
            }
        }
        assert_eq!(budget_resource(ResourceKind::StorageOccupancy), None);
        let budget = ResourceBudget::new_complete(100, 131072, 100, 100, 8, 100, 2);
        let mut usages = Vec::new();
        for price in [1, 7, u64::MAX] {
            let schedule = FeeSchedule::declared().with_occupancy_byte_batch_price(price);
            let mut meter = Meter::new(budget, schedule);
            meter
                .charge_cpu(9)
                .unwrap_or_else(|error| panic!("cpu: {error}"));
            meter
                .charge_storage_read(11)
                .unwrap_or_else(|error| panic!("read: {error}"));
            meter
                .charge_storage_write(13)
                .unwrap_or_else(|error| panic!("write: {error}"));
            let usage = meter
                .finish()
                .unwrap_or_else(|error| panic!("finish: {error}"));
            assert_eq!(usage.storage_read_bytes, 11);
            assert_eq!(usage.storage_write_bytes, 13);
            assert_eq!(usage.occupancy_byte_batches, 0);
            assert_eq!(usage.occupancy_fee_units, 0);
            usages.push(usage);
        }
        assert!(usages.windows(2).all(|pair| pair[0] == pair[1]));
    }
}
