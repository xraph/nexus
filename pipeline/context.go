package pipeline

import (
	"context"
	"time"
)

type ctxKey string

const (
	ctxTenantID  ctxKey = "nexus.tenant_id"
	ctxKeyID     ctxKey = "nexus.key_id"
	ctxRequestID ctxKey = "nexus.request_id"
	ctxProvider  ctxKey = "nexus.provider"
	ctxCacheHit  ctxKey = "nexus.cache_hit"
	ctxStartTime ctxKey = "nexus.start_time"
)

// TenantID returns the tenant ID from context.
func TenantID(ctx context.Context) string {
	v, _ := ctx.Value(ctxTenantID).(string)
	return v
}

// WithTenantID sets the tenant ID in context.
func WithTenantID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxTenantID, id)
}

// KeyID returns the key ID from context.
func KeyID(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyID).(string)
	return v
}

// WithKeyID sets the key ID in context.
func WithKeyID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyID, id)
}

// RequestID returns the request ID from context.
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(ctxRequestID).(string)
	return v
}

// WithRequestID sets the request ID in context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxRequestID, id)
}

// ProviderName returns the provider name from context.
func ProviderName(ctx context.Context) string {
	v, _ := ctx.Value(ctxProvider).(string)
	return v
}

// WithProviderName sets the provider name in context.
func WithProviderName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, ctxProvider, name)
}

// CacheHit returns whether the response was served from cache.
func CacheHit(ctx context.Context) bool {
	v, _ := ctx.Value(ctxCacheHit).(bool)
	return v
}

// WithCacheHit sets the cache hit flag in context.
func WithCacheHit(ctx context.Context, hit bool) context.Context {
	return context.WithValue(ctx, ctxCacheHit, hit)
}

// StartTime returns the request start time from context.
func StartTime(ctx context.Context) time.Time {
	v, _ := ctx.Value(ctxStartTime).(time.Time)
	return v
}

// WithStartTime sets the request start time in context.
func WithStartTime(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, ctxStartTime, t)
}
