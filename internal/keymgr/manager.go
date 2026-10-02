package keymgr

import (
	"encoding/hex"
	"errors"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
)

var (
	ErrInvalidSecret = errors.New("invalid secret key format")
	ErrNoSecret      = errors.New("no secret key loaded")
)

// Identity exposes only public data derived from the in-memory secret key.
type Identity struct {
	PubKeyHex string
	NPub      string
}

// Manager keeps a single Nostr secret key in memory.
type Manager struct {
	mu     sync.RWMutex
	secret nostr.SecretKey
	loaded bool
}

// New returns an empty in-memory key manager.
func New() *Manager {
	return &Manager{}
}

// Import validates and loads a secret key in nsec or 64-char hex format.
func (m *Manager) Import(input string) (Identity, error) {
	sk, err := ParseSecret(input)
	if err != nil {
		return Identity{}, err
	}

	m.setSecret(sk)
	return deriveIdentity(sk), nil
}

// Generate creates a new Nostr secret key and keeps it in memory.
func (m *Manager) Generate() (Identity, error) {
	sk := nostr.Generate()
	m.setSecret(sk)
	return deriveIdentity(sk), nil
}

// CurrentIdentity derives pubkey and npub from the loaded in-memory secret.
func (m *Manager) CurrentIdentity() (Identity, error) {
	m.mu.RLock()
	sk := m.secret
	loaded := m.loaded
	m.mu.RUnlock()

	if !loaded {
		return Identity{}, ErrNoSecret
	}

	return deriveIdentity(sk), nil
}

// HasSecret reports whether a secret is currently loaded in memory.
func (m *Manager) HasSecret() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loaded
}

// Clear removes the secret key from memory.
func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.secret {
		m.secret[i] = 0
	}
	m.loaded = false
}

// ParseSecret validates and parses either nsec or 64-char hex into a secret key.
func ParseSecret(input string) (nostr.SecretKey, error) {
	clean := strings.TrimSpace(input)
	if clean == "" {
		return nostr.SecretKey{}, ErrInvalidSecret
	}

	lower := strings.ToLower(clean)
	if strings.HasPrefix(lower, "nsec1") {
		prefix, value, err := nip19.Decode(lower)
		if err != nil || prefix != "nsec" {
			return nostr.SecretKey{}, ErrInvalidSecret
		}

		sk, ok := value.(nostr.SecretKey)
		if !ok {
			return nostr.SecretKey{}, ErrInvalidSecret
		}

		return sk, nil
	}

	if len(clean) != 64 {
		return nostr.SecretKey{}, ErrInvalidSecret
	}

	if _, err := hex.DecodeString(clean); err != nil {
		return nostr.SecretKey{}, ErrInvalidSecret
	}

	sk, err := nostr.SecretKeyFromHex(lower)
	if err != nil {
		return nostr.SecretKey{}, ErrInvalidSecret
	}

	return sk, nil
}

func deriveIdentity(sk nostr.SecretKey) Identity {
	pk := sk.Public()

	return Identity{
		PubKeyHex: pk.Hex(),
		NPub:      nip19.EncodeNpub(pk),
	}
}

func (m *Manager) setSecret(next nostr.SecretKey) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.secret {
		m.secret[i] = 0
	}

	m.secret = next
	m.loaded = true
}
