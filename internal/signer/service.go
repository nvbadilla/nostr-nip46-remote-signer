package signer

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip04"
	"fiatjaf.com/nostr/nip44"
)

const (
	// NostrConnectKind is NIP-46's event kind.
	NostrConnectKind nostr.Kind = 24133
)

var (
	ErrInvalidEventKind  = errors.New("invalid event kind")
	ErrInvalidEventID    = errors.New("invalid event id")
	ErrInvalidEventSig   = errors.New("invalid event signature")
	ErrInvalidPTag       = errors.New("missing or invalid p-tag")
	ErrSessionConnected  = errors.New("session already connected")
	ErrSessionNotReady   = errors.New("session not connected")
	ErrConnectedClient   = errors.New("request not from connected client")
	ErrBunkerSecretUsed  = errors.New("bunker secret already used")
	ErrUnsupportedMethod = errors.New("unsupported method")
)

func diagf(format string, args ...any) {
	log.Printf("[NIP46] "+format, args...)
}

// transportScheme identifies which NIP-46 content encryption was used for a request/response.
type transportScheme int

const (
	// schemeNIP44 is the default, current NIP-46 transport encryption.
	schemeNIP44 transportScheme = iota
	// schemeNIP04 is legacy interoperability only, kept for compatibility with
	// older NIP-46 clients/signers that have not migrated to NIP-44. It is never
	// chosen for outbound requests initiated by this service; it is only used
	// to decrypt/respond to inbound requests that already arrive in that format.
	schemeNIP04
)

func (t transportScheme) String() string {
	if t == schemeNIP04 {
		return "nip04"
	}
	return "nip44"
}

// isNIP04Content reports whether content uses the legacy NIP-04 "?iv=" wire format.
func isNIP04Content(content string) bool {
	return strings.Contains(content, "?iv=")
}

// Request is the JSON-RPC payload carried in kind 24133 content.
type Request struct {
	ID     string   `json:"id"`
	Method string   `json:"method"`
	Params []string `json:"params"`
}

// Response is the JSON-RPC response payload sent in kind 24133 content.
type Response struct {
	ID     string `json:"id"`
	Error  string `json:"error,omitempty"`
	Result string `json:"result,omitempty"`
}

// Service holds user key material and request/session state in memory.
type Service struct {
	mu sync.Mutex

	userSecret      nostr.SecretKey
	userPublicKey   nostr.PubKey
	transportSecret nostr.SecretKey
	transportPubKey nostr.PubKey

	relays []string

	bunkerSecret     string
	bunkerSecretUsed bool

	connectedClient nostr.PubKey
	connected       bool
}

// New creates a NIP-46 request/session handler.
func New(userSecret nostr.SecretKey, relays []string) (*Service, error) {
	if userSecret == (nostr.SecretKey{}) {
		return nil, errors.New("user secret key is required")
	}

	transportSecret := nostr.Generate()
	bunkerSecret, err := generateBunkerSecret()
	if err != nil {
		return nil, err
	}

	return newWithState(userSecret, transportSecret, relays, bunkerSecret)
}

func newWithState(
	userSecret nostr.SecretKey,
	transportSecret nostr.SecretKey,
	relays []string,
	bunkerSecret string,
) (*Service, error) {
	if userSecret == (nostr.SecretKey{}) {
		return nil, errors.New("user secret key is required")
	}
	if transportSecret == (nostr.SecretKey{}) {
		return nil, errors.New("transport secret key is required")
	}
	if strings.TrimSpace(bunkerSecret) == "" {
		return nil, errors.New("bunker secret is required")
	}

	normalizedRelays, err := normalizeRelays(relays)
	if err != nil {
		return nil, err
	}

	return &Service{
		userSecret:      userSecret,
		userPublicKey:   userSecret.Public(),
		transportSecret: transportSecret,
		transportPubKey: transportSecret.Public(),
		relays:          normalizedRelays,
		bunkerSecret:    bunkerSecret,
	}, nil
}

// TransportPublicKey returns the signer transport pubkey.
func (s *Service) TransportPublicKey() nostr.PubKey {
	return s.transportPubKey
}

// UserPublicKey returns the user's identity pubkey.
func (s *Service) UserPublicKey() nostr.PubKey {
	return s.userPublicKey
}

// BunkerSecret returns the current one-time bunker secret (if any).
func (s *Service) BunkerSecret() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bunkerSecret
}

// Relays returns a copy of currently configured relays.
func (s *Service) Relays() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.relays))
	copy(out, s.relays)
	return out
}

// BunkerURI returns bunker:// URI for client connection.
func (s *Service) BunkerURI() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.bunkerSecret == "" {
		return "", errors.New("no active bunker secret")
	}

	vals := url.Values{}
	for _, relay := range s.relays {
		vals.Add("relay", relay)
	}
	vals.Set("secret", s.bunkerSecret)

	return "bunker://" + s.transportPubKey.Hex() + "?" + vals.Encode(), nil
}

// HandleEvent validates, decrypts and handles one request event, returning an encrypted response event.
//
// Two content transports are supported: NIP-44 (default/current) and NIP-04
// (legacy interoperability only, detected via the "?iv=" content marker).
// The response is always encrypted using the same scheme as the request.
func (s *Service) HandleEvent(event nostr.Event) (Request, Response, nostr.Event, error) {
	var req Request
	var resp Response
	var responseEvent nostr.Event

	if event.Kind != NostrConnectKind {
		return req, resp, responseEvent, fmt.Errorf("%w: got %d", ErrInvalidEventKind, event.Kind)
	}
	if !event.CheckID() {
		return req, resp, responseEvent, ErrInvalidEventID
	}
	if !event.VerifySignature() {
		return req, resp, responseEvent, ErrInvalidEventSig
	}
	pTag := event.Tags.FindWithValue("p", s.transportPubKey.Hex())
	if pTag == nil {
		return req, resp, responseEvent, ErrInvalidPTag
	}
	pTagTarget := pTag[1]

	scheme := schemeNIP44
	if isNIP04Content(event.Content) {
		scheme = schemeNIP04
	}

	plain, err := s.decryptEventContent(scheme, event.Content, event.PubKey)
	if err != nil {
		return req, resp, responseEvent, fmt.Errorf("decrypt request: %w", err)
	}

	if err := json.Unmarshal([]byte(plain), &req); err != nil {
		return req, resp, responseEvent, fmt.Errorf("decode request: %w", err)
	}

	result, callErr := s.dispatch(event.PubKey, req, pTagTarget, scheme)
	status := "ok"
	resp.ID = req.ID
	if callErr != nil {
		status = "error"
		resp.Error = callErr.Error()
	} else {
		resp.Result = result
	}
	diagf("request handled method=%s status=%s", req.Method, status)

	responseEvent, err = s.encryptResponse(scheme, event.PubKey, resp)
	if err != nil {
		return req, resp, responseEvent, err
	}

	return req, resp, responseEvent, nil
}

func (s *Service) dispatch(clientPubKey nostr.PubKey, req Request, pTagTarget string, scheme transportScheme) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch req.Method {
	case "connect":
		if s.connected {
			return "", ErrSessionConnected
		}
		if len(req.Params) < 2 {
			return "", errors.New("connect requires params: [signer_pubkey, bunker_secret]")
		}

		var (
			claimedSigner nostr.PubKey
			err           error
		)

		if scheme == schemeNIP04 && req.Params[0] == "" {
			// Legacy interoperability path: NIP-04 connect can omit signer pubkey;
			// in that case, use the already-validated p-tag target.
			claimedSigner, err = nostr.PubKeyFromHex(pTagTarget)
		} else {
			claimedSigner, err = nostr.PubKeyFromHex(req.Params[0])
		}
		if err != nil {
			return "", errors.New("connect requires valid signer pubkey")
		}
		if claimedSigner != s.transportPubKey {
			return "", errors.New("connect signer pubkey mismatch")
		}
		if s.bunkerSecretUsed || s.bunkerSecret == "" {
			return "", ErrBunkerSecretUsed
		}
		secretMatch := subtle.ConstantTimeCompare([]byte(req.Params[1]), []byte(s.bunkerSecret)) == 1
		if !secretMatch {
			return "", errors.New("invalid bunker secret")
		}

		s.connected = true
		s.connectedClient = clientPubKey
		s.bunkerSecretUsed = true
		s.bunkerSecret = ""

		return "ack", nil

	case "get_public_key":
		if err := s.requireConnectedClient(clientPubKey); err != nil {
			return "", err
		}
		return s.userPublicKey.Hex(), nil

	case "sign_event":
		if err := s.requireConnectedClient(clientPubKey); err != nil {
			return "", err
		}
		if len(req.Params) != 1 {
			return "", errors.New("sign_event requires exactly 1 param")
		}

		var evt nostr.Event
		if err := json.Unmarshal([]byte(req.Params[0]), &evt); err != nil {
			return "", fmt.Errorf("sign_event invalid event json: %w", err)
		}
		if err := evt.Sign(s.userSecret); err != nil {
			return "", fmt.Errorf("sign_event failed: %w", err)
		}

		out, _ := json.Marshal(evt)
		return string(out), nil

	case "ping":
		if err := s.requireConnectedClient(clientPubKey); err != nil {
			return "", err
		}
		return "pong", nil

	case "switch_relays":
		if err := s.requireConnectedClient(clientPubKey); err != nil {
			return "", err
		}
		if len(req.Params) != 0 {
			return "", errors.New("switch_relays requires no params")
		}

		if len(s.relays) == 0 {
			return "null", nil
		}

		data, _ := json.Marshal(s.relays)
		return string(data), nil

	case "logout":
		if err := s.requireConnectedClient(clientPubKey); err != nil {
			return "", err
		}

		s.connected = false
		s.connectedClient = nostr.PubKey{}
		nextSecret, err := generateBunkerSecret()
		if err != nil {
			return "", err
		}
		s.bunkerSecret = nextSecret
		s.bunkerSecretUsed = false
		return "ack", nil

	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedMethod, req.Method)
	}
}

func (s *Service) requireConnectedClient(clientPubKey nostr.PubKey) error {
	if !s.connected {
		return ErrSessionNotReady
	}
	if s.connectedClient != clientPubKey {
		return ErrConnectedClient
	}
	return nil
}

func (s *Service) encryptResponse(scheme transportScheme, clientPubKey nostr.PubKey, resp Response) (nostr.Event, error) {
	plain, err := json.Marshal(resp)
	if err != nil {
		return nostr.Event{}, fmt.Errorf("encode response: %w", err)
	}

	ciphertext, err := s.encryptEventContent(scheme, clientPubKey, string(plain))
	if err != nil {
		return nostr.Event{}, fmt.Errorf("encrypt response: %w", err)
	}

	evt := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      NostrConnectKind,
		Tags:      nostr.Tags{nostr.Tag{"p", clientPubKey.Hex()}},
		Content:   ciphertext,
	}

	if err := evt.Sign(s.transportSecret); err != nil {
		return nostr.Event{}, fmt.Errorf("sign response: %w", err)
	}

	return evt, nil
}

// decryptEventContent decrypts content using the given transport scheme.
// NIP-04 is supported only as legacy interoperability; NIP-44 remains the default.
func (s *Service) decryptEventContent(scheme transportScheme, content string, counterparty nostr.PubKey) (string, error) {
	if scheme == schemeNIP04 {
		shared, err := nip04.ComputeSharedSecret(counterparty, s.transportSecret)
		if err != nil {
			return "", fmt.Errorf("compute nip04 shared secret: %w", err)
		}
		return nip04.Decrypt(content, shared)
	}

	conversationKey, err := nip44.GenerateConversationKey(counterparty, s.transportSecret)
	if err != nil {
		return "", fmt.Errorf("generate conversation key: %w", err)
	}
	return nip44.Decrypt(content, conversationKey)
}

// encryptEventContent encrypts content using the given transport scheme.
// NIP-04 is supported only as legacy interoperability; NIP-44 remains the default.
func (s *Service) encryptEventContent(scheme transportScheme, counterparty nostr.PubKey, plain string) (string, error) {
	if scheme == schemeNIP04 {
		shared, err := nip04.ComputeSharedSecret(counterparty, s.transportSecret)
		if err != nil {
			return "", fmt.Errorf("compute nip04 shared secret: %w", err)
		}
		return nip04.Encrypt(plain, shared)
	}

	conversationKey, err := nip44.GenerateConversationKey(counterparty, s.transportSecret)
	if err != nil {
		return "", fmt.Errorf("generate conversation key: %w", err)
	}
	return nip44.Encrypt(plain, conversationKey)
}

func normalizeRelays(relays []string) ([]string, error) {
	normalized := make([]string, 0, len(relays))
	for _, relay := range relays {
		trimmed := strings.TrimSpace(relay)
		if trimmed == "" {
			continue
		}
		if !nostr.IsValidRelayURL(trimmed) {
			return nil, fmt.Errorf("invalid relay url: %s", trimmed)
		}
		normalized = append(normalized, nostr.NormalizeURL(trimmed))
	}

	return normalized, nil
}

func generateBunkerSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate bunker secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}
