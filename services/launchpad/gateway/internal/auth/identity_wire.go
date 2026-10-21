package auth

import (
	"encoding/json"
	"time"
)

func wireTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
func wireNullableTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := wireTime(*t)
	return &s
}
func (c Challenge) MarshalJSON() ([]byte, error) {
	type alias Challenge
	return json.Marshal(struct {
		alias
		IssuedAt  string `json:"issuedAt"`
		ExpiresAt string `json:"expiresAt"`
	}{alias(c), wireTime(c.IssuedAt), wireTime(c.ExpiresAt)})
}
func (s Session) MarshalJSON() ([]byte, error) {
	type alias Session
	return json.Marshal(struct {
		alias
		AccessExpiresAt  string  `json:"accessExpiresAt"`
		RefreshExpiresAt string  `json:"refreshExpiresAt"`
		RevokedAt        *string `json:"revokedAt"`
	}{alias(s), wireTime(s.AccessExpiresAt), wireTime(s.RefreshExpiresAt), wireNullableTime(s.RevokedAt)})
}
func (w WalletLink) MarshalJSON() ([]byte, error) {
	type alias WalletLink
	return json.Marshal(struct {
		alias
		LinkedAt *string `json:"linkedAt"`
	}{alias(w), wireNullableTime(w.LinkedAt)})
}
func (e Envelope[T]) MarshalJSON() ([]byte, error) {
	type alias Envelope[T]
	return json.Marshal(struct {
		alias
		GeneratedAt string `json:"generatedAt"`
	}{alias(e), wireTime(e.GeneratedAt)})
}
