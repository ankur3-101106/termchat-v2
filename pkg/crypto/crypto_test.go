package crypto_test

import (
	"testing"

	"github.com/viveksec/termchat/pkg/crypto"
)

func TestKeyPairGeneration(t *testing.T) {
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	if len(kp.PublicKey) != 32 {
		t.Errorf("expected 32-byte public key, got %d", len(kp.PublicKey))
	}
	if len(kp.PrivateKey) != 32 {
		t.Errorf("expected 32-byte private key, got %d", len(kp.PrivateKey))
	}
	if kp.PublicKeyBase64() == "" {
		t.Error("PublicKeyBase64 returned empty string")
	}
}

func TestDiffieHellmanExchange(t *testing.T) {
	alice, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	bob, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	secretA, err := crypto.DeriveSharedSecret(alice.PrivateKey, bob.PublicKeyBase64())
	if err != nil {
		t.Fatalf("Alice failed to derive secret: %v", err)
	}
	secretB, err := crypto.DeriveSharedSecret(bob.PrivateKey, alice.PublicKeyBase64())
	if err != nil {
		t.Fatalf("Bob failed to derive secret: %v", err)
	}

	if len(secretA) != 32 || len(secretB) != 32 {
		t.Fatalf("expected 32-byte shared secrets, got A=%d, B=%d", len(secretA), len(secretB))
	}

	for i := range secretA {
		if secretA[i] != secretB[i] {
			t.Fatalf("mismatch at byte %d: %d != %d", i, secretA[i], secretB[i])
		}
	}
}

func TestAESGCMEncryptionDecryption(t *testing.T) {
	alice, _ := crypto.GenerateKeyPair()
	bob, _ := crypto.GenerateKeyPair()
	secret, _ := crypto.DeriveSharedSecret(alice.PrivateKey, bob.PublicKeyBase64())

	messages := []string{
		"Hello, Bob!",
		"",
		"Special chars: !@#$%^&*()_+{}|:\"<>?~`-=[]\\;',./",
		"Unicode: 🚀🔒🔐🛡️",
	}

	for _, msg := range messages {
		ciphertext, err := crypto.Encrypt(secret, []byte(msg))
		if err != nil {
			t.Fatalf("failed to encrypt %q: %v", msg, err)
		}

		plaintext, err := crypto.Decrypt(secret, ciphertext)
		if err != nil {
			t.Fatalf("failed to decrypt %q: %v", msg, err)
		}

		if string(plaintext) != msg {
			t.Errorf("decrypted message mismatch: got %q, want %q", string(plaintext), msg)
		}
	}
}

func TestTamperedCiphertextFails(t *testing.T) {
	alice, _ := crypto.GenerateKeyPair()
	bob, _ := crypto.GenerateKeyPair()
	secret, _ := crypto.DeriveSharedSecret(alice.PrivateKey, bob.PublicKeyBase64())

	ciphertext, _ := crypto.Encrypt(secret, []byte("Top secret info"))
	tampered := ciphertext[:len(ciphertext)-4] + "XXXX"

	_, err := crypto.Decrypt(secret, tampered)
	if err == nil {
		t.Error("expected decryption error on tampered ciphertext, got nil")
	}
}

func TestInvalidKeySize(t *testing.T) {
	badKey := []byte("short_key")
	_, err := crypto.Encrypt(badKey, []byte("data"))
	if err == nil {
		t.Error("expected error for non-32-byte key in Encrypt")
	}

	_, err = crypto.Decrypt(badKey, "dGVzdA==")
	if err == nil {
		t.Error("expected error for non-32-byte key in Decrypt")
	}
}

func TestSafetyNumber(t *testing.T) {
	pubA := "keyA_base64_string_sample_123456"
	pubB := "keyB_base64_string_sample_654321"

	code1 := crypto.CalculateSafetyNumber(pubA, pubB)
	code2 := crypto.CalculateSafetyNumber(pubB, pubA)

	if code1 == "" {
		t.Error("safety number should not be empty")
	}
	if code1 != code2 {
		t.Errorf("safety number must be symmetric regardless of argument order: %s != %s", code1, code2)
	}
	if len(code1) != 7 { // XXX-XXX format is 7 chars
		t.Errorf("expected XXX-XXX format (7 chars), got %q (%d chars)", code1, len(code1))
	}
}

