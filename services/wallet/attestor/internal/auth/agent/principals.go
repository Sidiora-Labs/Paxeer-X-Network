package agent

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

var ErrPrincipalFile = errors.New("agent: principal file invalid")

type principalJSON struct {
	DID          string   `json:"did,omitempty"`
	OwnerSubject string   `json:"owner_subject,omitempty"`
	PublicKey    string   `json:"public_key"`
	Frozen       bool     `json:"frozen"`
	KeyIDs       []string `json:"key_ids"`
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

func ParsePrincipals(raw []byte) (*StaticPrincipals, error) { return parsePrincipals(raw, false) }

func parsePrincipals(raw []byte, allowEmpty bool) (*StaticPrincipals, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var entries []principalJSON
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPrincipalFile, err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing principal data", ErrPrincipalFile)
	}
	if len(entries) > 65_536 {
		return nil, ErrPrincipalFile
	}
	dids := make(map[string]bool)
	out := &StaticPrincipals{byKey: make(map[[ed25519.PublicKeySize]byte]Principal, len(entries))}
	for i, e := range entries {
		key, err := hex.DecodeString(strings.TrimPrefix(e.PublicKey, "0x"))
		if err != nil || len(key) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w: entry %d: public_key must be 32 hex bytes", ErrPrincipalFile, i)
		}
		if strings.TrimSpace(e.OwnerSubject) != e.OwnerSubject || len(e.OwnerSubject) > 256 {
			return nil, ErrPrincipalFile
		}
		if e.DID != "" {
			if !regexp.MustCompile(`^did:matrix:[A-Za-z0-9_-]{1,128}:[0-9a-f]{16}$`).MatchString(e.DID) ||
				!strings.HasSuffix(e.DID, ":"+hex.EncodeToString(key[:8])) || dids[e.DID] {
				return nil, fmt.Errorf("%w: entry %d: DID differs from registered public key", ErrPrincipalFile, i)
			}
			dids[e.DID] = true
		}
		var pub [ed25519.PublicKeySize]byte
		copy(pub[:], key)
		if _, dup := out.byKey[pub]; dup {
			return nil, fmt.Errorf("%w: entry %d: duplicate public key", ErrPrincipalFile, i)
		}
		if len(e.KeyIDs) == 0 && !allowEmpty {
			return nil, fmt.Errorf("%w: entry %d: key_ids is empty", ErrPrincipalFile, i)
		}
		for _, id := range e.KeyIDs {
			if id == "" {
				return nil, fmt.Errorf("%w: entry %d: empty key id", ErrPrincipalFile, i)
			}
		}
		out.byKey[pub] = Principal{DID: e.DID, OwnerSubject: e.OwnerSubject, PublicKey: pub, Frozen: e.Frozen, KeyIDs: append([]string(nil), e.KeyIDs...)}
	}
	return out, nil
}

func LoadPrincipals(path string) (*StaticPrincipals, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPrincipalFile, err)
	}
	principals, err := ParsePrincipals(raw)
	if err != nil {
		return nil, err
	}
	for _, principal := range principals.byKey {
		if principal.OwnerSubject != "" {
			return nil, ErrPrincipalFile
		}
	}
	return principals, nil
}
