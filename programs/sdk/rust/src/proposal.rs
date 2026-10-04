use crate::payments::{PaymentGrant, PreparedProgramAccount, ProgramPaymentCapabilities};
use crate::{
    AccountId, Amount, AssetId, Field, HostRefusal, ProgramAccountPayment, ProgramError, ProgramId,
    Reason,
};

pub const MAX_PROPOSALS: usize = 16;
pub const MAX_PROPOSAL_BYTES: usize = 4394;
const MAGIC: &[u8; 8] = b"LXPPRP01";
const HEADER_BYTES: usize = 10;
const FIXED_PROPOSAL_BYTES: usize = 146;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PaymentProposal<'a> {
    account: PreparedProgramAccount<'a>,
    to: AccountId,
    amount: Amount,
}

impl<'a> PaymentProposal<'a> {
    pub fn new(
        account: PreparedProgramAccount<'a>,
        to: AccountId,
        amount: Amount,
    ) -> Result<Self, ProgramError> {
        if account.seed().bytes().is_empty() {
            return Err(ProgramError::value(Field::Account, Reason::Empty));
        }
        account.payment(to, amount)?;
        Ok(Self {
            account,
            to,
            amount,
        })
    }

    #[must_use]
    pub const fn account(self) -> PreparedProgramAccount<'a> {
        self.account
    }

    #[must_use]
    pub const fn to(self) -> AccountId {
        self.to
    }

    #[must_use]
    pub const fn amount(self) -> Amount {
        self.amount
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ProposalSet<'a> {
    proposals: [Option<PaymentProposal<'a>>; MAX_PROPOSALS],
    length: usize,
}

impl<'a> ProposalSet<'a> {
    pub fn from_proposals(proposals: &[PaymentProposal<'a>]) -> Result<Self, ProgramError> {
        check_count(proposals.len())?;
        let mut result = Self {
            proposals: [None; MAX_PROPOSALS],
            length: proposals.len(),
        };
        for (index, proposal) in proposals.iter().copied().enumerate() {
            result.proposals[index] = Some(proposal);
        }
        Ok(result)
    }

    pub fn decode(bytes: &'a [u8]) -> Result<Self, ProgramError> {
        if bytes.len() > MAX_PROPOSAL_BYTES {
            return Err(ProgramError::value(Field::Buffer, Reason::TooLarge));
        }
        let mut input = Cursor { bytes, offset: 0 };
        if input.take(8)? != MAGIC {
            return Err(malformed());
        }
        let count = usize::from(u16::from_be_bytes(input.array()?));
        check_count(count)?;
        let mut result = Self {
            proposals: [None; MAX_PROPOSALS],
            length: count,
        };
        for index in 0..count {
            let owner = ProgramId::new(input.array()?)?;
            let seed_length = usize::from(u16::from_be_bytes(input.array()?));
            if seed_length == 0 || seed_length > crate::MAX_PROGRAM_ACCOUNT_SEED_BYTES {
                return Err(malformed());
            }
            let seed = input.take(seed_length)?;
            let source = AccountId::new(input.array()?)?;
            let asset = AssetId::new(input.array()?)?;
            let to = AccountId::new(input.array()?)?;
            let amount = Amount::from_be_bytes(input.array()?);
            let account = PreparedProgramAccount::new(owner, seed, asset)?;
            if account.account() != source {
                return Err(ProgramError::Host(HostRefusal::Evidence));
            }
            result.proposals[index] = Some(PaymentProposal::new(account, to, amount)?);
        }
        if input.offset != bytes.len() {
            return Err(malformed());
        }
        Ok(result)
    }

    #[must_use]
    pub const fn len(&self) -> usize {
        self.length
    }

    #[must_use]
    pub const fn is_empty(&self) -> bool {
        self.length == 0
    }

    pub fn proposals(&self) -> impl Iterator<Item = PaymentProposal<'a>> + '_ {
        self.proposals[..self.length].iter().copied().flatten()
    }

    pub fn encode_into(&self, output: &mut [u8]) -> Result<usize, ProgramError> {
        check_count(self.length)?;
        let length = self
            .proposals()
            .try_fold(HEADER_BYTES, |length, proposal| {
                length
                    .checked_add(FIXED_PROPOSAL_BYTES + proposal.account.seed().bytes().len())
                    .ok_or_else(|| ProgramError::value(Field::Buffer, Reason::TooLarge))
            })?;
        if length > MAX_PROPOSAL_BYTES {
            return Err(ProgramError::value(Field::Buffer, Reason::TooLarge));
        }
        if output.len() < length {
            return Err(ProgramError::value(Field::Buffer, Reason::TooSmall));
        }
        let count = u16::try_from(self.length).map_err(|_| malformed())?;
        output[..8].copy_from_slice(MAGIC);
        output[8..HEADER_BYTES].copy_from_slice(&count.to_be_bytes());
        let mut offset = HEADER_BYTES;
        for proposal in self.proposals() {
            let account = proposal.account;
            let seed = account.seed().bytes();
            let seed_length = u16::try_from(seed.len()).map_err(|_| malformed())?;
            put(output, &mut offset, &account.program().bytes());
            put(output, &mut offset, &seed_length.to_be_bytes());
            put(output, &mut offset, seed);
            put(output, &mut offset, &account.account().bytes());
            put(output, &mut offset, &account.asset().bytes());
            put(output, &mut offset, &proposal.to.bytes());
            put(output, &mut offset, &proposal.amount.to_be_bytes());
        }
        Ok(offset)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ValidatedProposals<'a> {
    payments: [Option<ProgramAccountPayment<'a>>; MAX_PROPOSALS],
    length: usize,
}

impl<'a> ValidatedProposals<'a> {
    pub fn payments(&self) -> impl Iterator<Item = ProgramAccountPayment<'a>> + '_ {
        self.payments[..self.length].iter().copied().flatten()
    }
}

#[derive(Debug, Eq, PartialEq)]
pub struct ProposalBudget<'a, const N: usize> {
    owner: ProgramId,
    original: [Option<PaymentGrant<'a>>; N],
    length: usize,
    spent: [Amount; N],
}

impl<'a, const N: usize> ProposalBudget<'a, N> {
    pub fn new(
        owner: ProgramId,
        original: &ProgramPaymentCapabilities<'a, N>,
    ) -> Result<Self, ProgramError> {
        let mut result = Self {
            owner,
            original: [None; N],
            length: original.len(),
            spent: [Amount::ZERO; N],
        };
        for (index, grant) in original.grants().iter().copied().enumerate() {
            result.original[index] = Some(grant);
        }
        Ok(result)
    }

    pub fn validate<'p, const EDGE: usize>(
        &mut self,
        owner: ProgramId,
        edge: &ProgramPaymentCapabilities<'_, EDGE>,
        set: &ProposalSet<'p>,
    ) -> Result<ValidatedProposals<'p>, ProgramError> {
        if owner != self.owner {
            return Err(denied());
        }
        check_count(set.length)?;
        let mut spent = self.spent;
        let mut edge_spent = [Amount::ZERO; EDGE];
        let mut payments = ValidatedProposals {
            payments: [None; MAX_PROPOSALS],
            length: set.length,
        };
        for (payment_index, proposal) in set.proposals().enumerate() {
            if proposal.account.program() != owner {
                return Err(denied());
            }
            let (original_index, original_maximum) = self.original[..self.length]
                .iter()
                .enumerate()
                .find_map(|(index, grant)| {
                    grant.and_then(|grant| {
                        matching_grant(grant, proposal).map(|maximum| (index, maximum))
                    })
                })
                .ok_or_else(denied)?;
            let (edge_index, edge_maximum) = edge
                .grants()
                .iter()
                .copied()
                .enumerate()
                .find_map(|(index, grant)| {
                    matching_grant(grant, proposal).map(|maximum| (index, maximum))
                })
                .ok_or_else(denied)?;
            if edge_maximum > original_maximum {
                return Err(denied());
            }
            spent[original_index] = spent[original_index].checked_add(proposal.amount)?;
            edge_spent[edge_index] = edge_spent[edge_index].checked_add(proposal.amount)?;
            if spent[original_index] > original_maximum || edge_spent[edge_index] > edge_maximum {
                return Err(denied());
            }
            payments.payments[payment_index] =
                Some(proposal.account.payment(proposal.to, proposal.amount)?);
        }
        self.spent = spent;
        Ok(payments)
    }
}

fn matching_grant(grant: PaymentGrant<'_>, proposal: PaymentProposal<'_>) -> Option<Amount> {
    match grant {
        PaymentGrant::ProgramSpend {
            account,
            to,
            maximum,
        } if account.program() == proposal.account.program()
            && account.seed().bytes() == proposal.account.seed().bytes()
            && account.account() == proposal.account.account()
            && account.asset() == proposal.account.asset()
            && to == proposal.to =>
        {
            Some(maximum)
        }
        _ => None,
    }
}

fn check_count(count: usize) -> Result<(), ProgramError> {
    if count == 0 {
        Err(malformed())
    } else if count > MAX_PROPOSALS {
        Err(ProgramError::value(Field::Buffer, Reason::TooLarge))
    } else {
        Ok(())
    }
}

fn malformed() -> ProgramError {
    ProgramError::value(Field::Buffer, Reason::Malformed)
}

fn denied() -> ProgramError {
    ProgramError::Host(HostRefusal::Denied)
}

fn put(output: &mut [u8], offset: &mut usize, bytes: &[u8]) {
    let end = *offset + bytes.len();
    output[*offset..end].copy_from_slice(bytes);
    *offset = end;
}

struct Cursor<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Cursor<'a> {
    fn take(&mut self, length: usize) -> Result<&'a [u8], ProgramError> {
        let end = self.offset.checked_add(length).ok_or_else(malformed)?;
        let bytes = self.bytes.get(self.offset..end).ok_or_else(malformed)?;
        self.offset = end;
        Ok(bytes)
    }

    fn array<const N: usize>(&mut self) -> Result<[u8; N], ProgramError> {
        self.take(N)?.try_into().map_err(|_| malformed())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn must<T>(value: Result<T, ProgramError>) -> T {
        value.unwrap_or_else(|error| panic!("real proposal value: {error}"))
    }

    fn owner() -> ProgramId {
        must(ProgramId::new([1; 32]))
    }
    fn destination() -> AccountId {
        must(AccountId::new([3; 32]))
    }
    fn account(seed: &[u8]) -> PreparedProgramAccount<'_> {
        must(PreparedProgramAccount::new(
            owner(),
            seed,
            must(AssetId::new([2; 32])),
        ))
    }
    fn grant<'a>(
        account: PreparedProgramAccount<'a>,
        maximum: Amount,
    ) -> ProgramPaymentCapabilities<'a, 1> {
        let mut capabilities = ProgramPaymentCapabilities::empty();
        must(capabilities.insert(PaymentGrant::ProgramSpend {
            account,
            to: destination(),
            maximum,
        }));
        capabilities
    }
    fn proposal<'a>(account: PreparedProgramAccount<'a>, amount: Amount) -> PaymentProposal<'a> {
        must(PaymentProposal::new(account, destination(), amount))
    }

    #[test]
    fn owner_proposal_maximum_canonical_roundtrip() {
        let seed = [7; 128];
        let value = proposal(account(&seed), Amount::MAX);
        let set = must(ProposalSet::from_proposals(&[value; MAX_PROPOSALS]));
        let mut output = [0; MAX_PROPOSAL_BYTES];
        assert_eq!(must(set.encode_into(&mut output)), MAX_PROPOSAL_BYTES);
        assert_eq!(&output[..10], b"LXPPRP01\x00\x10");
        let decoded = must(ProposalSet::decode(&output));
        assert_eq!(decoded, set);
        assert_eq!(decoded.proposals().count(), 16);
    }

    #[test]
    fn owner_proposal_decoder_refuses_noncanonical_carriers() {
        let set = must(ProposalSet::from_proposals(&[proposal(
            account(b"a"),
            Amount::from_u128(4),
        )]));
        let mut output = [0; MAX_PROPOSAL_BYTES];
        let length = must(set.encode_into(&mut output));
        for truncated in 0..length {
            assert!(ProposalSet::decode(&output[..truncated]).is_err());
        }
        let encoded = &output[..length];
        for offset in [0, 10, 45] {
            let mut changed = encoded.to_vec();
            changed[offset] ^= 1;
            assert!(
                ProposalSet::decode(&changed).is_err(),
                "changed offset {offset}"
            );
        }
        for count in [0_u16, 17] {
            let mut changed = encoded.to_vec();
            changed[8..10].copy_from_slice(&count.to_be_bytes());
            assert!(ProposalSet::decode(&changed).is_err());
        }
        for seed_length in [0_u16, 129] {
            let mut changed = encoded.to_vec();
            changed[42..44].copy_from_slice(&seed_length.to_be_bytes());
            assert!(ProposalSet::decode(&changed).is_err());
        }
        for range in [10..42, 45..77, 77..109, 109..141, 141..157] {
            let mut changed = encoded.to_vec();
            changed[range].fill(0);
            assert!(ProposalSet::decode(&changed).is_err());
        }
        let mut trailing = encoded.to_vec();
        trailing.push(0);
        assert!(ProposalSet::decode(&trailing).is_err());
        assert!(ProposalSet::decode(&[0; MAX_PROPOSAL_BYTES + 1]).is_err());
    }

    #[test]
    fn owner_proposal_encoding_refuses_short_buffers_without_writing() {
        let value = proposal(account(b"a"), Amount::from_u128(1));
        let set = must(ProposalSet::from_proposals(&[value]));
        let mut output = [0xa5; 156];
        assert!(set.encode_into(&mut output).is_err());
        assert_eq!(output, [0xa5; 156]);
        assert!(ProposalSet::from_proposals(&[]).is_err());
        assert!(ProposalSet::from_proposals(&[value; MAX_PROPOSALS + 1]).is_err());
        assert!(PaymentProposal::new(account(b""), destination(), Amount::from_u128(1)).is_err());
        assert!(PaymentProposal::new(account(b"a"), destination(), Amount::ZERO).is_err());
    }

    #[test]
    fn owner_proposal_budget_keeps_original_ceiling_across_call_edges() {
        let source = account(b"a");
        let original = grant(source, Amount::from_u128(20));
        let first_edge = grant(source, Amount::from_u128(10));
        let second_edge = grant(source, Amount::from_u128(14));
        let mut budget = must(ProposalBudget::new(owner(), &original));
        let first = must(ProposalSet::from_proposals(&[proposal(
            source,
            Amount::from_u128(7),
        )]));
        assert_eq!(
            must(budget.validate(owner(), &first_edge, &first))
                .payments()
                .count(),
            1
        );
        let rejected = must(ProposalSet::from_proposals(&[proposal(
            source,
            Amount::from_u128(14),
        )]));
        assert!(budget.validate(owner(), &second_edge, &rejected).is_err());
        let accepted = must(ProposalSet::from_proposals(&[proposal(
            source,
            Amount::from_u128(13),
        )]));
        assert_eq!(
            must(budget.validate(owner(), &second_edge, &accepted))
                .payments()
                .next()
                .map(ProgramAccountPayment::amount),
            Some(Amount::from_u128(13))
        );
        assert!(budget.validate(owner(), &first_edge, &first).is_err());
    }

    #[test]
    fn owner_proposal_budget_requires_exact_tag9_authority() {
        let source = account(b"a");
        let original = grant(source, Amount::from_u128(20));
        let edge = grant(source, Amount::from_u128(10));
        let mut budget = must(ProposalBudget::new(owner(), &original));
        let other_owner = must(ProgramId::new([4; 32]));
        let other_asset = must(AssetId::new([5; 32]));
        for proposed in [
            proposal(account(b"b"), Amount::from_u128(1)),
            proposal(
                must(PreparedProgramAccount::new(
                    other_owner,
                    b"a",
                    source.asset(),
                )),
                Amount::from_u128(1),
            ),
            proposal(
                must(PreparedProgramAccount::new(owner(), b"a", other_asset)),
                Amount::from_u128(1),
            ),
            must(PaymentProposal::new(
                source,
                must(AccountId::new([6; 32])),
                Amount::from_u128(1),
            )),
        ] {
            let set = must(ProposalSet::from_proposals(&[proposed]));
            assert!(budget.validate(owner(), &edge, &set).is_err());
        }
        let set = must(ProposalSet::from_proposals(&[proposal(
            source,
            Amount::from_u128(1),
        )]));
        assert!(budget.validate(other_owner, &edge, &set).is_err());
        let mut basic = ProgramPaymentCapabilities::<1>::empty();
        must(
            basic.insert(PaymentGrant::Basic(must(crate::Capability::transfer(
                source.asset(),
                destination(),
                Amount::from_u128(10),
            )))),
        );
        assert!(budget.validate(owner(), &basic, &set).is_err());
        let escalated = grant(source, Amount::from_u128(21));
        assert!(budget.validate(owner(), &escalated, &set).is_err());
        let mut encoded = [0; MAX_PROPOSAL_BYTES];
        let length = must(set.encode_into(&mut encoded));
        for offset in [77, 109, 141] {
            let mut changed = encoded[..length].to_vec();
            changed[offset] ^= 1;
            let decoded = must(ProposalSet::decode(&changed));
            assert!(budget.validate(owner(), &edge, &decoded).is_err());
        }
        assert!(budget.validate(owner(), &edge, &set).is_ok());
    }

    #[test]
    fn owner_proposal_budget_cumulative_and_overflow_refusals_are_atomic() {
        let source = account(b"a");
        let original = grant(source, Amount::from_u128(20));
        let edge = grant(source, Amount::from_u128(15));
        let mut budget = must(ProposalBudget::new(owner(), &original));
        let repeated = must(ProposalSet::from_proposals(
            &[proposal(source, Amount::from_u128(8)); 2],
        ));
        assert!(budget.validate(owner(), &edge, &repeated).is_err());
        let accepted = must(ProposalSet::from_proposals(&[proposal(
            source,
            Amount::from_u128(15),
        )]));
        assert!(budget.validate(owner(), &edge, &accepted).is_ok());
        let maximum = grant(source, Amount::MAX);
        let mut budget = must(ProposalBudget::new(owner(), &maximum));
        let all = must(ProposalSet::from_proposals(&[proposal(
            source,
            Amount::MAX,
        )]));
        assert!(budget.validate(owner(), &maximum, &all).is_ok());
        let extra = must(ProposalSet::from_proposals(&[proposal(
            source,
            Amount::from_u128(1),
        )]));
        assert_eq!(
            budget.validate(owner(), &maximum, &extra),
            Err(ProgramError::value(Field::Amount, Reason::Overflow))
        );
        assert_eq!(budget.spent[0], Amount::MAX);
    }
}
