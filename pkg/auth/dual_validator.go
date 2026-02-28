package auth

import "fmt"

// DualValidator attempts NATS JWT validation first, then falls back to
// EdDSA OAuth validation if configured. This allows both Connector/Chatbot
// clients (NATS JWTs) and SPA clients (OAuth JWTs) to authenticate.
type DualValidator struct {
	nats  *Validator
	oauth *OAuthValidator // nil if OAuth is not configured
}

// NewDualValidator creates a DualValidator. The oauth parameter may be nil,
// in which case only NATS JWT validation is active.
func NewDualValidator(nats *Validator, oauth *OAuthValidator) *DualValidator {
	return &DualValidator{nats: nats, oauth: oauth}
}

// Validate attempts NATS JWT validation first. If it fails and an
// OAuthValidator is configured, it attempts EdDSA OAuth validation.
// appID is used only for OAuth tokens (extracted from the X-App-ID header).
func (d *DualValidator) Validate(tokenStr, appID string) (*Claims, error) {
	claims, natsErr := d.nats.Validate(tokenStr)
	if natsErr == nil {
		return claims, nil
	}

	if d.oauth != nil {
		claims, oauthErr := d.oauth.Validate(tokenStr, appID)
		if oauthErr == nil {
			return claims, nil
		}
		return nil, fmt.Errorf("NATS auth: %w; OAuth auth: %v", natsErr, oauthErr)
	}

	return nil, natsErr
}
