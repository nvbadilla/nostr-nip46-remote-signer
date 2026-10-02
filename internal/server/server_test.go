package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHealth(t *testing.T) {
	staticDir := createStaticDir(t)
	mux := NewMux(staticDir)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unable to decode response: %v", err)
	}

	if got["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", got["status"])
	}
}

func TestHealthMethodNotAllowed(t *testing.T) {
	staticDir := createStaticDir(t)
	mux := NewMux(staticDir)

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestStaticServing(t *testing.T) {
	staticDir := createStaticDir(t)
	mux := NewMux(staticDir)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty static response body")
	}
}

func createStaticDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.html")
	content := []byte("<!doctype html><html><body>ok</body></html>")

	if err := os.WriteFile(indexPath, content, 0o600); err != nil {
		t.Fatalf("unable to create static fixture: %v", err)
	}

	return dir
}
