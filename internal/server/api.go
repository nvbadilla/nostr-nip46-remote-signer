package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
	"nostr-app/internal/keymgr"
	"nostr-app/internal/signer"
)

const (
	stateStopped   = "stopped"
	stateWaiting   = "waiting"
	stateConnected = "connected"
	stateSigned    = "signed"
	stateError     = "error"
)

type api struct {
	mu sync.Mutex

	defaultRelay string
	relay        string

	secret      nostr.SecretKey
	hasIdentity bool

	state     string
	lastError string

	runner       *signer.RelayRunner
	runnerCancel context.CancelFunc
	runnerDone   chan error
	service      *signer.Service

	activeRunnerID uint64
	nextRunnerID   uint64
}

type identityResponse struct {
	Loaded    bool   `json:"loaded"`
	PublicKey string `json:"public_key,omitempty"`
	NPub      string `json:"npub,omitempty"`
}

type signerStatusResponse struct {
	State     string           `json:"state"`
	Relay     string           `json:"relay,omitempty"`
	LastError string           `json:"last_error,omitempty"`
	Identity  identityResponse `json:"identity"`
}

type signerStartResponse struct {
	State     string           `json:"state"`
	Relay     string           `json:"relay,omitempty"`
	LastError string           `json:"last_error,omitempty"`
	Identity  identityResponse `json:"identity"`
	BunkerURL string           `json:"bunker_url"`
}

func newAPI(defaultRelay string) *api {
	relay := strings.TrimSpace(defaultRelay)
	if relay == "" || !nostr.IsValidRelayURL(relay) {
		relay = "wss://relay.damus.io"
	}
	relay = nostr.NormalizeURL(relay)

	return &api{
		defaultRelay: relay,
		relay:        relay,
		state:        stateStopped,
	}
}

// NewAPI creates API handlers for signer UI operations.
func NewAPI(defaultRelay string) *api {
	return newAPI(defaultRelay)
}

func (a *api) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/identity", a.handleIdentity)
	mux.HandleFunc("/api/identity/import", a.handleImportIdentity)
	mux.HandleFunc("/api/identity/generate", a.handleGenerateIdentity)
	mux.HandleFunc("/api/signer/start", a.handleStartSigner)
	mux.HandleFunc("/api/signer/stop", a.handleStopSigner)
	mux.HandleFunc("/api/signer/status", a.handleSignerStatus)
}

func (a *api) handleIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	writeJSON(w, http.StatusOK, a.statusLocked())
}

func (a *api) handleImportIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	var payload struct {
		Secret string `json:"secret"`
	}
	if err := decodeJSONBody(r, &payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	sk, err := keymgr.ParseSecret(payload.Secret)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid key format, expected nsec or 64-char hex")
		return
	}

	if err := a.stopRunner(); err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}

	a.mu.Lock()
	a.setSecretLocked(sk)
	a.state = stateStopped
	a.lastError = ""
	a.mu.Unlock()

	writeJSON(w, http.StatusOK, a.statusLocked())
}

func (a *api) handleGenerateIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	if err := a.stopRunner(); err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}

	a.mu.Lock()
	a.setSecretLocked(nostr.Generate())
	a.state = stateStopped
	a.lastError = ""
	a.mu.Unlock()

	writeJSON(w, http.StatusOK, a.statusLocked())
}

func (a *api) handleStartSigner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	var payload struct {
		Relay string `json:"relay"`
	}
	if err := decodeJSONBody(r, &payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	a.mu.Lock()
	if !a.hasIdentity {
		a.mu.Unlock()
		writeJSONError(w, http.StatusBadRequest, "load or generate an identity first")
		return
	}
	secret := a.secret
	relay := strings.TrimSpace(payload.Relay)
	if relay == "" {
		relay = a.defaultRelay
	}
	a.mu.Unlock()

	if !nostr.IsValidRelayURL(relay) {
		writeJSONError(w, http.StatusBadRequest, "invalid relay url")
		return
	}
	relay = nostr.NormalizeURL(relay)

	if err := a.stopRunner(); err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}

	service, err := signer.New(secret, []string{relay})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	runner, err := signer.NewRelayRunner(service, relay)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	a.mu.Lock()
	a.nextRunnerID++
	runnerID := a.nextRunnerID
	a.mu.Unlock()

	runner.SetHooks(a.onRequestHandled(runnerID), a.onRequestError(runnerID))

	bunkerURI, err := runner.BunkerURI()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	a.mu.Lock()
	a.runner = runner
	a.runnerCancel = cancel
	a.runnerDone = done
	a.service = service
	a.activeRunnerID = runnerID
	a.relay = relay
	a.state = stateWaiting
	a.lastError = ""
	status := signerStatusResponse{
		State:     a.state,
		Relay:     a.relay,
		LastError: a.lastError,
		Identity:  a.identityLocked(),
	}
	a.mu.Unlock()

	go func() {
		err := runner.Run(ctx)
		done <- err

		a.mu.Lock()
		defer a.mu.Unlock()
		if a.activeRunnerID != runnerID {
			return
		}

		if err == nil || errors.Is(err, context.Canceled) {
			return
		}

		a.state = stateError
		a.lastError = err.Error()
	}()

	writeJSON(w, http.StatusOK, signerStartResponse{
		State:     status.State,
		Relay:     status.Relay,
		LastError: status.LastError,
		Identity:  status.Identity,
		BunkerURL: bunkerURI,
	})
}

func (a *api) handleStopSigner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	if err := a.stopRunner(); err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.statusLocked())
}

func (a *api) handleSignerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	writeJSON(w, http.StatusOK, a.statusLocked())
}

func (a *api) onRequestHandled(runnerID uint64) func(req signer.Request, resp signer.Response) {
	return func(req signer.Request, resp signer.Response) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.activeRunnerID != runnerID || a.state == stateStopped {
			return
		}

		if resp.Error != "" {
			a.lastError = resp.Error
			return
		}

		switch req.Method {
		case "connect":
			a.state = stateConnected
			a.lastError = ""
		case "sign_event":
			a.state = stateSigned
			a.lastError = ""
		case "logout":
			a.state = stateWaiting
			a.lastError = ""
		}
	}
}

func (a *api) onRequestError(runnerID uint64) func(err error) {
	return func(err error) {
		if err == nil {
			return
		}

		a.mu.Lock()
		defer a.mu.Unlock()
		if a.activeRunnerID != runnerID || a.state == stateStopped {
			return
		}

		a.lastError = err.Error()
	}
}

func (a *api) stopRunner() error {
	a.mu.Lock()
	cancel := a.runnerCancel
	done := a.runnerDone
	if cancel == nil || done == nil {
		a.runnerCancel = nil
		a.runnerDone = nil
		a.runner = nil
		a.service = nil
		a.activeRunnerID = 0
		a.state = stateStopped
		a.lastError = ""
		a.mu.Unlock()
		return nil
	}
	a.activeRunnerID = 0
	a.state = stateStopped
	a.lastError = ""
	a.mu.Unlock()

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		return errors.New("failed to stop signer runner in time")
	}

	a.mu.Lock()
	a.runnerCancel = nil
	a.runnerDone = nil
	a.runner = nil
	a.service = nil
	a.activeRunnerID = 0
	a.state = stateStopped
	a.lastError = ""
	a.mu.Unlock()

	return nil
}

func (a *api) statusLocked() signerStatusResponse {
	a.mu.Lock()
	defer a.mu.Unlock()

	return signerStatusResponse{
		State:     a.state,
		Relay:     a.relay,
		LastError: a.lastError,
		Identity:  a.identityLocked(),
	}
}

func (a *api) identityLocked() identityResponse {
	if !a.hasIdentity {
		return identityResponse{Loaded: false}
	}

	pk := a.secret.Public()
	return identityResponse{
		Loaded:    true,
		PublicKey: pk.Hex(),
		NPub:      nip19.EncodeNpub(pk),
	}
}

func (a *api) setSecretLocked(next nostr.SecretKey) {
	for i := range a.secret {
		a.secret[i] = 0
	}
	a.secret = next
	a.hasIdentity = true
}

func decodeJSONBody(r *http.Request, target any) error {
	defer r.Body.Close()

	dec := json.NewDecoder(io.LimitReader(r.Body, 8192))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("invalid json body: %w", err)
	}

	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("invalid json body: multiple objects")
	}

	return nil
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
