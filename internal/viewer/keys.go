// Package viewer is spark's live viewer: a socket through bytesocks that the
// web viewer connects to, signed packets, and the keys of trusted viewers.
// spark: ws/*
package viewer

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"sync"
)

// keyBits is the size of the server's RSA key. spark: CryptoAlgorithm.RSA2048
const keyBits = 2048

// KeyStore holds the server's key pair, the viewers' keys it trusts, and
// the keys of viewers waiting to be trusted. spark: TrustedKeyStore
type KeyStore struct {
	local   func() (*rsa.PrivateKey, error)
	mu      sync.Mutex
	trusted map[string]bool           // PKIX DER of trusted keys
	pending map[string]*rsa.PublicKey // client id -> key
	// save is called with the trusted keys, base64-encoded, when they change.
	save func(encoded []string)
}

// NewKeyStore generates the server's key on another goroutine and trusts
// the base64 PKIX keys in trusted; save receives them back when a key is
// trusted.
func NewKeyStore(trusted []string, save func(encoded []string)) *KeyStore {
	ks := &KeyStore{
		local:   sync.OnceValues(func() (*rsa.PrivateKey, error) { return rsa.GenerateKey(rand.Reader, keyBits) }),
		trusted: map[string]bool{}, pending: map[string]*rsa.PublicKey{}, save: save,
	}
	go func() { _, _ = ks.local() }()
	for _, enc := range trusted {
		der, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			continue
		}
		if _, err := parsePublicKey(der); err == nil {
			ks.trusted[string(der)] = true
		}
	}
	return ks
}

// PublicKey answers the server's public key as PKIX DER, as Java encodes it.
func (ks *KeyStore) PublicKey() ([]byte, error) {
	key, err := ks.local()
	if err != nil {
		return nil, err
	}
	return x509.MarshalPKIXPublicKey(&key.PublicKey)
}

// sign signs msg with the server's key: SHA256withRSA.
func (ks *KeyStore) sign(msg []byte) ([]byte, error) {
	key, err := ks.local()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(msg)
	return rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
}

func parsePublicKey(der []byte) (*rsa.PublicKey, error) {
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("viewer: not an RSA key")
	}
	return rsaKey, nil
}

func (ks *KeyStore) isTrusted(key *rsa.PublicKey) bool {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return false
	}
	ks.mu.Lock()
	defer ks.mu.Unlock()
	return ks.trusted[string(der)]
}

func (ks *KeyStore) addPending(clientID string, key *rsa.PublicKey) {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.pending[clientID] = key
}

// TrustPending trusts the key of the viewer waiting with clientID, and
// reports whether there was one. spark: TrustedKeyStore.trustPendingKey
func (ks *KeyStore) TrustPending(clientID string) bool {
	ks.mu.Lock()
	key, ok := ks.pending[clientID]
	delete(ks.pending, clientID)
	var encoded []string
	if ok {
		if der, err := x509.MarshalPKIXPublicKey(key); err == nil {
			ks.trusted[string(der)] = true
		}
		for der := range ks.trusted {
			encoded = append(encoded, base64.StdEncoding.EncodeToString([]byte(der)))
		}
	}
	ks.mu.Unlock()
	if ok && ks.save != nil {
		ks.save(encoded)
	}
	return ok
}
