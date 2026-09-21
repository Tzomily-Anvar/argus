package gh

import (
	"os"
	"testing"
)

// Which credential wins.
//
// The App's client id has a built-in default so that `argus login` works
// on a fresh install. That makes "an App is configured" true of every
// installation, so it cannot also mean "prefer the App" - doing so would
// take the token out of the hands of everyone already using one.
func TestExplicitTokenBeatsTheDefaultApp(t *testing.T) {
	for _, k := range []string{"ARGUS_GITHUB_APP_CLIENT_ID", "GH_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	t.Setenv("ARGUS_GITHUB_TOKEN", "ghp_pretend")
	t.Setenv("ARGUS_DATA_DIR", t.TempDir()) // nobody is signed in here

	c, err := NewFromEnv()
	if err != nil {
		t.Fatalf("a configured token should be usable on its own: %v", err)
	}
	if c.tokenType != TokenClassic {
		t.Errorf("token type = %v, want the classic token that was configured", c.tokenType)
	}
}

// And the reverse: with no token anywhere, the published App is the path,
// so the error has to send someone to `argus login` rather than to a
// token setup page they were trying to avoid.
func TestNoTokenPointsAtLogin(t *testing.T) {
	for _, k := range []string{"ARGUS_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	t.Setenv("ARGUS_DATA_DIR", t.TempDir())

	_, err := NewFromEnv()
	if err == nil {
		t.Fatal("expected an error when nothing is configured")
	}
	if !contains(err.Error(), "argus login") {
		t.Errorf("error should point at `argus login`, got: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// A sign-in is a deliberate act; GITHUB_TOKEN in the environment usually
// is not. Someone who runs `argus login` and happens to export a token
// for some other tool should still get the App.
func TestSignInBeatsAnAmbientToken(t *testing.T) {
	os.Unsetenv("ARGUS_GITHUB_TOKEN")
	t.Setenv("GITHUB_TOKEN", "ghp_for_some_other_tool")

	dir := t.TempDir()
	t.Setenv("ARGUS_DATA_DIR", dir)
	session := `{"access_token":"ghu_x","refresh_token":"ghr_y",` +
		`"expires_at":"2099-01-01T00:00:00Z","refresh_expires_at":"2099-01-01T00:00:00Z"}`
	if err := os.WriteFile(dir+"/github-app-session.json", []byte(session), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := NewFromEnv()
	if err != nil {
		t.Fatalf("signed in, so this should work: %v", err)
	}
	if c.tokenType != TokenAppUser {
		t.Errorf("token type = %v, want the App session the user signed into", c.tokenType)
	}
}
