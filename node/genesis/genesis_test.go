package genesis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// SHA-256(genDoc)
var expectedGenesisDigests = map[string]string{
	"arctic-1":   "554e0fc2b7d2a654a96c8763eb615394f99245740e84ca3a9f52d101462969c0",
	"atlantic-2": "9a15bea2c4d0ce23da3c1053fb2b4c838d64843c7178fe487b66bed9815f61eb",
	"pacific-1":  "f45e1add09fd37d7eed07eb94a528fa69d7fd0587603541bc5d99d74affda15a",
}

func genesisDocDigest(chainID string) ([]byte, error) {
	genDoc, err := EmbeddedGenesisDoc(chainID)
	if err != nil {
		return nil, err
	}
	ser, err := json.Marshal(genDoc)
	if err != nil {
		return nil, fmt.Errorf("marshaling genesis doc: %w", err)
	}
	hash := sha256.Sum256(ser)
	return hash[:], nil
}

func TestPrintGenesisDigestsForUpdate(t *testing.T) {
	if os.Getenv("UPDATE_GENESIS_DIGESTS") == "" {
		t.Skip("run with UPDATE_GENESIS_DIGESTS=1 to print expected digests after adding or modifying chains/*.json")
	}
	ids, err := WellKnownChainIDs()
	require.NoError(t, err)
	fmt.Println("// Copy the following into expectedGenesisDigests:")
	fmt.Println("var expectedGenesisDigests = map[string]string{")
	for _, chainID := range ids {
		digest, err := genesisDocDigest(chainID)
		require.NoError(t, err)
		fmt.Printf("\t%q: %q,\n", chainID, hex.EncodeToString(digest))
	}
	fmt.Println("}")
}

func TestEmbeddedGenesisDigests(t *testing.T) {
	ids, err := WellKnownChainIDs()
	require.NoError(t, err)
	for _, chainID := range ids {
		t.Run(chainID, func(t *testing.T) {
			digest, err := genesisDocDigest(chainID)
			require.NoError(t, err)
			got := hex.EncodeToString(digest)
			require.Equal(t, expectedGenesisDigests[chainID], got, "embedded genesis %s.json was modified; update expected digest or restore file", chainID)
		})
	}
}

func TestWellKnownChainIDs(t *testing.T) {
	ids, err := WellKnownChainIDs()
	require.NoError(t, err)
	for chainID := range expectedGenesisDigests {
		require.Contains(t, ids, chainID)
	}
}

func TestEmbeddedGenesisDoc(t *testing.T) {
	ids, err := WellKnownChainIDs()
	require.NoError(t, err)
	for _, chainID := range ids {
		genDoc, err := EmbeddedGenesisDoc(chainID)
		require.NoError(t, err)
		require.NotNil(t, genDoc)
		require.Equal(t, chainID, genDoc.ChainID)
	}
}

func TestEmbeddedGenesisUnknownChain(t *testing.T) {
	_, err := EmbeddedGenesis("unknown")
	require.ErrorContains(t, err, "unknown chain-id")

	_, err = EmbeddedGenesisDoc("unknown")
	require.Error(t, err)
}
