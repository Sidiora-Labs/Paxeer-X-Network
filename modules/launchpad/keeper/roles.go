package keeper

import (
	"fmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
)

// DefaultAdminRole is DEFAULT_ADMIN_ROLE (Launch-x AccessControl), the admin
// of every role; it is not migrated, so no account holds it.
var DefaultAdminRole = [32]byte{}

// RoleError is a role refusal and the account and role its custom error
// carries: MissingRole(account, role) or
// AccessControlUnauthorizedAccount(account, 0x00).
type RoleError struct {
	Err     *sdkerrors.Error
	Account common.Address
	Role    [32]byte
}

func (e *RoleError) Error() string {
	return fmt.Sprintf("%s: account %s role %s", e.Err.Error(), e.Account.Hex(), common.Hash(e.Role).Hex())
}

func (e *RoleError) Unwrap() error { return e.Err }

// Cause lets the SDK ABCI error mapping reach the registered code.
func (e *RoleError) Cause() error { return e.Err }

// Revert is the EVM revert data of the refusal.
func (e *RoleError) Revert() ([]byte, error) {
	contractError, ok := types.ContractErrorOf(e.Err)
	if !ok {
		return nil, fmt.Errorf("%w: %s has no contract error", types.ErrInvalidParams, e.Err.Error())
	}
	return contractError.Revert(e.Account, e.Role)
}

func missingRole(account common.Address, role [32]byte) error {
	return &RoleError{Err: types.ErrMissingRole, Account: account, Role: role}
}

// AdminRefusal is the refusal of every DEFAULT_ADMIN_ROLE method at a
// surface (grantRole, revokeRole, upgradeToAndCall and the admin setters):
// administration runs through governance only (ruling 7).
func AdminRefusal(caller common.Address) error {
	return &RoleError{Err: types.ErrAccessControlUnauthorizedAccount, Account: caller, Role: DefaultAdminRole}
}

// HasRole reports whether account holds role in the role table of the role
// surface contract.
func (k *Keeper) HasRole(ctx sdk.Context, contract common.Address, role [32]byte, account common.Address) bool {
	return k.store(ctx).Has(types.RoleKey(contract, role, account))
}

// RoleAdmin is getRoleAdmin(role): no served role contract sets a role admin,
// so every role is administered by DEFAULT_ADMIN_ROLE.
func (k *Keeper) RoleAdmin(_ sdk.Context, _ common.Address, _ [32]byte) [32]byte {
	return DefaultAdminRole
}

// RegisteredDefaultAdminCount is registeredDefaultAdminCount(): zero, since
// DEFAULT_ADMIN_ROLE is not migrated.
func (k *Keeper) RegisteredDefaultAdminCount(_ sdk.Context, _ common.Address) uint64 { return 0 }

// CheckRole is onlyRole(role) for account: a DEFAULT_ADMIN_ROLE check is
// refused for every account, and a non-admin role the account does not hold
// reverts MissingRole(account, role).
func (k *Keeper) CheckRole(ctx sdk.Context, contract common.Address, role [32]byte, account common.Address) error {
	if role == DefaultAdminRole {
		return AdminRefusal(account)
	}
	if !k.HasRole(ctx, contract, role, account) {
		return missingRole(account, role)
	}
	return nil
}

// RenounceRole is renounceRole(role, callerConfirmation) sent by caller: a
// confirmation other than the caller reverts MissingRole(caller, role),
// otherwise the caller loses the role. It reports whether the caller held the
// role, which is when RoleRevoked(role, caller, caller) is emitted.
func (k *Keeper) RenounceRole(ctx sdk.Context, contract common.Address, role [32]byte,
	caller, callerConfirmation common.Address) (bool, error) {
	if callerConfirmation != caller {
		return false, missingRole(caller, role)
	}
	return k.revokeRole(ctx, contract, role, caller), nil
}

// GovernanceGrant grants a non-admin role at a role surface under the
// governance authority (MsgUpdateParams role_grants). It reports whether the
// account did not already hold the role, which is when RoleGranted is
// emitted at the surface.
func (k *Keeper) GovernanceGrant(ctx sdk.Context, authority string, contract common.Address, role [32]byte,
	account common.Address) (bool, error) {
	if err := k.checkAdministration(ctx, authority, contract, role, account); err != nil {
		return false, err
	}
	if k.HasRole(ctx, contract, role, account) {
		return false, nil
	}
	k.store(ctx).Set(types.RoleKey(contract, role, account), []byte{1})
	return true, nil
}

// GovernanceRevoke revokes a non-admin role at a role surface under the
// governance authority (MsgUpdateParams role_revokes). It reports whether the
// account held the role, which is when RoleRevoked is emitted at the
// surface.
func (k *Keeper) GovernanceRevoke(ctx sdk.Context, authority string, contract common.Address, role [32]byte,
	account common.Address) (bool, error) {
	if err := k.checkAdministration(ctx, authority, contract, role, account); err != nil {
		return false, err
	}
	return k.revokeRole(ctx, contract, role, account), nil
}

func (k *Keeper) checkAdministration(ctx sdk.Context, authority string, contract common.Address, role [32]byte,
	account common.Address) error {
	if authority != k.authority {
		return fmt.Errorf("%w: %s", types.ErrUnauthorized, authority)
	}
	if role == DefaultAdminRole {
		return fmt.Errorf("%w: DEFAULT_ADMIN_ROLE is not administered", types.ErrInvalidParams)
	}
	if account == (common.Address{}) {
		return types.ErrInvalidRoleAccount
	}
	surface, found, err := k.GetSurface(ctx, contract)
	if err != nil {
		return err
	}
	if !found || surface.Role == types.RolePool {
		return fmt.Errorf("%w: %s is not a role surface", types.ErrInvalidParams, contract.Hex())
	}
	return nil
}

func (k *Keeper) revokeRole(ctx sdk.Context, contract common.Address, role [32]byte, account common.Address) bool {
	key := types.RoleKey(contract, role, account)
	store := k.store(ctx)
	if !store.Has(key) {
		return false
	}
	store.Delete(key)
	return true
}
