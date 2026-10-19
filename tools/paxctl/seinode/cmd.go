// Package seinode registers the `seictl node` verb tree, which manages
// standalone SeiNode (seinodes.sei.io/v1alpha1) custom resources via
// embedded presets. It is the follower/full-node half of the split that
// replaced the single `seictl nodedeployment` tree; the genesis-validator
// half is `seictl network` (package seinetwork).
//
// See design/WS-A-seictl-lld.md for the command surface, the object-label
// producer contract (§2.2b), and the peer auto-wiring rail (§3).
package seinode

import (
	"github.com/urfave/cli/v3"
)

// Cmd is the `seictl node` command tree.
var Cmd = cli.Command{
	Name:  "node",
	Usage: "Manage standalone SeiNode custom resources via embedded presets",
	Commands: []*cli.Command{
		&applyCmd,
		&getCmd,
		&listCmd,
		&deleteCmd,
		&watchCmd,
	},
}
