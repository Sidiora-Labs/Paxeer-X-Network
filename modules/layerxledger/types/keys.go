package types

const (
	// ModuleName defines the module name.
	ModuleName = "layerxledger"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName

	// RouterKey is the message route.
	RouterKey = ModuleName

	// QuerierRoute defines the module's query routing key.
	QuerierRoute = ModuleName
)

var (
	ParamsKey = []byte{0x01}
	// KernelKVPrefix holds the kernel module-kv entries the shim persists for
	// the asset and ledger kernel modules.
	KernelKVPrefix = []byte{0x02}
)
