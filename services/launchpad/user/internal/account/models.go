// Package account implements stable-account owned state. Principals must come
// from the authenticated session service; wallet connection alone grants nothing.
package account

import (
	"bytes"
	"encoding/json"
	"errors"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"io"
	"math/big"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrDenied      = errors.New("SCOPE_DENIED")
	ErrConflict    = errors.New("REVISION_CONFLICT")
	ErrState       = errors.New("STATE_CONFLICT")
	ErrMissing     = errors.New("NOT_FOUND")
	ErrInvalid     = errors.New("FIELD_INVALID")
	ErrUnknown     = errors.New("FIELD_UNKNOWN")
	ErrIdempotency = errors.New("IDEMPOTENCY_CONFLICT")
	ErrCursor      = errors.New("CURSOR_INVALID")
)

type Principal struct {
	AccountID uuid.UUID
	ProofAt   time.Time
}

func (p Principal) Own(id uuid.UUID) error {
	if id == uuid.Nil || p.AccountID != id {
		return ErrDenied
	}
	return nil
}
func (p Principal) Fresh(now time.Time) error {
	if p.AccountID == uuid.Nil || p.ProofAt.IsZero() || p.ProofAt.After(now) || now.Sub(p.ProofAt) > 5*time.Minute {
		return ErrDenied
	}
	return nil
}

// Optional preserves absent versus explicit null; strings remain inert text.
type Optional[T any] struct {
	Set   bool
	Value *T
}

func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(b, []byte("null")) {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

type ProfilePatch struct {
	ExpectedRevision string           `json:"expectedRevision"`
	DisplayName      *string          `json:"displayName,omitempty"`
	Bio              *string          `json:"bio,omitempty"`
	AvatarAssetID    Optional[string] `json:"avatarAssetId,omitempty"`
}
type PublicProfile struct {
	Wallet        string  `json:"wallet"`
	DisplayName   string  `json:"displayName"`
	Bio           string  `json:"bio"`
	AvatarAssetID *string `json:"avatarAssetId"`
	BannerAssetID *string `json:"bannerAssetId"`
	Website       *string `json:"website"`
	Revision      string  `json:"revision"`
}
type Profile struct {
	PublicProfile
	Socials       map[string]string `json:"socials,omitempty"`
	SchemaVersion int               `json:"schemaVersion"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,77})$`)
var address = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

func Wallet(s string) (string, error) {
	if !address.MatchString(s) {
		return "", ErrInvalid
	}
	return strings.ToLower(s), nil
}
func Decode(r io.Reader, v any) error {
	b, e := io.ReadAll(io.LimitReader(r, 16385))
	if e != nil {
		return e
	}
	if len(b) > 16384 || !utf8.Valid(b) {
		return ErrInvalid
	}
	if _, e := sharedauth.BodyDigest(b); e != nil {
		return ErrInvalid
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return ErrInvalid
	}
	for k, v := range raw {
		if bytes.Equal(v, []byte("null")) && k != "avatarAssetId" {
			return ValidationError{"/" + k, ErrInvalid}
		}
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(v); e != nil {
		if strings.Contains(e.Error(), "unknown field") {
			parts := strings.Split(e.Error(), "\"")
			pointer := ""
			if len(parts) > 1 {
				pointer = "/" + parts[1]
			}
			return ValidationError{pointer, ErrUnknown}
		}
		return ErrInvalid
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func (p *ProfilePatch) Validate() error {
	if !validRevision(p.ExpectedRevision) {
		return ErrInvalid
	}
	if p.DisplayName != nil {
		v := norm.NFC.String(strings.TrimSpace(*p.DisplayName))
		n := utf8.RuneCountInString(v)
		if !utf8.ValidString(v) || n < 1 || n > 50 {
			return ValidationError{"/displayName", ErrInvalid}
		}
		p.DisplayName = &v
	}
	if p.Bio != nil && (!utf8.ValidString(*p.Bio) || utf8.RuneCountInString(*p.Bio) > 500) {
		return ValidationError{"/bio", ErrInvalid}
	}
	if p.AvatarAssetID.Set && p.AvatarAssetID.Value != nil {
		if _, e := uuid.Parse(*p.AvatarAssetID.Value); e != nil {
			return ErrInvalid
		}
	}
	return nil
}
func SafeLink(s string) error {
	u, e := url.Parse(s)
	if e != nil || len(s) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || strings.ContainsAny(s, " \r\n\t") {
		return ErrInvalid
	}
	return nil
}

type QuietHours struct {
	Timezone string `json:"timezone"`
	Start    string `json:"start"`
	End      string `json:"end"`
}
type Preferences struct {
	SchemaVersion          int             `json:"schemaVersion"`
	Revision               string          `json:"revision"`
	UpdatedAt              time.Time       `json:"updatedAt"`
	Locale                 string          `json:"locale"`
	Timezone               string          `json:"timezone"`
	Channels               []string        `json:"channels"`
	QuietHours             QuietHours      `json:"quietHours"`
	NotificationCategories map[string]bool `json:"notificationCategories"`
	AnalyticsConsent       bool            `json:"analyticsConsent"`
	Currency               string          `json:"currency"`
	ChartInterval          string          `json:"chartInterval"`
	ReducedMotion          bool            `json:"reducedMotion"`
	PrivateActivity        bool            `json:"privateActivity"`
}
type PreferencesPatch struct {
	ExpectedRevision string         `json:"expectedRevision"`
	Locale           *string        `json:"locale,omitempty"`
	Timezone         *string        `json:"timezone,omitempty"`
	Channels         *InputChannels `json:"channels,omitempty"`
	QuietHours       *QuietHours    `json:"quietHours,omitempty"`
}

var locale = regexp.MustCompile(`^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$`)
var hour = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)

func (p PreferencesPatch) Validate() error {
	if !validRevision(p.ExpectedRevision) {
		return ErrInvalid
	}
	if p.Locale != nil && (len(*p.Locale) > 80 || !locale.MatchString(*p.Locale)) {
		return ValidationError{"/locale", ErrInvalid}
	}
	if p.Timezone != nil {
		if len(*p.Timezone) > 100 || (*p.Timezone != "UTC" && !strings.Contains(*p.Timezone, "/")) {
			return ValidationError{"/timezone", ErrInvalid}
		}
		if _, e := time.LoadLocation(*p.Timezone); e != nil {
			return ValidationError{"/timezone", ErrInvalid}
		}
	}
	if p.Channels != nil {
		if p.Channels.Email == nil || p.Channels.InApp == nil || p.Channels.Push == nil {
			return ErrInvalid
		}

	}
	if p.QuietHours != nil && (!hour.MatchString(p.QuietHours.Start) || !hour.MatchString(p.QuietHours.End)) {
		return ErrInvalid
	}
	if p.QuietHours != nil {
		if _, e := time.LoadLocation(p.QuietHours.Timezone); e != nil || p.QuietHours.Timezone == "" {
			return ErrInvalid
		}
	}
	return nil
}
func validChannel(s string) bool { return s == "push" || s == "email" || s == "inapp" }
func validCategory(s string) bool {
	switch s {
	case "trade", "watchlist", "creator", "chat", "security", "marketing":
		return true
	}
	return false
}
func DefaultPreferences() Preferences {
	return Preferences{SchemaVersion: 1, Revision: "0", Locale: "en", Timezone: "UTC", Channels: []string{}, QuietHours: QuietHours{Timezone: "UTC", Start: "22:00", End: "07:00"}, NotificationCategories: map[string]bool{}, Currency: "USD", ChartInterval: "1h", PrivateActivity: true}
}

type Consent struct {
	Category       string    `json:"category"`
	Channel        string    `json:"channel"`
	Enabled        bool      `json:"enabled"`
	Source         string    `json:"source"`
	PolicyVersion  string    `json:"policyVersion"`
	Revision       int64     `json:"revision"`
	RevokeRevision int64     `json:"revokeRevision"`
	At             time.Time `json:"changedAt"`
}

func (c Consent) Validate() error {
	if !validCategory(c.Category) || !validChannel(c.Channel) || c.Source == "" || len(c.Source) > 80 || c.PolicyVersion == "" || len(c.PolicyVersion) > 80 {
		return ErrInvalid
	}
	return nil
}

type WatchEntry struct {
	Pool         string    `json:"pool"`
	AddedAt      time.Time `json:"addedAt"`
	Revision     int64     `json:"revision"`
	AlertEnabled bool      `json:"alertEnabled"`
}
type WatchCursor struct {
	At   time.Time `json:"at"`
	Pool string    `json:"pool"`
}

// Retention distinguishes immutable chain facts from removable account data.
type RetentionRule struct {
	Class  string
	Owner  string
	Policy string
}

var RetentionManifest = []RetentionRule{{"chain_fact", "indexer", "retain committed chain provenance; never enrich from deleted profile"}, {"required_audit", "security", "restricted retention under disclosed legal policy"}, {"personal_profile", "user", "purge within 30 days; keep opaque tombstone"}, {"ephemeral_telemetry", "observability", "expire under bounded telemetry policy"}}

func validRevision(s string) bool {
	if !decimal.MatchString(s) {
		return false
	}
	n, ok := new(big.Int).SetString(s, 10)
	return ok && n.BitLen() <= 256
}

type InputChannels struct {
	InApp *bool `json:"inapp"`
	Push  *bool `json:"push"`
	Email *bool `json:"email"`
}

func (c InputChannels) Enabled() []string {
	v := []string{}
	if c.InApp != nil && *c.InApp {
		v = append(v, "inapp")
	}
	if c.Push != nil && *c.Push {
		v = append(v, "push")
	}
	if c.Email != nil && *c.Email {
		v = append(v, "email")
	}
	return v
}

type ExtendedProfilePatch struct {
	ProfilePatch
	Website       Optional[string]            `json:"website,omitempty"`
	BannerAssetID Optional[string]            `json:"bannerAssetId,omitempty"`
	Socials       map[string]Optional[string] `json:"socials,omitempty"`
}

func DecodeExtendedProfile(r io.Reader, v *ExtendedProfilePatch) error {
	if v == nil {
		return ErrInvalid
	}
	*v = ExtendedProfilePatch{}
	b, e := io.ReadAll(io.LimitReader(r, 16385))
	if e != nil || len(b) > 16384 || !utf8.Valid(b) {
		return ErrInvalid
	}
	if _, e = sharedauth.BodyDigest(b); e != nil {
		return ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(b, &fields) != nil {
		return ErrInvalid
	}
	for key, value := range fields {
		if bytes.Equal(value, []byte("null")) && key != "avatarAssetId" && key != "bannerAssetId" && key != "website" {
			return ValidationError{"/" + key, ErrInvalid}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(v); e != nil {
		return ErrUnknown
	}
	if decoder.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	if e = v.ProfilePatch.Validate(); e != nil {
		return e
	}
	if v.Website.Set && v.Website.Value != nil {
		if e = SafeLink(*v.Website.Value); e != nil {
			return ValidationError{"/website", e}
		}
	}
	if v.BannerAssetID.Set && v.BannerAssetID.Value != nil {
		if id, e := uuid.Parse(*v.BannerAssetID.Value); e != nil || id == uuid.Nil {
			return ValidationError{"/bannerAssetId", ErrInvalid}
		}
	}
	if len(v.Socials) > 4 {
		return ValidationError{"/socials", ErrInvalid}
	}
	for k, value := range v.Socials {
		switch k {
		case "website", "twitter", "telegram", "discord":
		default:
			return ValidationError{"/socials/" + k, ErrUnknown}
		}
		if value.Value != nil {
			if e = SafeLink(*value.Value); e != nil {
				return ValidationError{"/socials/" + k, e}
			}
		}
	}
	return nil
}
