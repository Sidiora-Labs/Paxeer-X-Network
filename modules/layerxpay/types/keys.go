package types

import "encoding/binary"

const (
	ModuleName                 = "layerxpay"
	StoreKey                   = ModuleName
	RouterKey                  = ModuleName
	QuerierRoute               = ModuleName
	EscrowModuleID      uint16 = 2
	BudgetModuleID      uint16 = 3
	StreamModuleID      uint16 = 4
	ServiceModuleID     uint16 = 5
	GovernanceModuleID  uint16 = 7
	MaxKernelRecords           = 512
	MaxKernelKeyBytes          = 128
	MaxKernelValueBytes        = 1024
)

var (
	ParamsKey          = []byte{0x05}
	KernelKVPrefix     = []byte{0x01}
	EscrowKVPrefix     = []byte{0x01, 0x00, 0x02}
	BudgetKVPrefix     = []byte{0x01, 0x00, 0x03}
	StreamKVPrefix     = []byte{0x01, 0x00, 0x04}
	ServiceKVPrefix    = []byte{0x01, 0x00, 0x05}
	GovernanceKVPrefix = []byte{0x01, 0x00, 0x07}
)

func KernelKVKey(moduleID uint16, key []byte) []byte {
	out := make([]byte, 3, 3+len(key))
	out[0] = KernelKVPrefix[0]
	binary.BigEndian.PutUint16(out[1:3], moduleID)
	return append(out, key...)
}

func KernelModuleEnableKey(moduleID uint16) ([]byte, error) {
	var name string
	switch moduleID {
	case EscrowModuleID:
		name = "escrow"
	case BudgetModuleID:
		name = "budget"
	case StreamModuleID:
		name = "stream"
	case ServiceModuleID:
		name = "service"
	case 6:
		name = "perps"
	case 10:
		name = "spot"
	case 11:
		name = "web"
	default:
		return nil, ErrInvalidGenesis.Wrapf("module %d has no kernel enable flag", moduleID)
	}
	key := make([]byte, 32)
	copy(key, "module-enable:"+name)
	return KernelKVKey(GovernanceModuleID, key), nil
}
