package types

const (
	// ModuleName is the sequencer module, mounted by the activation store
	// upgrade.
	ModuleName = "sequencer"
	StoreKey   = ModuleName
)

var (
	// LastSeqKey holds the last committed sequence position, u64 big-endian.
	LastSeqKey = []byte{0x01}
	// LastSeqHashKey holds the seqHash of LastSeq, 32 bytes.
	LastSeqHashKey = []byte{0x02}
	// ReceiptKeysKey holds the pinned receipt keys with their height ranges.
	ReceiptKeysKey = []byte{0x03}
	// LeaderListKey holds the pinned leader list.
	LeaderListKey = []byte{0x04}
	// StoredVotesKey holds the decided votes fed to the next BeginBlock.
	StoredVotesKey = []byte{0x05}
)
