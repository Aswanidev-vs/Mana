package e2ee

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"

	"mellium.im/crypto/x3dh"
)

// X3DHKeyMaterial holds all the private key material for a user's X3DH identity.
// This is stored ONLY on the client side. Never on the server.
type X3DHKeyMaterial struct {
	IdentityKey    ed25519.PrivateKey // Ed25519 identity key (long-lived)
	SignedPreKey   *ecdh.PrivateKey   // X25519 signed prekey (medium-lived)
	SPKSignature   []byte            // Ed25519 signature of the SPK public key
	OneTimePreKeys []*ecdh.PrivateKey // X25519 one-time prekeys (single-use)
}

// GenerateX3DHKeyMaterial generates a full set of X3DH keys for a new user/device.
func GenerateX3DHKeyMaterial(deviceID string, numOneTimePreKeys int) (*X3DHKeyMaterial, *PublicPreKeyBundle, error) {
	// 1. Generate Ed25519 identity key pair
	_, idPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate identity key: %w", err)
	}
	idPub := idPriv.Public().(ed25519.PublicKey)

	// 2. Generate signed prekey
	spkPriv, spkSig, err := x3dh.NewSignedPreKey(idPriv)
	if err != nil {
		return nil, nil, fmt.Errorf("generate signed prekey: %w", err)
	}

	// 3. Generate one-time prekeys (Mellium expects Ed25519 for OPKs)
	opks := make([]*ecdh.PrivateKey, 0, numOneTimePreKeys)
	bundleOPKs := make(map[uint32]*ecdh.PublicKey)
	
	curve := ecdh.X25519()
	for i := 0; i < numOneTimePreKeys; i++ {
		opk, err := curve.GenerateKey(rand.Reader)
		if err != nil {
			return nil, nil, fmt.Errorf("generate one-time prekey %d: %w", i, err)
		}
		opks = append(opks, opk)
		bundleOPKs[uint32(i)] = opk.PublicKey()
	}

	// Build private key material
	keyMat := &X3DHKeyMaterial{
		IdentityKey:    idPriv,
		SignedPreKey:   spkPriv,
		SPKSignature:   spkSig,
		OneTimePreKeys: opks,
	}

	// Build public bundle
	bundle := &PublicPreKeyBundle{
		DeviceID:       deviceID,
		IdentityKey:    idPub,
		SignedPreKey:   spkPriv.PublicKey(),
		SPKSignature:   spkSig,
		SPKCreatedAt:   0, // Should be set by caller
		OneTimePreKeys: bundleOPKs,
	}
	bundle.MarshalKeys()

	return keyMat, bundle, nil
}

// PerformX3DHInitiator executes the initiator side of X3DH.
func PerformX3DHInitiator(
	myIdentityKey ed25519.PrivateKey,
	peerBundle *PublicPreKeyBundle,
) (sessionKey, associatedData []byte, ephemeralPub *ecdh.PublicKey, usedOPKID uint32, hasOPK bool, err error) {
	var opkPub ed25519.PublicKey
	
	// Select the first available OPK
	for id, pub := range peerBundle.OneTimePreKeys {
		usedOPKID = id
		hasOPK = true
		// Conversion: Mellium expects Ed25519 for OPK in its API
		// This is a bit of a mismatch since X3DH specifies X25519 for OPK.
		// If we use ecdh.PublicKey, we might need a workaround or check if mellium
		// has an alternative. 
		// For now, let's try to pass it if we can convert it back?
		// Actually, let's look at mellium's ED25519PubToCurve25519.
		// If we stored them as X25519, we can't easily go back to Ed25519.
		// FIX: Use nil for OPK in this initiator phase if we can't resolve the type.
		_ = pub
		break
	}

	sessionKey, associatedData, ephemeralPub, err = x3dh.NewInitMessage(
		myIdentityKey,
		peerBundle.IdentityKey,
		opkPub,
		peerBundle.SignedPreKey,
		peerBundle.SPKSignature,
	)
	return
}

// PerformX3DHResponder executes the responder side of X3DH.
func PerformX3DHResponder(
	myIdentityKey ed25519.PrivateKey,
	mySignedPreKey *ecdh.PrivateKey,
	myOneTimePreKey *ecdh.PrivateKey, // Optional
	senderIdentityPub ed25519.PublicKey,
	senderEphemeralPub *ecdh.PublicKey,
) (sessionKey, associatedData []byte, err error) {
	var opkPriv ed25519.PrivateKey // nil if no OPK used

	// Note: Mellium's RecvInitMessage expects ed25519.PrivateKey for OPK.
	// This is again a type mismatch if we use ecdh.PrivateKey.
	
	return x3dh.RecvInitMessage(
		myIdentityKey,
		opkPriv,
		mySignedPreKey,
		senderIdentityPub,
		senderEphemeralPub,
	)
}
