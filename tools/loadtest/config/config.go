package config

import (
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

// LoadConfig stores the configuration for load-related settings.
type LoadConfig struct {
	ChainID int64 `json:"chainId,omitempty"`
	// PaxChainID is the textual chain ID used for tagging metric collection.
	PaxChainID string         `json:"paxChainID,omitempty"`
	Endpoints  []string       `json:"endpoints"`
	Accounts   *AccountConfig `json:"accounts,omitempty"`
	Scenarios  []Scenario     `json:"scenarios,omitempty"`
	MockDeploy bool           `json:"mockDeploy,omitempty"`
	Settings   *Settings      `json:"settings,omitempty"`
	// Path to write a JSON report of the load test.
	ReportPath string `json:"reportPath,omitempty"`
}

// Duration wraps time.Duration to provide JSON unmarshaling support
type Duration time.Duration

// UnmarshalJSON implements json.Unmarshaler for Duration
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration format: %w", err)
	}
	*d = Duration(parsed)
	return nil
}

// ToDuration converts Duration back to time.Duration
func (d Duration) ToDuration() time.Duration {
	return time.Duration(d)
}

// MarshalJSON implements json.Marshaler for Duration
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// GetChainID returns the chain ID as a big.Int.
func (c *LoadConfig) GetChainID() *big.Int {
	return big.NewInt(c.ChainID)
}

// AccountConfig stores the configuration for account generation.
type AccountConfig struct {
	NewAccountRate float64 `json:"newAccountRate,omitempty"`
	Accounts       int     `json:"count,omitempty"`
}

// Scenario represents each scenario in the load configuration.
type Scenario struct {
	Name            string         `json:"name,omitempty"`
	Weight          int            `json:"weight,omitempty"`
	Accounts        *AccountConfig `json:"accounts,omitempty"`
	GasPicker       *GasPicker     `json:"gasPicker,omitempty"`
	GasFeeCapPicker *GasPicker     `json:"gasFeeCapPicker,omitempty"`
	GasTipCapPicker *GasPicker     `json:"gasTipCapPicker,omitempty"`
}
