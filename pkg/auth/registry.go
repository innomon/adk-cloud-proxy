package auth

import (
	"fmt"
	"sync"
)

// ValidatorFactory is a function that creates a Validator from a configuration map.
type ValidatorFactory func(config map[string]interface{}) (Validator, error)

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
func CreateValidator(typeName string, config map[string]interface{}) (Validator, error) {
	mu.RLock()
	factory, ok := factories[typeName]
	mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown validator type: %s", typeName)
	}

	return factory(config)
}

func init() {
	// Register single_key validator
	Register("single_key", func(config map[string]interface{}) (Validator, error) {
		pubKey, ok := config["public_key"].(string)
		if !ok || pubKey == "" {
			return nil, fmt.Errorf("single_key validator requires 'public_key' string")
		}
		return NewValidator(pubKey)
	})

	// Register multi_key validator
	Register("multi_key", func(config map[string]interface{}) (Validator, error) {
		rawKeys, ok := config["public_keys"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("multi_key validator requires 'public_keys' array")
		}
		keys := make([]string, 0, len(rawKeys))
		for _, rk := range rawKeys {
			if k, ok := rk.(string); ok && k != "" {
				keys = append(keys, k)
			}
		}
		if len(keys) == 0 {
			return nil, fmt.Errorf("multi_key validator requires at least one valid 'public_keys' string")
		}
		return NewMultiKeyValidator(keys)
	})
}
