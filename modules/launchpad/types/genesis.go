package types

import (
	"fmt"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// AirdropEpochAmount is the amount snapshotted for one airdrop epoch.
type AirdropEpochAmount struct {
	Denom  string  `json:"denom"`
	Epoch  uint64  `json:"epoch"`
	Amount sdk.Int `json:"amount"`
}

// AirdropClaim records that holder claimed denom's airdrop for epoch.
type AirdropClaim struct {
	Denom  string `json:"denom"`
	Holder string `json:"holder"`
	Epoch  uint64 `json:"epoch"`
}

// AirdropEpochBasis is the entitlement basis fixed when an airdrop epoch
// opened: holders share the epoch amount by their balance at the boundary
// over Supply, paid in PayoutDenom; Paid is what has been claimed so far.
type AirdropEpochBasis struct {
	Denom       string  `json:"denom"`
	Epoch       uint64  `json:"epoch"`
	Supply      sdk.Int `json:"supply"`
	PayoutDenom string  `json:"payout_denom"`
	Height      int64   `json:"height"`
	Paid        sdk.Int `json:"paid"`
}

// HoldingHistory is a launched denom's recorded epoch interval.
type HoldingHistory struct {
	Denom        string `json:"denom"`
	FirstEpoch   uint64 `json:"first_epoch"`
	CurrentEpoch uint64 `json:"current_epoch"`
}

// HoldingCheckpoint is one holder's balance before its first write in Epoch.
type HoldingCheckpoint struct {
	Denom   string  `json:"denom"`
	Holder  string  `json:"holder"`
	Index   uint64  `json:"index"`
	Epoch   uint64  `json:"epoch"`
	Balance sdk.Int `json:"balance"`
}

type ProtocolFeeEntry struct {
	Token  string  `json:"token"`
	Amount sdk.Int `json:"amount"`
}

// Contract roles of the Launch-x deployment: the roles served at surfaces and
// the roles named in the retired list.
const (
	RolePool            = "SidioraPool"
	RoleFactory         = "SidioraFactory"
	RoleRouter          = "Router"
	RoleQuoter          = "Quoter"
	RoleFeesRouter      = "FeesRouter"
	RoleFeeAccumulator  = "FeeAccumulator"
	RolePoolRegistry    = "PoolRegistry"
	RoleProtocolConfig  = "ProtocolConfig"
	RoleFeeRights       = "SidioraNFT"
	RoleTreasury        = "Treasury"
	RoleOpticalRegistry = "OpticalRegistry"
)

// surfaceRoles are the roles a surface address serves.
var surfaceRoles = map[string]bool{RolePool: true, RoleFactory: true, RoleRouter: true, RoleQuoter: true,
	RoleFeesRouter: true, RoleFeeAccumulator: true, RolePoolRegistry: true, RoleProtocolConfig: true,
	RoleFeeRights: true, RoleTreasury: true}

// retiredRoles are the generation 1 roles whose addresses are retired.
var retiredRoles = map[string]bool{RoleFactory: true, RoleRouter: true, RoleQuoter: true, RoleFeesRouter: true,
	RoleFeeAccumulator: true, RolePoolRegistry: true, RoleProtocolConfig: true, RoleFeeRights: true,
	RoleTreasury: true, RoleOpticalRegistry: true}

// Surface is an address holding the surface runtime that forwards to the
// precompile, and the role whose ABI it serves.
type Surface struct {
	Address string `json:"address"`
	Role    string `json:"role"`
}

// RoleMember is one non-admin AccessControl membership of the role table of
// the role surface Contract.
type RoleMember struct {
	Contract string `json:"contract"`
	Role     string `json:"role"`
	Account  string `json:"account"`
}

// FeeRight is one token of the generation 2 fee-right collection.
type FeeRight struct {
	ID       uint64      `json:"id"`
	Owner    string      `json:"owner"`
	Approved string      `json:"approved,omitempty"`
	Strategy FeeStrategy `json:"strategy"`
	Pool     string      `json:"pool"`
}

// FeeRightOperator is an operator approval of the fee-right collection.
type FeeRightOperator struct {
	Owner    string `json:"owner"`
	Operator string `json:"operator"`
}

// FeeRightIDMapping maps a generation 1 fee-right id to the id it was minted
// under in the generation 2 collection.
type FeeRightIDMapping struct {
	Generation1ID uint64 `json:"generation_1_id"`
	Generation2ID uint64 `json:"generation_2_id"`
}

// FeeRightCollection is the generation 2 fee-right collection: its counters,
// tokens, operator approvals and the generation 1 id map. Balances, the
// enumeration arrays and poolTokenId are derived from Tokens.
type FeeRightCollection struct {
	NextTokenID              uint64              `json:"next_token_id"`
	PoolIndexMigrationCursor uint64              `json:"pool_index_migration_cursor"`
	PoolIndexReady           bool                `json:"pool_index_ready"`
	Tokens                   []FeeRight          `json:"tokens,omitempty"`
	Operators                []FeeRightOperator  `json:"operators,omitempty"`
	IDMap                    []FeeRightIDMapping `json:"id_map,omitempty"`
}

// PoolFeeAccount is the per-pool accounting of the generation 2
// FeeAccumulator.
type PoolFeeAccount struct {
	Pool                     string  `json:"pool"`
	AccumulatedFees          sdk.Int `json:"accumulated_fees"`
	AirdropBalance           sdk.Int `json:"airdrop_balance"`
	AirdropEpoch             uint64  `json:"airdrop_epoch"`
	LpRewardsBalance         sdk.Int `json:"lp_rewards_balance"`
	OpticalSurplus           sdk.Int `json:"optical_surplus"`
	LegacyAirdropUnallocated sdk.Int `json:"legacy_airdrop_unallocated"`
	AccountingMigrated       bool    `json:"accounting_migrated"`
}

// PoolAirdropEpoch is one airdrop epoch of a pool: the snapshot fields set by
// triggerAirdrop and the legacy Merkle allocation of allocateLegacyAirdrop.
type PoolAirdropEpoch struct {
	Pool            string  `json:"pool"`
	Epoch           uint64  `json:"epoch"`
	Amount          sdk.Int `json:"amount"`
	SnapshotBlock   uint64  `json:"snapshot_block"`
	EligibleSupply  sdk.Int `json:"eligible_supply"`
	Remaining       sdk.Int `json:"remaining"`
	ClaimedSupply   sdk.Int `json:"claimed_supply"`
	LegacyRoot      string  `json:"legacy_root,omitempty"`
	LegacyRemaining sdk.Int `json:"legacy_remaining"`
}

// PoolAirdropClaim records that Holder claimed Pool's airdrop for Epoch.
type PoolAirdropClaim struct {
	Pool   string `json:"pool"`
	Holder string `json:"holder"`
	Epoch  uint64 `json:"epoch"`
}

// FeeAccounting is the merged accounting of the generation 2 FeeAccumulator.
type FeeAccounting struct {
	PendingProtocolFees       sdk.Int            `json:"pending_protocol_fees"`
	TotalLiabilities          sdk.Int            `json:"total_liabilities"`
	AccountingInitialized     bool               `json:"accounting_initialized"`
	AccountingMigrationCursor uint64             `json:"accounting_migration_cursor"`
	Pools                     []PoolFeeAccount   `json:"pools,omitempty"`
	Epochs                    []PoolAirdropEpoch `json:"epochs,omitempty"`
	Claims                    []PoolAirdropClaim `json:"claims,omitempty"`
}

// HeldQuote is the USDL a pool holds in custody at the launchpad module EVM
// address.
type HeldQuote struct {
	Pool   string  `json:"pool"`
	Amount sdk.Int `json:"amount"`
}

// TreasuryBalance is the Treasury ledger of one token and the amount of it
// held in custody for the Treasury.
type TreasuryBalance struct {
	Token  string  `json:"token"`
	Ledger sdk.Int `json:"ledger"`
	Held   sdk.Int `json:"held"`
}

// BookHeldDelta records a migrated pool whose book amount of Asset differed
// from the amount it held; Delta is Held minus Book.
type BookHeldDelta struct {
	Pool  string  `json:"pool"`
	Asset string  `json:"asset"`
	Book  sdk.Int `json:"book"`
	Held  sdk.Int `json:"held"`
	Delta sdk.Int `json:"delta"`
}

// RetiredAddress is a generation 1 role address that reverts Retired with
// the address of the same role to call instead.
type RetiredAddress struct {
	Address     string `json:"address"`
	Role        string `json:"role"`
	Replacement string `json:"replacement"`
}

type GenesisState struct {
	Params             Params               `json:"params"`
	Markets            []Market             `json:"markets"`
	AirdropEpochs      []AirdropEpochAmount `json:"airdrop_epochs"`
	AirdropClaims      []AirdropClaim       `json:"airdrop_claims"`
	ProtocolFees       []ProtocolFeeEntry   `json:"protocol_fees"`
	AirdropBases       []AirdropEpochBasis  `json:"airdrop_bases,omitempty"`
	HoldingHistories   []HoldingHistory     `json:"holding_histories,omitempty"`
	HoldingCheckpoints []HoldingCheckpoint  `json:"holding_checkpoints,omitempty"`
	Surfaces           []Surface            `json:"surfaces,omitempty"`
	RoleMembers        []RoleMember         `json:"role_members,omitempty"`
	FeeRights          FeeRightCollection   `json:"fee_rights"`
	FeeAccounting      FeeAccounting        `json:"fee_accounting"`
	HeldQuote          []HeldQuote          `json:"held_quote,omitempty"`
	FeeHeld            sdk.Int              `json:"fee_held"`
	Treasury           []TreasuryBalance    `json:"treasury,omitempty"`
	BookHeldDeltas     []BookHeldDelta      `json:"book_held_deltas,omitempty"`
	Retired            []RetiredAddress     `json:"retired,omitempty"`
}

func DefaultGenesis() *GenesisState {
	return &GenesisState{Params: DefaultParams(), Markets: []Market{}, AirdropEpochs: []AirdropEpochAmount{},
		AirdropClaims: []AirdropClaim{}, ProtocolFees: []ProtocolFeeEntry{},
		FeeAccounting: FeeAccounting{PendingProtocolFees: sdk.ZeroInt(), TotalLiabilities: sdk.ZeroInt()},
		FeeHeld:       sdk.ZeroInt()}
}

func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	feeTokens := map[string]bool{}
	for _, entry := range gs.ProtocolFees {
		if sdk.ValidateDenom(entry.Token) != nil || feeTokens[entry.Token] || entry.Amount.IsNil() ||
			entry.Amount.IsNegative() || entry.Amount.BigInt().Cmp(MaxUint256()) > 0 {
			return fmt.Errorf("%w: protocol fees %s", ErrInvalidGenesis, entry.Token)
		}
		feeTokens[entry.Token] = true
	}
	denoms := map[string]Market{}
	indexes := map[uint64]bool{}
	for _, market := range gs.Markets {
		if err := market.Validate(); err != nil {
			return err
		}
		if _, dup := denoms[market.Denom]; dup || indexes[market.Index] {
			return fmt.Errorf("%w: duplicate market %s", ErrInvalidGenesis, market.Denom)
		}
		denoms[market.Denom] = market
		indexes[market.Index] = true
	}
	for i := uint64(1); i <= uint64(len(gs.Markets)); i++ {
		if !indexes[i] {
			return fmt.Errorf("%w: market indexes must be 1..%d", ErrInvalidGenesis, len(gs.Markets))
		}
	}
	amounts := map[string]sdk.Int{}
	for _, epoch := range gs.AirdropEpochs {
		amounts[fmt.Sprintf("%s/%d", epoch.Denom, epoch.Epoch)] = epoch.Amount
		market, ok := denoms[epoch.Denom]
		if !ok || epoch.Epoch == 0 || epoch.Epoch > market.AirdropEpoch || epoch.Amount.IsNil() || epoch.Amount.IsNegative() {
			return fmt.Errorf("%w: airdrop epoch %s/%d", ErrInvalidGenesis, epoch.Denom, epoch.Epoch)
		}
	}
	for _, claim := range gs.AirdropClaims {
		market, ok := denoms[claim.Denom]
		if _, err := sdk.AccAddressFromBech32(claim.Holder); !ok || err != nil || claim.Epoch == 0 || claim.Epoch > market.AirdropEpoch {
			return fmt.Errorf("%w: airdrop claim %s/%s/%d", ErrInvalidGenesis, claim.Denom, claim.Holder, claim.Epoch)
		}
	}
	histories := map[string]HoldingHistory{}
	for _, history := range gs.HoldingHistories {
		market, ok := denoms[history.Denom]
		if _, dup := histories[history.Denom]; !ok || dup || history.FirstEpoch == 0 ||
			history.FirstEpoch > history.CurrentEpoch || history.CurrentEpoch > market.AirdropEpoch {
			return fmt.Errorf("%w: holding history %s", ErrInvalidGenesis, history.Denom)
		}
		histories[history.Denom] = history
	}
	type holderKey struct{ denom, holder string }
	last := map[holderKey]HoldingCheckpoint{}
	for _, checkpoint := range gs.HoldingCheckpoints {
		history, ok := histories[checkpoint.Denom]
		key := holderKey{checkpoint.Denom, checkpoint.Holder}
		previous, seen := last[key]
		_, err := sdk.AccAddressFromBech32(checkpoint.Holder)
		if !ok || err != nil || checkpoint.Epoch > history.CurrentEpoch || checkpoint.Balance.IsNil() ||
			checkpoint.Balance.IsNegative() || (!seen && checkpoint.Index != 0) ||
			(seen && (checkpoint.Index != previous.Index+1 || checkpoint.Epoch <= previous.Epoch)) {
			return fmt.Errorf("%w: holding checkpoint %s/%s/%d", ErrInvalidGenesis, checkpoint.Denom, checkpoint.Holder,
				checkpoint.Index)
		}
		last[key] = checkpoint
	}
	bases := map[string]bool{}
	for _, basis := range gs.AirdropBases {
		id := fmt.Sprintf("%s/%d", basis.Denom, basis.Epoch)
		amount, funded := amounts[id]
		history, ok := histories[basis.Denom]
		if !ok || !funded || bases[id] || basis.Epoch < history.FirstEpoch || basis.Epoch > history.CurrentEpoch ||
			basis.Supply.IsNil() || !basis.Supply.IsPositive() || sdk.ValidateDenom(basis.PayoutDenom) != nil ||
			basis.Height < 0 || basis.Paid.IsNil() || basis.Paid.IsNegative() || basis.Paid.GT(amount) {
			return fmt.Errorf("%w: airdrop basis %s", ErrInvalidGenesis, id)
		}
		bases[id] = true
	}
	return gs.validateContracts()
}

// canonicalAddress reports whether value is the checksummed hex form of a
// non-zero address.
func canonicalAddress(value string) bool {
	return common.IsHexAddress(value) && common.HexToAddress(value).Hex() == value &&
		common.HexToAddress(value) != (common.Address{})
}

// amount reports whether value is a uint256.
func amount(value sdk.Int) bool {
	return !value.IsNil() && fits(value.BigInt()) == nil
}

// word reports whether value is the 0x-prefixed lowercase hex form of a
// non-zero 32-byte word.
func word(value string) bool {
	hash := common.HexToHash(value)
	return len(value) == 2+2*common.HashLength && hash.Hex() == value && hash != (common.Hash{})
}

func invalid(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrInvalidGenesis, fmt.Sprintf(format, args...))
}

// validateContracts checks the surfaces, role tables, fee rights,
// FeeAccumulator accounting, custody ledgers, book-held deltas and retired
// list that replace the Launch-x contracts.
func (gs GenesisState) validateContracts() error {
	surfaces := map[string]string{}
	singleRoles := map[string]bool{}
	for _, surface := range gs.Surfaces {
		_, dup := surfaces[surface.Address]
		if !canonicalAddress(surface.Address) || surface.Address == common.HexToAddress(LaunchpadAddress).Hex() || dup ||
			!surfaceRoles[surface.Role] || (surface.Role != RolePool && singleRoles[surface.Role]) {
			return invalid("surface %s %s", surface.Address, surface.Role)
		}
		surfaces[surface.Address] = surface.Role
		singleRoles[surface.Role] = true
	}
	isPool := func(address string) bool { return surfaces[address] == RolePool }
	members := map[RoleMember]bool{}
	for _, member := range gs.RoleMembers {
		role, ok := surfaces[member.Contract]
		if !ok || role == RolePool || !word(member.Role) || !canonicalAddress(member.Account) || members[member] {
			return invalid("role member %s %s %s", member.Contract, member.Role, member.Account)
		}
		members[member] = true
	}
	if err := gs.FeeRights.validate(isPool); err != nil {
		return err
	}
	if err := gs.FeeAccounting.validate(isPool); err != nil {
		return err
	}
	held := map[string]bool{}
	for _, entry := range gs.HeldQuote {
		if !isPool(entry.Pool) || held[entry.Pool] || !amount(entry.Amount) {
			return invalid("held quote %s", entry.Pool)
		}
		held[entry.Pool] = true
	}
	if !amount(gs.FeeHeld) || gs.FeeHeld.LT(gs.FeeAccounting.TotalLiabilities) {
		return invalid("fee held below total liabilities")
	}
	treasury := map[string]bool{}
	for _, entry := range gs.Treasury {
		if !canonicalAddress(entry.Token) || treasury[entry.Token] || !amount(entry.Ledger) || !amount(entry.Held) ||
			entry.Held.LT(entry.Ledger) {
			return invalid("treasury %s", entry.Token)
		}
		treasury[entry.Token] = true
	}
	type poolAsset struct{ pool, asset string }
	deltas := map[poolAsset]bool{}
	for _, delta := range gs.BookHeldDeltas {
		key := poolAsset{delta.Pool, delta.Asset}
		if !isPool(delta.Pool) || !canonicalAddress(delta.Asset) || deltas[key] || !amount(delta.Book) ||
			!amount(delta.Held) || delta.Delta.IsNil() || delta.Delta.IsZero() || !delta.Delta.Equal(delta.Held.Sub(delta.Book)) {
			return invalid("book-held delta %s %s", delta.Pool, delta.Asset)
		}
		deltas[key] = true
	}
	retired := map[string]bool{}
	retiredRole := map[string]bool{}
	for _, entry := range gs.Retired {
		_, isSurface := surfaces[entry.Address]
		replacementRole, replacementIsSurface := surfaces[entry.Replacement]
		if !canonicalAddress(entry.Address) || !canonicalAddress(entry.Replacement) || entry.Address == entry.Replacement ||
			isSurface || retired[entry.Address] || !retiredRoles[entry.Role] || retiredRole[entry.Role] ||
			(entry.Role != RoleOpticalRegistry && (!replacementIsSurface || replacementRole != entry.Role)) ||
			(entry.Role == RoleOpticalRegistry && replacementIsSurface) {
			return invalid("retired %s %s", entry.Address, entry.Role)
		}
		retired[entry.Address] = true
		retiredRole[entry.Role] = true
	}
	return nil
}

func (c FeeRightCollection) validate(isPool func(string) bool) error {
	if c.PoolIndexMigrationCursor > c.NextTokenID || (c.PoolIndexReady && c.PoolIndexMigrationCursor != c.NextTokenID) {
		return invalid("fee-right collection counters")
	}
	ids := map[uint64]bool{}
	pools := map[string]bool{}
	for _, token := range c.Tokens {
		if token.ID == 0 || token.ID >= c.NextTokenID || ids[token.ID] || !canonicalAddress(token.Owner) ||
			(token.Approved != "" && !canonicalAddress(token.Approved)) || token.Strategy.Validate() != nil ||
			!isPool(token.Pool) || pools[token.Pool] {
			return invalid("fee right %d", token.ID)
		}
		ids[token.ID] = true
		pools[token.Pool] = true
	}
	operators := map[FeeRightOperator]bool{}
	for _, operator := range c.Operators {
		if !canonicalAddress(operator.Owner) || !canonicalAddress(operator.Operator) || operator.Owner == operator.Operator ||
			operators[operator] {
			return invalid("fee-right operator %s %s", operator.Owner, operator.Operator)
		}
		operators[operator] = true
	}
	generation1 := map[uint64]bool{}
	generation2 := map[uint64]bool{}
	for _, mapping := range c.IDMap {
		if mapping.Generation1ID == 0 || generation1[mapping.Generation1ID] || generation2[mapping.Generation2ID] ||
			!ids[mapping.Generation2ID] {
			return invalid("fee-right id map %d to %d", mapping.Generation1ID, mapping.Generation2ID)
		}
		generation1[mapping.Generation1ID] = true
		generation2[mapping.Generation2ID] = true
	}
	return nil
}

func (a FeeAccounting) validate(isPool func(string) bool) error {
	if !amount(a.PendingProtocolFees) || !amount(a.TotalLiabilities) {
		return invalid("fee accounting totals")
	}
	accounts := map[string]PoolFeeAccount{}
	for _, account := range a.Pools {
		_, dup := accounts[account.Pool]
		if !isPool(account.Pool) || dup || !amount(account.AccumulatedFees) || !amount(account.AirdropBalance) ||
			!amount(account.LpRewardsBalance) || !amount(account.OpticalSurplus) || !amount(account.LegacyAirdropUnallocated) {
			return invalid("fee account %s", account.Pool)
		}
		accounts[account.Pool] = account
	}
	type poolEpoch struct {
		pool  string
		epoch uint64
	}
	epochs := map[poolEpoch]bool{}
	for _, epoch := range a.Epochs {
		key := poolEpoch{epoch.Pool, epoch.Epoch}
		account, ok := accounts[epoch.Pool]
		if !ok || epoch.Epoch == 0 || epoch.Epoch > account.AirdropEpoch || epochs[key] || !amount(epoch.Amount) ||
			!amount(epoch.EligibleSupply) || !amount(epoch.Remaining) || !amount(epoch.ClaimedSupply) ||
			!amount(epoch.LegacyRemaining) || epoch.Remaining.GT(epoch.Amount) ||
			epoch.ClaimedSupply.GT(epoch.EligibleSupply) ||
			(epoch.LegacyRoot != "" && (!word(epoch.LegacyRoot) || !epoch.EligibleSupply.IsZero())) ||
			(epoch.LegacyRoot == "" && !epoch.LegacyRemaining.IsZero()) {
			return invalid("airdrop epoch %s/%d", epoch.Pool, epoch.Epoch)
		}
		epochs[key] = true
	}
	claims := map[PoolAirdropClaim]bool{}
	for _, claim := range a.Claims {
		account, ok := accounts[claim.Pool]
		if !ok || !canonicalAddress(claim.Holder) || claim.Epoch == 0 || claim.Epoch > account.AirdropEpoch || claims[claim] {
			return invalid("airdrop claim %s/%s/%d", claim.Pool, claim.Holder, claim.Epoch)
		}
		claims[claim] = true
	}
	return nil
}
