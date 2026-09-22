package jsonstore

// Versioning the data directory.
//
// Until now these files carried no version at all, which was defensible
// only while every change was additive: a field added to a struct reads
// as its zero value in records written before it existed. The moment a
// change is not additive - a rename, a retype, a file split - an
// unversioned directory has no way to say so, and Argus would read an old
// roster as if it were a new one and write the result back.
//
// The version lives in its own file rather than as a field inside each
// one. people.json is a bare JSON array and capacity-reviews.json a bare
// object; giving either a version field means wrapping it in an envelope,
// which is itself the breaking change this is meant to prevent. A
// separate marker costs nothing and breaks nothing.
//
// It is one marker for the whole directory, not one per file, because the
// directory is what a release upgrades. Every file in it is written by
// the same binary, and a per-sprint capacity file appears at an arbitrary
// later date; a version per file would only record when each was last
// touched, which is not the question being asked.
//
// An absent marker means the current version. Every install that exists
// today has no marker, and refusing to start on them would be an upgrade
// that breaks everybody to protect against nothing.

import (
	"errors"
	"fmt"
	"os"
)

// SchemaVersion is the layout this build writes and understands.
//
// Bump it only for a change old files cannot be read through. Adding a
// field is not one: the whole point of the registry test in
// internal/store is that additive changes stay additive.
const SchemaVersion = 1

// schemaFile is the marker's name, without the .json suffix that path()
// appends.
const schemaFile = "schema"

// ErrSchemaVersion is returned when the directory was written by a build
// that disagrees about the layout.
//
// Refusing to start is the right answer, and a deliberate one. The
// alternative is to read a roster, a set of baselines and years of
// absence records through the wrong shape and then write them back - and
// unlike a failed start, that cannot be undone by installing a different
// version of Argus.
var ErrSchemaVersion = errors.New("unrecognised data directory schema version")

// schemaMarker is the entire content of schema.json.
type schemaMarker struct {
	SchemaVersion int `json:"schema_version"`
}

// readSchemaVersion reports the version recorded in the directory and
// whether a marker was there at all.
func (s *Store) readSchemaVersion() (version int, present bool, err error) {
	if _, err := os.Stat(s.path(schemaFile)); os.IsNotExist(err) {
		return SchemaVersion, false, nil
	} else if err != nil {
		return 0, false, fmt.Errorf("reading %s: %w", s.path(schemaFile), err)
	}
	var m schemaMarker
	if err := s.readInto(schemaFile, &m); err != nil {
		return 0, true, err
	}
	return m.SchemaVersion, true, nil
}

// checkSchema refuses to open a directory this build cannot read. The
// message names the file and both versions, because the person seeing it
// has to decide which Argus to run, and neither number is guessable from
// the outside.
func (s *Store) checkSchema() error {
	version, present, err := s.readSchemaVersion()
	if err != nil {
		return err
	}
	if !present || version == SchemaVersion {
		return nil
	}
	if version > SchemaVersion {
		return fmt.Errorf("%s records schema version %d, but this build of Argus understands version %d: "+
			"the data directory was written by a newer Argus, so run that one rather than "+
			"letting this one read the roster through the wrong shape: %w",
			s.path(schemaFile), version, SchemaVersion, ErrSchemaVersion)
	}
	return fmt.Errorf("%s records schema version %d, which this build of Argus (version %d) does not recognise: "+
		"refusing to read the data directory rather than risk rewriting it wrongly: %w",
		s.path(schemaFile), version, SchemaVersion, ErrSchemaVersion)
}

// stampSchema writes the marker if it is not already there.
//
// Only Migrate calls this, so opening a store to read it never writes to
// the directory. A store that is only ever read stays unmarked, which is
// harmless: absent means current.
func (s *Store) stampSchema() error {
	version, present, err := s.readSchemaVersion()
	if err != nil {
		return err
	}
	if present && version == SchemaVersion {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(schemaFile, schemaMarker{SchemaVersion: SchemaVersion})
}
