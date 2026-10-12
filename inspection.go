package nexus

import "github.com/xraph/nexus/cache"

// CacheKinds reports completion and stream cache backends separately. A
// custom cache without a Kind method is unknown; an absent cache is none.
func (gw *Gateway) CacheKinds() (completion, stream string) {
	return cache.KindOf(gw.cache), cache.KindOf(gw.streamCache)
}
