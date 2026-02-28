package auth

import (
	"testing"
	"time"

	"github.com/nats-io/nkeys"
)

func TestGenerateAndValidate(t *testing.T) {
	// Create an NKey keypair for testing.
	kp, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatalf("failed to create account keypair: %v", err)
	}
	seed, err := kp.Seed()
	if err != nil {
		t.Fatalf("failed to get seed: %v", err)
	}
	pubKey, err := kp.PublicKey()
	if err != nil {
		t.Fatalf("failed to get public key: %v", err)
	}

	// Generate a token.
	token, err := GenerateToken(seed, "user1", "app1", "sess1", 1*time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// Validate the token.
	validator, err := NewValidator(pubKey)
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}

	claims, err := validator.Validate(token)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if claims.UserID != "user1" {
		t.Errorf("expected userid=user1, got %q", claims.UserID)
	}
	if claims.AppID != "app1" {
		t.Errorf("expected appid=app1, got %q", claims.AppID)
	}
	if claims.SessionID != "sess1" {
		t.Errorf("expected sessionid=sess1, got %q", claims.SessionID)
	}
	if claims.Issuer != pubKey {
		t.Errorf("expected issuer=%s, got %s", pubKey, claims.Issuer)
	}
}

func TestValidateWrongIssuer(t *testing.T) {
	kp, _ := nkeys.CreateAccount()
	seed, _ := kp.Seed()

	token, err := GenerateToken(seed, "user1", "app1", "", 1*time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// Use a different key for validation.
	otherKP, _ := nkeys.CreateAccount()
	otherPub, _ := otherKP.PublicKey()

	validator, _ := NewValidator(otherPub)
	_, err = validator.Validate(token)
	if err == nil {
		t.Fatal("expected validation to fail with wrong issuer")
	}
}

func TestValidateExpiredToken(t *testing.T) {
	kp, _ := nkeys.CreateAccount()
	seed, _ := kp.Seed()
	pubKey, _ := kp.PublicKey()

	// Generate a token that expired a minute ago.
	token, err := GenerateToken(seed, "user1", "app1", "", -1*time.Minute)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	validator, _ := NewValidator(pubKey)
	_, err = validator.Validate(token)
	if err == nil {
		t.Fatal("expected validation to fail for expired token")
	}
}

func TestValidateMissingClaims(t *testing.T) {
	kp, _ := nkeys.CreateAccount()
	seed, _ := kp.Seed()
	pubKey, _ := kp.PublicKey()

	// Token without userid.
	token, err := GenerateToken(seed, "", "app1", "", 1*time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	validator, _ := NewValidator(pubKey)
	_, err = validator.Validate(token)
	if err == nil {
		t.Fatal("expected validation to fail with missing userid")
	}
}
