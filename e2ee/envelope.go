package e2ee

import (
	"encoding/json"
	"fmt"
)

// EncryptedMessage represents the wire format for an E2EE message.
// It contains the encrypted ciphertext and the ratchet header needed
// by the receiver to derive the correct decryption key.
type EncryptedMessage struct {
	SenderID   string `json:"sender_id"`
	ReceiverID string `json:"receiver_id"`
	Ciphertext []byte `json:"ciphertext"`
	Header     []byte `json:"header"` // Double Ratchet header (DH public key, message counter, etc.)
	Timestamp  int64  `json:"timestamp"`
}

// Serialize encodes the encrypted message to JSON for transport.
func (m *EncryptedMessage) Serialize() ([]byte, error) {
	return json.Marshal(m)
}

// Deserialize decodes a JSON payload into an EncryptedMessage.
func Deserialize(data []byte) (*EncryptedMessage, error) {
	var m EncryptedMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to deserialize E2EE message: %w", err)
	}
	return &m, nil
}
