package types

import (
	"crypto/sha256"
	"encoding/hex"
)

// maxPostgresIdentifierLen is PostgreSQL's identifier length limit in bytes
// (NAMEDATALEN - 1 = 63). An identifier longer than this is SILENTLY TRUNCATED
// by the server on creation, so a generated name that exceeds it would be stored
// under a different (truncated) name than the one SchemaHero looks up on the next
// reconcile — the lookup misses, an ADD is emitted, then a DROP of the original
// long name, and the re-apply fails 42710/42P07, permanently wedging the table's
// plan. Capping the name ourselves keeps the generated name byte-identical across
// reconciles.
const maxPostgresIdentifierLen = 63

// identifierHashSuffixLen is the number of hex characters of the FULL-name hash
// appended after truncation to preserve uniqueness. Two distinct long names that
// share a 63-byte prefix would otherwise collide once truncated; the hash makes
// that astronomically unlikely while staying deterministic (same input -> same
// suffix on every reconcile).
const identifierHashSuffixLen = 8

// capPostgresIdentifier returns name unchanged when it already fits within
// PostgreSQL's 63-byte identifier limit; otherwise it returns a deterministic,
// uniqueness-preserving name that fits: a truncated prefix of the original joined
// by '_' to an 8-hex-char prefix of sha256(name). The result is stable for a
// given input (so a second reconcile generates the identical name and produces no
// DDL) and distinguishes inputs that share a long common prefix (so two different
// long expressions do not collapse to the same constraint name).
//
// This mirrors the intent of the MySQL index-name generator (which caps at its
// own 64-byte limit) but, unlike that one's naive prefix slice, preserves
// uniqueness via the hash suffix — naive truncation can alias two distinct names
// to the same identifier and is itself a churn/collision hazard.
func capPostgresIdentifier(name string) string {
	if len(name) <= maxPostgresIdentifierLen {
		return name
	}

	sum := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(sum[:])[:identifierHashSuffixLen]

	// Reserve room for the '_' separator and the hash suffix, then truncate the
	// prefix to fill the rest. prefixLen is always >= 0 because the suffix plus
	// separator (9) is far below the 63-byte budget.
	prefixLen := maxPostgresIdentifierLen - len(suffix) - 1
	prefix := name[:prefixLen]

	return prefix + "_" + suffix
}
