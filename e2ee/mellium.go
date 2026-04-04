package e2ee

import (
	"crypto/ecdh"
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"

	"mellium.im/crypto/doubleratchet"
)

// DoubleRatchetSession wraps mellium's DHRatchet with message encryption.
// Each peer maintains one of these for every active conversation.
type DoubleRatchetSession struct {
	ratchet *doubleratchet.DHRatchet
	sendKey []byte // current sending message key
	recvKey []byte // current receiving message key
}

// NewDoubleRatchetInitiator creates a session for the initiator (Alice).
// After X3DH, Alice uses the shared session key as the root key and her
// ephemeral key as the initial ratchet private key.
//
// Flow: Alice calls Step(Bob's SPK public) to derive the first send key.
func NewDoubleRatchetInitiator(sessionKey []byte, ephemeralPriv *ecdh.PrivateKey, peerSPKPub *ecdh.PublicKey) (*DoubleRatchetSession, error) {
	ratchet, err := doubleratchet.NewActive(sessionKey, ephemeralPriv)
	if err != nil {
		return nil, fmt.Errorf("create active ratchet: %w", err)
	}

	// Perform initial ratchet step to derive sending key
	send, _, err := ratchet.Step(peerSPKPub)
	if err != nil {
		return nil, fmt.Errorf("initial ratchet step: %w", err)
	}

	return &DoubleRatchetSession{
		ratchet: ratchet,
		sendKey: send,
	}, nil
}

// NewDoubleRatchetResponder creates a session for the responder (Bob).
// After X3DH, Bob uses the shared session key as the root key and generates
// a new ratchet key pair. Bob waits for Alice's first message before stepping.
func NewDoubleRatchetResponder(sessionKey []byte, spkPriv *ecdh.PrivateKey) (*DoubleRatchetSession, error) {
	ratchet := doubleratchet.NewPassive(sessionKey, spkPriv)
	return &DoubleRatchetSession{
		ratchet: ratchet,
	}, nil
}

// Ratchet advances the ratchet state with the peer's current DH public key.
// Returns new send and recv message keys.
func (s *DoubleRatchetSession) Ratchet(peerPub *ecdh.PublicKey) error {
	send, recv, err := s.ratchet.Step(peerPub)
	if err != nil {
		return fmt.Errorf("ratchet step: %w", err)
	}
	if send != nil {
		s.sendKey = send
	}
	if recv != nil {
		s.recvKey = recv
	}
	return nil
}

// Encrypt encrypts a plaintext message using the current sending key.
// Uses XChaCha20-Poly1305 for AEAD encryption.
func (s *DoubleRatchetSession) Encrypt(plaintext []byte) ([]byte, error) {
	if s.sendKey == nil {
		return nil, fmt.Errorf("no sending key available; ratchet step required")
	}

	aead, err := chacha20poly1305.NewX(s.sendKey)
	if err != nil {
		return nil, fmt.Errorf("create AEAD: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// Seal: nonce || ciphertext+tag
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts a ciphertext message using the current receiving key.
func (s *DoubleRatchetSession) Decrypt(ciphertext []byte) ([]byte, error) {
	if s.recvKey == nil {
		return nil, fmt.Errorf("no receiving key available; ratchet step required")
	}

	aead, err := chacha20poly1305.NewX(s.recvKey)
	if err != nil {
		return nil, fmt.Errorf("create AEAD: %w", err)
	}

	nonceSize := aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return aead.Open(nil, nonce, ct, nil)
}

// SendKey returns the current sending key (useful for session serialization).
func (s *DoubleRatchetSession) SendKey() []byte {
	return s.sendKey
}

// RecvKey returns the current receiving key (useful for session serialization).
func (s *DoubleRatchetSession) RecvKey() []byte {
	return s.recvKey
}
