package ghauth

import "testing"

// The macOS keychain hex-encodes anything containing a newline, without
// saying so. This cost a real session before it was noticed.
func TestHexEncodedSecretIsDecoded(t *testing.T) {
	plain := `{"access_token":"ghu_x"}`
	hexed := "7b226163636573735f746f6b656e223a226768755f78227d"

	if got := decodeSecurityOutput(hexed); got != plain {
		t.Errorf("hex was not decoded:\n got %q\nwant %q", got, plain)
	}
	if got := decodeSecurityOutput(plain); got != plain {
		t.Errorf("plain JSON should pass through untouched, got %q", got)
	}
}
