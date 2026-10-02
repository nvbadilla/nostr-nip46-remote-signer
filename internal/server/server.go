package server

import (
	"encoding/json"
	"net/http"
	"time"
)

// New builds an HTTP server with health endpoint and static content routes.
func New(addr, staticDir string) *http.Server {
	return NewWithAPI(addr, staticDir, newAPI(""))
}

// NewWithAPI builds an HTTP server using the provided API handlers.
func NewWithAPI(addr, staticDir string, apiHandlers *api) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           NewMuxWithAPI(staticDir, apiHandlers),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// NewMux wires all application routes.
func NewMux(staticDir string) *http.ServeMux {
	return NewMuxWithAPI(staticDir, newAPI(""))
}

// NewMuxWithAPI wires all routes, including API handlers and static files.
func NewMuxWithAPI(staticDir string, apiHandlers *api) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	if apiHandlers != nil {
		apiHandlers.register(mux)
	}
	mux.Handle("/", http.FileServer(http.Dir(staticDir)))
	return mux
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
