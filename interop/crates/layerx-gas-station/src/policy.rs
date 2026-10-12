use crate::config::{ConfigError, StationConfig};
use crate::quote::Address;
use std::collections::BTreeMap;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PolicyRefusal {
    PerQuote,
    PerAccount,
    PerInterval,
    BalanceFloor,
    InvalidAmount,
    ClockRegression,
}
impl std::fmt::Display for PolicyRefusal {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "quote policy refused: {self:?}")
    }
}
impl std::error::Error for PolicyRefusal {}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DidBudget {
    pub gas_limit: u64,
    interval: Option<u64>,
    used: BTreeMap<String, u64>,
}
impl DidBudget {
    #[must_use]
    pub fn new(gas_limit: u64) -> Self {
        Self {
            gas_limit,
            interval: None,
            used: BTreeMap::new(),
        }
    }

    #[must_use]
    pub fn used(&self, did: &str, interval: u64) -> u64 {
        if self.interval == Some(interval) {
            self.used.get(did).copied().unwrap_or(0)
        } else {
            0
        }
    }

    fn reserve(&mut self, did: &str, gas: u64, interval: u64) -> Result<(), PolicyRefusal> {
        if self.gas_limit == 0 {
            return Err(PolicyRefusal::InvalidAmount);
        }
        if !valid_did(did) || gas == 0 {
            return Err(PolicyRefusal::InvalidAmount);
        }
        let used = self
            .used(did, interval)
            .checked_add(gas)
            .filter(|used| *used <= self.gas_limit)
            .ok_or(PolicyRefusal::PerAccount)?;
        if self.interval != Some(interval) {
            self.used.clear();
        }
        self.interval = Some(interval);
        self.used.insert(did.to_owned(), used);
        Ok(())
    }
}

pub(crate) fn valid_did(did: &str) -> bool {
    let (hexadecimal, length) = if let Some(key) = did.strip_prefix("did:layerx:") {
        (key, 64)
    } else if let Some(address) = did.strip_prefix("0x") {
        (address, 40)
    } else {
        return false;
    };
    hexadecimal.len() == length
        && hexadecimal
            .bytes()
            .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
        && hexadecimal.bytes().any(|byte| byte != b'0')
}

#[derive(Clone)]
pub struct QuotePolicy {
    config: StationConfig,
    interval: Option<u64>,
    last_time: u64,
    accounts: BTreeMap<String, u128>,
    total: u128,
    reserved_pax: u128,
    finalized_spend: u128,
    did_budget: DidBudget,
    replay_key: Option<String>,
}
impl QuotePolicy {
    /// # Errors
    /// Refuses inconsistent policy configuration.
    pub fn new(config: &StationConfig) -> Result<Self, ConfigError> {
        config.validate()?;
        Ok(Self {
            config: config.clone(),
            interval: None,
            last_time: 0,
            accounts: BTreeMap::new(),
            total: 0,
            reserved_pax: 0,
            finalized_spend: 0,
            did_budget: DidBudget::new(0),
            replay_key: None,
        })
    }

    #[must_use]
    pub fn with_did_budget(mut self, budget: DidBudget) -> Self {
        self.did_budget = budget;
        self
    }

    #[must_use]
    pub const fn did_budget(&self) -> &DidBudget {
        &self.did_budget
    }

    /// The token amount reserved under the budget key `key` (a DID, or the
    /// EVM address of an account with no bound DID) in the interval of `now`.
    #[must_use]
    pub fn used(&self, key: &str, now: u64) -> u128 {
        if self.interval == Some(now / self.config.interval_seconds) {
            self.accounts.get(key).copied().unwrap_or(0)
        } else {
            0
        }
    }

    pub(crate) fn restore_did_usage(
        &mut self,
        did: &str,
        gas: u64,
        now: u64,
    ) -> Result<(), PolicyRefusal> {
        self.did_budget
            .reserve(did, gas, now / self.config.interval_seconds)?;
        self.replay_key = Some(did.to_owned());
        Ok(())
    }

    pub(crate) fn restore_usage(
        &mut self,
        account: Address,
        amount: u128,
        gas: u128,
        now: u64,
    ) -> Result<(), PolicyRefusal> {
        if account == [0; 20] {
            return Err(PolicyRefusal::InvalidAmount);
        }
        let key = self
            .replay_key
            .take()
            .unwrap_or_else(|| crate::rpc::hex(&account));
        let active = self.reserved_pax;
        self.reserved_pax = 0;
        let result = self.reserve_key(&key, amount, gas, u128::MAX, now);
        self.reserved_pax = active;
        result
    }

    pub(crate) fn reconcile(&mut self, active: u128, finalized_spend: u128) {
        self.reserved_pax = active;
        self.finalized_spend = finalized_spend;
    }

    pub(crate) fn ensure_balance(
        &self,
        balance: u128,
        additional: u128,
    ) -> Result<(), PolicyRefusal> {
        let promised = self
            .reserved_pax
            .checked_add(additional)
            .ok_or(PolicyRefusal::BalanceFloor)?;
        if balance
            .checked_sub(promised)
            .is_none_or(|remaining| remaining < self.config.balance_floor)
        {
            return Err(PolicyRefusal::BalanceFloor);
        }
        Ok(())
    }

    #[must_use]
    pub const fn outstanding_pax(&self) -> u128 {
        self.reserved_pax
    }

    #[must_use]
    pub const fn finalized_spend(&self) -> u128 {
        self.finalized_spend
    }

    /// # Errors
    /// Refuses fees whose gas cost overflows, and names the budget or balance floor
    /// that would be exceeded. Failed checks consume no budget.
    pub fn reserve_for_did(
        &mut self,
        did: &str,
        account: Address,
        amount: u128,
        fees: crate::tx::Fees,
        balance: u128,
        now: u64,
    ) -> Result<(), PolicyRefusal> {
        let gas_cost = fees.gas_cost().map_err(|_| PolicyRefusal::InvalidAmount)?;
        if account == [0; 20] {
            return Err(PolicyRefusal::InvalidAmount);
        }
        let mut next = self.clone();
        next.reserve_key(did, amount, gas_cost, balance, now)?;
        next.did_budget
            .reserve(did, fees.gas_limit, now / self.config.interval_seconds)?;
        *self = next;
        Ok(())
    }

    /// # Errors
    /// Refuses an invalid request, an unpriceable or above-maximum amount, a policy
    /// refusal or a signer failure. Failed quotes consume no budget.
    pub fn quote_for_did(
        &mut self,
        request: &crate::QuoteRequest,
        rate: &crate::price::GovernedRate,
        fees: crate::tx::Fees,
        signer: &impl crate::signer::QuoteSigner,
        did: &str,
        context: (u128, u64),
    ) -> Result<crate::SignedQuote, crate::QuoteError> {
        let (balance, now) = context;
        if request.account == [0; 20]
            || request.account == signer.address()
            || request.deadline < now
            || request.deadline / self.config.interval_seconds != now / self.config.interval_seconds
            || fees
                .gas_cost()
                .map_err(|_| crate::QuoteError::InvalidRequest)?
                != request.gas_cost
        {
            return Err(crate::QuoteError::InvalidRequest);
        }
        let pricing = crate::price::Pricing::new(&self.config)
            .map_err(|_| crate::QuoteError::InvalidRequest)?;
        let amount = pricing
            .quote(rate, request.gas_cost, now)
            .map_err(crate::QuoteError::Price)?;
        if amount > request.max_token_amount {
            return Err(crate::QuoteError::AboveMaximum);
        }
        let mut next = self.clone();
        next.reserve_for_did(did, request.account, amount, fees, balance, now)
            .map_err(crate::QuoteError::Policy)?;
        let quote = crate::quote::Quote {
            sponsor: signer.address(),
            token: self.config.token,
            max_token_amount: crate::quote::word(request.max_token_amount),
            token_amount: crate::quote::word(amount),
            deadline: crate::quote::word(u128::from(request.deadline)),
            nonce: request.quote_nonce,
            gas_cost: crate::quote::word(request.gas_cost),
        };
        let digest = crate::quote::quote_digest(
            crate::quote::word(u128::from(self.config.chain_id)),
            request.account,
            &quote,
        );
        let signature = signer
            .sign_digest(digest)
            .map_err(crate::QuoteError::Signer)?;
        if crate::tx::recover(digest, &signature)
            .map_err(|_| crate::QuoteError::Signer(crate::signer::SignerError::Signing))?
            != signer.address()
        {
            return Err(crate::QuoteError::Signer(
                crate::signer::SignerError::Signing,
            ));
        }
        *self = next;
        Ok(crate::SignedQuote {
            quote,
            digest,
            signature,
        })
    }

    /// Reserves for an account with no bound DID, whose budget key is its EVM address.
    /// # Errors
    /// Names the budget or balance floor that would be exceeded. Failed checks consume no budget.
    pub fn reserve(
        &mut self,
        account: Address,
        amount: u128,
        gas_cost: u128,
        balance: u128,
        now: u64,
    ) -> Result<(), PolicyRefusal> {
        if account == [0; 20] {
            return Err(PolicyRefusal::InvalidAmount);
        }
        self.reserve_key(&crate::rpc::hex(&account), amount, gas_cost, balance, now)
    }

    fn reserve_key(
        &mut self,
        key: &str,
        amount: u128,
        gas_cost: u128,
        balance: u128,
        now: u64,
    ) -> Result<(), PolicyRefusal> {
        if amount == 0 || gas_cost == 0 || !valid_did(key) {
            return Err(PolicyRefusal::InvalidAmount);
        }
        if now < self.last_time {
            return Err(PolicyRefusal::ClockRegression);
        }
        let interval = now / self.config.interval_seconds;
        let same_interval = self.interval == Some(interval);
        let prior_account = if same_interval {
            self.accounts.get(key).copied().unwrap_or(0)
        } else {
            0
        };
        let prior_total = if same_interval { self.total } else { 0 };
        if amount > self.config.per_quote_limit {
            return Err(PolicyRefusal::PerQuote);
        }
        let account_total = prior_account
            .checked_add(amount)
            .filter(|value| *value <= self.config.per_account_limit)
            .ok_or(PolicyRefusal::PerAccount)?;
        let total = prior_total
            .checked_add(amount)
            .filter(|value| *value <= self.config.per_interval_limit)
            .ok_or(PolicyRefusal::PerInterval)?;
        let reserved = self
            .reserved_pax
            .checked_add(gas_cost)
            .ok_or(PolicyRefusal::BalanceFloor)?;
        if balance
            .checked_sub(reserved)
            .is_none_or(|remaining| remaining < self.config.balance_floor)
        {
            return Err(PolicyRefusal::BalanceFloor);
        }
        if !same_interval {
            self.accounts.clear();
        }
        self.interval = Some(interval);
        self.last_time = now;
        self.accounts.insert(key.to_owned(), account_total);
        self.total = total;
        self.reserved_pax = reserved;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::tests::config;
    #[test]
    fn every_limit_and_atomic_refusal() -> Result<(), Box<dyn std::error::Error>> {
        let mut policy = QuotePolicy::new(&config())?;
        assert_eq!(
            policy.reserve([1; 20], 3_000_001, 1, 1000, 1000),
            Err(PolicyRefusal::PerQuote)
        );
        assert_eq!(policy.reserve([1; 20], 2_000_000, 1, 1000, 1000), Ok(()));
        assert_eq!(
            policy.reserve([1; 20], 2_000_001, 1, 1000, 1000),
            Err(PolicyRefusal::PerAccount)
        );
        assert_eq!(policy.reserve([1; 20], 2_000_000, 1, 1000, 1000), Ok(()));
        assert_eq!(policy.reserve([2; 20], 3_000_000, 1, 1000, 1000), Ok(()));
        assert_eq!(
            policy.reserve([3; 20], 1_000_001, 1, 1000, 1000),
            Err(PolicyRefusal::PerInterval)
        );
        assert_eq!(policy.reserve([3; 20], 1_000_000, 1, 1000, 1000), Ok(()));
        assert_eq!(policy.reserve([1; 20], 3_000_000, 1, 1000, 1060), Ok(()));
        assert_eq!(
            policy.reserve([4; 20], 1, 1, 1000, 1059),
            Err(PolicyRefusal::ClockRegression)
        );
        Ok(())
    }
    #[test]
    fn balance_floor_accounts_for_outstanding_promises() -> Result<(), Box<dyn std::error::Error>> {
        let mut policy = QuotePolicy::new(&config())?;
        assert_eq!(
            policy.reserve([1; 20], 1, 1, 99, 1000),
            Err(PolicyRefusal::BalanceFloor)
        );
        assert_eq!(
            policy.reserve([1; 20], 1, 1, 100, 1000),
            Err(PolicyRefusal::BalanceFloor)
        );
        assert_eq!(policy.reserve([1; 20], 1, 1, 101, 1000), Ok(()));
        assert_eq!(
            policy.reserve([1; 20], 1, 1, 101, 1060),
            Err(PolicyRefusal::BalanceFloor)
        );
        assert_eq!(
            policy.reserve([1; 20], 1, u128::MAX, u128::MAX, 1060),
            Err(PolicyRefusal::BalanceFloor)
        );
        assert_eq!(
            policy.reserve([1; 20], 0, 1, 1000, 1060),
            Err(PolicyRefusal::InvalidAmount)
        );
        Ok(())
    }
    #[test]
    fn settled_capacity_preserves_historical_limits() -> Result<(), Box<dyn std::error::Error>> {
        let mut policy = QuotePolicy::new(&config())?;
        policy.reserve([1; 20], 2_000_000, 1, 101, 1000)?;
        assert_eq!(policy.outstanding_pax(), 1);
        policy.reconcile(0, 1);
        assert_eq!(policy.outstanding_pax(), 0);
        assert_eq!(policy.finalized_spend(), 1);
        policy.reserve([1; 20], 2_000_000, 1, 101, 1000)?;
        policy.reconcile(0, 2);
        assert_eq!(
            policy.reserve([1; 20], 1, 1, 101, 1000),
            Err(PolicyRefusal::PerAccount)
        );
        assert_eq!(policy.outstanding_pax(), 0);
        assert_eq!(policy.finalized_spend(), 2);
        policy.reserve([1; 20], 1, 1, 101, 1060)?;
        assert_eq!(policy.outstanding_pax(), 1);
        assert_eq!(
            policy.ensure_balance(100, 0),
            Err(PolicyRefusal::BalanceFloor)
        );
        Ok(())
    }
}
