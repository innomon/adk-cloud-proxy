package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

// Claims holds the validated claims extracted from a JWT.
type Claims struct {
	Subject   string
	Issuer    string
	UserID    string
	AppID     string
	SessionID string
}

// Validator validates JWTs signed with NKeys.
type Validator struct {
	issuerPubKey string
}

// NewValidator creates a Validator that accepts tokens signed by the given
// issuer public key (an NKey encoded Ed25519 public key).
func NewValidator(issuerPubKey string) (*Validator, error) {
	if _, err := nkeys.FromPublicKey(issuerPubKey); err != nil {
		return nil, fmt.Errorf("invalid issuer public key: %w", err)
	}
	return &Validator{issuerPubKey: issuerPubKey}, nil
}

// Validate parses and validates the raw JWT string. DecodeGeneric verifies
// the Ed25519 signature. This method additionally checks that the issuer
// matches the expected public key and that the token is not expired.
func (v *Validator) Validate(tokenStr string) (*Claims, error) {
	// DecodeGeneric verifies the signature as part of decoding.
	gc, err := jwt.DecodeGeneric(tokenStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode/verify JWT: %w", err)
	}

	// Verify issuer matches.
	if gc.Issuer != v.issuerPubKey {
		return nil, errors.New("JWT issuer does not match expected issuer")
	}

	// Check expiration.
	if gc.Expires > 0 && time.Now().Unix() > gc.Expires {
		return nil, errors.New("JWT has expired")
	}

	// Extract custom claims.
	userID, _ := gc.Data["userid"].(string)
	appID, _ := gc.Data["appid"].(string)
	sessionID, _ := gc.Data["sessionid"].(string)

	if appID == "" {
		return nil, errors.New("JWT missing required 'appid' claim")
	}

	return &Claims{
		Subject:   gc.Subject,
		Issuer:    gc.Issuer,
		UserID:    userID,
		AppID:     appID,
		SessionID: sessionID,
	}, nil
}

// GenerateToken creates a JWT signed with the given NKey seed, embedding the
// provided claims. This is used by connectors and clients to produce auth tokens.
func GenerateToken(seed []byte, userID, appID, sessionID string, ttl time.Duration) (string, error) {
	kp, err := nkeys.FromSeed(seed)
	if err != nil {
		return "", fmt.Errorf("failed to create keypair from seed: %w", err)
	}

	pubKey, err := kp.PublicKey()
	if err != nil {
		return "", fmt.Errorf("failed to get public key: %w", err)
	}

	gc := jwt.NewGenericClaims(pubKey)
	gc.Issuer = pubKey
	gc.IssuedAt = time.Now().Unix()
	if ttl != 0 {
		gc.Expires = time.Now().Add(ttl).Unix()
	}
	gc.Data = map[string]interface{}{
		"userid":    userID,
		"appid":     appID,
		"sessionid": sessionID,
	}

	token, err := gc.Encode(kp)
	if err != nil {
		return "", fmt.Errorf("failed to encode JWT: %w", err)
	}
	return token, nil
}
