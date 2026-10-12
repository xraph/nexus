package cache

// KindOf reports a cache's optional Kind capability. Custom caches need not
// implement it; unknown means the backend cannot be identified.
func KindOf(c any) string {
	if c == nil {
		return "none"
	}
	if k, ok := c.(interface{ Kind() string }); ok {
		if kind := k.Kind(); kind != "" {
			return kind
		}
	}
	return "unknown"
}

func (s *cacheService) Kind() string { return KindOf(s.cache) }
