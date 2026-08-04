// Package media signs and verifies access to private uploads (e.g. a student's
// homework photo). Files live outside the public /uploads mount and are served
// only via a signed URL, so the images aren't publicly enumerable/fetchable.
package media

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Sign returns the HMAC-SHA256 signature (hex) of a filename with the secret.
// The filename is cryptographically random, so a valid (file, sig) pair can't be
// forged without the secret — this is the access token for the file.
func Sign(file, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(file))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify checks a signature in constant time.
func Verify(file, sig, secret string) bool {
	if file == "" || sig == "" || secret == "" {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(Sign(file, secret)))
}
