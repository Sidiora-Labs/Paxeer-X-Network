package wire

// GenesisAccountEntry represents one externally-supplied genesis account.
// Mirrors SeiNetwork.Spec.Genesis.Accounts[] on the controller-CRD side.
//
// This is the single definition both sides of the wire use: sidecar/client
// aliases it for the request it builds, and sidecar/tasks aliases it for the
// payload it unmarshals. The two used to be separate structs whose json tags
// had to be kept in step by hand, checked by a cross-package round-trip test.
// One definition makes that drift unrepresentable, so no test is needed.
type GenesisAccountEntry struct {
	Address string `json:"address"`
	Balance string `json:"balance"`

	// Vesting, when set, locks Balance under a vesting schedule instead of
	// a standard account; nil produces today's plain account.
	Vesting *GenesisAccountVesting `json:"vesting,omitempty"`
}

// GenesisAccountVesting locks part of a GenesisAccountEntry's Balance on an
// unlock schedule completing at EndTime: linear from genesis time by default,
// or all-at-once when Delayed. Amount must not exceed Balance.
type GenesisAccountVesting struct {
	Amount  string `json:"amount"`
	EndTime int64  `json:"endTime"`
	Delayed bool   `json:"delayed,omitempty"`
}
