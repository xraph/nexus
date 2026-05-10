package httpstream

import (
	"net/http"
	"strings"
)

// Negotiate picks the encoder a client requested, falling back to the
// registry default. Order of precedence:
//
//  1. ?stream_format=  query parameter
//  2. X-Nexus-Stream-Format header
//  3. Accept header — first matching encoder
//  4. Registry default
//
// The matcher is case-insensitive and accepts both content-type values
// ("application/x-ndjson") and aliases ("ndjson", "sse"). Unknown values
// fall through to the next step.
func Negotiate(r *http.Request, reg *Registry) StreamEncoder {
	if reg == nil {
		return nil
	}
	if r != nil {
		if q := r.URL.Query().Get("stream_format"); q != "" {
			if enc := reg.Lookup(q); enc != nil {
				return enc
			}
		}
		if h := r.Header.Get("X-Nexus-Stream-Format"); h != "" {
			if enc := reg.Lookup(h); enc != nil {
				return enc
			}
		}
		if accept := r.Header.Get("Accept"); accept != "" {
			for _, part := range strings.Split(accept, ",") {
				ct := strings.TrimSpace(part)
				if i := strings.Index(ct, ";"); i >= 0 {
					ct = strings.TrimSpace(ct[:i])
				}
				if ct == "" || ct == "*/*" {
					continue
				}
				if enc := reg.Lookup(ct); enc != nil {
					return enc
				}
			}
		}
	}
	return reg.Default()
}
