package torrent

import (
	"bytes"
	"encoding/base64"
	"io"

	"golang.org/x/time/rate"
)

// newRateLimiter creates a rate.Limiter for the given bytes-per-second limit.
func newRateLimiter(bytesPerSec int) *rate.Limiter {
	return rate.NewLimiter(rate.Limit(bytesPerSec), bytesPerSec)
}

// bytes_reader wraps a byte slice in an io.ReadSeeker.
func bytes_reader(b []byte) io.ReadSeeker {
	return bytes.NewReader(b)
}

// base64Decode decodes a base64-encoded string.
func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
