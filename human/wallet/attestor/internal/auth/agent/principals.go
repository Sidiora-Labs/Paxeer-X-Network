package agent

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

var ErrPrincipalFile = errors.New("agent: principal file invalid")

type principalJSON struct {
	PublicKey string   `json:"public_key"`
	Frozen    bool     `json:"frozen"`
	KeyIDs    []string `json:"key_ids"`
}

type StaticPrincipals struct {
	byKey map[[ed25519.PublicKeySize]byte]Principal
}

func (s *StaticPrincipals) Lookup(pub [ed25519.PublicKeySize]byte) (Principal, bool) {
	p, ok := s.byKey[pub]
	if !ok {
		return Principal{}, false
	}
	p.KeyIDs = append([]string(nil), p.KeyIDs...)
	return p, true
}

func (s *StaticPrincipals) Len() int { return len(s.byKey) }

func ParsePrincipals(raw []byte) (*StaticPrincipals, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var entries []principalJSON
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPrincipalFile, err)
	}
	out := &StaticPrincipals{byKey: make(map[[ed25519.PublicKeySize]byte]Principal, len(entries))}
	for i, e := range entries {
		key, err := hex.DecodeString(strings.TrimPrefix(e.PublicKey, "0x"))
		if err != nil || len(key) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w: entry %d: public_key must be 32 hex bytes", ErrPrincipalFile, i)
		}
		var pub [ed25519.PublicKeySize]byte
		copy(pub[:], key)
		if _, dup := out.byKey[pub]; dup {
			return nil, fmt.Errorf("%w: entry %d: duplicate public key", ErrPrincipalFile, i)
		}
		if len(e.KeyIDs) == 0 {
			return nil, fmt.Errorf("%w: entry %d: key_ids is empty", ErrPrincipalFile, i)
		}
		for _, id := range e.KeyIDs {
			if id == "" {
				return nil, fmt.Errorf("%w: entry %d: empty key id", ErrPrincipalFile, i)
			}
		}
		out.byKey[pub] = Principal{PublicKey: pub, Frozen: e.Frozen, KeyIDs: append([]string(nil), e.KeyIDs...)}
	}
	return out, nil
}

func LoadPrincipals(path string) (*StaticPrincipals, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPrincipalFile, err)
	}
	return ParsePrincipals(raw)
}
