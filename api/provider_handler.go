package api

import (
	"net/http"
)

func (a *API) handleListProviders(w http.ResponseWriter, r *http.Request) {
	providers := a.gw.Providers().All()

	type providerInfo struct {
		Name         string `json:"name"`
		Healthy      bool   `json:"healthy"`
		Capabilities any    `json:"capabilities"`
	}

	data := make([]providerInfo, 0, len(providers))
	for _, p := range providers {
		data = append(data, providerInfo{
			Name:         p.Name(),
			Healthy:      p.Healthy(r.Context()),
			Capabilities: p.Capabilities(),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": data,
	})
}
