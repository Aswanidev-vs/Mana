package e2ee

import (
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
)

// NOTE: In a Zero-Trust E2EE system, the server NEVER generates or stores private keys.
// This package provides utilities for CLIENTS to use, and a HandshakeManager for the server
// to facilitate public key exchange between peers.

// --- Encryptor (renamed from ClientEncryptor for test compatibility) ---

// Encryptor is a utility for encryption/decryption using XChaCha20-Poly1305.
type Encryptor = ClientEncryptor

// ClientEncryptor is a utility for client-side encryption/decryption using XChaCha20-Poly1305.
type ClientEncryptor struct {
	aead cipher.AEAD
}

// GenerateKey generates a random 32-byte key suitable for XChaCha20-Poly1305.
func GenerateKey() ([]byte, error) {
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	return key, nil
}

// NewEncryptor creates a new encryptor using a 32-byte shared secret.
func NewEncryptor(sharedSecret []byte) (*ClientEncryptor, error) {
	if len(sharedSecret) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("invalid key size: expected 32 bytes")
	}
	// XChaCha20-Poly1305: 256-bit key, 24-byte extended nonce, AEAD (authenticated encryption).
	// DevSkim: false positive — this is the AEAD construction (not a weak stream cipher mode).
	aead, err := chacha20poly1305.NewX(sharedSecret)
	if err != nil {
		return nil, err
	}
	return &ClientEncryptor{aead: aead}, nil
}

// Encrypt encrypts plaintext and prepends a random 24-byte nonce.
func (e *ClientEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return e.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts ciphertext with leading nonce.
func (e *ClientEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := e.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return e.aead.Open(nil, nonce, ct, nil)
}

// RotateKey rotates the encryption key. Old ciphertexts cannot be decrypted with the new key.
func (e *ClientEncryptor) RotateKey(newKey []byte) error {
	if len(newKey) != chacha20poly1305.KeySize {
		return fmt.Errorf("invalid key size: expected 32 bytes")
	}
	aead, err := chacha20poly1305.NewX(newKey)
	if err != nil {
		return err
	}
	e.aead = aead
	return nil
}

// --- HandshakeManager ---

// HandshakeManager facilitates public key exchange between peers.
// It stores ONLY public keys temporarily during the handshake process.
type HandshakeManager struct {
	mu         sync.RWMutex
	publicKeys map[string][]byte // peerID -> publicKey
}

// NewHandshakeManager creates a new server-side handshake facilitator.
func NewHandshakeManager() *HandshakeManager {
	return &HandshakeManager{
		publicKeys: make(map[string][]byte),
	}
}

// StorePublicKey stores a peer's public key (received from the client).
func (hm *HandshakeManager) StorePublicKey(peerID string, pubKey []byte) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.publicKeys[peerID] = pubKey
}

// GetPublicKey retrieves a peer's public key.
func (hm *HandshakeManager) GetPublicKey(peerID string) ([]byte, bool) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	pub, ok := hm.publicKeys[peerID]
	return pub, ok
}

// RemovePeer cleans up public key material for a peer.
func (hm *HandshakeManager) RemovePeer(peerID string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	delete(hm.publicKeys, peerID)
}

// --- X25519 Key Exchange ---

// KeyPair holds an X25519 public/private key pair.
type KeyPair struct {
	PublicKey  []byte
	PrivateKey []byte
}

// X25519KeyExchange implements Diffie-Hellman key exchange using Curve25519.
type X25519KeyExchange struct {
	mu    sync.RWMutex
	pairs map[string]*KeyPair
}

// NewX25519KeyExchange creates a new X25519 key exchange manager.
func NewX25519KeyExchange() *X25519KeyExchange {
	return &X25519KeyExchange{
		pairs: make(map[string]*KeyPair),
	}
}

// GenerateKeyPair generates a new X25519 key pair for a peer.
func (kx *X25519KeyExchange) GenerateKeyPair(peerID string) (*KeyPair, error) {
	privateKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, privateKey); err != nil {
		return nil, fmt.Errorf("generate private key: %w", err)
	}

	publicKey, err := curve25519.X25519(privateKey, curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("derive public key: %w", err)
	}

	kp := &KeyPair{
		PublicKey:  publicKey,
		PrivateKey: privateKey,
	}

	kx.mu.Lock()
	kx.pairs[peerID] = kp
	kx.mu.Unlock()

	return kp, nil
}

// GetPublicKey returns the public key for a peer.
func (kx *X25519KeyExchange) GetPublicKey(peerID string) ([]byte, error) {
	kx.mu.RLock()
	defer kx.mu.RUnlock()

	kp, ok := kx.pairs[peerID]
	if !ok {
		return nil, fmt.Errorf("no key pair for peer %s", peerID)
	}
	return kp.PublicKey, nil
}

// DeriveSharedSecret computes the shared secret using ECDH (Curve25519).
// The caller must supply the remote peer's public key.
func (kx *X25519KeyExchange) DeriveSharedSecret(peerID string, remotePublicKey []byte) ([]byte, error) {
	kx.mu.RLock()
	kp, ok := kx.pairs[peerID]
	kx.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no key pair for peer %s", peerID)
	}

	shared, err := curve25519.X25519(kp.PrivateKey, remotePublicKey)
	if err != nil {
		return nil, fmt.Errorf("derive shared secret: %w", err)
	}

	return shared, nil
}

// RemovePeer zeroes out the private key and removes the key pair.
func (kx *X25519KeyExchange) RemovePeer(peerID string) {
	kx.mu.Lock()
	defer kx.mu.Unlock()

	if kp, ok := kx.pairs[peerID]; ok {
		// Zero the private key material
		for i := range kp.PrivateKey {
			kp.PrivateKey[i] = 0
		}
		delete(kx.pairs, peerID)
	}
}

// --- Legacy SimpleKeyExchange ---

// SimpleKeyExchange is a legacy key exchange that just returns the remote key directly.
// Kept for backwards compatibility with tests.
type SimpleKeyExchange struct {
	mu    sync.RWMutex
	pairs map[string]*KeyPair
}

// NewSimpleKeyExchange creates a new simple key exchange manager.
func NewSimpleKeyExchange() *SimpleKeyExchange {
	return &SimpleKeyExchange{
		pairs: make(map[string]*KeyPair),
	}
}

// GenerateKeyPair generates a random key pair for a peer.
func (kx *SimpleKeyExchange) GenerateKeyPair(peerID string) (*KeyPair, error) {
	key, err := GenerateKey()
	if err != nil {
		return nil, err
	}

	kp := &KeyPair{
		PublicKey:  key,
		PrivateKey: key,
	}

	kx.mu.Lock()
	kx.pairs[peerID] = kp
	kx.mu.Unlock()

	return kp, nil
}

// ExchangeKey performs a simple key exchange by returning the remote key.
func (kx *SimpleKeyExchange) ExchangeKey(peerID string, remoteKey []byte) ([]byte, error) {
	return remoteKey, nil
}
