package pgstore

// The backlog tool's tables: acks, one row per acknowledged item,
// requesters, and backlog_labels. See migrations 006 and 007 for why
// each exists.

import (
	"context"
	"fmt"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/store"
)

func (s *Store) PutAcks(ctx context.Context, acks []store.Ack) error {
	if len(acks) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	for _, a := range acks {
		if a.Key == "" || a.Kind == "" {
			return fmt.Errorf("an acknowledgement needs a kind and a key")
		}
		at := a.At
		if at.IsZero() {
			at = now
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO acks (kind, key, watermark, at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (kind, key) DO UPDATE SET
				watermark = EXCLUDED.watermark,
				at = EXCLUDED.at`,
			a.Kind, a.Key, a.Watermark, at); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListAcks(ctx context.Context, kind string) ([]store.Ack, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT kind, key, watermark, at FROM acks
		WHERE kind = $1
		ORDER BY key`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []store.Ack{}
	for rows.Next() {
		var a store.Ack
		if err := rows.Scan(&a.Kind, &a.Key, &a.Watermark, &a.At); err != nil {
			return nil, err
		}
		a.At = a.At.UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAcks(ctx context.Context, kind string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM acks WHERE kind = $1 AND key = ANY($2)`, kind, keys)
	return err
}

func (s *Store) ListRequesters(ctx context.Context) ([]store.Requester, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT account_id, name, updated_at FROM requesters ORDER BY name, account_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []store.Requester{}
	for rows.Next() {
		var m store.Requester
		if err := rows.Scan(&m.AccountID, &m.Name, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.UpdatedAt = m.UpdatedAt.UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

// PutRequesters replaces the list whole, in one transaction, so a failed
// save leaves the previous list rather than half of the new one.
func (s *Store) PutRequesters(ctx context.Context, members []store.Requester) error {
	for _, m := range members {
		if m.AccountID == "" {
			return fmt.Errorf("a requester needs an account id")
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM requesters`); err != nil {
		return err
	}
	for _, m := range members {
		// The same person listed twice is one person; the conflict clause
		// keeps the first spelling of the name.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO requesters (account_id, name, updated_at)
			VALUES ($1, $2, now())
			ON CONFLICT (account_id) DO NOTHING`, m.AccountID, m.Name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListLabels(ctx context.Context) ([]store.Label, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, updated_at FROM backlog_labels ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []store.Label{}
	for rows.Next() {
		var l store.Label
		if err := rows.Scan(&l.Name, &l.UpdatedAt); err != nil {
			return nil, err
		}
		l.UpdatedAt = l.UpdatedAt.UTC()
		out = append(out, l)
	}
	return out, rows.Err()
}

// PutLabels replaces the list whole, in one transaction, as PutRequesters does.
func (s *Store) PutLabels(ctx context.Context, labels []store.Label) error {
	for _, l := range labels {
		if err := store.CheckLabel(l.Name); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM backlog_labels`); err != nil {
		return err
	}
	for _, l := range labels {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO backlog_labels (name, updated_at)
			VALUES ($1, now())
			ON CONFLICT (name) DO NOTHING`, l.Name); err != nil {
			return err
		}
	}
	return tx.Commit()
}
