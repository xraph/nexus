package key

import "errors"

// ErrNotFound reports that no API key matched. Stores return it from
// FindByID, FindByPrefix and Update, never a nil key with a nil error.
var ErrNotFound = errors.New("nexus: api key not found")
