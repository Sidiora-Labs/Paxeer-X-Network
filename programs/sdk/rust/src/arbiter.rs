use crate::{Field, ProgramError, Reason};

pub const MARKET_SANDBOX_PROFILE_DOMAIN: &[u8] = b"LXP/market-sandbox-profile/v1\0";
pub const MARKET_SANDBOX_BILLING_DOMAIN: &[u8] = b"LXP/market-sandbox-billing/v1\0";
pub const MARKET_SANDBOX_PROFILE_CAPACITY: usize =
    MARKET_SANDBOX_PROFILE_DOMAIN.len() + 2 + 4 + 13 * 32 + 65 + 12 + 64 + 16 + 24 + 8 + 4;
pub const MARKET_SANDBOX_BILLING_CAPACITY: usize =
    MARKET_SANDBOX_BILLING_DOMAIN.len() + 2 + 128 + 48 + 32 + 8 + 4;
pub const MARKET_SANDBOX_CAPABILITIES: [u8; 4] = [0, 2, 1, 2];

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ProfileLimits {
    pub cpu_fuel: u64,
    pub memory_bytes: u64,
    pub storage_read_bytes: u64,
    pub storage_write_bytes: u64,
    pub output_values: u64,
    pub output_bytes: u64,
    pub table_elements: u64,
    pub namespace_bytes: u64,
}

impl ProfileLimits {
    fn values(self) -> [u64; 8] {
        [
            self.cpu_fuel,
            self.memory_bytes,
            self.storage_read_bytes,
            self.storage_write_bytes,
            self.output_values,
            self.output_bytes,
            self.table_elements,
            self.namespace_bytes,
        ]
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct MarketSandboxProfile {
    pub network_id: u32,
    pub market_program: [u8; 32],
    pub sandbox_program: [u8; 32],
    pub offer_id: [u8; 32],
    pub lease_id: [u8; 32],
    pub claim_id: [u8; 32],
    pub provider: [u8; 32],
    pub tenant: [u8; 32],
    pub code_hash: [u8; 32],
    pub input_digest: [u8; 32],
    pub attested_input_commitment: [u8; 32],
    pub namespace: [u8; 32],
    pub baseline_state_root: [u8; 32],
    pub initial_execution_state_root: [u8; 32],
    pub entrypoint_bytes: [u8; 64],
    pub entrypoint_length: u8,
    pub runtime_version: u16,
    pub abi_version: u16,
    pub fee_schedule_version: u32,
    pub metering_schedule_version: u32,
    pub limits: ProfileLimits,
    pub fee_budget: u128,
    pub interval_start: u64,
    pub interval_end: u64,
    pub response_deadline: u64,
    pub maximum_boundaries: u32,
    pub maximum_bytes: u32,
}

impl MarketSandboxProfile {
    pub fn entrypoint(&self) -> Result<&str, ProgramError> {
        let length = usize::from(self.entrypoint_length);
        if length == 0
            || length > self.entrypoint_bytes.len()
            || self.entrypoint_bytes[length..]
                .iter()
                .any(|byte| *byte != 0)
            || self.entrypoint_bytes[..length].contains(&0)
        {
            return Err(malformed());
        }
        core::str::from_utf8(&self.entrypoint_bytes[..length]).map_err(|_| malformed())
    }

    pub fn validate(&self) -> Result<(), ProgramError> {
        self.entrypoint()?;
        let identifiers = [
            self.market_program,
            self.sandbox_program,
            self.offer_id,
            self.lease_id,
            self.claim_id,
            self.provider,
            self.tenant,
            self.code_hash,
            self.input_digest,
            self.attested_input_commitment,
            self.namespace,
            self.baseline_state_root,
            self.initial_execution_state_root,
        ];
        if self.network_id == 0
            || identifiers.iter().any(|value| *value == [0; 32])
            || self.runtime_version == 0
            || !(1..=4).contains(&self.abi_version)
            || self.fee_schedule_version == 0
            || self.metering_schedule_version == 0
            || self.limits.cpu_fuel == 0
            || self.limits.cpu_fuel > 1_000_000_000
            || self.limits.memory_bytes > (1 << 30)
            || self.limits.output_values > 65_536
            || self.limits.table_elements > (1 << 20)
            || self.limits.namespace_bytes > (1 << 30)
            || self.limits.storage_read_bytes > (1 << 30)
            || self.limits.storage_write_bytes > (1 << 30)
            || self.limits.output_bytes > (1 << 30)
            || self.fee_budget == 0
            || self.interval_start >= self.interval_end
            || self.interval_end >= self.response_deadline
            || !(2..=4096).contains(&self.maximum_boundaries)
            || self.maximum_bytes == 0
            || self.maximum_bytes > 1_048_576
        {
            return Err(malformed());
        }
        Ok(())
    }

    pub fn encode(&self, output: &mut [u8]) -> Result<usize, ProgramError> {
        self.validate()?;
        let mut writer = Writer {
            bytes: output,
            offset: 0,
        };
        writer.append(MARKET_SANDBOX_PROFILE_DOMAIN)?;
        writer.append(&1u16.to_be_bytes())?;
        writer.append(&self.network_id.to_be_bytes())?;
        for value in [
            self.market_program,
            self.sandbox_program,
            self.offer_id,
            self.lease_id,
            self.claim_id,
            self.provider,
            self.tenant,
            self.code_hash,
            self.input_digest,
            self.attested_input_commitment,
            self.namespace,
            self.baseline_state_root,
            self.initial_execution_state_root,
        ] {
            writer.append(&value)?;
        }
        writer.append(&self.entrypoint_bytes)?;
        writer.append(&[self.entrypoint_length])?;
        writer.append(&self.runtime_version.to_be_bytes())?;
        writer.append(&self.abi_version.to_be_bytes())?;
        writer.append(&self.fee_schedule_version.to_be_bytes())?;
        writer.append(&self.metering_schedule_version.to_be_bytes())?;
        for value in self.limits.values() {
            writer.append(&value.to_be_bytes())?;
        }
        writer.append(&self.fee_budget.to_be_bytes())?;
        for value in [
            self.interval_start,
            self.interval_end,
            self.response_deadline,
        ] {
            writer.append(&value.to_be_bytes())?;
        }
        writer.append(&self.maximum_boundaries.to_be_bytes())?;
        writer.append(&self.maximum_bytes.to_be_bytes())?;
        writer.append(&MARKET_SANDBOX_CAPABILITIES)?;
        Ok(writer.offset)
    }

    pub fn decode(input: &[u8]) -> Result<Self, ProgramError> {
        let mut reader = Reader(input);
        if reader.take(MARKET_SANDBOX_PROFILE_DOMAIN.len())? != MARKET_SANDBOX_PROFILE_DOMAIN
            || reader.u16()? != 1
        {
            return Err(malformed());
        }
        let profile = Self {
            network_id: reader.u32()?,
            market_program: reader.array()?,
            sandbox_program: reader.array()?,
            offer_id: reader.array()?,
            lease_id: reader.array()?,
            claim_id: reader.array()?,
            provider: reader.array()?,
            tenant: reader.array()?,
            code_hash: reader.array()?,
            input_digest: reader.array()?,
            attested_input_commitment: reader.array()?,
            namespace: reader.array()?,
            baseline_state_root: reader.array()?,
            initial_execution_state_root: reader.array()?,
            entrypoint_bytes: reader.array()?,
            entrypoint_length: reader.array::<1>()?[0],
            runtime_version: reader.u16()?,
            abi_version: reader.u16()?,
            fee_schedule_version: reader.u32()?,
            metering_schedule_version: reader.u32()?,
            limits: ProfileLimits {
                cpu_fuel: reader.u64()?,
                memory_bytes: reader.u64()?,
                storage_read_bytes: reader.u64()?,
                storage_write_bytes: reader.u64()?,
                output_values: reader.u64()?,
                output_bytes: reader.u64()?,
                table_elements: reader.u64()?,
                namespace_bytes: reader.u64()?,
            },
            fee_budget: reader.u128()?,
            interval_start: reader.u64()?,
            interval_end: reader.u64()?,
            response_deadline: reader.u64()?,
            maximum_boundaries: reader.u32()?,
            maximum_bytes: reader.u32()?,
        };
        if reader.take(4)? != MARKET_SANDBOX_CAPABILITIES || !reader.0.is_empty() {
            return Err(malformed());
        }
        profile.validate()?;
        Ok(profile)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct MarketBillingCommitment {
    pub profile_digest: [u8; 32],
    pub provider_trace_root: [u8; 32],
    pub final_execution_state_root: [u8; 32],
    pub output_digest: [u8; 32],
    pub usage: [u64; 6],
    pub payable: u128,
    pub challenger_stake: u128,
    pub challenge_window_batches: u64,
    pub boundary_count: u32,
}

impl MarketBillingCommitment {
    pub fn validate(&self) -> Result<(), ProgramError> {
        if [
            self.profile_digest,
            self.provider_trace_root,
            self.final_execution_state_root,
            self.output_digest,
        ]
        .iter()
        .any(|value| *value == [0; 32])
            || self.usage[0] == 0
            || self.usage.iter().any(|value| *value > 1_000_000_000_000)
            || self.payable == 0
            || self.challenger_stake == 0
            || self.challenge_window_batches == 0
            || self.challenge_window_batches > 100_000
            || !(2..=4096).contains(&self.boundary_count)
        {
            return Err(malformed());
        }
        Ok(())
    }

    pub fn encode(&self, output: &mut [u8]) -> Result<usize, ProgramError> {
        self.validate()?;
        let mut writer = Writer {
            bytes: output,
            offset: 0,
        };
        writer.append(MARKET_SANDBOX_BILLING_DOMAIN)?;
        writer.append(&1u16.to_be_bytes())?;
        for value in [
            self.profile_digest,
            self.provider_trace_root,
            self.final_execution_state_root,
            self.output_digest,
        ] {
            writer.append(&value)?;
        }
        for value in self.usage {
            writer.append(&value.to_be_bytes())?;
        }
        writer.append(&self.payable.to_be_bytes())?;
        writer.append(&self.challenger_stake.to_be_bytes())?;
        writer.append(&self.challenge_window_batches.to_be_bytes())?;
        writer.append(&self.boundary_count.to_be_bytes())?;
        Ok(writer.offset)
    }

    pub fn decode(input: &[u8]) -> Result<Self, ProgramError> {
        let mut reader = Reader(input);
        if reader.take(MARKET_SANDBOX_BILLING_DOMAIN.len())? != MARKET_SANDBOX_BILLING_DOMAIN
            || reader.u16()? != 1
        {
            return Err(malformed());
        }
        let billing = Self {
            profile_digest: reader.array()?,
            provider_trace_root: reader.array()?,
            final_execution_state_root: reader.array()?,
            output_digest: reader.array()?,
            usage: [
                reader.u64()?,
                reader.u64()?,
                reader.u64()?,
                reader.u64()?,
                reader.u64()?,
                reader.u64()?,
            ],
            payable: reader.u128()?,
            challenger_stake: reader.u128()?,
            challenge_window_batches: reader.u64()?,
            boundary_count: reader.u32()?,
        };
        if !reader.0.is_empty() {
            return Err(malformed());
        }
        billing.validate()?;
        Ok(billing)
    }
}

/// Opens one boundary of the committed provider trace.
pub const MARKET_STEP_OPEN: u8 = 1;
/// Judges the transition from one committed boundary to the next.
pub const MARKET_STEP_TRANSITION: u8 = 2;
/// Judges the last committed boundary as the end of the execution.
pub const MARKET_STEP_TERMINAL: u8 = 3;
pub const MARKET_STEP_HOLDS: u8 = 1;
pub const MARKET_STEP_FAILS: u8 = 2;
pub const MARKET_STEP_OUTCOME_BYTES: usize = 65;
pub const MARKET_STEP_HEADER_CAPACITY: usize =
    1 + 4 + 2 + MARKET_SANDBOX_PROFILE_CAPACITY + MARKET_SANDBOX_BILLING_CAPACITY;

/// The step a market program asks the host to adjudicate: the mode, the trace position and the
/// market's own authorized profile and billing commitment rows.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct MarketStepRequest<'a> {
    pub mode: u8,
    pub position: u32,
    pub profile: &'a [u8],
    pub billing: &'a [u8],
}

impl<'a> MarketStepRequest<'a> {
    pub fn encode(&self, output: &mut [u8]) -> Result<usize, ProgramError> {
        if !(MARKET_STEP_OPEN..=MARKET_STEP_TERMINAL).contains(&self.mode)
            || self.profile.len() > MARKET_SANDBOX_PROFILE_CAPACITY
            || self.billing.len() > MARKET_SANDBOX_BILLING_CAPACITY
        {
            return Err(malformed());
        }
        let profile_length = u16::try_from(self.profile.len()).map_err(|_| malformed())?;
        let mut writer = Writer {
            bytes: output,
            offset: 0,
        };
        writer.append(&[self.mode])?;
        writer.append(&self.position.to_be_bytes())?;
        writer.append(&profile_length.to_be_bytes())?;
        writer.append(self.profile)?;
        writer.append(self.billing)?;
        Ok(writer.offset)
    }

    pub fn decode(input: &'a [u8]) -> Result<Self, ProgramError> {
        let mut reader = Reader(input);
        let mode = reader.array::<1>()?[0];
        let position = reader.u32()?;
        let profile_length = usize::from(reader.u16()?);
        if !(MARKET_STEP_OPEN..=MARKET_STEP_TERMINAL).contains(&mode)
            || profile_length > MARKET_SANDBOX_PROFILE_CAPACITY
        {
            return Err(malformed());
        }
        let profile = reader.take(profile_length)?;
        let billing = reader.0;
        if billing.len() > MARKET_SANDBOX_BILLING_CAPACITY {
            return Err(malformed());
        }
        Ok(Self {
            mode,
            position,
            profile,
            billing,
        })
    }
}

/// The host-authenticated result of one market-step adjudication.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct MarketStepOutcome {
    pub holds: bool,
    pub leaf_digest: [u8; 32],
    pub commitment: [u8; 32],
}

impl MarketStepOutcome {
    #[must_use]
    pub fn encode(&self) -> [u8; MARKET_STEP_OUTCOME_BYTES] {
        let mut output = [0; MARKET_STEP_OUTCOME_BYTES];
        output[0] = if self.holds {
            MARKET_STEP_HOLDS
        } else {
            MARKET_STEP_FAILS
        };
        output[1..33].copy_from_slice(&self.leaf_digest);
        output[33..].copy_from_slice(&self.commitment);
        output
    }

    pub fn decode(input: &[u8]) -> Result<Self, ProgramError> {
        if input.len() != MARKET_STEP_OUTCOME_BYTES {
            return Err(malformed());
        }
        let mut reader = Reader(&input[1..]);
        let holds = match input[0] {
            MARKET_STEP_HOLDS => true,
            MARKET_STEP_FAILS => false,
            _ => return Err(malformed()),
        };
        let leaf_digest = reader.array()?;
        if leaf_digest == [0; 32] {
            return Err(malformed());
        }
        Ok(Self {
            holds,
            leaf_digest,
            commitment: reader.array()?,
        })
    }
}

/// Asks the host to adjudicate one step of the committed provider trace for the calling market.
///
/// # Errors
/// Returns the host refusal for a malformed request, an unauthorized caller or sandbox module,
/// evidence that does not open against the committed trace, or exhausted fuel.
#[cfg(target_arch = "wasm32")]
pub fn adjudicate_market_step(
    request: &MarketStepRequest<'_>,
    evidence: &[u8],
) -> Result<MarketStepOutcome, ProgramError> {
    let mut header = [0; MARKET_STEP_HEADER_CAPACITY];
    let length = request.encode(&mut header)?;
    let mut output = [0; MARKET_STEP_OUTCOME_BYTES];
    let written = crate::host::market_step_adjudicate(&header[..length], evidence, &mut output)?;
    if usize::try_from(written).map_err(|_| malformed())? != MARKET_STEP_OUTCOME_BYTES {
        return Err(malformed());
    }
    MarketStepOutcome::decode(&output)
}

fn malformed() -> ProgramError {
    ProgramError::value(Field::CallInput, Reason::Malformed)
}
struct Writer<'a> {
    bytes: &'a mut [u8],
    offset: usize,
}
impl Writer<'_> {
    fn append(&mut self, bytes: &[u8]) -> Result<(), ProgramError> {
        let end = self.offset.checked_add(bytes.len()).ok_or_else(malformed)?;
        self.bytes
            .get_mut(self.offset..end)
            .ok_or_else(malformed)?
            .copy_from_slice(bytes);
        self.offset = end;
        Ok(())
    }
}
struct Reader<'a>(&'a [u8]);
impl<'a> Reader<'a> {
    fn take(&mut self, size: usize) -> Result<&'a [u8], ProgramError> {
        let result = self.0.get(..size).ok_or_else(malformed)?;
        self.0 = &self.0[size..];
        Ok(result)
    }
    fn array<const N: usize>(&mut self) -> Result<[u8; N], ProgramError> {
        self.take(N)?.try_into().map_err(|_| malformed())
    }
    fn u16(&mut self) -> Result<u16, ProgramError> {
        Ok(u16::from_be_bytes(self.array()?))
    }
    fn u32(&mut self) -> Result<u32, ProgramError> {
        Ok(u32::from_be_bytes(self.array()?))
    }
    fn u64(&mut self) -> Result<u64, ProgramError> {
        Ok(u64::from_be_bytes(self.array()?))
    }
    fn u128(&mut self) -> Result<u128, ProgramError> {
        Ok(u128::from_be_bytes(self.array()?))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn market_step_request_and_outcome_round_trip_and_refuse_malformed_framing() {
        let profile = [7u8; 40];
        let billing = [9u8; 20];
        let request = MarketStepRequest {
            mode: MARKET_STEP_TRANSITION,
            position: 513,
            profile: &profile,
            billing: &billing,
        };
        let mut header = [0; MARKET_STEP_HEADER_CAPACITY];
        let length = request
            .encode(&mut header)
            .unwrap_or_else(|error| panic!("encoded request: {error}"));
        assert_eq!(length, 7 + profile.len() + billing.len());
        assert_eq!(
            MarketStepRequest::decode(&header[..length])
                .unwrap_or_else(|error| panic!("decoded request: {error}")),
            request
        );
        for mode in [0, MARKET_STEP_TERMINAL + 1] {
            let mut forged = header;
            forged[0] = mode;
            assert!(MarketStepRequest::decode(&forged[..length]).is_err());
            assert!(MarketStepRequest { mode, ..request }
                .encode(&mut [0; MARKET_STEP_HEADER_CAPACITY])
                .is_err());
        }
        assert!(MarketStepRequest::decode(&header[..6]).is_err());
        let mut long = header;
        long[5..7].copy_from_slice(&u16::MAX.to_be_bytes());
        assert!(MarketStepRequest::decode(&long[..length]).is_err());
        let outcome = MarketStepOutcome {
            holds: false,
            leaf_digest: [3; 32],
            commitment: [4; 32],
        };
        let encoded = outcome.encode();
        assert_eq!(encoded[0], MARKET_STEP_FAILS);
        assert_eq!(
            MarketStepOutcome::decode(&encoded).unwrap_or_else(|error| panic!("outcome: {error}")),
            outcome
        );
        let mut forged = encoded;
        forged[0] = 0;
        assert!(MarketStepOutcome::decode(&forged).is_err());
        let mut empty = encoded;
        empty[1..33].fill(0);
        assert!(MarketStepOutcome::decode(&empty).is_err());
        assert!(MarketStepOutcome::decode(&encoded[..64]).is_err());
    }
}
