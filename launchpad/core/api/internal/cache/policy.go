package cache

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

type RouteClass string

const (
	PublicSnapshot  RouteClass = "public_snapshot"
	PrivateAccount  RouteClass = "private_account"
	Authorization   RouteClass = "authorization"
	Ownership       RouteClass = "ownership"
	ExecutableQuote RouteClass = "executable_quote"
	AccountWrite    RouteClass = "account_write"
	Administrative  RouteClass = "administrative"
)

type ParameterRule struct {
	Required   bool
	MaxBytes   int
	Pattern    string
	MaxInteger int64
}
type RoutePolicy struct {
	ID, Version, Origin, Generation string
	Class                           RouteClass
	Parameters                      map[string]ParameterRule
	FreshTTL, StaleTTL, Jitter      time.Duration
	RequestLimit                    int64
	RequestWindow                   time.Duration
}

var policyName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func (p RoutePolicy) validate() error {
	for _, s := range []string{p.ID, p.Version, p.Origin, p.Generation} {
		if !policyName.MatchString(s) {
			return errors.New("cache: invalid policy identity")
		}
	}
	switch p.Class {
	case PublicSnapshot, PrivateAccount, Authorization, Ownership, ExecutableQuote, AccountWrite, Administrative:
	default:
		return errors.New("cache: unsupported route class")
	}
	if p.FreshTTL < 0 || p.StaleTTL < 0 || p.Jitter < 0 || p.FreshTTL > 24*time.Hour || p.StaleTTL > 24*time.Hour || p.Jitter > 12*time.Hour || p.FreshTTL+p.Jitter+p.StaleTTL > 24*time.Hour || p.Jitter > p.FreshTTL/2 {
		return errors.New("cache: invalid freshness bounds")
	}
	if p.Class != PublicSnapshot && (p.StaleTTL != 0 || p.Jitter != 0) {
		return errors.New("cache: sensitive stale policy prohibited")
	}
	if p.Class != PublicSnapshot && p.Class != PrivateAccount && p.FreshTTL != 0 {
		return errors.New("cache: authority decisions cannot be cached")
	}
	if p.RequestLimit < 1 || p.RequestLimit > 1000000000 || p.RequestWindow < time.Millisecond || p.RequestWindow > 24*time.Hour || len(p.Parameters) > 32 {
		return errors.New("cache: invalid request allocation")
	}
	for name, rule := range p.Parameters {
		if !policyName.MatchString(name) || len(name) > 64 || rule.MaxBytes < 1 || rule.MaxBytes > 256 || len(rule.Pattern) > 512 || rule.MaxInteger < 0 {
			return errors.New("cache: invalid parameter rule")
		}
		if rule.Pattern != "" {
			if _, e := regexp.Compile("^(?:" + rule.Pattern + ")$"); e != nil {
				return e
			}
		}
	}
	return nil
}
func (p RoutePolicy) normalize(q url.Values) (url.Values, error) {
	if len(q) > 32 || len(q.Encode()) > 4096 {
		return nil, errors.New("cache: query too large")
	}
	out := url.Values{}
	for k, values := range q {
		rule, ok := p.Parameters[k]
		if !ok || len(values) != 1 || len(values[0]) == 0 || len(values[0]) > rule.MaxBytes {
			return nil, errors.New("cache: unknown or malformed parameter")
		}
		if rule.Pattern != "" && !regexp.MustCompile("^(?:"+rule.Pattern+")$").MatchString(values[0]) {
			return nil, errors.New("cache: invalid parameter")
		}
		if rule.MaxInteger > 0 {
			n, e := strconv.ParseInt(values[0], 10, 64)
			if e != nil || n < 1 || n > rule.MaxInteger || strconv.FormatInt(n, 10) != values[0] {
				return nil, errors.New("cache: numeric parameter outside bound")
			}
		}
		out.Set(k, values[0])
	}
	for k, rule := range p.Parameters {
		if rule.Required && out.Get(k) == "" {
			return nil, errors.New("cache: missing parameter")
		}
	}
	return out, nil
}
func (p RoutePolicy) visibility() string {
	if p.Class == PublicSnapshot {
		return "public"
	}
	return "private"
}
func (p RoutePolicy) metricClass() string {
	switch p.Class {
	case PublicSnapshot:
		return "anonymous_read"
	case ExecutableQuote:
		return "quote"
	case AccountWrite:
		return "account_write"
	case Administrative:
		return "admin"
	default:
		return "account_read"
	}
}
