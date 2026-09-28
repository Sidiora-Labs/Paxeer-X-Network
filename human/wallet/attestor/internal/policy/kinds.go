package policy

import (
	"errors"
	"fmt"
)

const (
	KindLXActivity = "lx_activity"
	KindLXBind     = "lx_bind"
	KindLXGrant    = "lx_grant"
)

func KernelKinds() []string {
	return []string{KindLXActivity, KindLXBind, KindLXGrant}
}

func (p *Policy) RegisterKernel(activity, bind, grant Inspector) error {
	if activity == nil || bind == nil || grant == nil {
		return errors.New("register kernel kinds: every inspector is required")
	}
	inspectors := map[string]Inspector{KindLXActivity: activity, KindLXBind: bind, KindLXGrant: grant}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, kind := range KernelKinds() {
		if _, exists := p.kinds[kind]; exists {
			return fmt.Errorf("register kernel kinds: kind %q is already registered", kind)
		}
	}
	for _, kind := range KernelKinds() {
		p.kinds[kind] = inspectors[kind]
	}
	return nil
}
