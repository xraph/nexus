package anthropic

// CacheTTL is how long Anthropic keeps a cached prompt prefix.
type CacheTTL string

const (
	// CacheTTL5m is the default five minute lifetime.
	CacheTTL5m CacheTTL = "5m"
	// CacheTTL1h is the one hour lifetime, billed at a higher cache write rate.
	CacheTTL1h CacheTTL = "1h"
)

// CacheConfig turns on prompt caching. A breakpoint goes on the last tool
// definition and on the system prompt. Every breakpoint uses TTL, because the
// API rejects a longer TTL after a shorter one.
type CacheConfig struct {
	// TTL is "5m" or "1h". Empty or unknown means "5m".
	TTL CacheTTL
	// Messages also caches the conversation tail, through the top-level
	// automatic cache_control. Replayed messages are never edited.
	Messages bool
}

// WithPromptCaching enables prompt caching. It is off by default, so
// existing requests are unchanged on the wire.
func WithPromptCaching(c CacheConfig) Option {
	return func(p *Provider) {
		c.TTL = normalizeTTL(c.TTL)
		p.cache = &c
	}
}

func normalizeTTL(t CacheTTL) CacheTTL {
	if t == CacheTTL1h {
		return CacheTTL1h
	}
	return CacheTTL5m
}

// cacheControl is the cache_control marker. The 5m lifetime is the API
// default, so it is sent without a ttl.
type cacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

// anthropicSystemBlock is a system prompt text block.
type anthropicSystemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

func (c *CacheConfig) marker() *cacheControl {
	m := &cacheControl{Type: "ephemeral"}
	if c.TTL == CacheTTL1h {
		m.TTL = string(CacheTTL1h)
	}
	return m
}

// apply sets the system prompt and, when caching is on, the breakpoints.
// That is at most three: the last tool, the system block and the top-level
// marker. Messages are left alone, so replayed native content keeps its
// bytes and signed thinking blocks.
func (c *CacheConfig) apply(r *anthropicRequest, system string) {
	if c == nil {
		if system != "" {
			r.System = system
		}
		return
	}
	if system != "" {
		r.System = []anthropicSystemBlock{{Type: "text", Text: system, CacheControl: c.marker()}}
	}
	if n := len(r.Tools); n > 0 {
		r.Tools[n-1].CacheControl = c.marker()
	}
	if c.Messages {
		r.CacheControl = c.marker()
	}
}
