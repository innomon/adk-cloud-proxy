package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func generateEdDSAToken(t *testing.T, privKey ed25519.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := token.SignedString(privKey)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

func newTestKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}
	return pub, priv
}

func TestOAuthValidateValid(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	pubB64 := base64.RawURLEncoding.EncodeToString(pub)

	v, err := NewOAuthValidator(pubB64, "whatsadk-gateway", "adk-cloud-proxy")
	if err != nil {
		t.Fatalf("NewOAuthValidator failed: %v", err)
	}

	tokenStr := generateEdDSAToken(t, priv, jwt.MapClaims{
		"sub": "919876543210",
		"iss": "whatsadk-gateway",
		"aud": "adk-cloud-proxy",
		"exp": jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		"iat": jwt.NewNumericDate(time.Now()),
	})

	claims, err := v.Validate(tokenStr, "my-app")
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if claims.UserID != "919876543210" {
		t.Errorf("expected UserID=919876543210, got %q", claims.UserID)
	}
	if claims.AppID != "my-app" {
		t.Errorf("expected AppID=my-app, got %q", claims.AppID)
	}
	if claims.Issuer != "whatsadk-gateway" {
		t.Errorf("expected Issuer=whatsadk-gateway, got %q", claims.Issuer)
	}
}

func TestOAuthValidateExpired(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	pubB64 := base64.RawURLEncoding.EncodeToString(pub)

	v, _ := NewOAuthValidator(pubB64, "whatsadk-gateway", "adk-cloud-proxy")

	tokenStr := generateEdDSAToken(t, priv, jwt.MapClaims{
		"sub": "919876543210",
		"iss": "whatsadk-gateway",
		"aud": "adk-cloud-proxy",
		"exp": jwt.NewNumericDate(time.Now().Add(-1 * time.Minute)),
	})

	_, err := v.Validate(tokenStr, "my-app")
	if err == nil {
		t.Fatal("expected validation to fail for expired token")
	}
}

func TestOAuthValidateWrongIssuer(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	pubB64 := base64.RawURLEncoding.EncodeToString(pub)

	v, _ := NewOAuthValidator(pubB64, "whatsadk-gateway", "adk-cloud-proxy")

	tokenStr := generateEdDSAToken(t, priv, jwt.MapClaims{
		"sub": "919876543210",
		"iss": "wrong-issuer",
		"aud": "adk-cloud-proxy",
		"exp": jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
	})

	_, err := v.Validate(tokenStr, "my-app")
	if err == nil {
		t.Fatal("expected validation to fail with wrong issuer")
	}
}

func TestOAuthValidateWrongAudience(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	pubB64 := base64.RawURLEncoding.EncodeToString(pub)

	v, _ := NewOAuthValidator(pubB64, "whatsadk-gateway", "adk-cloud-proxy")

	tokenStr := generateEdDSAToken(t, priv, jwt.MapClaims{
		"sub": "919876543210",
		"iss": "whatsadk-gateway",
		"aud": "wrong-audience",
		"exp": jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
	})

	_, err := v.Validate(tokenStr, "my-app")
	if err == nil {
		t.Fatal("expected validation to fail with wrong audience")
	}
}

func TestOAuthValidateTamperedSignature(t *testing.T) {
	_, priv := newTestKeyPair(t)
	otherPub, _ := newTestKeyPair(t) // different key
	otherPubB64 := base64.RawURLEncoding.EncodeToString(otherPub)

	v, _ := NewOAuthValidator(otherPubB64, "whatsadk-gateway", "adk-cloud-proxy")

	tokenStr := generateEdDSAToken(t, priv, jwt.MapClaims{
		"sub": "919876543210",
		"iss": "whatsadk-gateway",
		"aud": "adk-cloud-proxy",
		"exp": jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
	})

	_, err := v.Validate(tokenStr, "my-app")
	if err == nil {
		t.Fatal("expected validation to fail with tampered signature")
	}
}

func TestOAuthValidateMissingAppID(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	pubB64 := base64.RawURLEncoding.EncodeToString(pub)

	v, _ := NewOAuthValidator(pubB64, "whatsadk-gateway", "adk-cloud-proxy")

	tokenStr := generateEdDSAToken(t, priv, jwt.MapClaims{
		"sub": "919876543210",
		"iss": "whatsadk-gateway",
		"aud": "adk-cloud-proxy",
		"exp": jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
	})

	_, err := v.Validate(tokenStr, "")
	if err == nil {
		t.Fatal("expected validation to fail with missing app ID")
	}
}

func TestNewOAuthValidatorInvalidKey(t *testing.T) {
	_, err := NewOAuthValidator("not-valid-base64!!!", "iss", "aud")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}

	// Valid base64 but wrong size.
	shortKey := base64.RawURLEncoding.EncodeToString([]byte("tooshort"))
	_, err = NewOAuthValidator(shortKey, "iss", "aud")
	if err == nil {
		t.Fatal("expected error for wrong key size")
	}
}
