package chainconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// OverlayEnv names the environment variable carrying the path of the deploy
// overlay: the private file, never committed, that fills in what a committed
// configuration leaves as a placeholder. Load applies it to every configuration
// it reads while the variable is set.
const OverlayEnv = "BRIDGE_DEPLOY_OVERLAY"

// Overlay is the deploy-time values of the nine chains, keyed by chain name.
type Overlay struct {
	Chains map[string]ChainOverlay `json:"chains"`
}

// ChainOverlay is what the overlay may set on one chain. An empty field leaves
// the committed value in place. Environment names the variables the endpoint
// and the keys arrive in; the values themselves stay in the environment.
type ChainOverlay struct {
	Owner                 string              `json:"owner,omitempty"`
	Deployer              string              `json:"deployer,omitempty"`
	Attestors             []string            `json:"attestors,omitempty"`
	Threshold             uint32              `json:"threshold,omitempty"`
	Environment           *Environment        `json:"environment,omitempty"`
	Caps                  map[string]AssetCap `json:"caps,omitempty"`
	BigBlocksAcknowledged bool                `json:"big_blocks_acknowledged,omitempty"`
	ProgramID             string              `json:"program_id,omitempty"`
}

// AssetCap is the per-transaction and total cap of one asset, by symbol.
type AssetCap struct {
	PerTxCap string `json:"per_tx_cap"`
	TotalCap string `json:"total_cap"`
}

// LoadOverlay reads an overlay, refusing an unknown field, a second JSON
// document and a chain the bridge does not carry.
func LoadOverlay(path string) (*Overlay, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer file.Close()
	overlay := &Overlay{}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(overlay); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: the file carries more than one JSON document", path)
	}
	for name := range overlay.Chains {
		if _, ok := ChainByName(name); !ok {
			return nil, fmt.Errorf("%s: chains.%s: %q is not a bridge chain", path, name, name)
		}
	}
	return overlay, nil
}

// Apply fills a configuration from the overlay entry of its chain. The result
// still goes through Validate and RequireDeployable: the overlay replaces
// placeholders, it does not bypass a rule.
func (o *Overlay) Apply(c *ChainConfig) error {
	entry, ok := o.Chains[c.Chain]
	if !ok {
		return nil
	}
	if entry.Owner != "" {
		c.Owner = entry.Owner
	}
	if entry.Deployer != "" {
		c.Deployer = entry.Deployer
	}
	if len(entry.Attestors) != 0 {
		c.Attestors = append([]string(nil), entry.Attestors...)
	}
	if entry.Threshold != 0 {
		c.Threshold = entry.Threshold
	}
	if entry.Environment != nil {
		c.Environment = *entry.Environment
	}
	for symbol, limits := range entry.Caps {
		found := false
		for index := range c.Assets {
			if c.Assets[index].Symbol == symbol {
				c.Assets[index].PerTxCap = limits.PerTxCap
				c.Assets[index].TotalCap = limits.TotalCap
				found = true
			}
		}
		if !found {
			return c.refuse("caps."+symbol, "the overlay caps %s, which %s does not list", symbol, c.Chain)
		}
	}
	if entry.BigBlocksAcknowledged {
		if c.BigBlocks == nil {
			return c.refuse("big_blocks_acknowledged", "only %s needs the deploying account switched to big blocks", BigBlockChain)
		}
		c.BigBlocks.Acknowledged = true
	}
	if entry.ProgramID != "" {
		if c.Solana == nil {
			return c.refuse("program_id", "only the Solana chain holds custody in a program")
		}
		c.Solana.ProgramID = entry.ProgramID
	}
	return nil
}
