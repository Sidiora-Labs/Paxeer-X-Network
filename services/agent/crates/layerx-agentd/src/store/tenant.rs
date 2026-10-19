use super::TenantId;

/// A value that cannot exist without one exact tenant scope.
#[derive(Clone, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct TenantScoped<T> {
    tenant: TenantId,
    value: T,
}

impl<T> TenantScoped<T> {
    #[must_use]
    pub const fn new(tenant: TenantId, value: T) -> Self {
        Self { tenant, value }
    }

    #[must_use]
    pub const fn tenant(&self) -> &TenantId {
        &self.tenant
    }

    #[must_use]
    pub const fn value(&self) -> &T {
        &self.value
    }

    #[must_use]
    pub fn into_parts(self) -> (TenantId, T) {
        (self.tenant, self.value)
    }
}
