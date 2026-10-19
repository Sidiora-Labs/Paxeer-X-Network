// Package seinetwork registers the `seictl network` verb tree, which
// manages SeiNetwork (seinetworks.sei.io/v1alpha1) custom resources via
// embedded presets. A SeiNetwork bootstraps a new chain through a genesis
// ceremony that mints the founding validator set. It is the genesis-
// validator half of the split that replaced the single `seictl
// nodedeployment` tree; the follower/full-node half is `seictl node`
// (package seinode).
//
// See design/WS-A-seictl-lld.md for the command surface and the
// admission-immutability caveats on spec.genesis / spec.replicas (§2.2a).
package seinetwork

import (
	"github.com/urfave/cli/v3"
)

// Cmd is the `seictl network` command tree.
var Cmd = cli.Command{
	Name:  "network",
	Usage: "Manage SeiNetwork custom resources via embedded presets",
	Commands: []*cli.Command{
		&applyCmd,
		&getCmd,
		&listCmd,
		&deleteCmd,
		&watchCmd,
	},
}
