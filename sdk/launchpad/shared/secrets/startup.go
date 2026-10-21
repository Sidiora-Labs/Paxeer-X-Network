package secrets

import (
	"context"
	"errors"
	"regexp"
	"time"
)

type Startup struct {
	Provider        Provider
	Values          map[string]string
	RefreshInterval time.Duration
}

func LoadProduction(ctx context.Context, provider ProviderType, refresh time.Duration, required []string) (*Startup, error) {
	if provider == "" || refresh <= 0 || refresh > time.Minute || len(required) == 0 || len(required) > 128 {
		return nil, errors.New("secrets: invalid production inventory")
	}
	p, err := NewChecked(Options{Provider: provider})
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, name := range required {
		if !regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`).MatchString(name) {
			return nil, errors.New("secrets: invalid reference")
		}
		if _, ok := values[name]; ok {
			return nil, errors.New("secrets: duplicate reference")
		}
		value, e := p.GetRequired(ctx, name)
		if e != nil {
			return nil, errors.New("secrets: required value unavailable")
		}
		values[name] = value
	}
	return &Startup{Provider: p, Values: values, RefreshInterval: refresh}, nil
}
func (s *Startup) Ready(ctx context.Context) error {
	if s == nil || s.Provider == nil || len(s.Values) == 0 {
		return errors.New("secrets: not initialized")
	}
	for name, consumed := range s.Values {
		fresh, err := s.Provider.GetRequired(ctx, name)
		if err != nil || fresh != consumed {
			return errors.New("secrets: unavailable or rotation requires restart")
		}
	}
	return nil
}
