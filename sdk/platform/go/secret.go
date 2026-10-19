package layerx

import (
	"encoding/json"
	"runtime"
	"sync"
)

type SecretBytes struct {
	mu        sync.Mutex
	value     []byte
	destroyed bool
}

func NewSecretBytes(value []byte) (*SecretBytes, error) {
	if len(value) == 0 {
		return nil, newSDKError(ErrorInvalidArgument, RetryNever)
	}
	secret := &SecretBytes{value: append([]byte(nil), value...)}
	runtime.SetFinalizer(secret, (*SecretBytes).Destroy)
	return secret, nil
}

func (secret *SecretBytes) Expose(consumer func([]byte) error) error {
	if secret == nil || consumer == nil {
		return newSDKError(ErrorInvalidArgument, RetryNever)
	}
	secret.mu.Lock()
	defer secret.mu.Unlock()
	if secret.destroyed {
		return newSDKError(ErrorInvalidArgument, RetryNever)
	}
	return consumer(secret.value)
}

func (secret *SecretBytes) Destroy() {
	if secret == nil {
		return
	}
	secret.mu.Lock()
	defer secret.mu.Unlock()
	for index := range secret.value {
		secret.value[index] = 0
	}
	secret.value = nil
	secret.destroyed = true
	runtime.SetFinalizer(secret, nil)
}

func (*SecretBytes) String() string   { return "[REDACTED]" }
func (*SecretBytes) GoString() string { return "SecretBytes([REDACTED])" }

func (*SecretBytes) MarshalJSON() ([]byte, error) {
	return json.Marshal("[REDACTED]")
}
