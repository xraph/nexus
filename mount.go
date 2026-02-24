package nexus

import "net/http"

// mountHandlers registers the HTTP handlers on the given router.
// Full route implementation is in the api/ package (Phase 12).
// This is a stub that registers basic health and placeholder routes.
func mountHandlers(_ *Gateway, mux Router, basePath string) {
	// Health check
	mux.HandleFunc("GET "+basePath+"/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
			return
		}
	})
}
