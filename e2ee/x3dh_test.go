package e2ee

import (
	"bytes"
	"testing"
)

// =====================================================================
// KDF Chain Tests
// =====================================================================

func TestKDFRootProducesDifferentKeys(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := range rootKey {
		rootKey[i] = byte(i)
	}
	dhOutput := make([]byte, 32)
	for i := range dhOutput {
		dhOutput[i] = byte(i + 100)
	}

	newRoot, chainKey := kdfRoot(rootKey, dhOutput)

	if len(newRoot) != 32 {
		t.Fatalf("expected root key 32 bytes, got %d", len(newRoot))
	}
	if len(chainKey) != 32 {
		t.Fatalf("expected chain key 32 bytes, got %d", len(chainKey))
	}
	if bytes.Equal(newRoot, chainKey) {
		t.Fatal("root key and chain key should differ")
	}
	if bytes.Equal(newRoot, rootKey) {
		t.Fatal("new root key should differ from old root key")
	}
}

func TestKDFChainKeyEvolution(t *testing.T) {
	chainKey := make([]byte, 32)
	for i := range chainKey {
		chainKey[i] = 0xAA
	}

	ck1, mk1 := kdfCK(chainKey)
	ck2, mk2 := kdfCK(ck1)

	if bytes.Equal(ck1, ck2) {
		t.Fatal("chain keys should evolve")
	}
	if bytes.Equal(mk1, mk2) {
		t.Fatal("message keys should be unique")
	}
	if bytes.Equal(ck1, mk1) {
		t.Fatal("chain key and message key should differ")
	}
}

func TestKDFChainKeyDeterministic(t *testing.T) {
	chainKey := []byte("deterministic-test-key-32-bytes!!")
	ck1, mk1 := kdfCK(chainKey)
	ck2, mk2 := kdfCK(chainKey)
	if !bytes.Equal(ck1, ck2) || !bytes.Equal(mk1, mk2) {
		t.Fatal("KDF should be deterministic")
	}
}

// =====================================================================
// X3DH Tests
// =====================================================================

func TestX3DHIdentityGeneration(t *testing.T) {
	id, err := NewX3DHIdentity()
	if err != nil {
		t.Fatalf("NewX3DHIdentity: %v", err)
	}
	if len(id.IdentityKey.PublicKey) != 32 {
		t.Fatalf("identity public key should be 32 bytes")
	}
	if len(id.SignedPreKey.PublicKey) != 32 {
		t.Fatalf("signed pre key should be 32 bytes")
	}
	if len(id.SignedPreKey.Signature) == 0 {
		t.Fatal("signed pre key should have a signature")
	}
}

func TestX3DHOneTimePreKeys(t *testing.T) {
	id, _ := NewX3DHIdentity()
	id.GenerateOneTimePreKeys(5)
	if len(id.OneTimePreKeys) != 5 {
		t.Fatalf("expected 5, got %d", len(id.OneTimePreKeys))
	}
	seen := make(map[uint32]bool)
	for keyID := range id.OneTimePreKeys {
		if seen[keyID] {
			t.Fatalf("duplicate key ID: %d", keyID)
		}
		seen[keyID] = true
	}
}

func TestX3DHBundle(t *testing.T) {
	id, _ := NewX3DHIdentity()
	id.GenerateOneTimePreKeys(3)
	bundle := id.GetBundle()
	if bundle == nil {
		t.Fatal("GetBundle returned nil")
	}
	if !bytes.Equal(bundle.IdentityKey, id.IdentityKey.PublicKey) {
		t.Fatal("identity key mismatch")
	}
	if bundle.OneTimePreKeyID == 0 {
		t.Fatal("should include one-time pre-key")
	}
}

func TestX3DHConsumeOneTimePreKey(t *testing.T) {
	id, _ := NewX3DHIdentity()
	id.GenerateOneTimePreKeys(2)
	bundle := id.GetBundle()
	id.ConsumeOneTimePreKey(bundle.OneTimePreKeyID)
	if _, ok := id.OneTimePreKeys[bundle.OneTimePreKeyID]; ok {
		t.Fatal("should be removed")
	}
	if len(id.OneTimePreKeys) != 1 {
		t.Fatalf("expected 1, got %d", len(id.OneTimePreKeys))
	}
}

func TestX3DHFullHandshake(t *testing.T) {
	alice, _ := NewX3DHIdentity()
	bob, _ := NewX3DHIdentity()
	bob.GenerateOneTimePreKeys(5)

	bundle := bob.GetBundle()
	bobOPKPriv := bob.OneTimePreKeys[bundle.OneTimePreKeyID].PrivateKey

	ssAlice, ephAlice, err := X3DHInitiatorKeys(
		alice.IdentityKey.PrivateKey, alice.IdentityKey.PublicKey,
		bundle.IdentityKey, bundle.SignedPreKey, bundle.OneTimePreKey,
	)
	if err != nil {
		t.Fatalf("X3DHInitiator: %v", err)
	}

	ssBob, err := X3DHResponderKeys(
		bob.SignedPreKey.PrivateKey, bobOPKPriv,
		alice.IdentityKey.PublicKey, ephAlice.PublicKey, bob.IdentityKey.PrivateKey,
	)
	if err != nil {
		t.Fatalf("X3DHResponder: %v", err)
	}

	if !bytes.Equal(ssAlice, ssBob) {
		t.Fatalf("shared secrets don't match")
	}
	if len(ssAlice) != 32 {
		t.Fatalf("shared secret should be 32 bytes")
	}
}

// createRatchetPair performs X3DH and creates ratchet sessions for both sides.
func createRatchetPair(t *testing.T) (*RatchetState, *RatchetState) {
	t.Helper()

	alice, _ := NewX3DHIdentity()
	bob, _ := NewX3DHIdentity()
	bob.GenerateOneTimePreKeys(1)

	bundle := bob.GetBundle()
	bobOPKPriv := bob.OneTimePreKeys[bundle.OneTimePreKeyID].PrivateKey

	ssAlice, ephAlice, _ := X3DHInitiatorKeys(
		alice.IdentityKey.PrivateKey, alice.IdentityKey.PublicKey,
		bundle.IdentityKey, bundle.SignedPreKey, bundle.OneTimePreKey,
	)

	ssBob, _ := X3DHResponderKeys(
		bob.SignedPreKey.PrivateKey, bobOPKPriv,
		alice.IdentityKey.PublicKey, ephAlice.PublicKey, bob.IdentityKey.PrivateKey,
	)

	// Alice's ratchet: reuses the X3DH ephemeral key pair as her first ratchet DH key
	aliceR := NewRatchetSession(ssAlice, alice.IdentityKey.PrivateKey, bundle.SignedPreKey, ephAlice)

	// Bob's ratchet: uses his own fresh DH key pair
	bobEph, _ := generateX25519KeyPair()
	bobR := NewRatchetSessionFromReceived(
		ssBob, bob.SignedPreKey.PrivateKey,
		alice.IdentityKey.PublicKey, ephAlice.PublicKey, bobEph,
	)

	return aliceR, bobR
}

func TestX3DHIntegration(t *testing.T) {
	aliceR, bobR := createRatchetPair(t)

	msg1, _ := aliceR.Encrypt([]byte("hello bob"))
	plain1, err := bobR.Decrypt(msg1)
	if err != nil {
		t.Fatalf("bob decrypt: %v", err)
	}
	if string(plain1) != "hello bob" {
		t.Fatalf("mismatch: %q", plain1)
	}

	msg2, _ := aliceR.Encrypt([]byte("second message"))
	plain2, err := bobR.Decrypt(msg2)
	if err != nil {
		t.Fatalf("bob decrypt 2: %v", err)
	}
	if string(plain2) != "second message" {
		t.Fatalf("mismatch: %q", plain2)
	}
}

func TestX3DHBidirectionalRatchet(t *testing.T) {
	aliceR, bobR := createRatchetPair(t)

	// Alice -> Bob
	m1, _ := aliceR.Encrypt([]byte("hi bob"))
	p1, err := bobR.Decrypt(m1)
	if err != nil {
		t.Fatalf("bob decrypt: %v", err)
	}
	if string(p1) != "hi bob" {
		t.Fatalf("expected 'hi bob', got %q", p1)
	}

	// Bob -> Alice (triggers DH ratchet step)
	m2, _ := bobR.Encrypt([]byte("hi alice"))
	p2, err := aliceR.Decrypt(m2)
	if err != nil {
		t.Fatalf("alice decrypt: %v", err)
	}
	if string(p2) != "hi alice" {
		t.Fatalf("expected 'hi alice', got %q", p2)
	}

	// Alice -> Bob (new sending chain)
	m3, _ := aliceR.Encrypt([]byte("how are you"))
	p3, err := bobR.Decrypt(m3)
	if err != nil {
		t.Fatalf("bob decrypt 2: %v", err)
	}
	if string(p3) != "how are you" {
		t.Fatalf("expected 'how are you', got %q", p3)
	}

	// Bob -> Alice
	m4, _ := bobR.Encrypt([]byte("doing great"))
	p4, err := aliceR.Decrypt(m4)
	if err != nil {
		t.Fatalf("alice decrypt 2: %v", err)
	}
	if string(p4) != "doing great" {
		t.Fatalf("expected 'doing great', got %q", p4)
	}
}

func TestRatchetForwardSecrecy(t *testing.T) {
	aliceR, bobR := createRatchetPair(t)

	var messages []*RatchetMessage
	for i := 0; i < 5; i++ {
		msg, _ := aliceR.Encrypt([]byte("message"))
		messages = append(messages, msg)
	}
	for i, msg := range messages {
		plain, err := bobR.Decrypt(msg)
		if err != nil {
			t.Fatalf("decrypt %d: %v", i, err)
		}
		if string(plain) != "message" {
			t.Fatalf("message %d mismatch", i)
		}
	}
}

func TestRatchetOutOfOrderMessages(t *testing.T) {
	aliceR, bobR := createRatchetPair(t)

	m1, _ := aliceR.Encrypt([]byte("msg-1"))
	m2, _ := aliceR.Encrypt([]byte("msg-2"))
	m3, _ := aliceR.Encrypt([]byte("msg-3"))

	// Out of order: 3, 1, 2
	p3, err := bobR.Decrypt(m3)
	if err != nil {
		t.Fatalf("decrypt m3: %v", err)
	}
	if string(p3) != "msg-3" {
		t.Fatalf("expected msg-3, got %q", p3)
	}

	p1, err := bobR.Decrypt(m1)
	if err != nil {
		t.Fatalf("decrypt m1: %v", err)
	}
	if string(p1) != "msg-1" {
		t.Fatalf("expected msg-1, got %q", p1)
	}

	p2, err := bobR.Decrypt(m2)
	if err != nil {
		t.Fatalf("decrypt m2: %v", err)
	}
	if string(p2) != "msg-2" {
		t.Fatalf("expected msg-2, got %q", p2)
	}
}

func TestRatchetSerializeDeserialize(t *testing.T) {
	msg := &RatchetMessage{
		DH: make([]byte, 32), N: 42, PN: 7,
		Ciphertext: []byte("encrypted data here"),
	}
	serialized := SerializeRatchetMessage(msg)
	deserialized, err := DeserializeRatchetMessage(serialized)
	if err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if !bytes.Equal(deserialized.DH, msg.DH) {
		t.Fatal("DH mismatch")
	}
	if deserialized.N != msg.N {
		t.Fatalf("N mismatch")
	}
	if deserialized.PN != msg.PN {
		t.Fatalf("PN mismatch")
	}
	if !bytes.Equal(deserialized.Ciphertext, msg.Ciphertext) {
		t.Fatal("Ciphertext mismatch")
	}
}

func TestRatchetEmptyMessage(t *testing.T) {
	aliceR, bobR := createRatchetPair(t)
	msg, _ := aliceR.Encrypt([]byte{})
	plain, err := bobR.Decrypt(msg)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if len(plain) != 0 {
		t.Fatalf("expected empty, got %d bytes", len(plain))
	}
}

func TestRatchetLargeMessage(t *testing.T) {
	aliceR, bobR := createRatchetPair(t)
	large := make([]byte, 1024*1024)
	for i := range large {
		large[i] = byte(i % 256)
	}
	msg, _ := aliceR.Encrypt(large)
	plain, err := bobR.Decrypt(msg)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(plain, large) {
		t.Fatal("large message mismatch")
	}
}
