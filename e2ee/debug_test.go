package e2ee

import (
	"bytes"
	"testing"

	"golang.org/x/crypto/curve25519"
)

func TestDebugDH(t *testing.T) {
	alice, _ := NewX3DHIdentity()
	bob, _ := NewX3DHIdentity()
	bob.GenerateOneTimePreKeys(1)

	bundle := bob.GetBundle()
	opkID := bundle.OneTimePreKeyID
	bobOPKPriv := bob.OneTimePreKeys[opkID].PrivateKey

	ssAlice, ephAlice, _ := X3DHInitiatorKeys(
		alice.IdentityKey.PrivateKey,
		alice.IdentityKey.PublicKey,
		bundle.IdentityKey,
		bundle.SignedPreKey,
		bundle.OneTimePreKey,
	)

	ssBob, _ := X3DHResponderKeys(
		bob.SignedPreKey.PrivateKey,
		bobOPKPriv,
		alice.IdentityKey.PublicKey,
		ephAlice.PublicKey,
		bob.IdentityKey.PrivateKey,
	)

	t.Logf("ss match: %v", bytes.Equal(ssAlice, ssBob))

	dhAlice, _ := curve25519.X25519(alice.IdentityKey.PrivateKey, bundle.SignedPreKey)
	dhBob, _ := curve25519.X25519(bob.SignedPreKey.PrivateKey, alice.IdentityKey.PublicKey)
	t.Logf("DH match: %v", bytes.Equal(dhAlice, dhBob))

	_, ckAlice := kdfRK(ssAlice, dhAlice)
	_, ckBob := kdfRK(ssBob, dhBob)
	t.Logf("CK match: %v", bytes.Equal(ckAlice, ckBob))

	// Bob's CKs uses DH(bobSPKPriv, ephPub)
	dhBobSend, _ := curve25519.X25519(bob.SignedPreKey.PrivateKey, ephAlice.PublicKey)
	_, bobCKsInit := kdfRK(ssBob, dhBobSend)
	t.Logf("Bob CKs init: %x", bobCKsInit[:8])

	// Alice's ratchet uses ephAlice as DHs
	aliceR := NewRatchetSession(ssAlice, alice.IdentityKey.PrivateKey, bundle.SignedPreKey, ephAlice)
	bobEph, _ := generateX25519KeyPair()
	bobR := NewRatchetSessionFromReceived(ssBob, bob.SignedPreKey.PrivateKey, alice.IdentityKey.PublicKey, ephAlice.PublicKey, bobEph)

	// Alice sends m1
	m1, _ := aliceR.Encrypt([]byte("test"))
	t.Logf("Alice m1.DH: %x (ephAlice: %x)", m1.DH[:8], ephAlice.PublicKey[:8])

	// Bob decrypts
	p1, err := bobR.Decrypt(m1)
	t.Logf("Bob decrypt: %q, err: %v", p1, err)
}
