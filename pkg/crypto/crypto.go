// Package crypto provides end-to-end encryption primitives for the terminal chat
// application. It implements ephemeral X25519 Diffie-Hellman key exchange and
// AES-256-GCM authenticated encryption. The relay server never has access to
// private keys or plaintext — all encryption and decryption happens on the client.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/curve25519"
)

// KeyPair represents an ephemeral X25519 key pair. A new KeyPair must be
// generated for every chat session to ensure forward secrecy.
type KeyPair struct {
	// PrivateKey is the 32-byte scalar used in the DH computation.
	// It must never leave the local process.
	PrivateKey [32]byte
	// PublicKey is the 32-byte Curve25519 point to be shared with the peer.
	PublicKey [32]byte
}

// GenerateKeyPair creates a new ephemeral X25519 key pair using a
// cryptographically secure random number generator. It returns an error
// if the system entropy source fails.
func GenerateKeyPair() (*KeyPair, error) {
	kp := &KeyPair{}

	// Read 32 bytes of cryptographically secure randomness for the private key.
	if _, err := io.ReadFull(rand.Reader, kp.PrivateKey[:]); err != nil {
		return nil, fmt.Errorf("crypto: failed to generate private key: %w", err)
	}

	// Clamp the private scalar as required by RFC 7748 (X25519).
	// This prevents small-subgroup attacks.
	kp.PrivateKey[0] &= 248
	kp.PrivateKey[31] &= 127
	kp.PrivateKey[31] |= 64

	// Derive the public key by scalar-multiplying the base point.
	pub, err := curve25519.X25519(kp.PrivateKey[:], curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to derive public key: %w", err)
	}
	copy(kp.PublicKey[:], pub)

	return kp, nil
}

// PublicKeyBase64 returns the public key encoded as a URL-safe base64 string
// suitable for JSON wire transmission.
func (kp *KeyPair) PublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(kp.PublicKey[:])
}

// DeriveSharedSecret performs an X25519 Diffie-Hellman computation between
// the local private key and the peer's public key. The raw 32-byte DH output
// is then passed through SHA-256 to produce a uniformly distributed 256-bit
// key suitable for use with AES-256-GCM.
//
// Both parties derive the same shared secret without transmitting it over
// the network.
func DeriveSharedSecret(privKey [32]byte, peerPubKeyBase64 string) ([]byte, error) {
	peerPubBytes, err := base64.StdEncoding.DecodeString(peerPubKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("crypto: invalid peer public key encoding: %w", err)
	}
	if len(peerPubBytes) != 32 {
		return nil, errors.New("crypto: peer public key must be exactly 32 bytes")
	}

	// X25519 scalar multiplication.
	sharedPoint, err := curve25519.X25519(privKey[:], peerPubBytes)
	if err != nil {
		return nil, fmt.Errorf("crypto: X25519 computation failed: %w", err)
	}

	// Check for the all-zero output which would indicate a low-order point attack.
	var zeroPoint [32]byte
	if sharedPoint == nil || len(sharedPoint) != 32 {
		return nil, errors.New("crypto: X25519 returned invalid output")
	}
	isZero := true
	for i := 0; i < 32; i++ {
		if sharedPoint[i] != zeroPoint[i] {
			isZero = false
			break
		}
	}
	if isZero {
		return nil, errors.New("crypto: low-order point detected in key exchange")
	}

	// Hash through SHA-256 to produce the final symmetric key material.
	// This provides key derivation and domain separation.
	digest := sha256.Sum256(sharedPoint)
	return digest[:], nil
}

// Encrypt encrypts plaintext using AES-256-GCM with the provided 32-byte key.
// A random 12-byte nonce is generated for each encryption operation and
// prepended to the ciphertext. The GCM authentication tag is appended by
// the standard library. The entire output (nonce || ciphertext || tag) is
// returned as a base64-encoded string for safe JSON embedding.
func Encrypt(key []byte, plaintext []byte) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("crypto: AES-256 requires a 32-byte key, got %d bytes", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: failed to create GCM: %w", err)
	}

	// Generate a fresh random nonce for each message.
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: failed to generate nonce: %w", err)
	}

	// Seal appends the ciphertext and 16-byte authentication tag after the nonce.
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a base64-encoded ciphertext produced by Encrypt using
// AES-256-GCM. It extracts the prepended nonce, decrypts the ciphertext,
// and verifies the authentication tag. An error is returned if the tag
// verification fails, which indicates tampering or key mismatch.
func Decrypt(key []byte, ciphertextBase64 string) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: AES-256 requires a 32-byte key, got %d bytes", len(key))
	}

	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize+gcm.Overhead() {
		return nil, errors.New("crypto: ciphertext is too short to contain nonce and authentication tag")
	}

	nonce, ciphertextBody := ciphertext[:nonceSize], ciphertext[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertextBody, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decryption failed (authentication tag mismatch or corrupted data): %w", err)
	}

	return plaintext, nil
}

// CalculateSafetyNumber derives a 6-digit Short Authentication String (SAS)
// from the public keys of both session participants. By sorting the public
// keys lexicographically, both participants compute the exact same code.
// Reading this code out-of-band (e.g., over voice or in person) guarantees
// 100% protection against Man-in-the-Middle (MITM) attacks.
func CalculateSafetyNumber(pubKeyA, pubKeyB string) string {
	first, second := pubKeyA, pubKeyB
	if first > second {
		first, second = second, first
	}

	h := sha256.New()
	h.Write([]byte(first))
	h.Write([]byte(second))
	digest := h.Sum(nil)

	// Extract 6 digits formatted as XXX-XXX
	val1 := (uint32(digest[0])<<8 | uint32(digest[1])) % 1000
	val2 := (uint32(digest[2])<<8 | uint32(digest[3])) % 1000
	return fmt.Sprintf("%03d-%03d", val1, val2)
}

