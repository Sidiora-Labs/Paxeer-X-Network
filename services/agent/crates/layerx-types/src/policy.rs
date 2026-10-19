//! Build-time policy documents embedded for auditable enforcement.

/// Returns the complete unsafe-code exception registry read by CI.
#[must_use]
pub fn agent_unsafe_allowlist() -> &'static str {
    include_str!("../../../unsafe-allowlist.toml")
}

/// Returns the complete dependency policy read by CI.
#[must_use]
pub fn agent_dependency_policy() -> &'static str {
    include_str!("../../../deny.toml")
}
