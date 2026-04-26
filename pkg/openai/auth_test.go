package openai

import (
	"testing"
)

func TestSingleKeyValidator(t *testing.T) {
	v := &SingleKeyValidator{key: "secret"}
	if !v.Validate("secret") {
		t.Error("expected true for correct key")
	}
	if v.Validate("wrong") {
		t.Error("expected false for wrong key")
	}
	if v.Validate("") {
		t.Error("expected false for empty key")
	}
}

func TestMultiKeyValidator(t *testing.T) {
	v := &MultiKeyValidator{
		keys: map[string]struct{}{
			"key1": {},
			"key2": {},
		},
	}
	if !v.Validate("key1") {
		t.Error("expected true for key1")
	}
	if !v.Validate("key2") {
		t.Error("expected true for key2")
	}
	if v.Validate("key3") {
		t.Error("expected false for key3")
	}
}

func TestOpenAIRegistry(t *testing.T) {
	t.Run("Create single_key from registry", func(t *testing.T) {
		config := map[string]interface{}{
			"api_key": "secret",
		}
		v, err := CreateValidator("single_key", config)
		if err != nil {
			t.Fatalf("failed to create single_key validator: %v", err)
		}
		if _, ok := v.(*SingleKeyValidator); !ok {
			t.Fatal("expected *SingleKeyValidator")
		}
		if !v.Validate("secret") {
			t.Error("validation failed")
		}
	})

	t.Run("Create multi_key from registry", func(t *testing.T) {
		config := map[string]interface{}{
			"api_keys": []interface{}{"k1", "k2"},
		}
		v, err := CreateValidator("multi_key", config)
		if err != nil {
			t.Fatalf("failed to create multi_key validator: %v", err)
		}
		if _, ok := v.(*MultiKeyValidator); !ok {
			t.Fatal("expected *MultiKeyValidator")
		}
		if !v.Validate("k1") || !v.Validate("k2") {
			t.Error("validation failed")
		}
	})
}
