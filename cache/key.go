package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/xraph/nexus/provider"
)

// CacheKey generates a deterministic cache key from a request.
// The key is a SHA-256 hash of the relevant request fields.
func CacheKey(req *provider.CompletionRequest) string {
	h := sha256.New()

	// Model
	_, _ = fmt.Fprintf(h, "model:%s\n", req.Model)

	// Messages (deterministic serialization)
	for _, msg := range req.Messages {
		_, _ = fmt.Fprintf(h, "msg:%s:", msg.Role)
		switch c := msg.Content.(type) {
		case string:
			_, _ = fmt.Fprintf(h, "%s", c)
		default:
			data, _ := json.Marshal(c)
			h.Write(data)
		}
		h.Write([]byte("\n"))
	}

	// Temperature
	if req.Temperature != nil {
		_, _ = fmt.Fprintf(h, "temp:%f\n", *req.Temperature)
	}

	// MaxTokens
	if req.MaxTokens > 0 {
		_, _ = fmt.Fprintf(h, "max_tokens:%d\n", req.MaxTokens)
	}

	// TopP
	if req.TopP != nil {
		_, _ = fmt.Fprintf(h, "top_p:%f\n", *req.TopP)
	}

	// Stop sequences
	if len(req.Stop) > 0 {
		sorted := make([]string, len(req.Stop))
		copy(sorted, req.Stop)
		sort.Strings(sorted)
		for _, s := range sorted {
			_, _ = fmt.Fprintf(h, "stop:%s\n", s)
		}
	}

	// Tools (if present, changes the response)
	if len(req.Tools) > 0 {
		data, _ := json.Marshal(req.Tools)
		_, _ = fmt.Fprintf(h, "tools:%s\n", data)
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}
