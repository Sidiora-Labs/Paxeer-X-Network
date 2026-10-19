use std::collections::BTreeSet;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BoundAuthority {
    tenant: String,
    operations: BTreeSet<String>,
    counterparty: [u8; 32],
    approval_required: bool,
}

impl BoundAuthority {
    /// Binds one tenant's operation set and counterparty to a daemon-held authority.
    ///
    /// # Errors
    ///
    /// Returns `InvalidAuthority` for an empty, oversized or NUL-bearing tenant, an
    /// empty operation set, or an operation name that fails the same text validation.
    pub fn new(
        tenant: impl Into<String>,
        operations: BTreeSet<String>,
        counterparty: [u8; 32],
        approval_required: bool,
    ) -> Result<Self, ValidationError> {
        let tenant = tenant.into();
        if !valid_text(&tenant)
            || operations.is_empty()
            || operations.iter().any(|operation| !valid_text(operation))
            || counterparty == [0; 32]
        {
            return Err(ValidationError::InvalidAuthority);
        }
        Ok(Self {
            tenant,
            operations,
            counterparty,
            approval_required,
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ToolArguments {
    pub operation: String,
    pub counterparty: [u8; 32],
    pub tenant_override: Option<String>,
    pub scope_override: Option<String>,
    pub approval_override: Option<bool>,
    pub model_text: String,
    pub resource_text: String,
    pub tool_result_text: String,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ValidatedArguments {
    pub operation: String,
    pub tenant: String,
    pub counterparty: [u8; 32],
    pub approval_required: bool,
    pub opaque_model_text: String,
    pub opaque_resource_text: String,
    pub opaque_tool_result_text: String,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ValidationError {
    InvalidAuthority,
    Schema,
    ScopeDenied,
    CounterpartyDenied,
    AuthorityOverride,
}

/// Validates untrusted arguments while deriving authority only from daemon-held state.
///
/// # Errors
///
/// Returns `Schema` for a malformed operation name or oversized untrusted text,
/// `ScopeDenied` when the operation is outside the bound authority, `CounterpartyDenied`
/// for a counterparty the authority does not name, and `AuthorityOverride` when the
/// untrusted text attempts to widen the daemon-held authority.
pub fn validate(
    authority: &BoundAuthority,
    arguments: ToolArguments,
) -> Result<ValidatedArguments, ValidationError> {
    if !valid_text(&arguments.operation)
        || [
            &arguments.model_text,
            &arguments.resource_text,
            &arguments.tool_result_text,
        ]
        .iter()
        .any(|text| text.len() > 4_096 || text.as_bytes().contains(&0))
    {
        return Err(ValidationError::Schema);
    }
    if !authority.operations.contains(&arguments.operation) {
        return Err(ValidationError::ScopeDenied);
    }
    if arguments.counterparty != authority.counterparty {
        return Err(ValidationError::CounterpartyDenied);
    }
    if arguments
        .tenant_override
        .as_ref()
        .is_some_and(|tenant| tenant != &authority.tenant)
        || arguments
            .scope_override
            .as_ref()
            .is_some_and(|scope| scope != &arguments.operation)
        || arguments
            .approval_override
            .is_some_and(|approval| approval != authority.approval_required)
    {
        return Err(ValidationError::AuthorityOverride);
    }
    Ok(ValidatedArguments {
        operation: arguments.operation,
        tenant: authority.tenant.clone(),
        counterparty: authority.counterparty,
        approval_required: authority.approval_required,
        opaque_model_text: arguments.model_text,
        opaque_resource_text: arguments.resource_text,
        opaque_tool_result_text: arguments.tool_result_text,
    })
}

fn valid_text(value: &str) -> bool {
    !value.is_empty() && value.len() <= 255 && !value.as_bytes().contains(&0)
}
