package pubsub

import "encoding/json"

// InviteMessage is sent by the proxy to invite a connector to connect.
type InviteMessage struct {
	AppID    string `json:"app_id"`
	UserID   string `json:"user_id"`
	ProxyURL string `json:"proxy_url"`
}

// EncodeInviteMessage encodes an InviteMessage to JSON.
func EncodeInviteMessage(msg *InviteMessage) ([]byte, error) {
	return json.Marshal(msg)
}

// DecodeInviteMessage decodes an InviteMessage from JSON.
func DecodeInviteMessage(data []byte) (*InviteMessage, error) {
	var msg InviteMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
