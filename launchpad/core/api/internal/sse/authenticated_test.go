package sse

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
)

func TestSignedCursorEventFraming(t *testing.T) {
	now := time.Now()
	key := bytes.Repeat([]byte{5}, 32)
	c := broker.CursorV2{FormatVersion: 1, Scope: broker.CursorScopeV2{Chain: "125", Subject: "anonymous", SessionID: "public", PermissionEpoch: "0", ProjectionEpoch: "generation", SchemaMajor: 2, FilterHash: "topics", SnapshotID: "snapshot"}, Offsets: map[string]string{"partition": "9007199254740993"}, IssuedAt: now.Unix(), ExpiresAt: now.Add(72 * time.Hour).Unix()}
	token, err := broker.SignCursorV2(c, key, now)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := EncodeEvent([]byte(`{"sequence":"9007199254740993"}`), token)
	if err != nil || string(frame) != "id: "+token+"\ndata: {\"sequence\":\"9007199254740993\"}\n\n" {
		t.Fatal(string(frame), err)
	}
	id := strings.TrimPrefix(strings.SplitN(string(frame), "\n", 2)[0], "id: ")
	decoded, err := broker.VerifyCursorV2(id, key, c.Scope, now)
	if err != nil || decoded.Offsets["partition"] != "9007199254740993" {
		t.Fatal(decoded, err)
	}
	for _, cursor := range []string{"a\nid: forged", "a\rdata: forged", strings.Repeat("a", 2049)} {
		if _, err := EncodeEvent([]byte(`{}`), cursor); err == nil {
			t.Fatal("cursor injection admitted")
		}
	}
	if _, err := EncodeEvent([]byte("{\n\"value\":1}"), "valid"); err == nil {
		t.Fatal("multiline event injection admitted")
	}
	if _, err := EncodeEvent([]byte("invalid"), "valid"); err == nil {
		t.Fatal("invalid data admitted")
	}
}
