package sequencer_test

import (
	"bytes"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// seqConfigOptions loads a TOML app config into viper, the AppOptions paxd reads at start.
func seqConfigOptions(t *testing.T, toml string) *viper.Viper {
	t.Helper()
	v := viper.New()
	v.SetConfigType("toml")
	require.NoError(t, v.ReadConfig(strings.NewReader(toml)))
	return v
}

func TestSequencerConfigDefaultsSequential(t *testing.T) {
	def := sequencer.DefaultConfig()
	require.Equal(t, sequencer.Config{Mode: sequencer.ModeSequential}, def)
	require.Equal(t, "sequential", def.Mode.String())
	require.NoError(t, def.Validate())

	cfg, err := sequencer.ReadConfig(viper.New())
	require.NoError(t, err)
	require.Equal(t, def, cfg)

	// The rendered app.toml section reads back as the defaults.
	tmpl, err := template.New("app").Parse(sequencer.ConfigTemplate)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, tmpl.Execute(&buf, nil))
	require.Contains(t, buf.String(), "[sequencer]")
	cfg, err = sequencer.ReadConfig(seqConfigOptions(t, buf.String()))
	require.NoError(t, err)
	require.Equal(t, def, cfg)

	// An enabled section without a mode runs Sequential.
	cfg, err = sequencer.ReadConfig(seqConfigOptions(t, `
[sequencer]
enable = true
wal-dir = "data/sequencer-wal"
receipt-key-file = "config/receipt_key.hex"
standby-address = "localhost:26700"
replication-timeout = "250ms"
`))
	require.NoError(t, err)
	require.Equal(t, sequencer.Config{
		Enable:             true,
		Mode:               sequencer.ModeSequential,
		WALDir:             "data/sequencer-wal",
		ReceiptKeyFile:     "config/receipt_key.hex",
		StandbyAddress:     "localhost:26700",
		ReplicationTimeout: 250 * time.Millisecond,
	}, cfg)

	m, err := sequencer.ParseMode("sequential")
	require.NoError(t, err)
	require.Equal(t, sequencer.ModeSequential, m)
	for _, name := range []string{"", "Sequential", "micro-batch", "occ"} {
		_, err := sequencer.ParseMode(name)
		require.ErrorIs(t, err, sequencer.ErrConfigMode, name)
	}
	_, err = sequencer.ReadConfig(seqConfigOptions(t, "[sequencer]\nmode = \"micro-batch\"\n"))
	require.ErrorIs(t, err, sequencer.ErrConfigMode)

	bad := sequencer.Config{Enable: true, Mode: sequencer.Mode(7), WALDir: "w", ReceiptKeyFile: "k"}
	require.ErrorIs(t, bad.Validate(), sequencer.ErrConfigMode)
	require.Equal(t, "Mode(7)", sequencer.Mode(7).String())
}

func TestSequencerConfigRequiresWALAndKey(t *testing.T) {
	valid := sequencer.Config{
		Enable:             true,
		Mode:               sequencer.ModeSequential,
		WALDir:             "data/sequencer-wal",
		ReceiptKeyFile:     "config/receipt_key.hex",
		StandbyAddress:     "localhost:26700",
		ReplicationTimeout: time.Second,
	}
	require.NoError(t, valid.Validate())

	noWAL := valid
	noWAL.WALDir = ""
	require.ErrorIs(t, noWAL.Validate(), sequencer.ErrConfigWALDir)

	noKey := valid
	noKey.ReceiptKeyFile = ""
	require.ErrorIs(t, noKey.Validate(), sequencer.ErrConfigReceiptKeyFile)

	neither := valid
	neither.WALDir, neither.ReceiptKeyFile = "", ""
	require.ErrorIs(t, neither.Validate(), sequencer.ErrConfigWALDir)

	// A disabled section is not the sequencer's and is not refused.
	disabled := neither
	disabled.Enable = false
	require.NoError(t, disabled.Validate())

	noStandby := valid
	noStandby.StandbyAddress, noStandby.ReplicationTimeout = "", 0
	require.NoError(t, noStandby.Validate())

	negative := valid
	negative.ReplicationTimeout = -time.Millisecond
	require.ErrorIs(t, negative.Validate(), sequencer.ErrConfigReplicationTimeout)

	zeroTimeout := valid
	zeroTimeout.ReplicationTimeout = 0
	require.ErrorIs(t, zeroTimeout.Validate(), sequencer.ErrConfigReplicationTimeout)

	badAddr := valid
	badAddr.StandbyAddress = "localhost"
	require.ErrorIs(t, badAddr.Validate(), sequencer.ErrConfigStandbyAddress)

	// The same refusals surface from the section paxd reads at start.
	_, err := sequencer.ReadConfig(seqConfigOptions(t, `
[sequencer]
enable = true
receipt-key-file = "config/receipt_key.hex"
`))
	require.ErrorIs(t, err, sequencer.ErrConfigWALDir)

	_, err = sequencer.ReadConfig(seqConfigOptions(t, `
[sequencer]
enable = true
wal-dir = "data/sequencer-wal"
`))
	require.ErrorIs(t, err, sequencer.ErrConfigReceiptKeyFile)

	_, err = sequencer.ReadConfig(seqConfigOptions(t, `
[sequencer]
enable = "maybe"
`))
	require.ErrorIs(t, err, sequencer.ErrConfigValue)

	_, err = sequencer.ReadConfig(seqConfigOptions(t, `
[sequencer]
replication-timeout = "soon"
`))
	require.ErrorIs(t, err, sequencer.ErrConfigValue)
}
