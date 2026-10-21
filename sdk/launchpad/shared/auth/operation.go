package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

var tokenPattern = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,128}$`)

// OperationEnvelope v1 signs exact bounded JSON bytes. Clients must transmit those
// same bytes; JSON whitespace changes intentionally invalidate the authorization.
type OperationEnvelope struct {
	Version     int    `json:"version"`
	ChainID     string `json:"chainId"`
	Environment string `json:"environment"`
	Actor       string `json:"actor"`
	Action      string `json:"action"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	BodyHash    string `json:"bodyHash"`
	Nonce       string `json:"nonce"`
	IssuedAt    int64  `json:"issuedAt"`
	ExpiresAt   int64  `json:"expiresAt"`
}

func BodyDigest(body []byte) (string, error) {
	if len(body) > 1<<20 {
		return "", fmt.Errorf("body too large")
	}
	if err := rejectDuplicateKeys(body); err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var value any
	if decoder.Decode(&value) != nil {
		return "", fmt.Errorf("invalid json")
	}
	if decoder.Decode(&value) != io.EOF {
		return "", fmt.Errorf("trailing json")
	}
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:]), nil
}

func BodyDigestV2(body []byte) (string, error) {
	if len(body) == 0 {
		h := sha256.Sum256(body)
		return hex.EncodeToString(h[:]), nil
	}
	return BodyDigest(body)
}
func (e OperationEnvelope) Statement() (string, error) {
	normalized, err := NormalizeWallet(e.Actor)
	if err != nil || normalized != e.Actor {
		return "", fmt.Errorf("actor must be normalized")
	}
	parsed, err := url.ParseRequestURI(e.Path)
	if err != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || !strings.HasPrefix(e.Path, "/") || path.Clean(e.Path) != e.Path {
		return "", fmt.Errorf("noncanonical path")
	}
	if (e.Version != 1 && e.Version != 2) || e.ChainID != "125" || !tokenPattern.MatchString(e.Environment) || !tokenPattern.MatchString(e.Action) || e.Method != strings.ToUpper(e.Method) || !(e.Method == "POST" || e.Method == "PUT" || e.Method == "PATCH" || e.Method == "DELETE") || len(e.Nonce) != 64 || len(e.BodyHash) != 64 {
		return "", fmt.Errorf("invalid envelope")
	}
	for _, v := range []string{e.Nonce, e.BodyHash} {
		b, err := hex.DecodeString(v)
		if err != nil || len(b) != 32 || strings.ToLower(v) != v {
			return "", fmt.Errorf("invalid digest or nonce")
		}
	}
	if e.IssuedAt < 0 || e.ExpiresAt < 0 || e.ExpiresAt <= e.IssuedAt || e.ExpiresAt-e.IssuedAt > 300 {
		return "", fmt.Errorf("invalid lifetime")
	}
	data, _ := json.Marshal(e)
	if e.Version == 2 {
		return "KindleLaunch signed operation v2\n" + string(data), nil
	}
	return "KindleLaunch signed operation v1\n" + string(data), nil
}
func (e OperationEnvelope) Validate(now time.Time, environment, actor, action, method, requestPath string, body []byte) error {
	if _, err := e.Statement(); err != nil {
		return err
	}
	wallet, err := NormalizeWallet(actor)
	if err != nil {
		return err
	}
	digest, err := BodyDigest(body)
	if e.Version == 2 {
		if len(body) == 0 && e.Method != "DELETE" {
			return fmt.Errorf("empty body action")
		}
		digest, err = BodyDigestV2(body)
	}
	if err != nil {
		return err
	}
	if e.Environment != environment || e.Actor != wallet || e.Action != action || e.Method != method || e.Path != requestPath || e.BodyHash != digest {
		return fmt.Errorf("operation binding mismatch")
	}
	if e.IssuedAt > now.Unix()+30 || e.ExpiresAt <= now.Unix() {
		return fmt.Errorf("operation expired or future")
	}
	return nil
}

var ErrScopeDenied = errors.New("scope denied")

type Principal struct {
	Wallet    string   `json:"wallet"`
	SessionID string   `json:"sessionId"`
	Roles     []string `json:"roles"`
	TraceID   string   `json:"traceId"`
}
type Policy struct {
	Scope         string
	OwnerRequired bool
	RouteTemplate string
}
type PolicyRegistry struct {
	Version string
	Actions map[string]Policy
}

func DefaultPolicies() PolicyRegistry {
	return PolicyRegistry{Version: "1", Actions: map[string]Policy{"account.delete": {"owner", true, "/v1/me/deletion-requests"}, "profile.update": {"owner", true, "/v1/profiles/{wallet}"}, "preferences.update": {"owner", true, "/v1/me/preferences"}, "watchlist.update": {"owner", true, "/v1/me/watchlist/{poolAddress}"}, "wallet.link": {"owner", true, "/v1/me/wallet-links/{linkId}:verify"}}}
}
func (r PolicyRegistry) Authorize(p Principal, action, target string) error {
	policy, ok := r.Actions[action]
	if !ok || r.Version == "" {
		return ErrScopeDenied
	}
	wallet, err := NormalizeWallet(p.Wallet)
	if err != nil || p.SessionID == "" {
		return ErrScopeDenied
	}
	if policy.OwnerRequired {
		owner, err := NormalizeWallet(target)
		if err != nil || owner != wallet {
			return ErrScopeDenied
		}
	}
	for _, role := range p.Roles {
		if role == policy.Scope {
			return nil
		}
	}
	return ErrScopeDenied
}

func AccountOwnerPoliciesV2() (PolicyRegistry, map[string]ActionRoute) {
	policies := PolicyRegistry{Version: "2", Actions: map[string]Policy{}}
	routes := map[string]ActionRoute{}
	for _, route := range []ActionRoute{
		{"profile.update", "PATCH", "/v1/profiles/{wallet}"},
		{"preferences.update", "PATCH", "/v1/me/preferences"},
		{"watchlist.update", "PUT", "/v1/me/watchlist/{poolAddress}"},
		{"watchlist.remove", "DELETE", "/v1/me/watchlist/{poolAddress}"},
		{"wallet.link", "POST", "/v1/me/wallet-links/{linkId}:verify"},
		{"wallet.unlink", "DELETE", "/v1/me/wallet-links/{linkId}"},
		{"account.export", "POST", "/v1/me/exports"},
		{"account.delete", "POST", "/v1/me/deletion-requests"},
		{"device.register", "PUT", "/v1/me/devices/{deviceId}"},
		{"device.revoke", "DELETE", "/v1/me/devices/{deviceId}"},
	} {
		policies.Actions[route.Action] = Policy{Scope: "owner", OwnerRequired: true, RouteTemplate: route.Template}
		routes[route.Action] = route
	}
	return policies, routes
}

func AccountOwnerExtendedPoliciesV2() (PolicyRegistry, map[string]ActionRoute) {
	policies, routes := AccountOwnerPoliciesV2()
	for _, route := range []ActionRoute{{"profile.update", "PATCH", "/v2/profiles/{wallet}"}, {"preferences.update", "PATCH", "/v2/me/preferences"}} {
		policies.Actions[route.Action] = Policy{Scope: "owner", OwnerRequired: true, RouteTemplate: route.Template}
		routes[route.Action] = route
	}
	return policies, routes
}

// rejectDuplicateKeys prevents signed intent from differing across JSON parsers.
func rejectDuplicateKeys(body []byte) error {
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return fmt.Errorf("json depth")
		}
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("duplicate json key")
				}
				seen[name] = true
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("invalid json")
		}
		_, err = d.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing json")
	}
	return nil
}
