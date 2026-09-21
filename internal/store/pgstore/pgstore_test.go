package pgstore_test

import (
	"context"
	"os"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/pgstore"
	"github.com/Tzomily-Anvar/argus/internal/store/storetest"
)

// The Postgres backend runs the same conformance suite as the file
// backend. Two implementations of one interface stay honest only if the
// same tests hold both to it.
//
// Skipped without ARGUS_TEST_DATABASE_URL, so `go test ./...` still works
// on a machine with no database. CI sets it.
func TestConformance(t *testing.T) {
	dsn := os.Getenv("ARGUS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ARGUS_TEST_DATABASE_URL to run the Postgres conformance suite")
	}

	storetest.Run(t, func(t *testing.T) store.Store {
		ctx := context.Background()
		s, err := pgstore.New(ctx, dsn)
		if err != nil {
			t.Fatalf("connecting: %v", err)
		}
		if err := s.Migrate(ctx); err != nil {
			t.Fatalf("migrating: %v", err)
		}
		// Each subtest needs an empty database; truncating is faster than
		// recreating the schema, and CASCADE handles the foreign keys.
		if err := truncate(ctx, dsn); err != nil {
			t.Fatalf("truncating: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}

func truncate(ctx context.Context, dsn string) error {
	s, err := pgstore.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer s.Close()
	return s.Truncate(ctx)
}
