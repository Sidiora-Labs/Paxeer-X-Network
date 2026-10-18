package keeper

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	// ErrUSDLBalanceDiffersFromGolden refuses step 1 when a launchpad
	// contract holds a USDL balance other than the one usdl.json records.
	ErrUSDLBalanceDiffersFromGolden = errors.New("upgrade v6.12.0: launchpad: usdl balance differs from golden")
	// ErrAlreadyMigrated refuses a second run of MigrateV6120.
	ErrAlreadyMigrated = errors.New("upgrade v6.12.0: launchpad: already migrated")
)

var (
	treasuryHasRole        = crypto.Keccak256([]byte("hasRole(bytes32,address)"))[:4]
	treasuryGetBalance     = crypto.Keccak256([]byte("getBalance(address)"))[:4]
	treasuryWithdraw       = crypto.Keccak256([]byte("withdraw(address,address,uint256)"))[:4]
	treasuryRecoverSurplus = crypto.Keccak256([]byte("recoverSurplus(address,address,uint256)"))[:4]
)

// USDLHolding is one balances entry of usdl.json: the USDL.balanceOf of a
// launchpad contract address at the halt height.
type USDLHolding struct {
	Generation uint8          `json:"generation"`
	Role       string         `json:"role"`
	Address    common.Address `json:"address"`
	Balance    sdk.Int        `json:"balance"`
}

// TreasuryDrain names a Treasury, the DEFAULT_ADMIN_ROLE holder that drains
// it and the tokens it is drained of.
type TreasuryDrain struct {
	Treasury common.Address   `json:"treasury"`
	Admin    common.Address   `json:"admin"`
	Tokens   []common.Address `json:"tokens"`
}

// USDLGolden is the fixture of step 1: the USDL address and balances of
// usdl.json and the drain of both Treasuries.
type USDLGolden struct {
	Address    common.Address  `json:"address"`
	Balances   []USDLHolding   `json:"balances"`
	Treasuries []TreasuryDrain `json:"treasuries"`
}

// MigrateV6120USDL is step 1 of MigrateV6120: it checks every launchpad USDL
// balance against golden, moves the USDL of every pool and FeeAccumulator and
// of both Treasuries into custody at the launchpad module EVM address through
// nested calls, credits the custody ledgers and marks the migration done, so
// a second run is refused before any call is made.
func (k *Keeper) MigrateV6120USDL(ctx sdk.Context, caller NestedCaller, golden USDLGolden) (sdk.Context, error) {
	if k.store(ctx).Has(types.MigratedKey) {
		return ctx, ErrAlreadyMigrated
	}
	if err := k.migrateUSDL(ctx, caller, golden); err != nil {
		return ctx, err
	}
	ctx, err := k.moveIntoCustody(ctx, caller, golden)
	if err != nil {
		return ctx, err
	}
	k.usdlStepDone(ctx)
	return ctx, nil
}

// migrateUSDL validates golden and requires USDL.balanceOf of every address
// it lists, read by staticcall, to equal the recorded balance. It writes
// nothing.
func (k *Keeper) migrateUSDL(ctx sdk.Context, caller NestedCaller, golden USDLGolden) error {
	token := k.quoteToken(ctx)
	if golden.Address != token {
		return fmt.Errorf("%w: golden usdl %s is not the quote token %s", types.ErrInvalidConfiguration,
			golden.Address.Hex(), token.Hex())
	}
	roles := map[uint8]map[string]common.Address{1: {}, 2: {}}
	seen := map[common.Address]bool{}
	for _, h := range golden.Balances {
		byRole, ok := roles[h.Generation]
		if !ok {
			return fmt.Errorf("%w: usdl holding %s of generation %d", types.ErrInvalidConfiguration, h.Address.Hex(),
				h.Generation)
		}
		if h.Address == (common.Address{}) || seen[h.Address] {
			return fmt.Errorf("%w: usdl holding %s zero or repeated", types.ErrInvalidConfiguration, h.Address.Hex())
		}
		seen[h.Address] = true
		if !uint256Amount(h.Balance) {
			return fmt.Errorf("%w: usdl holding %s", types.ErrOverflow, h.Address.Hex())
		}
		switch h.Role {
		case types.RoleFeeAccumulator, types.RoleTreasury, types.RoleFeesRouter:
			if _, dup := byRole[h.Role]; dup {
				return fmt.Errorf("%w: generation %d %s repeated", types.ErrInvalidConfiguration, h.Generation, h.Role)
			}
			byRole[h.Role] = h.Address
		}
		balance, err := k.quoteBalance(ctx, caller, token, h.Address)
		if err != nil {
			return err
		}
		if !balance.Equal(h.Balance) {
			return fmt.Errorf("%w: %s %s holds %s, golden %s", ErrUSDLBalanceDiffersFromGolden, h.Role,
				h.Address.Hex(), balance, h.Balance)
		}
		if !h.Balance.IsZero() && h.Role != types.RolePool && h.Role != types.RoleFeeAccumulator &&
			h.Role != types.RoleTreasury && h.Role != types.RoleFeesRouter {
			return fmt.Errorf("%w: %s %s holds usdl outside every custody ledger", types.ErrInvalidConfiguration,
				h.Role, h.Address.Hex())
		}
	}
	for _, generation := range []uint8{1, 2} {
		byRole := roles[generation]
		for _, role := range []string{types.RoleFeeAccumulator, types.RoleFeesRouter, types.RoleTreasury} {
			if _, ok := byRole[role]; !ok {
				return fmt.Errorf("%w: generation %d %s missing", types.ErrInvalidConfiguration, generation, role)
			}
		}
	}
	if roles[2][types.RoleTreasury] != k.TreasuryContract() ||
		roles[2][types.RoleFeeAccumulator] != k.Generation2().FeeAccumulator {
		return fmt.Errorf("%w: generation 2 Treasury or FeeAccumulator is not the served address",
			types.ErrInvalidConfiguration)
	}
	treasuries := map[common.Address]bool{roles[1][types.RoleTreasury]: true, roles[2][types.RoleTreasury]: true}
	if len(golden.Treasuries) != len(treasuries) {
		return fmt.Errorf("%w: %d treasury drains, want %d", types.ErrInvalidConfiguration, len(golden.Treasuries),
			len(treasuries))
	}
	for _, d := range golden.Treasuries {
		if !treasuries[d.Treasury] || d.Admin == (common.Address{}) {
			return fmt.Errorf("%w: treasury drain %s with admin %s", types.ErrInvalidConfiguration, d.Treasury.Hex(),
				d.Admin.Hex())
		}
		delete(treasuries, d.Treasury)
		tokens := map[common.Address]bool{}
		for _, t := range d.Tokens {
			if t == (common.Address{}) || tokens[t] {
				return fmt.Errorf("%w: treasury %s token %s zero or repeated", types.ErrInvalidConfiguration,
					d.Treasury.Hex(), t.Hex())
			}
			tokens[t] = true
		}
		if !tokens[token] {
			return fmt.Errorf("%w: treasury %s drain omits usdl", types.ErrInvalidConfiguration, d.Treasury.Hex())
		}
	}
	return nil
}

// moveIntoCustody moves the USDL of every pool and FeeAccumulator into
// custody by USDL.transfer called by the holding contract, credited to
// HeldQuote(pool) or FeeHeld(accumulator), drains each Treasury, and checks
// that the custody balance rose by exactly the sum moved and that the custody
// invariant holds.
func (k *Keeper) moveIntoCustody(ctx sdk.Context, caller NestedCaller, golden USDLGolden) (sdk.Context, error) {
	token, custody := k.quoteToken(ctx), k.ModuleEVMAddress(ctx)
	before, err := k.quoteBalance(ctx, caller, token, custody)
	if err != nil {
		return ctx, err
	}
	moved := sdk.ZeroInt()
	accumulators := map[uint8]common.Address{}
	for _, h := range golden.Balances {
		if h.Role == types.RoleFeeAccumulator {
			accumulators[h.Generation] = h.Address
		}
	}
	for _, h := range golden.Balances {
		if h.Balance.IsZero() || (h.Role != types.RolePool && h.Role != types.RoleFeeAccumulator &&
			h.Role != types.RoleFeesRouter) {
			continue
		}
		var received sdk.Int
		if h.Role == types.RolePool {
			ctx, received, err = k.SweepPool(ctx, caller, h.Address)
		} else {
			accumulator := h.Address
			if h.Role == types.RoleFeesRouter {
				accumulator = accumulators[h.Generation]
			}
			ctx, received, err = k.receive(ctx, caller, h.Address,
				erc20Call(erc20Transfer, addressWord(custody), amountWord(h.Balance.BigInt())),
				FeeHeldLedger(accumulator), types.ErrTransferFailed)
		}
		if err != nil {
			return ctx, err
		}
		if !received.Equal(h.Balance) {
			return ctx, fmt.Errorf("%w: custody received %s of %s from %s", types.ErrInvalidBalance, received,
				h.Balance, h.Address.Hex())
		}
		moved = moved.Add(received)
	}
	for _, d := range golden.Treasuries {
		var received sdk.Int
		ctx, received, err = k.drainTreasury(ctx, caller, d, token)
		if err != nil {
			return ctx, err
		}
		moved = moved.Add(received)
	}
	after, err := k.quoteBalance(ctx, caller, token, custody)
	if err != nil {
		return ctx, err
	}
	if !after.Equal(before.Add(moved)) {
		return ctx, fmt.Errorf("%w: custody holds %s, want %s plus %s moved", types.ErrInvalidBalance, after, before,
			moved)
	}
	if message, broken := CustodyInvariant(k)(ctx); broken {
		return ctx, fmt.Errorf("%w: %s", types.ErrInvalidBalance, message)
	}
	return ctx, nil
}

// drainTreasury confirms hasRole(DEFAULT_ADMIN_ROLE, admin) at the Treasury
// and drains it of every listed token, returning the quote received into
// custody.
func (k *Keeper) drainTreasury(ctx sdk.Context, caller NestedCaller, d TreasuryDrain,
	quote common.Address) (sdk.Context, sdk.Int, error) {
	ret, err := caller.StaticCall(ctx, launchpadAddress, d.Treasury,
		erc20Call(treasuryHasRole, DefaultAdminRole[:], addressWord(d.Admin)))
	if err != nil {
		return ctx, sdk.Int{}, fmt.Errorf("%w: treasury %s hasRole: %w", types.ErrInvalidConfiguration,
			d.Treasury.Hex(), err)
	}
	if len(ret) != 32 || new(big.Int).SetBytes(ret).Cmp(big.NewInt(1)) != 0 {
		return ctx, sdk.Int{}, fmt.Errorf("treasury %s: %w", d.Treasury.Hex(), missingRole(d.Admin, DefaultAdminRole))
	}
	quoteReceived := sdk.ZeroInt()
	for _, token := range d.Tokens {
		var received sdk.Int
		ctx, received, err = k.withdrawTreasuryToken(ctx, caller, d, token)
		if err != nil {
			return ctx, sdk.Int{}, err
		}
		if token == quote {
			quoteReceived = received
		}
	}
	return ctx, quoteReceived, nil
}

// withdrawTreasuryToken runs withdraw(token, 0x1017, getBalance(token)) and
// recoverSurplus(token, 0x1017, surplus) from the Treasury admin, moves what
// 0x1017 received into custody by token.transfer called by 0x1017, credits
// TreasuryHeld with the full amount and TreasuryLedger with the withdrawn
// ledger amount, and requires the Treasury to hold none of token afterwards.
func (k *Keeper) withdrawTreasuryToken(ctx sdk.Context, caller NestedCaller, d TreasuryDrain,
	token common.Address) (sdk.Context, sdk.Int, error) {
	ret, err := caller.StaticCall(ctx, launchpadAddress, d.Treasury, erc20Call(treasuryGetBalance, addressWord(token)))
	if err != nil {
		return ctx, sdk.Int{}, fmt.Errorf("%w: treasury %s getBalance: %w", types.ErrInvalidBalance, d.Treasury.Hex(),
			err)
	}
	if len(ret) != 32 {
		return ctx, sdk.Int{}, fmt.Errorf("%w: treasury %s getBalance returned %d bytes", types.ErrInvalidBalance,
			d.Treasury.Hex(), len(ret))
	}
	ledger := sdk.NewIntFromBigInt(new(big.Int).SetBytes(ret))
	before, err := k.treasuryBalance(ctx, caller, token, launchpadAddress)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if ledger.IsPositive() {
		ctx, err = k.treasuryCall(ctx, caller, d.Admin, d.Treasury,
			erc20Call(treasuryWithdraw, addressWord(token), addressWord(launchpadAddress), amountWord(ledger.BigInt())),
			types.ErrTransferFailed)
		if err != nil {
			return ctx, sdk.Int{}, err
		}
	}
	surplus, err := k.treasuryBalance(ctx, caller, token, d.Treasury)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if surplus.IsPositive() {
		ctx, err = k.treasuryCall(ctx, caller, d.Admin, d.Treasury,
			erc20Call(treasuryRecoverSurplus, addressWord(token), addressWord(launchpadAddress),
				amountWord(surplus.BigInt())), types.ErrTransferFailed)
		if err != nil {
			return ctx, sdk.Int{}, err
		}
	}
	remaining, err := k.treasuryBalance(ctx, caller, token, d.Treasury)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if !remaining.IsZero() {
		return ctx, sdk.Int{}, fmt.Errorf("%w: treasury %s still holds %s of %s", types.ErrInvalidBalance,
			d.Treasury.Hex(), remaining, token.Hex())
	}
	after, err := k.treasuryBalance(ctx, caller, token, launchpadAddress)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if after.LT(before) {
		return ctx, sdk.Int{}, fmt.Errorf("%w: 0x1017 balance of %s fell from %s to %s", types.ErrInvalidBalance,
			token.Hex(), before, after)
	}
	withdrawn := after.Sub(before)
	if withdrawn.IsZero() {
		return ctx, withdrawn, nil
	}
	ctx, received, err := k.treasuryReceive(ctx, caller, launchpadAddress, token,
		erc20Call(erc20Transfer, addressWord(k.ModuleEVMAddress(ctx)), amountWord(withdrawn.BigInt())),
		types.ErrTransferFailed)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if err := k.addLedgerAmount(ctx, types.TreasuryHeldKey(token), received); err != nil {
		return ctx, sdk.Int{}, err
	}
	if ledger.IsPositive() {
		if err := k.treasuryCredit(ctx, token, ledger); err != nil {
			return ctx, sdk.Int{}, err
		}
	}
	return ctx, received, nil
}

// usdlStepDone marks the migration done under MigratedKey.
func (k *Keeper) usdlStepDone(ctx sdk.Context) { k.store(ctx).Set(types.MigratedKey, []byte{1}) }
