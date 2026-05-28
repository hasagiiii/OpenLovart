// Package projects implements the user-scoped Project CRUD endpoints. The
// list endpoint uses cursor pagination (see design.md D2 / spec.md): cursor
// values encode (updated_at, id) so the SQL predicate is a simple
// `(updated_at, id) < (?, ?)` on a covering composite index.
package projects

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidCursor is returned by DecodeCursor on any malformed input. The
// handler maps it to HTTP 400 invalid_cursor.
var ErrInvalidCursor = errors.New("projects: invalid cursor")

// Cursor names the (updated_at, id) pair that identifies the boundary
// between a returned page and "the rest". A nil *Cursor (or empty string)
// means "start at the head".
type Cursor struct {
	UpdatedAt time.Time
	ID        uuid.UUID
}

// EncodeCursor returns a URL-safe encoding of the cursor. Format:
//
//	base64url(<rfc3339-nano updated_at>|<uuid id>)
//
// The "|" separator is fine because RFC3339 timestamps and UUIDs both
// produce a stable known character set that excludes "|".
func EncodeCursor(updatedAt time.Time, id uuid.UUID) string {
	raw := updatedAt.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses an encoded cursor produced by EncodeCursor. Returns
// ErrInvalidCursor on every failure path so callers can map to a single
// HTTP error code.
func DecodeCursor(s string) (*Cursor, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return nil, ErrInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	return &Cursor{UpdatedAt: t, ID: id}, nil
}
