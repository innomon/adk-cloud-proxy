package openai

import (
	"fmt"
	"sync"
)

// APIKeyValidator validates an opaque API key string.
type APIKeyValidator interface {
	Validate(key string) bool
}

// AllowAllValidator allows any (or no) API key.
type AllowAllValidator struct{}

func (v *AllowAllValidator) Validate(key string) bool {
	return true
}

// SingleKeyValidator validates against a single static key.
type SingleKeyValidator struct {
	key string
}

func (v *SingleKeyValidator) Validate(key string) bool {
	return key == v.key
}

// MultiKeyValidator validates against a set of valid keys.
type MultiKeyValidator struct {
	keys map[string]struct{}
}

func (v *MultiKeyValidator) Validate(key string) bool {
	_, ok := v.keys[key]
	return ok
}

// ValidatorFactory is a function that creates an APIKeyValidator from a configuration map.
type ValidatorFactory func(config map[string]interface{}) (APIKeyValidator, error)

var (
	factories = make(map[string]ValidatorFactory)
	mu        sync.RWMutex
)

// Register registers a new validator factory.
func Register(name string, factory ValidatorFactory) {
	mu.Lock()
	defer mu.Unlock()
	factories[name] = factory
}

// CreateValidator creates a validator of the given type with the provided configuration.
func CreateValidator(typeName string, config map[string]interface{}) (APIKeyValidator, error) {
	mu.RLock()
	factory, ok := factories[typeName]
	mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown openai validator type: %s", typeName)
	}

	return factory(config)
}

func init() {
	// Register single_key validator
	Register("single_key", func(config map[string]interface{}) (APIKeyValidator, error) {
		key, ok := config["api_key"].(string)
		if !ok || key == "" {
			return nil, fmt.Errorf("openai single_key validator requires 'api_key' string")
		}
		return &SingleKeyValidator{key: key}, nil
	})

	// Register multi_key validator
	Register("multi_key", func(config map[string]interface{}) (APIKeyValidator, error) {
		rawKeys, ok := config["api_keys"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("openai multi_key validator requires 'api_keys' array")
		}
		keys := make(map[string]struct{})
		for _, rk := range rawKeys {
			if k, ok := rk.(string); ok && k != "" {
				keys[k] = struct{}{}
			}
		}
		if len(keys) == 0 {
			return nil, fmt.Errorf("openai multi_key validator requires at least one valid 'api_keys' string")
		}
		return &MultiKeyValidator{keys: keys}, nil
	})
}
