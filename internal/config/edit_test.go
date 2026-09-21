package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func setting(t *testing.T, key string) Setting {
	t.Helper()
	s, ok := Core().Find(key)
	if !ok {
		t.Fatalf("%s is not in the catalogue", key)
	}
	return s
}

// The file is a document someone has read and annotated. A writer that
// rebuilt it from a parse tree would hand back a tidy file with all of
// that gone, so the test is about what is left alone as much as what
// changes.
func TestSetUncommentsInPlace(t *testing.T) {
	path := write(t, `# Argus configuration.

# The port. Deliberately unusual so it does not collide with a dev server.
# ARGUS_PORT=18474

# How often the background sweep runs.
# ARGUS_REFRESH_MINUTES=15
`)
	ch, err := SetInFile(path, setting(t, "ARGUS_PORT"), "9000")
	if err != nil {
		t.Fatal(err)
	}
	if !ch.Uncommented {
		t.Error("a commented-out setting should be turned on where it stands")
	}

	got := read(t, path)
	want := `# Argus configuration.

# The port. Deliberately unusual so it does not collide with a dev server.
ARGUS_PORT=9000

# How often the background sweep runs.
# ARGUS_REFRESH_MINUTES=15
`
	if got != want {
		t.Errorf("file is\n%s\nwant\n%s", got, want)
	}
}

func TestSetReplacesRatherThanDuplicates(t *testing.T) {
	path := write(t, "ARGUS_PORT=9000\nARGUS_BIND=127.0.0.1\n")
	ch, err := SetInFile(path, setting(t, "ARGUS_PORT"), "9100")
	if err != nil {
		t.Fatal(err)
	}
	if !ch.Replaced || ch.Previous != "9000" {
		t.Errorf("Change = %+v, want the old value reported", ch)
	}
	got := read(t, path)
	if strings.Count(got, "ARGUS_PORT=") != 1 {
		t.Errorf("file is\n%s\nwant one ARGUS_PORT line", got)
	}
	if !strings.Contains(got, "ARGUS_PORT=9100") {
		t.Errorf("file is\n%s", got)
	}
}

// The first line wins when the file is read, so a second one is doing
// nothing but misleading whoever opens the file next.
func TestSetSilencesLinesThatWereNeverRead(t *testing.T) {
	path := write(t, "ARGUS_CONCURRENCY=5\n\n# added later, and wondered about ever since\nARGUS_CONCURRENCY=20\n")
	ch, err := SetInFile(path, setting(t, "ARGUS_CONCURRENCY"), "12")
	if err != nil {
		t.Fatal(err)
	}
	if ch.Duplicates != 1 {
		t.Errorf("Duplicates = %d, want 1", ch.Duplicates)
	}
	values, err := ReadFileValues(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["ARGUS_CONCURRENCY"] != "12" {
		t.Errorf("file now reads %q", values["ARGUS_CONCURRENCY"])
	}
	if strings.Count(read(t, path), "\nARGUS_CONCURRENCY=") != 0 {
		t.Error("the duplicate should be commented out")
	}
}

func TestSetAppendsWithItsDescription(t *testing.T) {
	path := write(t, "ARGUS_GITHUB_ORG=your-org\n")
	s := setting(t, "ARGUS_SPRINT_RECENT")
	if _, err := SetInFile(path, s, "6"); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.Contains(got, "# "+s.Desc) {
		t.Errorf("an appended setting should carry its description:\n%s", got)
	}
	values, _ := ReadFileValues(path)
	if values["ARGUS_SPRINT_RECENT"] != "6" {
		t.Errorf("file reads %q", values["ARGUS_SPRINT_RECENT"])
	}
}

// Creating the file produces the same annotated template `argus init`
// writes, so one `argus config set` does not leave someone with a file of
// one bare line and no idea what else there is.
func TestSetCreatesTheStarter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.env")
	ch, err := SetInFile(path, setting(t, "ARGUS_GITHUB_ORG"), "your-org")
	if err != nil {
		t.Fatal(err)
	}
	if !ch.Created {
		t.Error("Change should report that the file was created")
	}
	got := read(t, path)
	if !strings.Contains(got, "ARGUS_GITHUB_ORG=your-org") {
		t.Errorf("file is\n%s", got)
	}
	if !strings.Contains(got, "# Argus configuration.") {
		t.Errorf("file lost the template:\n%s", got)
	}
	if strings.Contains(got, "your-org-here") {
		t.Errorf("the placeholder should have been replaced:\n%s", got)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// It can hold a credential, so it is nobody else's business.
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("mode is %v, want owner-only", fi.Mode().Perm())
	}
}

// Commented out rather than deleted: the old value is worth seeing, and
// the next set finds that line and turns it back on where it stands
// instead of appending a second one further down.
func TestUnsetAndSetAgainReuseTheLine(t *testing.T) {
	path := write(t, "# The port.\nARGUS_PORT=9000\n")
	ch, err := UnsetInFile(path, setting(t, "ARGUS_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	if !ch.Had || ch.Previous != "9000" {
		t.Errorf("Change = %+v", ch)
	}
	values, _ := ReadFileValues(path)
	if _, still := values["ARGUS_PORT"]; still {
		t.Error("the setting should no longer be in force")
	}
	if !strings.Contains(read(t, path), "# ARGUS_PORT=9000") {
		t.Errorf("the old value should still be readable:\n%s", read(t, path))
	}

	if _, err := SetInFile(path, setting(t, "ARGUS_PORT"), "9100"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "# The port.\nARGUS_PORT=9100\n" {
		t.Errorf("file is\n%s", got)
	}
}

func TestUnsetOnAFileWithoutIt(t *testing.T) {
	path := write(t, "ARGUS_PORT=9000\n")
	ch, err := UnsetInFile(path, setting(t, "ARGUS_BIND"))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Had {
		t.Error("nothing was there to unset")
	}
}

// Whatever is written has to read back as what was asked for, or the
// command is quietly storing something else.
func TestValuesSurviveTheRoundTrip(t *testing.T) {
	for _, value := range []string{
		"Story Points",
		"Run:^\\[?run\\]?,Build:^\\[?build\\]?",
		"postgres://user:p%40ss@localhost:5432/argus?sslmode=disable",
		"a value with = in it",
	} {
		path := write(t, "")
		s := Setting{Key: "ARGUS_TEST_ROUND_TRIP", Kind: KindString}
		if _, err := SetInFile(path, s, value); err != nil {
			t.Fatal(err)
		}
		values, err := ReadFileValues(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := values[s.Key]; got != value {
			t.Errorf("wrote %q, read back %q", value, got)
		}
	}
}

// A file edited on Windows keeps its line endings, rather than being
// silently converted by whichever machine last ran a `config set`.
func TestCRLFIsPreserved(t *testing.T) {
	path := write(t, "# The port.\r\n# ARGUS_PORT=18474\r\n")
	if _, err := SetInFile(path, setting(t, "ARGUS_PORT"), "9000"); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("a bare newline crept in:\n%q", got)
	}
	values, _ := ReadFileValues(path)
	if values["ARGUS_PORT"] != "9000" {
		t.Errorf("file reads %q", values["ARGUS_PORT"])
	}
}

// ActiveFile has to name the file the server will actually read. Inside a
// checkout that is ./.env, and reporting on the one under the home
// directory instead would be reporting on a file nothing reads.
func TestActiveFileFollowsTheSearchOrder(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "elsewhere.env")
	if err := os.WriteFile(explicit, []byte("ARGUS_PORT=9000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARGUS_CONFIG", explicit)
	if got := ActiveFile(); got != explicit {
		t.Errorf("ActiveFile() = %q, want the explicit path", got)
	}

	t.Setenv("ARGUS_CONFIG", "")
	t.Setenv("ARGUS_CONFIG_DIR", dir)
	if got := ActiveFile(); got != filepath.Join(dir, "config.env") {
		t.Errorf("ActiveFile() = %q, want the file that would be created", got)
	}
}

func TestReadFileValuesOnAMissingFile(t *testing.T) {
	values, err := ReadFileValues(filepath.Join(t.TempDir(), "nothing.env"))
	if err != nil {
		t.Fatalf("a fresh install is not an error: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("got %v", values)
	}
}
