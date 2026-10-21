package lxwire

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	DIDPrefix             = "did:layerx:"
	MaxAccountNameBytes   = 512
	accountIDDomain       = "LX:ACCOUNT:v1"
	agentAccountPrefix    = "agent:"
	liquidityPrefix       = "system:liquidity:"
	assetMarker           = ":asset:"
	mainSuffix            = ":main"
	assetIdentifierLength = 64
)

var (
	ErrAccountName = errors.New("lxwire: account name is outside the native namespaces")
	ErrDID         = errors.New("lxwire: did is not did:layerx followed by 64 lowercase hex characters")
)

func DIDFromKey(pub [32]byte) string {
	return DIDPrefix + hex.EncodeToString(pub[:])
}

func KeyFromDID(did string) ([32]byte, error) {
	var key [32]byte
	if !strings.HasPrefix(did, DIDPrefix) {
		return key, ErrDID
	}
	body := did[len(DIDPrefix):]
	if len(body) != 64 || !lowerHex(body) {
		return key, ErrDID
	}
	if _, err := hex.Decode(key[:], []byte(body)); err != nil {
		return key, ErrDID
	}
	return key, nil
}

func MainAccountName(did string) string {
	return agentAccountPrefix + did + mainSuffix
}

func AssetAccountName(did string, asset, nativeAsset [32]byte) (string, error) {
	if did == "" || len(did) > MaxDIDBytes || strings.HasPrefix(did, ":") || strings.HasSuffix(did, ":") ||
		strings.Contains(did, "::") || strings.Contains(did, assetMarker) {
		return "", ErrAccountName
	}
	for i := 0; i < len(did); i++ {
		if !nameByte(did[i]) {
			return "", ErrAccountName
		}
	}
	name := MainAccountName(did)
	if asset != nativeAsset {
		name = agentAccountPrefix + did + assetMarker + hex.EncodeToString(asset[:])
	}
	if err := parseAccountName(name); err != nil {
		return "", err
	}
	return name, nil
}

func AccountID(name []byte) ([32]byte, error) {
	var out [32]byte
	text := string(name)
	if err := parseAccountName(text); err != nil {
		return out, err
	}
	if err := checkProtocolThreeName(text); err != nil {
		return out, err
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(name)))
	h := sha256.New()
	h.Write([]byte(accountIDDomain))
	h.Write(length[:])
	h.Write(name)
	h.Sum(out[:0])
	return out, nil
}

func lowerHex(text string) bool {
	for i := 0; i < len(text); i++ {
		c := text[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func nameByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' || c == ':'
}

func parseAccountName(name string) error {
	if name == "" || len(name) > MaxAccountNameBytes {
		return ErrAccountName
	}
	if agent, ok := strings.CutPrefix(name, agentAccountPrefix); ok {
		return parseAgentAccount(agent)
	}
	switch name {
	case "system:insurance", "system:fees", "system:paxeer-reserve", "system:paxeer-withdrawals":
		return nil
	}
	if market, ok := strings.CutPrefix(name, liquidityPrefix); ok {
		if market == "" {
			return ErrAccountName
		}
		return nil
	}
	return ErrAccountName
}

func parseAgentAccount(agent string) error {
	if did, asset, ok := strings.Cut(agent, assetMarker); ok {
		if did == "" || len(did) > MaxDIDBytes || len(asset) != assetIdentifierLength || !lowerHex(asset) {
			return ErrAccountName
		}
		return nil
	}
	if did, ok := strings.CutSuffix(agent, mainSuffix); ok {
		if did == "" || len(did) > MaxDIDBytes {
			return ErrAccountName
		}
		return nil
	}
	for _, marker := range []string{":budget:", ":escrow:", ":margin:"} {
		if index := strings.LastIndex(agent, marker); index >= 0 {
			did, component := agent[:index], agent[index+len(marker):]
			if did == "" || len(did) > MaxDIDBytes || component == "" {
				return ErrAccountName
			}
			return nil
		}
	}
	return ErrAccountName
}

func checkProtocolThreeName(name string) error {
	for i := 0; i < len(name); i++ {
		if !nameByte(name[i]) {
			return ErrAccountName
		}
	}
	validTail := true
	if agent, ok := strings.CutPrefix(name, agentAccountPrefix); ok {
		validTail = strings.HasSuffix(agent, mainSuffix)
		if !validTail {
			if did, asset, found := strings.Cut(agent, assetMarker); found {
				validTail = did != "" && len(asset) == assetIdentifierLength && lowerHex(asset)
			}
		}
		if !validTail {
			for _, marker := range []string{":budget:", ":escrow:", ":stream:", ":margin:"} {
				did, tail, found := strings.Cut(agent, marker)
				if found && did != "" && tail != "" && !strings.Contains(tail, ":") {
					validTail = true
					break
				}
			}
		}
	} else if tail, ok := strings.CutPrefix(name, liquidityPrefix); ok {
		validTail = !strings.Contains(tail, ":")
	}
	if !validTail || strings.HasPrefix(name, ":") || strings.HasSuffix(name, ":") || strings.Contains(name, "::") {
		return ErrAccountName
	}
	return nil
}
