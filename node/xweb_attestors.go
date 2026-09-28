package app

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"

	xwebtypes "github.com/sidiora-labs/paxeer-network/modules/xweb/types"
)

// XWebThreshold is the number of distinct attestor signatures a web-search
// fulfilment needs once the initial attestor set is registered.
const XWebThreshold = uint32(3)

// xwebAttestor is one entry of the initial web-search attestor set as its
// operator supplied it: the EVM address its signatures recover to, the account
// its payouts are credited to and the compressed secp256k1 public key the
// credential envelopes addressed to it are sealed to.
type xwebAttestor struct {
	signer    string
	payout    string
	publicKey string
}

// XWebAttestors is the initial web-search attestor set, in ascending signer
// order. The module's own attestor validation derives each signer from its
// public key, so an entry whose key and address disagree stops the binary
// before it serves a block.
var XWebAttestors = mustXWebAttestors([]xwebAttestor{
	{
		signer:    "0x06070D6094c60b593001875c69F12479E0f19960",
		payout:    "pax1dfsyfa3y4f98azvfxmuc3wmmnm99lcajdkgpk2",
		publicKey: "02a8e98ff88820cda7fd73928b69eac35de668ceebc37a3b8683cdc1d7d776622f",
	},
	{
		signer:    "0x6Db02d954A01C9210DA9d1a61F353F518619bB61",
		payout:    "pax1q48swtar079l4nve5c3pxcw92tr9qrrhrv3g2d",
		publicKey: "033e9cfdfb42b2be69d4524490dffb8ef02ab797088abd6236e43a801c27a8944c",
	},
	{
		signer:    "0x969C135d2816dAbeE7f7d47d93b4A92A276028a1",
		payout:    "pax1q3t0u84rp9fhe9lf5a4snrq4mc57wqrmswppll",
		publicKey: "03323196cfa3a15889e3671ef05d2558acd97962d98fce1ee3e8a09a4b23e609bf",
	},
	{
		signer:    "0xa76e747d450802eF3DC4b1c803b2dA23ec702341",
		payout:    "pax1g3748rzewdxvuyhgrcm66p5jsachfq4sn4fmyg",
		publicKey: "03bb6366c6f29b84fa90d301ec0eb03baa85a0aeff440eb868ba2b98a03bead97f",
	},
})

// mustXWebAttestors decodes the supplied entries and proves the set the plan
// registers through the module's own validation: every public key derives to
// its signer, no signer repeats, the threshold is a majority of the set and the
// signers ascend.
func mustXWebAttestors(sources []xwebAttestor) []xwebtypes.Attestor {
	attestors := make([]xwebtypes.Attestor, 0, len(sources))
	for _, source := range sources {
		attestor, err := source.decode()
		if err != nil {
			panic(fmt.Errorf("the web-search attestor %s: %w", source.signer, err))
		}
		attestors = append(attestors, attestor)
	}
	set := xwebtypes.AttestorSet{Attestors: attestors, Threshold: XWebThreshold}
	if err := set.Validate(); err != nil {
		panic(fmt.Errorf("the web-search attestor set: %w", err))
	}
	for i := 1; i < len(attestors); i++ {
		if bytes.Compare(attestors[i-1].Signer[:], attestors[i].Signer[:]) >= 0 {
			panic(fmt.Errorf("the web-search attestor %s does not follow %s in ascending signer order",
				attestors[i].Signer.Hex(), attestors[i-1].Signer.Hex()))
		}
	}
	return attestors
}

func (a xwebAttestor) decode() (xwebtypes.Attestor, error) {
	signer, err := decodeAttestorHex(a.signer, len(xwebtypes.Address20{}))
	if err != nil {
		return xwebtypes.Attestor{}, fmt.Errorf("signer: %w", err)
	}
	publicKey, err := decodeAttestorHex(a.publicKey, xwebtypes.EnvelopeKeyLength)
	if err != nil {
		return xwebtypes.Attestor{}, fmt.Errorf("public key: %w", err)
	}
	attestor := xwebtypes.Attestor{Payout: a.payout, PublicKey: publicKey}
	copy(attestor.Signer[:], signer)
	if err := attestor.Validate(); err != nil {
		return xwebtypes.Attestor{}, err
	}
	return attestor, nil
}

func decodeAttestorHex(text string, length int) ([]byte, error) {
	decoded, err := hex.DecodeString(strings.TrimPrefix(text, "0x"))
	if err != nil {
		return nil, err
	}
	if len(decoded) != length {
		return nil, fmt.Errorf("%d bytes, want %d", len(decoded), length)
	}
	return decoded, nil
}
