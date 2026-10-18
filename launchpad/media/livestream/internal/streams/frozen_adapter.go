package streams

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

var ErrUnrepresentable = errors.New("stream state cannot be represented by frozen contract")
var frozenUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// FrozenStream is exactly components.schemas.Stream in the accepted v1 registry.
// Envelope freshness/revision and viewer quality must be supplied by the trusted
// read adapter; this pure projection cannot establish those facts itself.
type FrozenStream struct {
	StreamID      string  `json:"streamId"`
	PoolAddress   string  `json:"poolAddress"`
	CreatorWallet string  `json:"creatorWallet"`
	Title         string  `json:"title"`
	Status        string  `json:"status"`
	PlaybackURL   *string `json:"playbackUrl"`
	ViewerCount   int64   `json:"viewerCount"`
}

func AdaptFrozenStream(o Operation, playback string, viewers int64) (FrozenStream, error) {
	if !frozenUUID.MatchString(o.ID) || !addressPattern.MatchString(o.Pool) || !addressPattern.MatchString(o.Wallet) || !utf8.ValidString(o.Title) || len([]rune(o.Title)) > 120 || viewers < 0 || viewers > 9007199254740991 {
		return FrozenStream{}, ErrInvalid
	}
	var status string
	switch o.State {
	case "REQUESTED":
		status = "created"
	case "PROVISIONING", "READY":
		status = "starting"
	case "LIVE":
		status = "live"
	case "ENDING":
		status = "ending"
	case "ENDED":
		status = "ended"
	case "FAILED":
		status = "failed"
	default:
		return FrozenStream{}, ErrUnrepresentable
	}
	out := FrozenStream{o.ID, o.Pool, o.Wallet, o.Title, status, nil, viewers}
	if o.State == "LIVE" {
		if !identityPattern.MatchString(o.ProviderID) || o.ProviderTime == nil || o.ProviderTime.IsZero() {
			return FrozenStream{}, ErrUnavailable
		}
		u, e := url.Parse(playback)
		if e != nil || len(playback) > 2048 || u.Scheme != "https" || u.Hostname() != "livepeercdn.studio" || u.Port() != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path == "" {
			return FrozenStream{}, ErrInvalid
		}
		out.PlaybackURL = &playback
	}
	return out, nil
}

// The frozen create response requires an immediate ingestCredential, while the
// required durable API returns REQUESTED before provider allocation. Until a
// reviewed async operation contract and credential authority exist, no state
// can fabricate that required token/provider/expiry object.
func AdaptFrozenCreate(_ Operation) error { return ErrUnrepresentable }

type FrozenFieldError struct {
	Pointer string `json:"pointer"`
	Rule    string `json:"rule"`
}
type FrozenError struct {
	Code         string             `json:"code"`
	Message      string             `json:"message"`
	RequestID    string             `json:"requestId"`
	Retryable    bool               `json:"retryable"`
	RetryAfterMS *int               `json:"retryAfterMs"`
	FieldErrors  []FrozenFieldError `json:"fieldErrors"`
	Details      *struct{}          `json:"details"`
}

// Pure bounded error mapping only; the serial HTTP adapter owns route dispatch
// and request correlation. Internal/provider errors are never echoed.
func FrozenAdapterError(err error, requestID string) (int, FrozenError, error) {
	if !frozenUUID.MatchString(requestID) {
		return 0, FrozenError{}, ErrInvalid
	}
	out := FrozenError{RequestID: requestID, FieldErrors: []FrozenFieldError{}}
	switch {
	case errors.Is(err, ErrUnrepresentable):
		out.Code = "FEATURE_UNAVAILABLE"
		out.Message = "Verified livestream contract capability unavailable"
		return 501, out, nil
	case errors.Is(err, ErrInvalid):
		out.Code = "FIELD_INVALID"
		out.Message = "Invalid livestream field"
		return 422, out, nil
	default:
		out.Code = "DEPENDENCY_UNAVAILABLE"
		out.Message = "Authoritative livestream dependency unavailable"
		out.Retryable = true
		return 503, out, nil
	}
}

type V2Stream struct {
	Revision      string              `json:"revision"`
	GeneratedAt   string              `json:"generatedAt"`
	StreamID      string              `json:"streamId"`
	PoolAddress   string              `json:"poolAddress"`
	CreatorWallet string              `json:"creatorWallet"`
	Title         string              `json:"title"`
	Status        string              `json:"status"`
	PlaybackURL   *string             `json:"playbackUrl"`
	ViewerCount   *int64              `json:"viewerCount"`
	ViewerQuality string              `json:"viewerQuality"`
	ThumbnailURL  v2.Optional[string] `json:"thumbnailUrl,omitzero"`
}
type V2Operation struct {
	OperationID string  `json:"operationId"`
	StreamID    string  `json:"streamId"`
	Status      string  `json:"status"`
	Revision    string  `json:"revision"`
	RetryAt     *string `json:"retryAt"`
}

func AdaptV2Operation(o Operation) (V2Operation, error) {
	if !frozenUUID.MatchString(o.ID) || o.Revision < 1 {
		return V2Operation{}, ErrInvalid
	}
	switch o.State {
	case "REQUESTED", "PROVISIONING", "READY", "LIVE", "ENDING", "ENDED", "FAILED", "SUSPENDED":
	default:
		return V2Operation{}, ErrInvalid
	}
	result := V2Operation{OperationID: o.ID, StreamID: o.ID, Status: strings.ToLower(o.State), Revision: strconv.FormatInt(o.Revision, 10)}
	if o.RetryAt != nil {
		retry := o.RetryAt.UTC().Format("2006-01-02T15:04:05.000Z")
		result.RetryAt = &retry
	}
	return result, nil
}
func AdaptV2Stream(o Operation, playback string, viewers *int64, quality string) (V2Stream, error) {
	if quality != "verified" && quality != "unavailable" && quality != "stale" {
		return V2Stream{}, ErrInvalid
	}
	if quality == "unavailable" && viewers != nil || quality != "unavailable" && viewers == nil {
		return V2Stream{}, ErrInvalid
	}
	if !frozenUUID.MatchString(o.ID) || !addressPattern.MatchString(o.Pool) || !addressPattern.MatchString(o.Wallet) || !utf8.ValidString(o.Title) || len([]rune(o.Title)) < 1 || len([]rune(o.Title)) > 120 || viewers != nil && (*viewers < 0 || *viewers > 9007199254740991) {
		return V2Stream{}, ErrInvalid
	}
	if o.State == "SUSPENDED" {
		return V2Stream{Revision: strconv.FormatInt(o.Revision, 10), GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), StreamID: o.ID, PoolAddress: o.Pool, CreatorWallet: o.Wallet, Title: o.Title, Status: "suspended", ViewerCount: viewers, ViewerQuality: quality}, nil
	}
	var count int64
	if viewers != nil {
		count = *viewers
	}
	old, err := AdaptFrozenStream(o, playback, count)
	if err != nil {
		return V2Stream{}, err
	}
	return V2Stream{Revision: strconv.FormatInt(o.Revision, 10), GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), StreamID: old.StreamID, PoolAddress: old.PoolAddress, CreatorWallet: old.CreatorWallet, Title: old.Title, Status: old.Status, PlaybackURL: old.PlaybackURL, ViewerCount: viewers, ViewerQuality: quality}, nil
}
