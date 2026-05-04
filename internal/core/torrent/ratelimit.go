package torrent

import (
	"bytes"
	"encoding/base64"
	"io"
)

// bytes_reader wraps a byte slice in an io.ReadSeeker.
func bytes_reader(b []byte) io.ReadSeeker {
	return bytes.NewReader(b)
}

// base64Decode decodes a base64-encoded string.
func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
