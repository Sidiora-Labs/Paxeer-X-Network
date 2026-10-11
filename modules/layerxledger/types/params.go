package types

// DefaultParams leaves the ledger unpaused.
func DefaultParams() Params { return Params{Paused: false} }

// Validate accepts both values of Paused, the only param; the fixed gas table
// is code, not a param.
func (p Params) Validate() error { return nil }
