package key

import "errors"

// ErrNotFound reports that no API key matched. Stores return it from
// FindByID, TouchLastUsed and Update, never a nil key with a nil error.
// FindByPrefix reports none found as an empty slice instead.
var ErrNotFound = errors.New("nexus: api key not found")
