package types

// DefaultParams leaves the module unpaused with no idempotency pruning; the
// production retention window is set at activation rehearsal.
func DefaultParams() Params {
	return Params{Paused: false, IdempotencyRetention: 0}
}

// Validate accepts every params value. IdempotencyRetention counts global
// sequence units, the unit of the journal global_sequence; zero keeps every
// entry, matching the C store where a zero retention never expires an entry.
func (p Params) Validate() error {
	return nil
}
