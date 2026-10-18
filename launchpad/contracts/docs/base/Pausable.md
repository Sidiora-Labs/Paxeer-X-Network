# Solidity API

## Pausable

Emergency pause mechanism with whenNotPaused/whenPaused modifiers

### Paused

```solidity
error Paused()
```

### NotPaused

```solidity
error NotPaused()
```

### PauseToggled

```solidity
event PauseToggled(bool paused)
```

### whenNotPaused

```solidity
modifier whenNotPaused()
```

### whenPaused

```solidity
modifier whenPaused()
```

### _paused

```solidity
function _paused() internal view returns (bool paused_)
```

### _pause

```solidity
function _pause() internal
```

### _unpause

```solidity
function _unpause() internal
```

