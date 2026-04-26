package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

// MultiKeyValidator validates JWTs signed by any of a set of trusted NKey public keys.
type MultiKeyValidator struct {
	trustedKeys map[string]struct{}
}

// NewMultiKeyValidator creates a new MultiKeyValidator with the given public keys.
func NewMultiKeyValidator(keys []string) (*MultiKeyValidator, error) {
	trusted := make(map[string]struct{})
	for _, k := range keys {
		if _, err := nkeys.FromPublicKey(k); err != nil {
			return nil, fmt.Errorf("invalid trusted public key '%s': %w", k, err)
		}
		trusted[k] = struct{}{}
	}
	if len(trusted) == 0 {
		return nil, errors.New("at least one trusted public key is required")
	}
	return &MultiKeyValidator{trustedKeys: trusted}, nil
}

// Validate parses and validates the raw JWT string.
func (v *MultiKeyValidator) Validate(tokenStr string) (*Claims, error) {
	// DecodeGeneric verifies the signature as part of decoding.
	gc, err := jwt.DecodeGeneric(tokenStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode/verify JWT: %w", err)
	}

	// Verify issuer is in the trusted set.
	if _, ok := v.trustedKeys[gc.Issuer]; !ok {
		return nil, errors.New("JWT issuer is not in the trusted keys list")
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
