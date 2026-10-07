package nexus

import "github.com/xraph/nexus/pipeline"

// RefusalError is the typed refusal every enforcement stage returns. It
// lives in pipeline so stages can return it without importing this package.
type RefusalError = pipeline.RefusalError
