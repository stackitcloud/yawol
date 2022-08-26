package helper

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"strings"
)

// HashData returns a 16 char hash for the given object.
func HashData(data interface{}) (string, error) {
	var jsonSpec []byte
	var err error
	if jsonSpec, err = json.Marshal(data); err != nil {
		return "", err
	}

	bytes := sha256.Sum256(jsonSpec)
	return strings.ToLower(base32.StdEncoding.EncodeToString(bytes[:]))[:16], nil
}
