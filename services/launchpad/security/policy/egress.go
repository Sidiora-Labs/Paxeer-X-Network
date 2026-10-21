package policy

import (
	"net/http"
	"net/url"
	"strings"
)

type EgressDestination struct {
	Name, Hostname, Authority string
	InternalChain             bool
}

func EgressClient(destinations []EgressDestination) (*http.Client, error) {
	if len(destinations) == 0 || len(destinations) > 128 {
		return nil, ErrDenied
	}
	hosts := map[string]bool{}
	names := map[string]bool{}
	for _, d := range destinations {
		u, err := url.Parse("https://" + d.Hostname)
		if err != nil || d.Name == "" || names[d.Name] || d.Authority == "" || d.InternalChain || u.Hostname() != d.Hostname || u.Port() != "" || u.Path != "" || u.User != nil {
			return nil, ErrDenied
		}
		names[d.Name] = true
		hosts[strings.ToLower(d.Hostname)] = true
	}
	return PublicClient(hosts), nil
}
