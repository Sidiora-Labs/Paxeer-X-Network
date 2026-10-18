package metadata

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProductionFrozenResponses(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	id := "11111111-1111-1111-1111-111111111111"
	a := AssetDomain{ID: id, Owner: "0x" + strings.Repeat("1", 40), Hash: strings.Repeat("a", 64), MIME: "image/png", State: "UPLOADING", Size: 12}
	emit := func(schema string, v any, e error) {
		t.Helper()
		if e != nil {
			t.Fatal(schema, e)
		}
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		t.Log("FROZEN_RESPONSE " + schema + " " + string(b))
	}
	create, err := ParseUploadCreate([]byte(`{"mediaType":"image/png","bytes":12,"sha256":"0x` + strings.Repeat("a", 64) + `"}`))
	emit("UploadCreateBody", create, err)
	completeBody, err := ParseUploadComplete([]byte(`{"sha256":"0x` + strings.Repeat("a", 64) + `"}`))
	emit("UploadCompleteBody", completeBody, err)
	v, e := UploadCreateResponse(id, a, "https://upload.example/private?signature=fixture", now.Add(time.Minute), now)
	emit("UploadCreateResponse", v, e)
	a.State = "VALIDATING"
	status, e := AssetResponse(id, a, now)
	emit("AssetGetResponse", status, e)
	complete, e := UploadCompleteResponse(id, a, now)
	emit("UploadCompleteResponse", complete, e)
	url := "https://cdn.example/approved/" + a.Hash + "/256/" + a.Hash + ".png"
	a.State = "APPROVED"
	a.ApprovedURL = &url
	status, e = AssetResponse(id, a, now)
	emit("AssetGetResponse", status, e)
	meta, e := MetadataResponse(id, a.Owner, "", "", "verified creator description", "8", nil, nil, nil, nil, now)
	emit("MetadataGetResponse", meta, e)
	patch, e := MetadataPatchResponse(id, meta.Data.Token, "8", now)
	emit("MetadataPatchResponse", patch, e)
	if meta.Data.Token.Decimals != nil || meta.Data.Token.TotalSupplyRaw != nil || meta.Data.Token.DecimalsQuality != "unavailable" || meta.Data.Token.MetadataRevision != meta.Data.Revision {
		t.Fatal(meta)
	}
	for _, state := range []string{"CREATED", "DELETED", "UNKNOWN"} {
		a.State = state
		if _, e := AssetToWire(a); e == nil {
			t.Fatal(state)
		}
	}
	a.State = "VALIDATING"
	if _, e := AssetToWire(a); e == nil {
		t.Fatal("quarantine URL leaked")
	}
	a.State = "APPROVED"
	a.ApprovedURL = nil
	if _, e := AssetToWire(a); e == nil {
		t.Fatal("fabricated approved delivery")
	}
	for _, desc := range []string{"", strings.Repeat("x", 257)} {
		if _, e := MetadataResponse(id, a.Owner, "", "", desc, "8", nil, nil, nil, nil, now); e != ErrUnrepresentable {
			t.Fatal(e)
		}
	}
	a.ApprovedURL = &url
	a.Blocked = true
	if _, e := AssetToWire(a); e == nil {
		t.Fatal("moderation leaked")
	}
}
func TestProductionFrozenUploadRequests(t *testing.T) {
	hash := "0x" + strings.Repeat("a", 64)
	raw := `{"mediaType":"image/png","bytes":12,"sha256":"` + hash + `"}`
	if _, e := ParseUploadCreate([]byte(raw)); e != nil {
		t.Fatal(e)
	}
	if _, e := ParseUploadComplete([]byte(`{"sha256":"` + hash + `"}`)); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(raw, `"bytes":12`, `"bytes":null`, 1), strings.Replace(raw, `"bytes":12`, `"bytes":0`, 1), strings.Replace(raw, `"bytes":12`, `"Bytes":12`, 1), strings.Replace(raw, `"bytes":12`, `"bytes":12,"bytes":13`, 1), strings.Replace(raw, hash, strings.Repeat("a", 64), 1), raw + `{}`} {
		if _, e := ParseUploadCreate([]byte(bad)); e == nil {
			t.Fatal(bad)
		}
	}
	if _, e := ParseUploadCreate([]byte(strings.Replace(raw, "image/png", "image/webp", 1))); e != ErrUnrepresentable {
		t.Fatal(e)
	}
	if _, e := ParseUploadComplete([]byte(`{"sha256":"` + hash + `","assetId":"x"}`)); e == nil {
		t.Fatal("unknown field")
	}
}

func TestProductionFrozenAdapterRejections(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	id := "11111111-1111-1111-1111-111111111111"
	a := AssetDomain{ID: id, Owner: "0x" + strings.Repeat("1", 40), Hash: strings.Repeat("a", 64), MIME: "image/png", State: "UPLOADING", Size: 12}
	for _, bad := range []string{"https://_/x", "https://[::1]/x", "https://user:pass@host/x"} {
		if _, e := UploadCreateResponse(id, a, bad, now.Add(time.Minute), now); e != ErrUnrepresentable {
			t.Fatal(bad, e)
		}
	}
	if _, e := UploadCreateResponse(id, a, "https://host/x", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), now); e != ErrUnrepresentable {
		t.Fatal(e)
	}
	a.State = "APPROVED"
	url := "https://cdn.example/approved/" + a.Hash + "/256/" + a.Hash + ".png"
	a.ApprovedURL = &url
	wire, e := AssetToWire(a)
	if e != nil {
		t.Fatal(e)
	}
	url = "https://evil.example/raw"
	if *wire.PublicURL == url {
		t.Fatal("url pointer alias")
	}
	d := uint8(6)
	supply := "100"
	logo := id
	meta, e := MetadataResponse(id, a.Owner, "", "", "description", "1", nil, &d, &supply, &logo, now)
	if e != nil {
		t.Fatal(e)
	}
	d = 255
	supply = "invalid"
	logo = "bad"
	if *meta.Data.Token.Decimals != 6 || *meta.Data.Token.TotalSupplyRaw != "100" || *meta.Data.Token.LogoAssetID != id {
		t.Fatal("pointer alias")
	}
}

func TestProductionFrozenUTCBoundaryAndQuality(t *testing.T) {
	id := "11111111-1111-1111-1111-111111111111"
	bad := time.Date(1, 1, 1, 0, 0, 0, 0, time.FixedZone("positive", 3600))
	if _, e := MetadataResponse(id, "0x"+strings.Repeat("1", 40), "", "", "description", "1", nil, nil, nil, nil, bad); e == nil {
		t.Fatal("UTC year zero accepted")
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v, e := MetadataResponse(id, "0x"+strings.Repeat("1", 40), "", "", "description", "1", nil, nil, nil, nil, now)
	if e != nil {
		t.Fatal(e)
	}
	token := v.Data.Token
	token.MetadataRevision = "2"
	if _, e := MetadataPatchResponse(id, token, "1", now); e == nil {
		t.Fatal("revision disagreement")
	}
	token = v.Data.Token
	token.SupplyQuality = "partial"
	if _, e := MetadataPatchResponse(id, token, "1", now); e == nil {
		t.Fatal("partial upgraded")
	}
}

func TestProductionFrozenForeignContentURL(t *testing.T) {
	u := "https://cdn.example/approved/" + strings.Repeat("b", 64) + "/256/" + strings.Repeat("c", 64) + ".png"
	a := AssetDomain{ID: "11111111-1111-1111-1111-111111111111", Owner: "0x" + strings.Repeat("1", 40), Hash: strings.Repeat("a", 64), MIME: "image/png", State: "APPROVED", Size: 12, ApprovedURL: &u}
	if _, e := AssetToWire(a); e == nil {
		t.Fatal("foreign content hash")
	}
}
