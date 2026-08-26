package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// HashBytes returns the lowercase hex SHA-256 digest of data.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashJSON returns a canonical SHA-256 digest of a JSON-serializable value.
func HashJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return HashBytes(b), nil
}
