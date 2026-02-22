package nexus

import "net/http"

// mountHandlers registers the HTTP handlers on the given router.
// Full route implementation is in the api/ package (Phase 12).
// This is a stub that registers basic health and placeholder routes.
func mountHandlers(gw *Gateway, mux Router, basePath string) {
	// Health check
	mux.HandleFunc("GET "+basePath+"/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
}
