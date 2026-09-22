package jsonstore_test

// What the version marker is for: refusing, clearly, rather than reading
// a roster through a shape it was not written in.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

func writeMarker(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "schema.json"), []byte(body), 0o640); err != nil {
		t.Fatalf("writing schema.json: %v", err)
	}
}

// Every installation that exists today has no marker. Refusing those
// would be an upgrade that breaks everybody in order to protect against
// nothing, so an absent version means the current one.
func TestAbsentVersionIsCurrent(t *testing.T) {
	dir := t.TempDir()
	s, err := jsonstore.New(dir)
	if err != nil {
		t.Fatalf("an unversioned directory must open: %v", err)
	}
	defer s.Close()

	// Opening must not write a marker: reading somebody's data directory
	// is not a reason to modify it. Only Migrate stamps.
	if _, err := os.Stat(filepath.Join(dir, "schema.json")); !os.IsNotExist(err) {
		t.Errorf("New should not write schema.json; stat gave %v", err)
	}
}

func TestCurrentVersionOpens(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, `{"schema_version": 1}`)
	s, err := jsonstore.New(dir)
	if err != nil {
		t.Fatalf("the current version must open: %v", err)
	}
	_ = s.Close()
}

// A directory written by a newer Argus is the case that matters. Loading
// it and writing it back is unrecoverable; refusing is a five-second fix.
func TestNewerVersionIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, `{"schema_version": 7}`)

	_, err := jsonstore.New(dir)
	if err == nil {
		t.Fatal("a directory from a newer Argus must not open")
	}
	if !errors.Is(err, jsonstore.ErrSchemaVersion) {
		t.Errorf("want ErrSchemaVersion, got %v", err)
	}

	// The person reading this has to decide which Argus to run, so the
	// message has to name the file and both versions.
	msg := err.Error()
	for _, want := range []string{filepath.Join(dir, "schema.json"), "7", "1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal should mention %q, got: %s", want, msg)
		}
	}
}

// A version below the current one has no upgrade path in this build
// either, so it is refused rather than guessed at.
func TestUnrecognisedOlderVersionIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, `{"schema_version": 0}`)

	_, err := jsonstore.New(dir)
	if !errors.Is(err, jsonstore.ErrSchemaVersion) {
		t.Errorf("want ErrSchemaVersion, got %v", err)
	}
}

// Refusing must leave the directory exactly as it was found. The whole
// point is that a wrong version never gets written through.
func TestRefusalDoesNotTouchData(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, `{"schema_version": 7}`)
	people := filepath.Join(dir, "people.json")
	original := `[{"account_id":"acc-a","name":"Person A","baseline":12.5,"active":true}]`
	if err := os.WriteFile(people, []byte(original), 0o640); err != nil {
		t.Fatalf("seeding people.json: %v", err)
	}

	if _, err := jsonstore.New(dir); err == nil {
		t.Fatal("expected a refusal")
	}
	got, err := os.ReadFile(people)
	if err != nil {
		t.Fatalf("reading people.json back: %v", err)
	}
	if string(got) != original {
		t.Errorf("the roster was modified by a refused open:\n got %s\nwant %s", got, original)
	}
}

// Migrate runs on every start, so it has to be safe to run on every
// start.
func TestMigrateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := jsonstore.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx()); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "schema.json"))
	if err != nil {
		t.Fatalf("Migrate should have written schema.json: %v", err)
	}
	if err := s.Migrate(ctx()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, "schema.json"))
	if string(first) != string(second) {
		t.Errorf("Migrate rewrote the marker:\n first %s\nsecond %s", first, second)
	}
}

// A store already open when the version turns out to be wrong must still
// refuse to migrate, rather than stamping the current version over a
// layout it cannot read.
func TestMigrateRefusesAnUnknownVersion(t *testing.T) {
	dir := t.TempDir()
	s, err := jsonstore.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	writeMarker(t, dir, `{"schema_version": 7}`)
	if err := s.Migrate(ctx()); !errors.Is(err, jsonstore.ErrSchemaVersion) {
		t.Fatalf("want ErrSchemaVersion, got %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "schema.json"))
	if !strings.Contains(string(b), "7") {
		t.Errorf("a refused migration must not overwrite the marker, got %s", b)
	}
}
