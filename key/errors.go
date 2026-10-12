package key

import "errors"

// ErrNotFound reports that no API key matched. Stores return it from
// FindByID, TouchLastUsed and Update, never a nil key with a nil error.
// FindByPrefix reports none found as an empty slice instead. Validate also
// returns it for any raw key that matches no stored hash, so a caller cannot
// tell an unknown prefix from a wrong key.
var (
	ErrNotFound = errors.New("nexus: api key not found")
	// ErrRevoked and ErrExpired are returned by Validate only for the right
	// raw key.
	ErrRevoked = errors.New("nexus: api key revoked")
	ErrExpired = errors.New("nexus: api key expired")
	// ErrInvalid wraps every input the service refuses.
	ErrInvalid = errors.New("nexus: invalid api key input")
	// ErrDuplicate is returned by Store.Insert when a key with the same hash
	// is already stored.
	ErrDuplicate = errors.New("nexus: api key already exists")
)
