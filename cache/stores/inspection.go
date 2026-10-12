package stores

// Kind identifies the backend without contacting it.
func (*MemoryCache) Kind() string { return "memory" }

// Kind identifies the backend without contacting it.
func (*RedisCache) Kind() string { return "redis" }

// Kind identifies the backend without contacting it.
func (*MemoryStreamCache) Kind() string { return "memory" }

// Kind identifies the backend without contacting it.
func (*RedisStreamCache) Kind() string { return "redis" }
