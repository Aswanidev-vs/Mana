package e2ee

import (
	"bytes"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	plaintext := []byte("hello mana framework")

	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext should differ from plaintext")
	}

	decrypted, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted %q != plaintext %q", decrypted, plaintext)
	}
}

func TestEncryptDifferentCiphertexts(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	plaintext := []byte("same message")

	ct1, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt 1: %v", err)
	}

	ct2, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt 2: %v", err)
	}

	if bytes.Equal(ct1, ct2) {
		t.Fatal("two encryptions of same plaintext should produce different ciphertexts")
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	plaintext := []byte("secret message")
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Tamper with the ciphertext
	ciphertext[len(ciphertext)-1] ^= 0xFF

	_, err = enc.Decrypt(ciphertext)
	if err == nil {
		t.Fatal("Decrypt should fail on tampered ciphertext")
	}
}

func TestDecryptTooShort(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	_, err = enc.Decrypt([]byte("short"))
	if err == nil {
		t.Fatal("Decrypt should fail on too-short ciphertext")
	}
}

func TestRotateKey(t *testing.T) {
	key1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey 1: %v", err)
	}

	enc, err := NewEncryptor(key1)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	plaintext := []byte("test rotation")
	ct1, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	key2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey 2: %v", err)
	}

	if err := enc.RotateKey(key2); err != nil {
		t.Fatalf("RotateKey: %v", err)
	}

	// Old ciphertext should fail to decrypt with new key
	_, err = enc.Decrypt(ct1)
	if err == nil {
		t.Fatal("old ciphertext should not decrypt with new key")
	}

	// New encryption should work
	ct2, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt with new key: %v", err)
	}

	decrypted, err := enc.Decrypt(ct2)
	if err != nil {
		t.Fatalf("Decrypt with new key: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted %q != plaintext %q", decrypted, plaintext)
	}
}

func TestEncryptEmptyPlaintext(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	ciphertext, err := enc.Encrypt([]byte{})
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}

	decrypted, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt empty: %v", err)
	}

	if len(decrypted) != 0 {
		t.Fatalf("expected empty plaintext, got %d bytes", len(decrypted))
	}
}

// --- X25519 Key Exchange Tests ---

func TestX25519KeyExchangeGenerateKeyPair(t *testing.T) {
	kx := NewX25519KeyExchange()

	kp, err := kx.GenerateKeyPair("peer1")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	if len(kp.PublicKey) != 32 {
		t.Fatalf("expected public key length 32, got %d", len(kp.PublicKey))
	}

	if len(kp.PrivateKey) != 32 {
		t.Fatalf("expected private key length 32, got %d", len(kp.PrivateKey))
	}

	// Public and private keys should be different
	if bytes.Equal(kp.PublicKey, kp.PrivateKey) {
		t.Fatal("public and private key should differ")
	}
}

func TestX25519KeyExchangeGetPublicKey(t *testing.T) {
	kx := NewX25519KeyExchange()

	kp, err := kx.GenerateKeyPair("peer1")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	pubKey, err := kx.GetPublicKey("peer1")
	if err != nil {
		t.Fatalf("GetPublicKey: %v", err)
	}

	if !bytes.Equal(pubKey, kp.PublicKey) {
		t.Fatal("GetPublicKey should return the same public key")
	}

	// Should fail for unknown peer
	_, err = kx.GetPublicKey("unknown")
	if err == nil {
		t.Fatal("expected error for unknown peer")
	}
}

func TestX25519DeriveSharedSecret(t *testing.T) {
	kx := NewX25519KeyExchange()

	// Generate key pairs for two peers
	kpA, err := kx.GenerateKeyPair("alice")
	if err != nil {
		t.Fatalf("GenerateKeyPair alice: %v", err)
	}

	kpB, err := kx.GenerateKeyPair("bob")
	if err != nil {
		t.Fatalf("GenerateKeyPair bob: %v", err)
	}

	// Alice computes shared secret with Bob's public key
	sharedA, err := kx.DeriveSharedSecret("alice", kpB.PublicKey)
	if err != nil {
		t.Fatalf("DeriveSharedSecret alice: %v", err)
	}

	// Bob computes shared secret with Alice's public key
	sharedB, err := kx.DeriveSharedSecret("bob", kpA.PublicKey)
	if err != nil {
		t.Fatalf("DeriveSharedSecret bob: %v", err)
	}

	// Both should derive the same shared secret (Diffie-Hellman property)
	if !bytes.Equal(sharedA, sharedB) {
		t.Fatal("shared secrets should be equal (DH key agreement)")
	}

	if len(sharedA) != 32 {
		t.Fatalf("expected shared secret length 32, got %d", len(sharedA))
	}
}

func TestX25519RemovePeerZeroesKey(t *testing.T) {
	kx := NewX25519KeyExchange()

	_, err := kx.GenerateKeyPair("peer1")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	kx.RemovePeer("peer1")

	// Should fail after removal
	_, err = kx.GetPublicKey("peer1")
	if err == nil {
		t.Fatal("expected error after peer removal")
	}
}

func TestX25519DeriveSharedSecretUnknownPeer(t *testing.T) {
	kx := NewX25519KeyExchange()

	dummyKey := make([]byte, 32)
	_, err := kx.DeriveSharedSecret("unknown", dummyKey)
	if err == nil {
		t.Fatal("expected error for unknown peer")
	}
}

// --- Legacy SimpleKeyExchange Tests ---

func TestSimpleKeyExchange(t *testing.T) {
	kx := NewSimpleKeyExchange()

	kp, err := kx.GenerateKeyPair("peer1")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	if len(kp.PublicKey) != 32 {
		t.Fatalf("expected key length 32, got %d", len(kp.PublicKey))
	}

	remoteKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	result, err := kx.ExchangeKey("peer2", remoteKey)
	if err != nil {
		t.Fatalf("ExchangeKey: %v", err)
	}

	if !bytes.Equal(result, remoteKey) {
		t.Fatal("exchange result should match remote key")
	}
}
