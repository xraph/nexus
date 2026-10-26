package usage

import (
	"errors"
	"fmt"
	"regexp"
)

const (
	// MaxMetadataPairs bounds the labels one record, or one filter, carries.
	MaxMetadataPairs = 16
	// MaxMetadataValueBytes bounds one label value.
	MaxMetadataValueBytes = 256
)

// ErrInvalidMetadata reports record metadata or a metadata filter that
// breaks the limits above or uses a key outside the allowed pattern.
var ErrInvalidMetadata = errors.New("nexus: invalid usage metadata")

// metadataKey is the allowed key shape. It keeps a key safe to embed in a
// JSON path and in a document field name, so no store has to escape it.
var metadataKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidateMetadata checks m as record metadata or as a filter: at most
// MaxMetadataPairs pairs, keys matching ^[A-Za-z0-9_-]{1,64}$ and values of
// at most MaxMetadataValueBytes bytes. A nil or empty map is valid.
func ValidateMetadata(m map[string]string) error {
	if len(m) > MaxMetadataPairs {
		return fmt.Errorf("%w: %d pairs, at most %d allowed", ErrInvalidMetadata, len(m), MaxMetadataPairs)
	}
	for k, v := range m {
		if !metadataKey.MatchString(k) {
			return fmt.Errorf("%w: key %q must match %s", ErrInvalidMetadata, k, metadataKey)
		}
		if len(v) > MaxMetadataValueBytes {
			return fmt.Errorf("%w: value of key %q is %d bytes, at most %d allowed", ErrInvalidMetadata, k, len(v), MaxMetadataValueBytes)
		}
	}
	return nil
}
