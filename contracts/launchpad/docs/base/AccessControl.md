# Solidity API

## AccessControl

Role-based access control with role admin hierarchy

_Roles are referred to by their bytes32 identifier.
     DEFAULT_ADMIN_ROLE is the admin for all roles by default._

### DEFAULT_ADMIN_ROLE

```solidity
bytes32 DEFAULT_ADMIN_ROLE
```

### RoleData

```solidity
struct RoleData {
  mapping(address => bool) hasRole;
  bytes32 adminRole;
}
```

### MissingRole

```solidity
error MissingRole(address account, bytes32 role)
```

### RoleGranted

```solidity
event RoleGranted(bytes32 role, address account, address sender)
```

### RoleRevoked

```solidity
event RoleRevoked(bytes32 role, address account, address sender)
```

### RoleAdminChanged

```solidity
event RoleAdminChanged(bytes32 role, bytes32 previousAdminRole, bytes32 newAdminRole)
```

### onlyRole

```solidity
modifier onlyRole(bytes32 role)
```

### hasRole

```solidity
function hasRole(bytes32 role, address account) public view returns (bool)
```

Returns true if account has the given role

### getRoleAdmin

```solidity
function getRoleAdmin(bytes32 role) public view returns (bytes32)
```

Returns the admin role for a given role

### grantRole

```solidity
function grantRole(bytes32 role, address account) external
```

Grants a role to an account. Caller must have the role's admin role.

### revokeRole

```solidity
function revokeRole(bytes32 role, address account) external
```

Revokes a role from an account. Caller must have the role's admin role.

### renounceRole

```solidity
function renounceRole(bytes32 role, address callerConfirmation) external
```

Account renounces a role from itself

### _checkRole

```solidity
function _checkRole(bytes32 role, address account) internal view
```

### _grantRole

```solidity
function _grantRole(bytes32 role, address account) internal returns (bool)
```

### _revokeRole

```solidity
function _revokeRole(bytes32 role, address account) internal returns (bool)
```

### _setRoleAdmin

```solidity
function _setRoleAdmin(bytes32 role, bytes32 adminRole) internal
```

