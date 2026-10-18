package metadata

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrUnrepresentable = errors.New("domain state cannot be represented by frozen media contract")

type WireAsset struct {
	AssetID     string  `json:"assetId"`
	OwnerWallet string  `json:"ownerWallet"`
	SHA256      string  `json:"sha256"`
	MediaType   string  `json:"mediaType"`
	Bytes       int64   `json:"bytes"`
	State       string  `json:"state"`
	PublicURL   *string `json:"publicUrl"`
}
type AssetDomain struct {
	ID, Owner, Hash, MIME, State string
	Size                         int64
	ApprovedURL                  *string
	Blocked                      bool
}
type WireToken struct {
	Address          string  `json:"address"`
	Name             string  `json:"name"`
	Symbol           string  `json:"symbol"`
	Decimals         *uint8  `json:"decimals"`
	TotalSupplyRaw   *string `json:"totalSupplyRaw"`
	DecimalsQuality  string  `json:"decimalsQuality"`
	SupplyQuality    string  `json:"supplyQuality"`
	SourcePosition   any     `json:"sourcePosition"`
	MetadataRevision string  `json:"metadataRevision"`
	LogoAssetID      *string `json:"logoAssetId"`
}
type WireMetadataData struct {
	Token       WireToken `json:"token"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	Revision    string    `json:"revision"`
}
type WireAssetData struct {
	Asset WireAsset `json:"asset"`
}
type WireUploadData struct {
	Asset           WireAsset `json:"asset"`
	UploadURL       string    `json:"uploadUrl"`
	UploadExpiresAt string    `json:"uploadExpiresAt"`
}
type WireResponse[T any] struct {
	RequestID   string  `json:"requestId"`
	Data        T       `json:"data"`
	Watermark   any     `json:"watermark"`
	Revision    *string `json:"revision"`
	Quality     string  `json:"quality"`
	NextCursor  *string `json:"nextCursor"`
	GeneratedAt string  `json:"generatedAt"`
}

func validHTTPS(s string) bool {
	u, e := url.Parse(s)
	return e == nil && regexp.MustCompile(`^https://[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?(?::[0-9]{1,5})?(?:[/?#][^\s]*)?$`).MatchString(s) && len(s) <= 2048 && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && !strings.ContainsAny(s, " \r\n\t")
}
func immutableURL(s string) bool {
	if !validHTTPS(s) {
		return false
	}
	u, _ := url.Parse(s)
	return u.RawQuery == "" && u.Fragment == "" && regexp.MustCompile(`^/approved/(?:[a-f0-9]{64}|account/0x[a-f0-9]{40}/[a-f0-9-]{36})/(256|512|1024)/[a-f0-9]{64}\.png$`).MatchString(u.Path)
}
func AssetToWire(a AssetDomain) (WireAsset, error) {
	var z WireAsset
	if !uuidRE.MatchString(a.ID) || !addressRE.MatchString(a.Owner) || len(a.Hash) != 64 || a.Size < 1 || a.Size > 6<<20 {
		return z, ErrUnrepresentable
	}
	if _, e := hex.DecodeString(a.Hash); e != nil {
		return z, ErrUnrepresentable
	}
	if a.MIME != "image/png" && a.MIME != "image/jpeg" && a.MIME != "image/webp" {
		return z, ErrUnrepresentable
	}
	states := map[string]string{"UPLOADING": "uploading", "QUARANTINED": "quarantined", "VALIDATING": "quarantined", "APPROVED": "approved", "REJECTED": "rejected"}
	state, ok := states[a.State]
	if !ok || a.Blocked {
		return z, ErrUnrepresentable
	}
	if state != "approved" && a.ApprovedURL != nil {
		return z, ErrUnrepresentable
	}
	if state == "approved" && a.ApprovedURL == nil {
		return z, ErrUnrepresentable
	}
	if a.ApprovedURL != nil && (!immutableURL(*a.ApprovedURL) || !strings.HasPrefix(func() string { u, _ := url.Parse(*a.ApprovedURL); return u.Path }(), "/approved/"+a.Hash+"/")) {
		return z, ErrUnrepresentable
	}
	if a.ApprovedURL != nil {
		copyURL := *a.ApprovedURL
		a.ApprovedURL = &copyURL
	}
	return WireAsset{a.ID, a.Owner, "0x" + a.Hash, a.MIME, a.Size, state, a.ApprovedURL}, nil
}
func envelope[T any](request string, data T, quality string, now time.Time) (WireResponse[T], error) {
	var z WireResponse[T]
	if !uuidRE.MatchString(request) || now.UTC().Year() < 1 || now.UTC().Year() > 9999 {
		return z, ErrValidation
	}
	return WireResponse[T]{RequestID: request, Data: data, Quality: quality, GeneratedAt: now.UTC().Format("2006-01-02T15:04:05.000Z")}, nil
}
func AssetResponse(request string, a AssetDomain, now time.Time) (WireResponse[WireAssetData], error) {
	v, e := AssetToWire(a)
	if e != nil {
		return WireResponse[WireAssetData]{}, e
	}
	q := "partial"
	if v.State == "quarantined" {
		q = "quarantined"
	}
	return envelope(request, WireAssetData{v}, q, now)
}

// Complete returns actual persisted asset state; it cannot manufacture approval.
func UploadCompleteResponse(request string, a AssetDomain, now time.Time) (WireResponse[WireAssetData], error) {
	return AssetResponse(request, a, now)
}
func UploadCreateResponse(request string, a AssetDomain, uploadURL string, expires, now time.Time) (WireResponse[WireUploadData], error) {
	v, e := AssetToWire(a)
	if e != nil {
		return WireResponse[WireUploadData]{}, e
	}
	if expires.UTC().Year() < 1 || expires.UTC().Year() > 9999 || v.State != "uploading" || !validHTTPS(uploadURL) || !expires.After(now) || expires.Sub(now) > 15*time.Minute {
		return WireResponse[WireUploadData]{}, ErrUnrepresentable
	}
	return envelope(request, WireUploadData{v, uploadURL, expires.UTC().Format("2006-01-02T15:04:05.000Z")}, "partial", now)
}

// Metadata read rejects frozen description incompatibility instead of truncating,
// supplying invented names/supply, or serializing a schema-invalid success.
func MetadataResponse(request, address, name, symbol, description, revision string, tags []string, decimals *uint8, supply *string, logo *string, now time.Time) (WireResponse[WireMetadataData], error) {
	var z WireResponse[WireMetadataData]
	if !addressRE.MatchString(address) || !revisionValid(revision) || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 100 || !utf8.ValidString(symbol) || utf8.RuneCountInString(symbol) > 16 {
		return z, ErrUnrepresentable
	}
	description, e := text(description, 1, 256)
	if e != nil || len(tags) > 100 {
		return z, ErrUnrepresentable
	}
	owned := make([]string, len(tags))
	for i, t := range tags {
		v, e := text(t, 1, 256)
		if e != nil {
			return z, ErrUnrepresentable
		}
		owned[i] = v
	}
	if supply != nil && !revisionValid(*supply) {
		return z, ErrUnrepresentable
	}
	if logo != nil && !uuidRE.MatchString(*logo) {
		return z, ErrUnrepresentable
	}
	if decimals != nil {
		v := *decimals
		decimals = &v
	}
	if supply != nil {
		v := *supply
		supply = &v
	}
	if logo != nil {
		v := *logo
		logo = &v
	}
	dq, sq := "unavailable", "unavailable"
	if decimals != nil {
		dq = "verified"
	}
	if supply != nil {
		sq = "verified"
	}
	t := WireToken{address, name, symbol, decimals, supply, dq, sq, nil, revision, logo}
	return envelope(request, WireMetadataData{t, description, owned, revision}, "partial", now)
}

type WireMetadataPatchData struct {
	Token    WireToken `json:"token"`
	Revision string    `json:"revision"`
}

func MetadataPatchResponse(request string, token WireToken, revision string, now time.Time) (WireResponse[WireMetadataPatchData], error) {
	if token.MetadataRevision != revision || token.SourcePosition != nil || (token.Decimals == nil && token.DecimalsQuality != "unavailable") || (token.Decimals != nil && token.DecimalsQuality != "verified") || (token.TotalSupplyRaw == nil && token.SupplyQuality != "unavailable") || (token.TotalSupplyRaw != nil && token.SupplyQuality != "verified") {
		return WireResponse[WireMetadataPatchData]{}, ErrUnrepresentable
	}
	// Only the canonical no-position projection supported here is accepted; quality
	// is checked, never upgraded. Position-bearing/partial inputs need an adapter.
	v, e := MetadataResponse(request, token.Address, token.Name, token.Symbol, "validation-only", revision, nil, token.Decimals, token.TotalSupplyRaw, token.LogoAssetID, now)
	if e != nil {
		return WireResponse[WireMetadataPatchData]{}, e
	}
	return envelope(request, WireMetadataPatchData{v.Data.Token, revision}, "partial", now)
}

type UploadCreateBody struct {
	MediaType string `json:"mediaType"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
}
type UploadCompleteBody struct {
	SHA256 string `json:"sha256"`
}

func decodeUpload(raw []byte, allowed map[string]bool, out any) error {
	if len(raw) > 16384 || !utf8.Valid(raw) {
		return ErrValidation
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return ErrValidation
	}
	seen := map[string]bool{}
	for d.More() {
		k, e := d.Token()
		if e != nil {
			return ErrValidation
		}
		key, ok := k.(string)
		if !ok || !allowed[key] || seen[key] {
			return ErrValidation
		}
		seen[key] = true
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return ErrValidation
		}
	}
	if _, e = d.Token(); e != nil {
		return ErrValidation
	}
	if d.Decode(new(any)) != io.EOF || len(seen) != len(allowed) {
		return ErrValidation
	}
	return json.Unmarshal(raw, out)
}
func wireHash(s string) bool {
	if len(s) != 66 || !strings.HasPrefix(s, "0x") {
		return false
	}
	_, e := hex.DecodeString(s[2:])
	return e == nil
}
func ParseUploadCreate(raw []byte) (UploadCreateBody, error) {
	var b UploadCreateBody
	if e := decodeUpload(raw, map[string]bool{"mediaType": true, "bytes": true, "sha256": true}, &b); e != nil {
		return b, ErrValidation
	}
	if !wireHash(b.SHA256) || b.Bytes < 1 || b.Bytes > 6<<20 || (b.MediaType != "image/png" && b.MediaType != "image/jpeg" && b.MediaType != "image/webp") {
		return b, ErrValidation
	}
	if b.MediaType == "image/webp" {
		return b, ErrUnrepresentable
	}
	return b, nil
}
func ParseUploadComplete(raw []byte) (UploadCompleteBody, error) {
	var b UploadCompleteBody
	if e := decodeUpload(raw, map[string]bool{"sha256": true}, &b); e != nil || !wireHash(b.SHA256) {
		return b, ErrValidation
	}
	return b, nil
}
