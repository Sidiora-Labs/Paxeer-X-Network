package auth

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"strings"
	"unicode/utf8"
)

// DecodeIdentityRequest enforces the frozen closed request object and required fields.
// Route adapters merge validated path parameters before decoding combined request types.
func DecodeIdentityRequest(r io.Reader, dest any) error {
	raw, e := io.ReadAll(io.LimitReader(r, 16385))
	if e != nil || len(raw) > 16384 {
		return invalid()
	}
	var required []string
	switch dest.(type) {
	case *ChallengeRequest:
		required = []string{"wallet", "origin"}
	case *LoginRequest:
		required = []string{"challengeId", "proof", "deviceLabel"}
	case *WalletLinkChallengeRequest:
		required = []string{"wallet", "chainId", "visibility"}
	case *WalletLinkVerifyRequest:
		required = []string{"linkId", "challengeId", "proof"}
	case *WalletLinkRemoveRequest:
		required = []string{"linkId", "reauthProof"}
	case *RefreshRequest:
		required = []string{}
	default:
		return invalid()
	}
	if validateJSONKeys(json.NewDecoder(bytes.NewReader(raw))) != nil {
		return invalid()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return invalid()
	}
	allowed := map[string]bool{}
	for _, k := range required {
		allowed[k] = true
	}
	for k := range fields {
		if !allowed[k] {
			return identityError("FIELD_UNKNOWN", 422)
		}
	}
	for _, k := range required {
		v, ok := fields[k]
		if !ok || bytes.Equal(v, []byte("null")) {
			return invalid()
		}
	}
	if p, ok := fields["proof"]; ok {
		var proof map[string]json.RawMessage
		if json.Unmarshal(p, &proof) != nil {
			return invalid()
		}
		for k := range proof {
			if k != "kind" && k != "wallet" && k != "signature" {
				return identityError("FIELD_UNKNOWN", 422)
			}
		}
		for _, k := range []string{"kind", "wallet", "signature"} {
			v, ok := proof[k]
			if !ok || bytes.Equal(v, []byte("null")) {
				return invalid()
			}
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(dest); e != nil {
		if strings.HasPrefix(e.Error(), "json: unknown field") {
			return identityError("FIELD_UNKNOWN", 422)
		}
		return invalid()
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return invalid()
	}
	switch v := dest.(type) {
	case *ChallengeRequest:
		if _, e := normalizeWallet(v.Wallet); e != nil {
			return e
		}
		u, e := url.Parse(v.Origin)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || len(v.Origin) > 2048 {
			return invalid()
		}
	case *LoginRequest:
		if !validUUID(v.ChallengeID) || !utf8.ValidString(v.DeviceLabel) || utf8.RuneCountInString(v.DeviceLabel) > 80 {
			return invalid()
		}
		return validateWalletProof(v.Proof)
	case *WalletLinkChallengeRequest:
		if _, e := normalizeWallet(v.Wallet); e != nil {
			return e
		}
		if v.ChainID != "125" || (v.Visibility != "private" && v.Visibility != "public_optin") {
			return invalid()
		}
	case *WalletLinkVerifyRequest:
		if !validUUID(v.LinkID) || !validUUID(v.ChallengeID) {
			return invalid()
		}
		return validateWalletProof(v.Proof)
	case *WalletLinkRemoveRequest:
		if !validUUID(v.LinkID) || len(v.ReauthProof) < 1 || len(v.ReauthProof) > 256 {
			return invalid()
		}
	}
	return nil
}
func validateWalletProof(p WalletProof) error {
	if _, e := normalizeWallet(p.Wallet); e != nil {
		return e
	}
	if len(p.Signature) < 2 || p.Signature[:2] != "0x" {
		return invalid()
	}
	if _, e := hex.DecodeString(p.Signature[2:]); e != nil {
		return invalid()
	}
	switch p.Kind {
	case "eoa_eip191":
		if len(p.Signature) != 132 {
			return invalid()
		}
	case "erc1271":
		if len(p.Signature) > 8194 {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}
func validateJSONKeys(d *json.Decoder) error {
	if e := jsonValue(d); e != nil {
		return e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return invalid()
	}
	return nil
}
func jsonValue(d *json.Decoder) error {
	tok, e := d.Token()
	if e != nil {
		return e
	}
	if delim, ok := tok.(json.Delim); ok {
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return e
				}
				k, ok := key.(string)
				if !ok || seen[k] {
					return invalid()
				}
				seen[k] = true
				if e = jsonValue(d); e != nil {
					return e
				}
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return invalid()
			}
		case '[':
			for d.More() {
				if e = jsonValue(d); e != nil {
					return e
				}
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	return nil
}
