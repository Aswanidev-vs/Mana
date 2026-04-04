package e2ee

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
)

// KeyStore defines the interface for persistence of E2EE key material.
// The server stores ONLY public keys and prekey bundles.
// Private keys are stored ONLY on the client side.
type KeyStore interface {
	// Identity Keys (public only, for server-side storage)
	SaveIdentityPublicKey(ctx context.Context, userID string, pubKey []byte) error
	LoadIdentityPublicKey(ctx context.Context, userID string) ([]byte, error)

	// PreKey Bundles (public bundle for X3DH)
	SavePreKeyBundle(ctx context.Context, userID string, bundle *PublicPreKeyBundle) error
	LoadPreKeyBundle(ctx context.Context, userID string) (*PublicPreKeyBundle, error)
	ConsumeOneTimePreKey(ctx context.Context, userID string) error

	// Session State (ratchet state, stored client-side)
	SaveSession(ctx context.Context, sessionID string, state []byte) error
	LoadSession(ctx context.Context, sessionID string) ([]byte, error)
}

// PublicPreKeyBundle contains the public keys a user (per device) publishes to the server
// for asynchronous X3DH key agreement. This is the Signal Protocol PreKeyBundle.
type PublicPreKeyBundle struct {
	DeviceID      string            `json:"device_id"`      // Unique ID for the device publishing this bundle
	IdentityKey   ed25519.PublicKey `json:"identity_key"`   // Ed25519 identity public key
	SignedPreKey  *ecdh.PublicKey   `json:"-"`              // X25519 signed prekey (public)
	SPKSignature  []byte            `json:"spk_signature"`  // Ed25519 signature of the SPK
	SPKCreatedAt  int64             `json:"spk_created_at"` // Timestamp when the SPK was generated
	OneTimePreKeys map[uint32]*ecdh.PublicKey `json:"-"`    // X25519 one-time prekeys (indexed)

	// Serialized forms for JSON transport
	SignedPreKeyBytes   []byte            `json:"signed_prekey"`
	OneTimePreKeysBytes map[uint32][]byte `json:"one_time_prekeys,omitempty"`
}

// MarshalKeys populates the byte fields from the key objects for transport.
func (b *PublicPreKeyBundle) MarshalKeys() {
	if b.SignedPreKey != nil {
		b.SignedPreKeyBytes = b.SignedPreKey.Bytes()
	}
	if len(b.OneTimePreKeys) > 0 {
		b.OneTimePreKeysBytes = make(map[uint32][]byte)
		for id, key := range b.OneTimePreKeys {
			if key != nil {
				b.OneTimePreKeysBytes[id] = key.Bytes()
			}
		}
	}
}

// UnmarshalKeys populates the key objects from the byte fields after transport.
func (b *PublicPreKeyBundle) UnmarshalKeys() error {
	curve := ecdh.X25519()
	if len(b.SignedPreKeyBytes) > 0 {
		spk, err := curve.NewPublicKey(b.SignedPreKeyBytes)
		if err != nil {
			return err
		}
		b.SignedPreKey = spk
	}
	if len(b.OneTimePreKeysBytes) > 0 {
		b.OneTimePreKeys = make(map[uint32]*ecdh.PublicKey)
		for id, bytes := range b.OneTimePreKeysBytes {
			opk, err := curve.NewPublicKey(bytes)
			if err != nil {
				return err
			}
			b.OneTimePreKeys[id] = opk
		}
	}
	return nil
}
