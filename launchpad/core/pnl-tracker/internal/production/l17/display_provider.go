package l17

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"
)

type DisplayProvider struct {
	Endpoint   string
	ProviderID string
	Client     *http.Client
	MaxAge     time.Duration
}

func (p DisplayProvider) Convert(ctx context.Context, raw string, now time.Time) USDDisplay {
	out := USDDisplay{UsdlRaw: raw}
	if p.ProviderID == "" || p.MaxAge <= 0 || p.MaxAge > time.Hour {
		return out
	}
	endpoint, e := url.Parse(p.Endpoint)
	if e != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	request, e := http.NewRequestWithContext(ctx, "GET", p.Endpoint, nil)
	if e != nil {
		return out
	}
	client := http.Client{Timeout: 200 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if p.Client != nil {
		client.Transport = p.Client.Transport
	}
	response, e := client.Do(request)
	if e != nil {
		return out
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return out
	}
	var q DisplayQuote
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4097))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&q) != nil {
		return out
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return out
	}
	if q.Provider != p.ProviderID || q.ObservedAt.After(now) || now.Sub(q.ObservedAt) > p.MaxAge {
		return out
	}
	converted, e := ConvertDisplay(raw, &q)
	if e != nil {
		return out
	}
	return converted
}
