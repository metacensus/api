package store

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// IDKind is the prefix naming what an id identifies. An id is
// "<kind>:<UUIDv7>", e.g. "topc:0192a642-817d-7a3e-a282-d7a282ebd482" — infra's
// scheme (core/shared/types), kept so an id carries its kind, sorts by when it
// was minted, and yields a suffix that is already a legal Fabric channel
// segment. The wire still types ids as plain strings.
type IDKind string

const (
	UserID  IDKind = "user"
	TopicID IDKind = "topc"
	PropID  IDKind = "prop"
)

// NewID mints a fresh id of kind k.
func NewID(k IDKind) string {
	// uuid.NewV7 fails only if crypto/rand does; a panic is better than
	// minting a predictable id.
	return string(k) + ":" + uuid.Must(uuid.NewV7()).String()
}

// ParseID returns the UUIDv7 suffix of an id of kind k. Anything else — another
// kind, another UUID version, or a UUID not in canonical lowercase form — is
// InvalidContent.
func ParseID(k IDKind, id string) (uuid.UUID, error) {
	suffix, ok := strings.CutPrefix(id, string(k)+":")
	if !ok {
		return uuid.UUID{}, fmt.Errorf("%w: %q is not a %s id", InvalidContent, id, k)
	}
	u, err := uuid.Parse(suffix)
	if err != nil || u.String() != suffix || u.Version() != 7 || u.Variant() != uuid.RFC4122 {
		return uuid.UUID{}, fmt.Errorf("%w: %q is not a %s id", InvalidContent, id, k)
	}
	return u, nil
}
