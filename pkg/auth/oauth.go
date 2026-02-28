package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// OAuthClaims represents the JWT claims from the WhatsApp Gateway OAuth flow.
type OAuthClaims struct {
	jwt.RegisteredClaims
	Nonce  string `json:"nonce,omitempty"`
	PubKey string `json:"pubkey,omitempty"`
}

// OAuthValidator validates EdDSA JWTs issued by the WhatsApp Gateway.
type OAuthValidator struct {
	publicKey ed25519.PublicKey
	issuer    string
	audience  string
}

// NewOAuthValidator creates a validator for EdDSA OAuth JWTs.
// pubKeyBase64 is the base64url-encoded raw 32-byte Ed25519 public key.
func NewOAuthValidator(pubKeyBase64, issuer, audience string) (*OAuthValidator, error) {
	raw, err := base64.RawURLEncoding.DecodeString(pubKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode public key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size: got %d bytes, want %d", len(raw), ed25519.PublicKeySize)
	}
	return &OAuthValidator{
		publicKey: ed25519.PublicKey(raw),
		issuer:    issuer,
		audience:  audience,
	}, nil
}

// Validate parses and validates an EdDSA OAuth JWT. It verifies the signature,
// issuer, audience, and expiration. The appID must be supplied externally
// (e.g., from the X-App-ID header) since OAuth JWTs do not embed it.
func (v *OAuthValidator) Validate(tokenStr, appID string) (*Claims, error) {
	oc := &OAuthClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, oc, func(t *jwt.Token) (interface{}, error) {
		if t.Method.Alg() != "EdDSA" {
			return nil, fmt.Errorf("unexpected signing method: %s", t.Method.Alg())
		}
		return v.publicKey, nil
	}, jwt.WithValidMethods([]string{"EdDSA"}))
	if err != nil {
		return nil, fmt.Errorf("failed to parse/verify OAuth JWT: %w", err)
	}
	if !token.Valid {
		return nil, errors.New("invalid OAuth JWT")
	}

	// Verify issuer.
	issuer, err := oc.GetIssuer()
	if err != nil || issuer != v.issuer {
		return nil, fmt.Errorf("OAuth JWT issuer mismatch: got %q, want %q", issuer, v.issuer)
	}

	// Verify audience.
	aud, err := oc.GetAudience()
	if err != nil {
		return nil, fmt.Errorf("failed to get audience: %w", err)
	}
	found := false
	for _, a := range aud {
		if a == v.audience {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("OAuth JWT audience mismatch: got %v, want %q", aud, v.audience)
	}

	sub, err := oc.GetSubject()
	if err != nil || sub == "" {
		return nil, errors.New("OAuth JWT missing required 'sub' claim")
	}

	if appID == "" {
		return nil, errors.New("missing required app ID (X-App-ID header)")
	}

	return &Claims{
		Subject: sub,
		Issuer:  issuer,
		UserID:  sub,
		AppID:   appID,
	}, nil
}
