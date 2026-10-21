// Command opvector produces operation-envelope test vectors with the production
// Go code (shared/auth OperationEnvelope.Statement + go-ethereum personal_sign),
// and verifies TS-produced headers through the same server-side checks.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
)

const keyHex = "4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318"

type vector struct {
	Name      string                        `json:"name"`
	Body      string                        `json:"body"`
	Envelope  sharedauth.OperationEnvelope  `json:"envelope"`
	Statement string                        `json:"statement"`
	Encoded   string                        `json:"encoded"`
	Signature string                        `json:"signature"`
}

func sign(statement string) string {
	key, err := crypto.HexToECDSA(keyHex)
	if err != nil {
		panic(err)
	}
	sig, err := crypto.Sign(accounts.TextHash([]byte(statement)), key)
	if err != nil {
		panic(err)
	}
	sig[64] += 27
	return "0x" + hex.EncodeToString(sig)
}

func digest(body string) string {
	d, err := sharedauth.BodyDigestV2([]byte(body))
	if err != nil {
		panic(fmt.Sprintf("digest %q: %v", body, err))
	}
	return d
}

// serverCheck mirrors OperationAuthorizer.AuthorizeRequest's envelope steps.
func serverCheck(encoded, signature, body, environment, actor, action, method, path string, now time.Time) error {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("header base64: %w", err)
	}
	var env sharedauth.OperationEnvelope
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&env) != nil || d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("header json")
	}
	statement, err := env.Statement()
	if err != nil {
		return fmt.Errorf("statement: %w", err)
	}
	if _, err = sharedauth.VerifyEOA(actor, statement, signature, nil); err != nil {
		return fmt.Errorf("signature: %w", err)
	}
	return env.Validate(now, environment, actor, action, method, path, []byte(body))
}

func main() {
	key, _ := crypto.HexToECDSA(keyHex)
	actor, _ := sharedauth.NormalizeWallet(crypto.PubkeyToAddress(key.PublicKey).Hex())
	if len(os.Args) > 1 && os.Args[1] == "verify" {
		var in []struct {
			Name, Encoded, Signature, Body, Environment, Action, Method, Path string
			IssuedAt int64
		}
		if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
			panic(err)
		}
		failed := false
		for _, v := range in {
			err := serverCheck(v.Encoded, v.Signature, v.Body, v.Environment, actor, v.Action, v.Method, v.Path, time.Unix(v.IssuedAt+1, 0))
			fmt.Printf("%-28s %v\n", v.Name, map[bool]string{true: "ACCEPTED", false: fmt.Sprint("REJECTED ", err)}[err == nil])
			failed = failed || err != nil
		}
		if failed {
			os.Exit(1)
		}
		return
	}
	pool := "0xce20da50f92d21d8bee497910c782e9aa25b6263"
	nonce := "9f2c4e1a7b3d5f60819a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f70"
	issued := int64(1_791_878_400)
	cases := []struct{ name, action, method, path, body string; version int; env string }{
		{"v3-profile-patch", "profile.update", "PATCH", "/v3/profiles/" + actor, `{"expectedRevision":"0","displayName":"Ada <Lovelace> & co","bio":"line sep","website":null,"socials":{"twitter":"ada"}}`, 2, "production"},
		{"v3-preferences-patch", "preferences.update", "PATCH", "/v3/me/preferences", `{"expectedRevision":"3","theme":"dark"}`, 2, "production"},
		{"v1-profile-patch", "profile.update", "PATCH", "/v1/profiles/" + actor, `{"expectedRevision":"7","displayName":"Ada"}`, 2, "production"},
		{"v1-watchlist-put", "watchlist.update", "PUT", "/v1/me/watchlist/" + pool, `{"alertEnabled":false}`, 2, "production"},
		{"v1-watchlist-delete", "watchlist.remove", "DELETE", "/v1/me/watchlist/" + pool, ``, 2, "production"},
		{"html-escaped-path", "profile.update", "PATCH", "/v1/a&b=c@d", `{"a":1}`, 2, "production"},
		{"livestream-v1-create", "stream.create", "POST", "/v2/streams", `{"poolAddress":"` + pool + `","title":"Launch"}`, 1, "production"},
	}
	out := []vector{}
	for _, c := range cases {
		env := sharedauth.OperationEnvelope{Version: c.version, ChainID: "125", Environment: c.env, Actor: actor, Action: c.action, Method: c.method, Path: c.path, BodyHash: digest(c.body), Nonce: nonce, IssuedAt: issued, ExpiresAt: issued + 300}
		statement, err := env.Statement()
		if err != nil {
			panic(fmt.Sprintf("%s: %v", c.name, err))
		}
		raw, _ := json.Marshal(env)
		encoded := base64.RawURLEncoding.EncodeToString(raw)
		signature := sign(statement)
		if err := serverCheck(encoded, signature, c.body, c.env, actor, c.action, c.method, c.path, time.Unix(issued+1, 0)); err != nil {
			panic(fmt.Sprintf("%s: self-check: %v", c.name, err))
		}
		out = append(out, vector{Name: c.name, Body: c.body, Envelope: env, Statement: statement, Encoded: encoded, Signature: signature})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{"generator": "shared/auth OperationEnvelope.Statement + go-ethereum crypto.Sign(accounts.TextHash)", "privateKey": "0x" + keyHex, "actor": actor, "vectors": out})
}
