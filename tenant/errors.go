package tenant

import "errors"

// ErrNotFound reports that no tenant matched. Stores return it from
// FindByID, FindBySlug and Update, never a nil tenant with a nil error.
var ErrNotFound = errors.New("nexus: tenant not found")

// ErrInvalid wraps every input the tenant service refuses, such as a
// missing name or slug.
var ErrInvalid = errors.New("nexus: invalid tenant input")
