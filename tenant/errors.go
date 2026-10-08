package tenant

import "errors"

// ErrNotFound reports that no tenant matched. Stores return it from
// FindByID, FindBySlug and Update, never a nil tenant with a nil error.
var ErrNotFound = errors.New("nexus: tenant not found")

// ErrInUse reports that a tenant cannot be deleted because it still has keys
// or usage history. Disable it instead.
var ErrInUse = errors.New("nexus: tenant is in use")

// ErrInvalid wraps every input the tenant service refuses, such as a
// missing name or slug.
var ErrInvalid = errors.New("nexus: invalid tenant input")
