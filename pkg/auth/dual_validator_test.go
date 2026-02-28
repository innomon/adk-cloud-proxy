package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/nats-io/nkeys"
)

func TestDualValidatorNATSPasses(t *testing.T) {
	kp, _ := nkeys.CreateAccount()
	seed, _ := kp.Seed()
	pubKey, _ := kp.PublicKey()

	natsV, _ := NewValidator(pubKey)
	dual := NewDualValidator(natsV, nil)

	token, err := GenerateToken(seed, "user1", "app1", "sess1", 1*time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := dual.Validate(token, "")
	if err != nil {
		t.Fatalf("DualValidator.Validate failed: %v", err)
	}
	if claims.UserID != "user1" {
		t.Errorf("expected UserID=user1, got %q", claims.UserID)
	}
}

func TestDualValidatorNATSFailsOAuthPasses(t *testing.T) {
	// NATS validator with a key that won't match.
	natsKP, _ := nkeys.CreateAccount()
	natsPub, _ := natsKP.PublicKey()
	natsV, _ := NewValidator(natsPub)

	// OAuth validator.
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pubB64 := base64.RawURLEncoding.EncodeToString(pub)
	oauthV, _ := NewOAuthValidator(pubB64, "whatsadk-gateway", "adk-cloud-proxy")

	dual := NewDualValidator(natsV, oauthV)

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"sub": "919876543210",
		"iss": "whatsadk-gateway",
		"aud": "adk-cloud-proxy",
		"exp": jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		"iat": jwt.NewNumericDate(time.Now()),
	})
	tokenStr, _ := token.SignedString(priv)

	claims, err := dual.Validate(tokenStr, "my-app")
	if err != nil {
		t.Fatalf("DualValidator.Validate failed: %v", err)
	}
	if claims.UserID != "919876543210" {
		t.Errorf("expected UserID=919876543210, got %q", claims.UserID)
	}
	if claims.AppID != "my-app" {
		t.Errorf("expected AppID=my-app, got %q", claims.AppID)
	}
}

func TestDualValidatorBothFail(t *testing.T) {
	natsKP, _ := nkeys.CreateAccount()
	natsPub, _ := natsKP.PublicKey()
	natsV, _ := NewValidator(natsPub)

	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	pubB64 := base64.RawURLEncoding.EncodeToString(pub)
	oauthV, _ := NewOAuthValidator(pubB64, "whatsadk-gateway", "adk-cloud-proxy")

	dual := NewDualValidator(natsV, oauthV)

	_, err := dual.Validate("bogus.invalid.token", "my-app")
	if err == nil {
		t.Fatal("expected validation to fail when both validators reject the token")
	}
}

func TestDualValidatorOAuthNotConfigured(t *testing.T) {
	natsKP, _ := nkeys.CreateAccount()
	natsPub, _ := natsKP.PublicKey()
	natsV, _ := NewValidator(natsPub)

	dual := NewDualValidator(natsV, nil)

	_, err := dual.Validate("bogus.invalid.token", "my-app")
	if err == nil {
		t.Fatal("expected validation to fail with no OAuth fallback")
	}
}
