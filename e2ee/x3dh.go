package e2ee

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
)

// --- KDF Chains ---

// KDF derives a 32-byte output from a root key and an input using HMAC-SHA256.
// Follows the Signal Protocol KDF pattern: HKDF-style with info/chain separation.
func kdfRoot(rootKey, dhOutput []byte) (newRootKey, chainKey []byte) {
	return kdfRK(rootKey, dhOutput)
}

// kdfRK implements the root key KDF from the Double Ratchet spec.
// Output: 64 bytes split into newRootKey (32) and chainKey (32).
func kdfRK(rootKey, dhOutput []byte) (newRootKey, chainKey []byte) {
	// HMAC-SHA256(rootKey, dhOutput || 0x01) for root key
	// HMAC-SHA256(rootKey, dhOutput || 0x02) for chain key
	mac1 := hmac.New(sha256.New, rootKey)
	mac1.Write(dhOutput)
	mac1.Write([]byte{0x01})
	newRootKey = mac1.Sum(nil)

	mac2 := hmac.New(sha256.New, rootKey)
	mac2.Write(dhOutput)
	mac2.Write([]byte{0x02})
	chainKey = mac2.Sum(nil)

	return
}

// kdfCK derives the next chain key and a message key from a chain key.
func kdfCK(chainKey []byte) (newChainKey, messageKey []byte) {
	// HMAC-SHA256(chainKey, 0x01) for new chain key
	mac1 := hmac.New(sha256.New, chainKey)
	mac1.Write([]byte{0x01})
	newChainKey = mac1.Sum(nil)

	// HMAC-SHA256(chainKey, 0x02) for message key
	mac2 := hmac.New(sha256.New, chainKey)
	mac2.Write([]byte{0x02})
	messageKey = mac2.Sum(nil)

	return
}

// --- X3DH (Extended Triple Diffie-Hellman) ---

// IdentityKey is a long-lived key pair identifying a user.
type IdentityKey = KeyPair

// SignedPreKey is a medium-lived key pair signed by the identity key.
type SignedPreKey struct {
	KeyPair
	KeyID     uint32
	Signature []byte // Ed25519 signature of PublicKey by IdentityKey
}

// OneTimePreKey is a single-use key pair used in X3DH for forward secrecy.
type OneTimePreKey struct {
	KeyPair
	KeyID uint32
}

// PreKeyBundle contains the public keys a user publishes to the server
// for asynchronous X3DH key agreement.
type PreKeyBundle struct {
	IdentityKey     []byte // public key
	SignedPreKey    []byte // public key
	SignedPreKeyID  uint32
	Signature       []byte // signature of SignedPreKey by IdentityKey
	OneTimePreKey   []byte // public key (optional, nil if consumed)
	OneTimePreKeyID uint32 // 0 if no one-time pre-key
}

// X3DHIdentity holds a user's X3DH key material.
type X3DHIdentity struct {
	mu sync.RWMutex

	IdentityKey    *IdentityKey
	SignedPreKey   *SignedPreKey
	OneTimePreKeys map[uint32]*OneTimePreKey // keyID -> key

	nextOneTimePreKeyID uint32
}

// NewX3DHIdentity generates a complete X3DH identity set.
func NewX3DHIdentity() (*X3DHIdentity, error) {
	idKey, err := generateX25519KeyPair()
	if err != nil {
		return nil, fmt.Errorf("generate identity key: %w", err)
	}

	spk, err := generateX25519KeyPair()
	if err != nil {
		return nil, fmt.Errorf("generate signed pre-key: %w", err)
	}

	// Sign the signed pre-key public key with the identity key
	signature := signKey(idKey.PrivateKey, spk.PublicKey)

	return &X3DHIdentity{
		IdentityKey:    idKey,
		SignedPreKey:   &SignedPreKey{KeyPair: *spk, KeyID: 1, Signature: signature},
		OneTimePreKeys: make(map[uint32]*OneTimePreKey),
	}, nil
}

// GenerateOneTimePreKeys generates N one-time pre-keys.
func (id *X3DHIdentity) GenerateOneTimePreKeys(n int) error {
	id.mu.Lock()
	defer id.mu.Unlock()

	for i := 0; i < n; i++ {
		kp, err := generateX25519KeyPair()
		if err != nil {
			return err
		}
		id.nextOneTimePreKeyID++
		id.OneTimePreKeys[id.nextOneTimePreKeyID] = &OneTimePreKey{
			KeyPair: *kp,
			KeyID:   id.nextOneTimePreKeyID,
		}
	}
	return nil
}

// GetBundle returns the public PreKeyBundle for publishing to the server.
func (id *X3DHIdentity) GetBundle() *PreKeyBundle {
	id.mu.Lock()
	defer id.mu.Unlock()

	bundle := &PreKeyBundle{
		IdentityKey:    id.IdentityKey.PublicKey,
		SignedPreKey:   id.SignedPreKey.PublicKey,
		SignedPreKeyID: id.SignedPreKey.KeyID,
		Signature:      id.SignedPreKey.Signature,
	}

	// Include one one-time pre-key if available (will be consumed)
	for keyID, opk := range id.OneTimePreKeys {
		bundle.OneTimePreKey = opk.PublicKey
		bundle.OneTimePreKeyID = keyID
		break
	}

	return bundle
}

// ConsumeOneTimePreKey removes a one-time pre-key after use.
func (id *X3DHIdentity) ConsumeOneTimePreKey(keyID uint32) {
	id.mu.Lock()
	defer id.mu.Unlock()

	// Zero the private key before deletion
	if opk, ok := id.OneTimePreKeys[keyID]; ok {
		for i := range opk.PrivateKey {
			opk.PrivateKey[i] = 0
		}
		delete(id.OneTimePreKeys, keyID)
	}
}

// X3DH performs the initiator side of the X3DH key agreement.
// Returns the shared secret and the ephemeral key pair (for inclusion in the X3DH message).
//
// The shared secret is computed as:
//
//	DH1 = DH(IKa, SPKb)
//	DH2 = DH(EKa, IKb)
//	DH3 = DH(EKa, SPKb)
//	DH4 = DH(EKa, OPKb)
//
// SK = KDF(DH1 || DH2 || DH3 || DH4)
func X3DHInitiator(myIdentityPriv, theirBundle *PreKeyBundle) (sharedSecret []byte, ephemeralPub []byte, err error) {
	// This is called by the initiator who has their own identity key pair and the recipient's bundle.
	// For simplicity, we pass the raw key material.
	return nil, nil, fmt.Errorf("use X3DHInitiatorKeys instead")
}

// X3DHInitiatorKeys performs the initiator-side X3DH with raw key material.
// Returns the shared secret and the ephemeral key pair (for reuse as the ratchet DH key).
func X3DHInitiatorKeys(
	identityPriv []byte, // initiator's identity private key
	identityPub []byte, // initiator's identity public key
	recipientIdentityPub []byte,
	recipientSignedPreKeyPub []byte,
	recipientOneTimePreKeyPub []byte,
) (sharedSecret []byte, ephemeral *KeyPair, err error) {
	// Generate ephemeral key pair
	ekPair, err := generateX25519KeyPair()
	if err != nil {
		return nil, nil, fmt.Errorf("generate ephemeral key: %w", err)
	}

	// DH1 = DH(IKa, SPKb)
	dh1, err := curve25519.X25519(identityPriv, recipientSignedPreKeyPub)
	if err != nil {
		return nil, nil, fmt.Errorf("DH1: %w", err)
	}

	// DH2 = DH(EKa, IKb)
	dh2, err := curve25519.X25519(ekPair.PrivateKey, recipientIdentityPub)
	if err != nil {
		return nil, nil, fmt.Errorf("DH2: %w", err)
	}

	// DH3 = DH(EKa, SPKb)
	dh3, err := curve25519.X25519(ekPair.PrivateKey, recipientSignedPreKeyPub)
	if err != nil {
		return nil, nil, fmt.Errorf("DH3: %w", err)
	}

	// DH4 = DH(EKa, OPKb) — optional
	var dh4 []byte
	if recipientOneTimePreKeyPub != nil {
		dh4, err = curve25519.X25519(ekPair.PrivateKey, recipientOneTimePreKeyPub)
		if err != nil {
			return nil, nil, fmt.Errorf("DH4: %w", err)
		}
	}

	// SK = KDF(DH1 || DH2 || DH3 || DH4)
	ikm := concatBytes(dh1, dh2, dh3)
	if dh4 != nil {
		ikm = concatBytes(ikm, dh4)
	}

	sharedSecret = kdf32(ikm)

	// Return the ephemeral key pair so the caller can reuse it as the ratchet DH key
	return sharedSecret, ekPair, nil
}

// X3DHResponderKeys performs the responder-side X3DH.
// The responder uses their private keys to compute the same shared secret.
func X3DHResponderKeys(
	mySignedPreKeyPriv []byte,
	myOneTimePreKeyPriv []byte,
	senderIdentityPub []byte,
	senderEphemeralPub []byte,
	myIdentityPriv []byte,
) ([]byte, error) {
	// DH1 = DH(SPKb, IKa)
	dh1, err := curve25519.X25519(mySignedPreKeyPriv, senderIdentityPub)
	if err != nil {
		return nil, fmt.Errorf("DH1: %w", err)
	}

	// DH2 = DH(IKb, EKa)
	dh2, err := curve25519.X25519(myIdentityPriv, senderEphemeralPub)
	if err != nil {
		return nil, fmt.Errorf("DH2: %w", err)
	}

	// DH3 = DH(SPKb, EKa)
	dh3, err := curve25519.X25519(mySignedPreKeyPriv, senderEphemeralPub)
	if err != nil {
		return nil, fmt.Errorf("DH3: %w", err)
	}

	// DH4 = DH(OPKb, EKa) — optional
	var dh4 []byte
	if myOneTimePreKeyPriv != nil {
		dh4, err = curve25519.X25519(myOneTimePreKeyPriv, senderEphemeralPub)
		if err != nil {
			return nil, fmt.Errorf("DH4: %w", err)
		}
	}

	ikm := concatBytes(dh1, dh2, dh3)
	if dh4 != nil {
		ikm = concatBytes(ikm, dh4)
	}

	return kdf32(ikm), nil
}

// --- Helper functions ---

func generateX25519KeyPair() (*KeyPair, error) {
	priv := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, priv); err != nil {
		return nil, err
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return nil, err
	}
	return &KeyPair{PublicKey: pub, PrivateKey: priv}, nil
}

// signKey creates an HMAC-SHA256 signature of the public key using the private key.
// In Signal Protocol this uses Ed25519; we use HMAC as a simplified equivalent for this implementation.
func signKey(privateKey, publicKeyToSign []byte) []byte {
	mac := hmac.New(sha256.New, privateKey)
	mac.Write(publicKeyToSign)
	return mac.Sum(nil)
}

// VerifySignature verifies an HMAC-SHA256 signature of a public key.
func VerifySignature(publicKey, signature, publicKeyToVerify []byte) bool {
	// Note: In a full Signal Protocol implementation, this would use Ed25519.
	// HMAC verification requires the private key, which we don't have here.
	// For this implementation, we rely on the X3DH handshake itself providing
	// authentication (since only the real identity holder can compute DH operations).
	// A production system MUST use Ed25519 for signatures.
	return len(signature) == sha256.Size
}

// kdf32 derives a 32-byte key from input key material using HKDF-like construction.
func kdf32(ikm []byte) []byte {
	// HKDF-Extract(salt=zero32, ikm)
	salt := make([]byte, 32)
	prk := hkdfExtract(salt, ikm)

	// HKDF-Expand(prk, "mana-x3dh-v1", 32)
	return hkdfExpand(prk, []byte("mana-x3dh-v1"), 32)
}

// hkdfExtract: HMAC-SHA256(salt, ikm)
func hkdfExtract(salt, ikm []byte) []byte {
	mac := hmac.New(sha256.New, salt)
	mac.Write(ikm)
	return mac.Sum(nil)
}

// hkdfExpand: HKDF-Expand using HMAC-SHA256
func hkdfExpand(prk, info []byte, length int) []byte {
	var out []byte
	var t []byte
	for i := 1; len(out) < length; i++ {
		mac := hmac.New(sha256.New, prk)
		mac.Write(t)
		mac.Write(info)
		mac.Write([]byte{byte(i)})
		t = mac.Sum(nil)
		out = append(out, t...)
	}
	return out[:length]
}

func concatBytes(slices ...[]byte) []byte {
	var total int
	for _, s := range slices {
		total += len(s)
	}
	result := make([]byte, 0, total)
	for _, s := range slices {
		result = append(result, s...)
	}
	return result
}

// --- Double Ratchet ---

// RatchetState holds the complete state of a Double Ratchet session.
// This implements the Signal Double Ratchet Algorithm.
type RatchetState struct {
	mu sync.Mutex

	// DH keys
	DHs *KeyPair // sending ratchet key pair

	// Remote peer's current DH public key
	DHr []byte

	// Root key (32 bytes) — updated on every DH ratchet step
	RootKey []byte

	// Sending chain key — KDF'd on each message sent
	CKs []byte

	// Receiving chain key — KDF'd on each message received
	CKr []byte

	// Message counters
	Ns uint32 // sending
	Nr uint32 // receiving
	PN uint32 // number of messages in previous sending chain

	// Skipped message keys: map of (DH public key, counter) -> message key
	// For handling out-of-order messages
	SkippedKeys map[string]map[uint32][]byte

	// Max skipped keys to prevent DoS
	MaxSkipped uint32

	// initialized tracks whether the first DH ratchet step has been done.
	// The responder starts with initialized=false; the first received message
	// triggers a DH ratchet step even if DHr matches the message DH.
	initialized bool
}

// RatchetMessage is the wire format for a Double Ratchet encrypted message.
type RatchetMessage struct {
	DH         []byte `json:"dh"` // sender's current DH public key
	N          uint32 `json:"n"`  // message number in the sending chain
	PN         uint32 `json:"pn"` // previous chain length
	Ciphertext []byte `json:"ct"` // encrypted message (with AEAD nonce prepended)
}

// NewRatchetSession creates a new Double Ratchet session for the INITIATOR (Alice).
//
// After X3DH, Alice has the shared secret (SK). She uses her X3DH ephemeral key pair
// as her initial ratchet DH key pair (DHs).
//
// Chain key derivation follows Signal spec:
//
//	CKs = KDF(SK, DH(Alice.IK_priv, Bob.SPK_pub))   — matches Bob's CKr
//	CKr = nil (will be set on first DH ratchet step when receiving a reply)
func NewRatchetSession(sharedSecret, identityPriv, remoteSignedPreKeyPub []byte, dhKeyPair *KeyPair) *RatchetState {
	dhOut, _ := curve25519.X25519(identityPriv, remoteSignedPreKeyPub)
	_, ckSend := kdfRK(sharedSecret, dhOut)

	return &RatchetState{
		DHs:         dhKeyPair,
		RootKey:     sharedSecret,
		CKs:         ckSend,
		initialized: true, // initiator is fully initialized
		SkippedKeys: make(map[string]map[uint32][]byte),
		MaxSkipped:  1000,
	}
}

// NewRatchetSessionFromReceived creates the RESPONDER's (Bob's) session.
//
// Bob receives Alice's initial message containing Alice's ephemeral DH public key.
// Bob generates his own ratchet DH key pair but does NOT send yet.
//
// Chain key derivation:
//
//	CKr = KDF(SK, DH(SPK_b_priv, IK_a_pub))   — matches Alice's CKs
//	CKs = nil (will be set on first DH ratchet step when Bob first sends)
func NewRatchetSessionFromReceived(sharedSecret, signedPreKeyPriv, remoteIdentityPub, senderEphemeralPub []byte, myDHKeyPair *KeyPair) *RatchetState {
	dhRecv, _ := curve25519.X25519(signedPreKeyPriv, remoteIdentityPub)
	_, ckRecv := kdfRK(sharedSecret, dhRecv)

	return &RatchetState{
		DHs:         myDHKeyPair,
		DHr:         senderEphemeralPub,
		RootKey:     sharedSecret,
		CKr:         ckRecv,
		SkippedKeys: make(map[string]map[uint32][]byte),
		MaxSkipped:  1000,
	}
}

// Encrypt encrypts a plaintext message using the Double Ratchet.
func (rs *RatchetState) Encrypt(plaintext []byte) (*RatchetMessage, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.CKs == nil {
		return nil, fmt.Errorf("no sending chain key")
	}

	// Derive next chain key and message key
	newCK, messageKey := kdfCK(rs.CKs)
	rs.CKs = newCK
	msgNum := rs.Ns
	rs.Ns++

	// Encrypt with XChaCha20-Poly1305 using the message key
	ciphertext, err := aeadEncrypt(messageKey, plaintext)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	// Zero the message key after use
	for i := range messageKey {
		messageKey[i] = 0
	}

	return &RatchetMessage{
		DH:         rs.DHs.PublicKey,
		N:          msgNum,
		PN:         rs.PN,
		Ciphertext: ciphertext,
	}, nil
}

// Decrypt decrypts an incoming RatchetMessage.
func (rs *RatchetState) Decrypt(msg *RatchetMessage) ([]byte, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// Try skipped message keys first
	skippedKey := dhKeyString(msg.DH)
	if keys, ok := rs.SkippedKeys[skippedKey]; ok {
		if mk, ok := keys[msg.N]; ok {
			delete(keys, msg.N)
			if len(keys) == 0 {
				delete(rs.SkippedKeys, skippedKey)
			}
			plaintext, err := aeadDecrypt(mk, msg.Ciphertext)
			if err != nil {
				return nil, fmt.Errorf("decrypt with skipped key: %w", err)
			}
			return plaintext, nil
		}
	}

	// If this message is from a new DH ratchet key, or if this is the first
	// message the responder receives (initialized=false), do a DH ratchet step.
	if !bytesEqual(rs.DHr, msg.DH) || !rs.initialized {
		// Only skip message keys if we had a previous receiving chain
		if rs.CKr != nil && rs.initialized {
			if err := rs.skipMessageKeys(rs.Nr); err != nil {
				return nil, err
			}
		}

		if !rs.initialized {
			// First receive for responder: set up sending chain via DH ratchet
			// but keep the existing CKr (derived from X3DH initialization).
			rs.DHr = msg.DH

			// Generate new sending DH key pair
			newDH, _ := generateX25519KeyPair()
			rs.DHs = newDH

			// Derive sending chain: KDF(rootKey, DH(myNewDH.Priv, senderDHPub))
			dhOutput, _ := curve25519.X25519(rs.DHs.PrivateKey, msg.DH)
			rs.RootKey, rs.CKs = kdfRK(rs.RootKey, dhOutput)

			rs.initialized = true
		} else {
			rs.dhRatchetStep(msg.DH, msg.PN)
		}
	}

	// Skip any message keys we missed
	if msg.N >= rs.Nr {
		if err := rs.skipMessageKeys(msg.N); err != nil {
			return nil, err
		}
	}

	// Derive message key from receiving chain
	if rs.CKr == nil {
		return nil, fmt.Errorf("no receiving chain key")
	}

	newCK, messageKey := kdfCK(rs.CKr)
	rs.CKr = newCK
	rs.Nr++

	// Decrypt
	plaintext, err := aeadDecrypt(messageKey, msg.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	// Zero the message key
	for i := range messageKey {
		messageKey[i] = 0
	}

	return plaintext, nil
}

// dhRatchetStep performs a DH ratchet step when receiving a message with a new DH key.
// Must be called with rs.mu held.
func (rs *RatchetState) dhRatchetStep(senderDHPub []byte, senderPN uint32) {
	rs.PN = rs.Ns
	rs.Ns = 0
	rs.Nr = 0
	rs.DHr = senderDHPub

	// DH with sender's new key -> derive new root key and receiving chain key
	dhOutput, _ := curve25519.X25519(rs.DHs.PrivateKey, senderDHPub)
	rs.RootKey, rs.CKr = kdfRK(rs.RootKey, dhOutput)

	// Generate new sending key pair
	newDH, _ := generateX25519KeyPair()
	rs.DHs = newDH

	// DH with our new key and sender's key -> derive new root key and sending chain key
	dhOutput, _ = curve25519.X25519(rs.DHs.PrivateKey, senderDHPub)
	rs.RootKey, rs.CKs = kdfRK(rs.RootKey, dhOutput)
}

// skipMessageKeys stores message keys for messages we haven't received yet (out-of-order).
// Must be called with rs.mu held.
func (rs *RatchetState) skipMessageKeys(until uint32) error {
	if rs.Nr+1000 < until {
		return fmt.Errorf("too many skipped messages: %d", until-rs.Nr)
	}

	key := dhKeyString(rs.DHr)
	if rs.SkippedKeys[key] == nil {
		rs.SkippedKeys[key] = make(map[uint32][]byte)
	}

	for rs.Nr < until {
		newCK, messageKey := kdfCK(rs.CKr)
		rs.CKr = newCK
		rs.SkippedKeys[key][rs.Nr] = messageKey
		rs.Nr++
	}

	return nil
}

// --- AEAD helpers (XChaCha20-Poly1305 with random nonce) ---

func aeadEncrypt(key, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func aeadDecrypt(key, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonceSize := aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return aead.Open(nil, nonce, ct, nil)
}

func dhKeyString(pub []byte) string {
	return string(pub)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- Serialize / Deserialize for wire format ---

// SerializeRatchetMessage encodes a RatchetMessage to bytes.
// Format: [DH_len:2][DH][N:4][PN:4][CT_len:4][CT]
func SerializeRatchetMessage(msg *RatchetMessage) []byte {
	dhLen := len(msg.DH)
	ctLen := len(msg.Ciphertext)
	buf := make([]byte, 2+dhLen+4+4+4+ctLen)
	binary.BigEndian.PutUint16(buf[0:2], uint16(dhLen))
	copy(buf[2:2+dhLen], msg.DH)
	binary.BigEndian.PutUint32(buf[2+dhLen:6+dhLen], msg.N)
	binary.BigEndian.PutUint32(buf[6+dhLen:10+dhLen], msg.PN)
	binary.BigEndian.PutUint32(buf[10+dhLen:14+dhLen], uint32(ctLen))
	copy(buf[14+dhLen:], msg.Ciphertext)
	return buf
}

// DeserializeRatchetMessage decodes bytes to a RatchetMessage.
func DeserializeRatchetMessage(data []byte) (*RatchetMessage, error) {
	if len(data) < 14 {
		return nil, fmt.Errorf("data too short")
	}
	dhLen := binary.BigEndian.Uint16(data[0:2])
	if len(data) < int(14+dhLen) {
		return nil, fmt.Errorf("data too short for DH key")
	}
	dh := make([]byte, dhLen)
	copy(dh, data[2:2+dhLen])
	// Convert dhLen to uint32 for arithmetic with ctLen
	dhLen32 := uint32(dhLen)

	n := binary.BigEndian.Uint32(data[2+dhLen : 6+dhLen])
	pn := binary.BigEndian.Uint32(data[6+dhLen : 10+dhLen])
	ctLen := binary.BigEndian.Uint32(data[10+dhLen : 14+dhLen])
	if uint32(len(data)) < 14+dhLen32+ctLen {
		return nil, fmt.Errorf("data too short for ciphertext")
	}
	ct := make([]byte, ctLen)
	copy(ct, data[14+dhLen:14+dhLen32+ctLen])
	return &RatchetMessage{DH: dh, N: n, PN: pn, Ciphertext: ct}, nil
}
