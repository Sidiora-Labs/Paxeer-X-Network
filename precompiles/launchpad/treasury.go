package launchpad

import (
	"errors"
	"fmt"
	"math/big"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

func init() {
	roleHandlers[types.RoleTreasury] = PrecompileExecutor.treasuryHandler
}

// treasuryHandler serves the Treasury ABI (Launch-x Treasury.sol) at the
// generation 2 Treasury surface over the keeper Treasury ledger: the views,
// deposit and recordDeposit under the Treasury lock, and renounceRole; every
// other method is refused by treasuryAdmin.
func (p PrecompileExecutor) treasuryHandler(c *call) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(c.args, len(c.method.Inputs)); err != nil {
		return nil, err
	}
	treasury := p.keeper.TreasuryContract()
	switch c.method.Name {
	case "DEFAULT_ADMIN_ROLE":
		return c.method.Outputs.Pack(launchpadkeeper.DefaultAdminRole)
	case "DEPOSITOR_ROLE":
		return c.method.Outputs.Pack([32]byte(launchpadkeeper.TreasuryDepositorRole))
	case "eventEmitter":
		return c.method.Outputs.Pack(p.keeper.Generation2().EventEmitter)
	case "getBalance":
		balance, err := p.keeper.TreasuryGetBalance(c.ctx, c.args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(balance.BigInt())
	case "getRoleAdmin":
		return c.method.Outputs.Pack(p.keeper.RoleAdmin(c.ctx, treasury, c.args[0].([32]byte)))
	case "hasRole":
		return c.method.Outputs.Pack(p.keeper.HasRole(c.ctx, treasury, c.args[0].([32]byte),
			c.args[1].(common.Address)))
	case "registeredDefaultAdminCount":
		return c.method.Outputs.Pack(new(big.Int).SetUint64(p.keeper.RegisteredDefaultAdminCount(c.ctx, treasury)))
	case "deposit", "recordDeposit":
		return p.treasuryDeposit(c, treasury)
	case "renounceRole":
		return p.treasuryRenounce(c, treasury)
	}
	return nil, treasuryAdmin(c)
}

// treasuryAdmin refuses the Treasury methods that do not run at the surface:
// withdraw, recoverSurplus and the DEFAULT_ADMIN_ROLE methods revert
// AccessControlUnauthorizedAccount(caller, 0x00) (ruling 7), withdraw and
// recoverSurplus running only through MsgTreasuryWithdraw; initialize reverts
// AlreadyInitialized() and proxiableUUID UUPSNotThroughActiveProxy(), as the
// proxy does.
func treasuryAdmin(c *call) error {
	switch c.method.Name {
	case "withdraw", "recoverSurplus", "grantRole", "revokeRole", "upgradeToAndCall":
		return launchpadkeeper.AdminRefusal(c.caller)
	case "initialize":
		return fmt.Errorf("%w: %s", types.ErrAlreadyInitialized, types.RoleTreasury)
	case "proxiableUUID":
		return fmt.Errorf("%w: proxiableUUID through the proxy", types.ErrUUPSNotThroughActiveProxy)
	}
	return fmt.Errorf("launchpad: the %s role does not serve %s", types.RoleTreasury, c.method.Sig)
}

// treasuryDeposit is deposit(token, amount) or recordDeposit(token, amount):
// onlyRole(DEPOSITOR_ROLE), then the Treasury lock, then the keeper ledger
// credit, and Deposited(token, caller, amount) at the Treasury with the
// received amount for deposit and the recorded amount for recordDeposit.
func (p PrecompileExecutor) treasuryDeposit(c *call, treasury common.Address) ([]byte, error) {
	token, amount := c.args[0].(common.Address), sdk.NewIntFromBigInt(c.args[1].(*big.Int))
	if err := p.keeper.CheckRole(c.ctx, treasury, launchpadkeeper.TreasuryDepositorRole, c.caller); err != nil {
		return nil, err
	}
	release, err := p.keeper.EnterLock(c.evm.StateDB, launchpadkeeper.TreasuryLock)
	if err != nil {
		return nil, err
	}
	defer release()
	if c.nested == nil {
		return nil, errors.New("launchpad: the Treasury run has no nested caller")
	}
	ctx, credited := c.ctx, amount
	if c.method.Name == "deposit" {
		ctx, credited, err = p.keeper.TreasuryDeposit(c.ctx, c.nested, c.caller, token, amount)
	} else {
		ctx, err = p.keeper.TreasuryRecordDeposit(c.ctx, c.nested, c.caller, token, amount)
	}
	if err != nil {
		return nil, err
	}
	c.ctx = ctx
	event := roleABIs[types.RoleTreasury].Events["Deposited"]
	data, err := event.Inputs.NonIndexed().Pack(credited.BigInt())
	if err != nil {
		return nil, err
	}
	topics := []common.Hash{event.ID, common.BytesToHash(token.Bytes()), common.BytesToHash(c.caller.Bytes())}
	if err := pcommon.EmitEVMLog(c.evm, treasury, topics, data); err != nil {
		return nil, err
	}
	return c.method.Outputs.Pack()
}

// treasuryRenounce is renounceRole(role, callerConfirmation): the caller
// drops its own Treasury role and RoleRevoked(role, caller, caller) is
// emitted at the Treasury when the caller held it.
func (p PrecompileExecutor) treasuryRenounce(c *call, treasury common.Address) ([]byte, error) {
	role := c.args[0].([32]byte)
	held, err := p.keeper.RenounceRole(c.ctx, treasury, role, c.caller, c.args[1].(common.Address))
	if err != nil {
		return nil, err
	}
	if held {
		caller := common.BytesToHash(c.caller.Bytes())
		topics := []common.Hash{roleABIs[types.RoleTreasury].Events["RoleRevoked"].ID, role, caller, caller}
		if err := pcommon.EmitEVMLog(c.evm, treasury, topics, nil); err != nil {
			return nil, err
		}
	}
	return c.method.Outputs.Pack()
}
