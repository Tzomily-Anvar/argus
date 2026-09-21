package ghauth_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/ghauth"
)

// Asks GitHub for a device code against the real App. It stops short of
// waiting for anyone to enter it, so it is safe to run unattended: it
// proves the App exists, that the device flow is switched on, and that no
// client secret is required.
func TestLiveDeviceCode(t *testing.T) {
	id := os.Getenv("ARGUS_GITHUB_APP_CLIENT_ID")
	if id == "" {
		t.Skip("set ARGUS_GITHUB_APP_CLIENT_ID to run the live check")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	code, err := ghauth.New(id).RequestDeviceCode(ctx)
	if err != nil {
		t.Fatalf("requesting a device code: %v", err)
	}
	if code.UserCode == "" || code.VerificationURI == "" {
		t.Fatalf("incomplete response: %+v", code)
	}
	t.Logf("device flow is enabled on this App")
	t.Logf("  user code:  %s", code.UserCode)
	t.Logf("  visit:      %s", code.VerificationURI)
	t.Logf("  expires in: %ds, poll every %ds", code.ExpiresIn, code.Interval)
}
