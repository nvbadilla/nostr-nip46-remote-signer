package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nostr-app/internal/signer"
)

func TestSignerStatusInitialState(t *testing.T) {
	mux := NewMuxWithAPI(createStaticDir(t), newAPI("wss://relay.damus.io"))

	req := httptest.NewRequest(http.MethodGet, "/api/signer/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var status signerStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("unable to decode response: %v", err)
	}

	if status.State != stateStopped {
		t.Fatalf("expected state %q, got %q", stateStopped, status.State)
	}
	if status.Identity.Loaded {
		t.Fatal("expected no identity loaded")
	}

	if strings.Contains(rec.Body.String(), "bunker_url") {
		t.Fatal("status response must not include bunker_url")
	}
}

func TestImportIdentityNeverReturnsPrivateKey(t *testing.T) {
	mux := NewMuxWithAPI(createStaticDir(t), newAPI("wss://relay.damus.io"))
	secret := strings.Repeat("a", 64)
	body := []byte(`{"secret":"` + secret + `"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/identity/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (%s)", http.StatusOK, rec.Code, rec.Body.String())
	}

	var status signerStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("unable to decode response: %v", err)
	}

	if !status.Identity.Loaded {
		t.Fatal("expected identity loaded")
	}
	if status.Identity.PublicKey == "" || status.Identity.NPub == "" {
		t.Fatal("expected public identity fields")
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unable to decode raw response: %v", err)
	}

	if _, exists := raw["secret"]; exists {
		t.Fatal("response should not contain private key")
	}

	if strings.Contains(rec.Body.String(), secret) {
		t.Fatal("response should not echo private key content")
	}
}

func TestImportIdentityInvalidKey(t *testing.T) {
	mux := NewMuxWithAPI(createStaticDir(t), newAPI("wss://relay.damus.io"))
	body := []byte(`{"secret":"not-a-valid-secret"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/identity/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestGenerateIdentity(t *testing.T) {
	mux := NewMuxWithAPI(createStaticDir(t), newAPI("wss://relay.damus.io"))

	req := httptest.NewRequest(http.MethodPost, "/api/identity/generate", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var status signerStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("unable to decode response: %v", err)
	}

	if !status.Identity.Loaded {
		t.Fatal("expected identity loaded")
	}
	if status.Identity.PublicKey == "" || status.Identity.NPub == "" {
		t.Fatal("expected generated public identity")
	}
}

func TestStartSignerRequiresIdentity(t *testing.T) {
	mux := NewMuxWithAPI(createStaticDir(t), newAPI("wss://relay.damus.io"))
	body := []byte(`{"relay":"wss://relay.damus.io"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/signer/start", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestOnRequestErrorDoesNotChangeGlobalState(t *testing.T) {
	a := newAPI("wss://relay.damus.io")
	a.activeRunnerID = 1
	a.state = stateWaiting

	a.onRequestError(1)(errors.New("invalid inbound request"))

	if a.state != stateWaiting {
		t.Fatalf("expected state to stay %q, got %q", stateWaiting, a.state)
	}
	if a.lastError == "" {
		t.Fatal("expected lastError to be updated")
	}
}

func TestStaleRunnerCallbackCannotChangeState(t *testing.T) {
	a := newAPI("wss://relay.damus.io")
	a.activeRunnerID = 2
	a.state = stateWaiting

	a.onRequestHandled(1)(signer.Request{Method: "connect"}, signer.Response{Result: "ack"})

	if a.state != stateWaiting {
		t.Fatalf("expected stale callback to be ignored, got state %q", a.state)
	}
}

func TestStopRunnerConfirmsTermination(t *testing.T) {
	a := newAPI("wss://relay.damus.io")

	called := false
	a.runnerCancel = func() { called = true }
	a.runnerDone = make(chan error, 1)
	a.runnerDone <- nil
	a.activeRunnerID = 7
	a.state = stateConnected

	if err := a.stopRunner(); err != nil {
		t.Fatalf("stopRunner() error: %v", err)
	}

	if !called {
		t.Fatal("expected cancel to be called")
	}
	if a.runnerCancel != nil || a.runnerDone != nil || a.activeRunnerID != 0 {
		t.Fatal("expected runner references to be cleared")
	}
	if a.state != stateStopped {
		t.Fatalf("expected state %q, got %q", stateStopped, a.state)
	}
}
