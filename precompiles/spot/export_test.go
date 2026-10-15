package spot

// Test-only handles on unexported identifiers for the external spot_test
// package, which cannot be an internal test because it reaches the node.
type VaultExecutor = vaultExecutor

var (
	NewVaultExecutor   = newVaultExecutor
	ErrIndexOutOfRange = errIndexOutOfRange
	PackError          = packError
	CheckFeeCollector  = checkFeeCollector
)
