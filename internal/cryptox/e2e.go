// Package cryptox implements the end-to-end payload encryption: an X25519 key
// exchange (the AES key is never transmitted) + AES-256-GCM for request/response
// bodies. Envelope format (JSON): {"iv": base64(nonce), "ct": base64(cipher+tag)}.
package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// hkdfSalt / hkdfInfo bind the derived key to this protocol version. Both sides
// (Go server and Dart client) must use identical values.
var (
	hkdfSalt = []byte("vaha-e2e-salt-v1")
	hkdfInfo = []byte("vaha-e2e-v1")
)

// ServerHandshake takes the client's base64 X25519 public key, generates a
// server keypair, performs ECDH, derives a 32-byte AES key via HKDF-SHA256, and
// returns the AES key + the server's base64 public key (to send back).
func ServerHandshake(clientPubB64 string) (aesKey []byte, serverPubB64 string, err error) {
	clientPubBytes, err := base64.StdEncoding.DecodeString(clientPubB64)
	if err != nil {
		return nil, "", fmt.Errorf("bad client public key: %w", err)
	}
	curve := ecdh.X25519()
	clientPub, err := curve.NewPublicKey(clientPubBytes)
	if err != nil {
		return nil, "", fmt.Errorf("invalid client public key: %w", err)
	}
	serverPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	shared, err := serverPriv.ECDH(clientPub)
	if err != nil {
		return nil, "", err
	}
	key, err := deriveKey(shared)
	if err != nil {
		return nil, "", err
	}
	return key, base64.StdEncoding.EncodeToString(serverPriv.PublicKey().Bytes()), nil
}

func deriveKey(shared []byte) ([]byte, error) {
	h := hkdf.New(sha256.New, shared, hkdfSalt, hkdfInfo)
	key := make([]byte, 32)
	if _, err := io.ReadFull(h, key); err != nil {
		return nil, err
	}
	return key, nil
}

type envelope struct {
	IV string `json:"iv"`
	CT string `json:"ct"`
}

// Encrypt AES-256-GCM encrypts plaintext and returns the JSON envelope.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	return json.Marshal(envelope{
		IV: base64.StdEncoding.EncodeToString(nonce),
		CT: base64.StdEncoding.EncodeToString(ct),
	})
}

// Decrypt parses the JSON envelope and AES-256-GCM decrypts it.
func Decrypt(key, envJSON []byte) ([]byte, error) {
	var e envelope
	if err := json.Unmarshal(envJSON, &e); err != nil {
		return nil, fmt.Errorf("bad envelope: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(e.IV)
	if err != nil {
		return nil, err
	}
	ct, err := base64.StdEncoding.DecodeString(e.CT)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ct, nil)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
