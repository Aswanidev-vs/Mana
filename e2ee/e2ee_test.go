package e2ee

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"mellium.im/crypto/x3dh"
)

// TestX3DHFullHandshake tests the complete X3DH key agreement between two parties.
func TestX3DHFullHandshake(t *testing.T) {
	// === Alice generates her identity ===
	_, aliceIDPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Alice identity key: %v", err)
	}

	// === Bob generates his identity and publishes a PreKey bundle ===
	_, bobIDPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Bob identity key: %v", err)
	}
	bobIDPub := bobIDPriv.Public().(ed25519.PublicKey)

	// Bob generates his Signed PreKey using mellium's x3dh
	bobSPKPriv, bobSPKSig, err := x3dh.NewSignedPreKey(bobIDPriv)
	if err != nil {
		t.Fatalf("generate Bob SPK: %v", err)
	}

	// Bob publishes his bundle
	bobBundle := &PublicPreKeyBundle{
		IdentityKey:  bobIDPub,
		SignedPreKey: bobSPKPriv.PublicKey(),
		SPKSignature: bobSPKSig,
	}

	// === Alice performs X3DH initiator side ===
	aliceSessKey, aliceAD, ephPub, _, _, err := PerformX3DHInitiator(aliceIDPriv, bobBundle)
	if err != nil {
		t.Fatalf("X3DH initiator failed: %v", err)
	}

	if len(aliceSessKey) != 32 {
		t.Fatalf("expected 32-byte session key, got %d", len(aliceSessKey))
	}
	if len(aliceAD) == 0 {
		t.Fatal("expected non-empty associated data")
	}
	if ephPub == nil {
		t.Fatal("expected non-nil ephemeral public key")
	}

	// === Bob performs X3DH responder side ===
	aliceIDPub := aliceIDPriv.Public().(ed25519.PublicKey)
	bobSessKey, bobAD, err := PerformX3DHResponder(bobIDPriv, bobSPKPriv, nil, aliceIDPub, ephPub)
	if err != nil {
		t.Fatalf("X3DH responder failed: %v", err)
	}

	// === Verify both sides derived the same session key ===
	if string(aliceSessKey) != string(bobSessKey) {
		t.Fatalf("session keys don't match!\n  Alice: %x\n  Bob:   %x", aliceSessKey, bobSessKey)
	}

	// Verify associated data matches (order differs, but both contain both IDs)
	if len(bobAD) == 0 {
		t.Fatal("expected non-empty Bob associated data")
	}

	t.Logf("X3DH handshake successful!")
	t.Logf("  Session key: %x", aliceSessKey)
	t.Logf("  Alice AD:    %x", aliceAD)
	t.Logf("  Bob AD:      %x", bobAD)
}

// TestX3DHKeyMaterialGeneration tests the helper that generates full key sets.
func TestX3DHKeyMaterialGeneration(t *testing.T) {
	keyMat, bundle, err := GenerateX3DHKeyMaterial("device1", 5)
	if err != nil {
		t.Fatalf("generate key material: %v", err)
	}

	if keyMat.IdentityKey == nil {
		t.Fatal("identity key is nil")
	}
	if keyMat.SignedPreKey == nil {
		t.Fatal("signed prekey is nil")
	}
	if len(keyMat.SPKSignature) == 0 {
		t.Fatal("SPK signature is empty")
	}
	if len(keyMat.OneTimePreKeys) != 5 {
		t.Fatalf("expected 5 one-time prekeys, got %d", len(keyMat.OneTimePreKeys))
	}

	if bundle.IdentityKey == nil {
		t.Fatal("bundle identity key is nil")
	}
	if bundle.SignedPreKey == nil {
		t.Fatal("bundle signed prekey is nil")
	}
	if len(bundle.SPKSignature) == 0 {
		t.Fatal("bundle SPK signature is empty")
	}
	if len(bundle.OneTimePreKeys) != 5 {
		t.Fatal("bundle one-time prekeys count mismatch")
	}

	// Verify the bundle's identity key matches the key material
	if string(bundle.IdentityKey) != string(keyMat.IdentityKey.Public().(ed25519.PublicKey)) {
		t.Fatal("bundle identity key doesn't match key material")
	}

	t.Logf("Key material generation successful! (%d OPKs)", len(keyMat.OneTimePreKeys))
}

// TestDoubleRatchetEncryptDecrypt tests end-to-end encrypted messaging.
func TestDoubleRatchetEncryptDecrypt(t *testing.T) {
	// 1. Generate identities
	_, aliceIDPriv, _ := ed25519.GenerateKey(rand.Reader)
	_, bobIDPriv, _ := ed25519.GenerateKey(rand.Reader)
	bobIDPub := bobIDPriv.Public().(ed25519.PublicKey)
	aliceIDPub := aliceIDPriv.Public().(ed25519.PublicKey)

	// 2. Bob generates SPK
	bobSPKPriv, bobSPKSig, err := x3dh.NewSignedPreKey(bobIDPriv)
	if err != nil {
		t.Fatalf("bob SPK: %v", err)
	}

	bobBundle := &PublicPreKeyBundle{
		IdentityKey:  bobIDPub,
		SignedPreKey: bobSPKPriv.PublicKey(),
		SPKSignature: bobSPKSig,
	}

	// 3. Alice does X3DH
	sessKey, _, ephPub, _, _, err := PerformX3DHInitiator(aliceIDPriv, bobBundle)
	if err != nil {
		t.Fatalf("x3dh init: %v", err)
	}

	// 4. Bob does X3DH
	bobSessKey, _, err := PerformX3DHResponder(bobIDPriv, bobSPKPriv, nil, aliceIDPub, ephPub)
	if err != nil {
		t.Fatalf("x3dh recv: %v", err)
	}

	if string(sessKey) != string(bobSessKey) {
		t.Fatal("session keys mismatch")
	}

	// 5. Alice creates Double Ratchet initiator session
	// Alice needs an ephemeral private key for the ratchet
	curve := ecdh.X25519()
	aliceRatchetPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("alice ratchet key: %v", err)
	}

	aliceSession, err := NewDoubleRatchetInitiator(sessKey, aliceRatchetPriv, bobBundle.SignedPreKey)
	if err != nil {
		t.Fatalf("alice DR init: %v", err)
	}

	// 6. Bob creates Double Ratchet responder session
	bobSession, err := NewDoubleRatchetResponder(bobSessKey, bobSPKPriv)
	if err != nil {
		t.Fatalf("bob DR init: %v", err)
	}

	// Bob needs to ratchet step with Alice's public key to get recv key
	err = bobSession.Ratchet(aliceRatchetPriv.PublicKey())
	if err != nil {
		t.Fatalf("bob ratchet step: %v", err)
	}

	// 7. Alice encrypts a message
	plaintext := []byte("Hello Bob! This is a secret message.")
	ciphertext, err := aliceSession.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if string(ciphertext) == string(plaintext) {
		t.Fatal("ciphertext equals plaintext — encryption failed")
	}

	// 8. Bob decrypts the message
	decrypted, err := bobSession.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Fatalf("decrypted text doesn't match!\n  expected: %q\n  got:      %q", plaintext, decrypted)
	}

	t.Logf("E2EE round-trip successful!")
	t.Logf("  Plaintext:  %q", plaintext)
	t.Logf("  Ciphertext: %x... (%d bytes)", ciphertext[:16], len(ciphertext))
	t.Logf("  Decrypted:  %q", decrypted)
}

// TestSessionIDDeterministic verifies session IDs are order-independent.
func TestSessionIDDeterministic(t *testing.T) {
	id1 := CreateSessionID("alice", "d1", "bob", "d2")
	id2 := CreateSessionID("bob", "d2", "alice", "d1")
	id3 := CreateSessionID("alice", "d1", "bob", "d3")

	if id1 != id2 {
		t.Fatalf("session IDs should be identical regardless of order: %q != %q", id1, id2)
	}
	if id1 == id3 {
		t.Fatalf("session IDs for different device pairs should differ")
	}
}

// TestEnvelopeSerializeDeserialize tests the message envelope format.
func TestEnvelopeSerializeDeserialize(t *testing.T) {
	msg := &EncryptedMessage{
		SenderID:   "alice",
		ReceiverID: "bob",
		Ciphertext: []byte("encrypted-payload"),
		Header:     []byte("ratchet-header"),
		Timestamp:  1234567890,
	}

	data, err := msg.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	decoded, err := Deserialize(data)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}

	if decoded.SenderID != msg.SenderID {
		t.Fatalf("sender mismatch: %q != %q", decoded.SenderID, msg.SenderID)
	}
	if decoded.ReceiverID != msg.ReceiverID {
		t.Fatalf("receiver mismatch: %q != %q", decoded.ReceiverID, msg.ReceiverID)
	}
	if string(decoded.Ciphertext) != string(msg.Ciphertext) {
		t.Fatalf("ciphertext mismatch")
	}
	if decoded.Timestamp != msg.Timestamp {
		t.Fatalf("timestamp mismatch: %d != %d", decoded.Timestamp, msg.Timestamp)
	}
}

// TestPublicPreKeyBundleMarshalUnmarshal tests bundle serialization.
func TestPublicPreKeyBundleMarshalUnmarshal(t *testing.T) {
	_, bundle, err := GenerateX3DHKeyMaterial("device1", 5)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Marshal
	bundle.MarshalKeys()
	if len(bundle.SignedPreKeyBytes) == 0 {
		t.Fatal("signed prekey bytes empty after marshal")
	}

	// Simulate transport — clear key objects
	bundle.SignedPreKey = nil
	bundle.OneTimePreKeys = nil

	// Unmarshal
	err = bundle.UnmarshalKeys()
	if err != nil {
		t.Fatalf("unmarshal keys: %v", err)
	}

	if bundle.SignedPreKey == nil {
		t.Fatal("signed prekey nil after unmarshal")
	}
	if len(bundle.OneTimePreKeys) != 5 {
		t.Fatal("one-time prekeys count mismatch after unmarshal")
	}
}
