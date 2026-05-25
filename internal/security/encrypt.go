package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"

	"golang.org/x/crypto/scrypt"
)

const (
	SaltSize = 16 // bytes
	KeySize  = 32 // bytes — AES-256

	// scrypt parameters tuned for laptop-grade hardware:
	//   N=32768 (CPU/memory cost), r=8 (block size), p=1 (parallelism)
	// Derivation takes ~100ms on a modern laptop — acceptable for init/unlock,
	// invisible for per-chunk operations (key is derived once per session).
	scryptN = 32768
	scryptR = 8
	scryptP = 1
)

// GenerateSalt generates a cryptographically random 16-byte salt.
// Store this in config (it is not secret — only the passphrase is secret).
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("cannot generate random salt: %w", err)
	}
	return salt, nil
}

// DeriveKey derives a 32-byte AES-256 key from a passphrase and salt using scrypt.
// The same passphrase + salt always produces the same key — deterministic by design.
func DeriveKey(passphrase, salt []byte) ([]byte, error) {
	key, err := scrypt.Key(passphrase, salt, scryptN, scryptR, scryptP, KeySize)
	if err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}
	return key, nil
}

// Encrypt encrypts plaintext with AES-256-GCM.
//
// Output format: [12-byte random nonce][ciphertext + 16-byte GCM auth tag]
//
// GCM provides both confidentiality and integrity — any tampering with the
// ciphertext causes Decrypt to return an error rather than corrupt plaintext.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cannot create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cannot create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("cannot generate nonce: %w", err)
	}

	// Seal appends ciphertext+tag after the nonce in one allocation.
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts data produced by Encrypt.
// Returns an error if the key is wrong or the data has been tampered with.
func Decrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cannot create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cannot create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize+gcm.Overhead() {
		return nil, fmt.Errorf("ciphertext too short to be valid")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// Don't wrap — the default message "message authentication failed" is clear enough
		// and doesn't hint at implementation details
		return nil, fmt.Errorf("decryption failed — wrong passphrase or corrupted data")
	}
	return plaintext, nil
}
