package portal

import (
	"context"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
)

// ErrNotFound reports a page key the gateway does not know, or a page this
// build does not render; the server answers 404 inside the layout.
var ErrNotFound = errors.New("portal: not found")

var layout = template.Must(template.New("layout").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>{{.}} - Paxeer X Network</title></head>
<body><main><h1>{{.}}</h1></main></body>
</html>
`))

// Pages renders the portal pages from the gateway at gatewayURL, linking
// transactions under explorerTxBase.
type Pages struct{}

// NewPages checks the gateway URL, the explorer transaction base and the
// client the pages read through.
func NewPages(gatewayURL, explorerTxBase string, client *http.Client) (*Pages, error) {
	if client == nil {
		return nil, errors.New("portal_http_client_required")
	}
	for _, raw := range []string{gatewayURL, explorerTxBase} {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, errors.New("portal_page_url_needs_http_or_https")
		}
	}
	return &Pages{}, nil
}

// Markets is the market list page; this build does not render it, so it answers ErrNotFound.
func (p *Pages) Markets(context.Context, io.Writer) error { return ErrNotFound }

// Market is the page of symbol; this build does not render it, so it answers ErrNotFound.
func (p *Pages) Market(context.Context, io.Writer, string) error { return ErrNotFound }

// Account is the page of the lower-case address; this build does not render it, so it answers ErrNotFound.
func (p *Pages) Account(context.Context, io.Writer, string) error { return ErrNotFound }

// Leaderboard is the leaderboard page; this build does not render it, so it answers ErrNotFound.
func (p *Pages) Leaderboard(context.Context, io.Writer) error { return ErrNotFound }

// Stats is the venue statistics page; this build does not render it, so it answers ErrNotFound.
func (p *Pages) Stats(context.Context, io.Writer) error { return ErrNotFound }

// NotFound renders message inside the layout.
func (p *Pages) NotFound(w io.Writer, message string) error {
	return layout.Execute(w, message)
}
