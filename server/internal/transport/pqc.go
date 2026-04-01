// Package transport provides PQC key encapsulation layered over TLS.
// Uses ML-KEM-768 (Kyber) from Cloudflare circl for post-quantum key agreement.
package transport

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/cloudflare/circl/kem"
	"github.com/cloudflare/circl/kem/mlkem/mlkem768"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

var kemScheme = mlkem768.Scheme()

// PQCServerKeys holds the server's ML-KEM-768 key pair.
type PQCServerKeys struct {
	pk kem.PublicKey
	sk kem.PrivateKey
}

// GeneratePQCKeys generates a new ML-KEM-768 key pair for the server.
func GeneratePQCKeys() (*PQCServerKeys, error) {
	pk, sk, err := kemScheme.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("generate ML-KEM-768 keys: %w", err)
	}
	return &PQCServerKeys{pk: pk, sk: sk}, nil
}

// PublicKeyBytes returns the serialized public key.
func (k *PQCServerKeys) PublicKeyBytes() ([]byte, error) {
	b, err := k.pk.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	return b, nil
}

// Decapsulate derives the shared secret from a client's ciphertext.
func (k *PQCServerKeys) Decapsulate(ciphertext []byte) ([]byte, error) {
	ss, err := kemScheme.Decapsulate(k.sk, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decapsulate: %w", err)
	}
	return deriveSymmetricKey(ss)
}

// Encapsulate creates a ciphertext and shared secret from a server's public key.
// Used by the client side.
func Encapsulate(publicKeyBytes []byte) (ciphertext []byte, sharedKey []byte, err error) {
	pk, err := kemScheme.UnmarshalBinaryPublicKey(publicKeyBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("unmarshal public key: %w", err)
	}
	ct, ss, err := kemScheme.Encapsulate(pk)
	if err != nil {
		return nil, nil, fmt.Errorf("encapsulate: %w", err)
	}
	key, err := deriveSymmetricKey(ss)
	if err != nil {
		return nil, nil, err
	}
	return ct, key, nil
}

// deriveSymmetricKey uses HKDF-SHA256 to derive a 32-byte key from the KEM shared secret.
func deriveSymmetricKey(sharedSecret []byte) ([]byte, error) {
	info := []byte("notbbg-pqc-v1")
	hk := hkdf.New(sha256.New, sharedSecret, nil, info)
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(hk, key); err != nil {
		return nil, fmt.Errorf("hkdf derive: %w", err)
	}
	return key, nil
}

// PQCEncrypt encrypts data using XChaCha20-Poly1305 with the PQC-derived key.
func PQCEncrypt(key, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

// PQCDecrypt decrypts data using XChaCha20-Poly1305 with the PQC-derived key.
func PQCDecrypt(key, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce := ciphertext[:aead.NonceSize()]
	return aead.Open(nil, nonce, ciphertext[aead.NonceSize():], nil)
}
