package auth

import (
	"testing"
	"time"

	"github.com/nats-io/nkeys"
)

func TestMultiKeyValidator(t *testing.T) {
	// Create two keypairs
	kp1, _ := nkeys.CreateAccount()
	pub1, _ := kp1.PublicKey()
	seed1, _ := kp1.Seed()

	kp2, _ := nkeys.CreateAccount()
	pub2, _ := kp2.PublicKey()
	seed2, _ := kp2.Seed()

	validator, err := NewMultiKeyValidator([]string{pub1, pub2})
	if err != nil {
		t.Fatalf("failed to create MultiKeyValidator: %v", err)
	}

	// Test token from first key
	token1, _ := GenerateToken(seed1, "user1", "app1", "", 1*time.Hour)
	claims, err := validator.Validate(token1)
	if err != nil {
		t.Fatalf("validation failed for token1: %v", err)
	}
	if claims.Issuer != pub1 {
		t.Errorf("expected issuer %s, got %s", pub1, claims.Issuer)
	}

	// Test token from second key
	token2, _ := GenerateToken(seed2, "user2", "app1", "", 1*time.Hour)
	claims, err = validator.Validate(token2)
	if err != nil {
		t.Fatalf("validation failed for token2: %v", err)
	}
	if claims.Issuer != pub2 {
		t.Errorf("expected issuer %s, got %s", pub2, claims.Issuer)
	}

	// Test token from untrusted key
	kp3, _ := nkeys.CreateAccount()
	seed3, _ := kp3.Seed()
	token3, _ := GenerateToken(seed3, "user3", "app1", "", 1*time.Hour)
	_, err = validator.Validate(token3)
	if err == nil {
		t.Fatal("expected validation to fail for untrusted key")
	}
}

func TestRegistry(t *testing.T) {
	kp, _ := nkeys.CreateAccount()
	pub, _ := kp.PublicKey()

	t.Run("Create single_key from registry", func(t *testing.T) {
		config := map[string]interface{}{
			"public_key": pub,
		}
		v, err := CreateValidator("single_key", config)
		if err != nil {
			t.Fatalf("failed to create single_key validator: %v", err)
		}
		if _, ok := v.(*SingleKeyValidator); !ok {
			t.Fatal("expected *SingleKeyValidator")
		}
	})

	t.Run("Create multi_key from registry", func(t *testing.T) {
		config := map[string]interface{}{
			"public_keys": []interface{}{pub},
		}
		v, err := CreateValidator("multi_key", config)
		if err != nil {
			t.Fatalf("failed to create multi_key validator: %v", err)
		}
		if _, ok := v.(*MultiKeyValidator); !ok {
			t.Fatal("expected *MultiKeyValidator")
		}
	})

	t.Run("Unknown validator type", func(t *testing.T) {
		_, err := CreateValidator("unknown", nil)
		if err == nil {
			t.Fatal("expected error for unknown validator type")
		}
	})
}
