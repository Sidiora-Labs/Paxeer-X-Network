//! Generated from `agent/schema/agent-api/v1.kvx`; do not hand-edit.

/// Exact source schema used to generate this module.
pub const AGENT_API_V1_SOURCE: &str = include_str!("../../../schema/agent-api/v1.kvx");

/// Contract metadata pinned into every generated consumer.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ContractSchema {
    pub name: &'static str,
    pub version: ContractVersion,
    pub node_interface_major: u16,
}

/// Agent API semantic version.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ContractVersion {
    pub major: u16,
    pub minor: u16,
}

/// Version negotiation request.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct VersionRequest {
    pub request_id: Sequence,
    pub supported: ContractVersion,
}

/// Version negotiation response.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct VersionResponse {
    pub request_id: Sequence,
    pub contract: ContractVersion,
    pub node_interface_major: u16,
}

impl VersionRequest {
    #[must_use]
    pub fn encode(self) -> [u8; 19] {
        let mut bytes = [0; 19];
        bytes[..2].copy_from_slice(&self.supported.major.to_be_bytes());
        bytes[2..4].copy_from_slice(&self.supported.minor.to_be_bytes());
        bytes[4..12].copy_from_slice(&self.request_id.get().to_be_bytes());
        bytes[15..17].copy_from_slice(&self.supported.major.to_be_bytes());
        bytes[17..19].copy_from_slice(&self.supported.minor.to_be_bytes());
        bytes
    }
}

impl VersionResponse {
    #[must_use]
    pub fn encode(self) -> [u8; 23] {
        let mut bytes = [0; 23];
        bytes[..2].copy_from_slice(&self.contract.major.to_be_bytes());
        bytes[2..4].copy_from_slice(&self.contract.minor.to_be_bytes());
        bytes[4..12].copy_from_slice(&self.request_id.get().to_be_bytes());
        bytes[17..19].copy_from_slice(&self.contract.major.to_be_bytes());
        bytes[19..21].copy_from_slice(&self.contract.minor.to_be_bytes());
        bytes[21..23].copy_from_slice(&self.node_interface_major.to_be_bytes());
        bytes
    }
}

impl TryFrom<VersionRequest> for VersionResponse {
    type Error = &'static str;

    fn try_from(request: VersionRequest) -> Result<Self, Self::Error> {
        let contract = agent_api_schema_v1();
        if request.supported.major != contract.version.major {
            return Err("unsupported_major");
        }
        Ok(Self {
            request_id: request.request_id,
            contract: contract.version,
            node_interface_major: contract.node_interface_major,
        })
    }
}

macro_rules! exact_integer {
    ($name:ident, $inner:ty) => {
        #[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
        pub struct $name(pub $inner);

        impl $name {
            #[must_use]
            pub const fn get(self) -> $inner {
                self.0
            }

            /// Parses a canonical decimal integer without a floating-point boundary.
            ///
            /// # Errors
            /// Returns the standard integer parse error for malformed or out-of-range input.
            pub fn parse_decimal(value: &str) -> Result<Self, std::num::ParseIntError> {
                value.parse::<$inner>().map(Self)
            }
        }
    };
}

exact_integer!(Amount, u128);
exact_integer!(Sequence, u64);
exact_integer!(BudgetLimit, u128);
exact_integer!(TimestampSeconds, u64);

/// Returns the immutable contract descriptor.
#[must_use]
pub const fn agent_api_schema_v1() -> ContractSchema {
    ContractSchema {
        name: "LayerX Agent API",
        version: ContractVersion { major: 2, minor: 0 },
        node_interface_major: 1,
    }
}

/// Enforces additive-only compatibility within a contract major version.
///
/// # Errors
/// Returns the first removed or changed declaration within an unchanged major version.
pub fn agent_api_compat_gate(
    previous_major: u16,
    current_major: u16,
    previous: &[(&str, &str)],
    current: &[(&str, &str)],
) -> Result<(), String> {
    if previous_major != current_major {
        return Ok(());
    }
    for (key, old_value) in previous {
        match current.iter().find(|(candidate, _)| candidate == key) {
            Some((_, new_value)) if new_value == old_value => {}
            Some((_, new_value)) => {
                return Err(format!(
                    "breaking contract change at {key}: {old_value} -> {new_value}"
                ));
            }
            None => return Err(format!("breaking contract removal: {key}")),
        }
    }
    Ok(())
}
