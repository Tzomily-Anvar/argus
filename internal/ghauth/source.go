package ghauth

import (
	"context"
	"fmt"
	"sync"
)

// Source hands out a valid access token, renewing it when needed.
//
// A GitHub App's user token lasts eight hours, so a long-running sweep
// cannot hold one for the life of the process the way a personal access
// token is held. Every request asks for the current one instead.
type Source struct {
	client *Client
	store  *Store

	mu    sync.Mutex
	token Token
}

func NewSource(clientID, dir string, useKeyring bool) *Source {
	return &Source{client: New(clientID), store: NewStore(dir, useKeyring)}
}

// SignedIn reports whether a usable session exists.
func (s *Source) SignedIn() bool {
	t, err := s.store.Load()
	return err == nil && (t.AccessToken != "" && !t.Expired() || t.RefreshUsable())
}

// Token returns a valid access token, refreshing if the current one has
// expired.
//
// The refreshed pair is stored before it is returned. GitHub invalidates
// the old refresh token the moment a new one is issued, so returning a
// token that was not persisted would leave the session unrecoverable
// after a restart.
func (s *Source) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token.AccessToken == "" {
		t, err := s.store.Load()
		if err != nil {
			return "", err
		}
		s.token = t
	}

	if !s.token.Expired() {
		return s.token.AccessToken, nil
	}

	if !s.token.RefreshUsable() {
		// The bare command, not `./run.sh login`: that script exists only
		// in a checkout, and this is reached just as often from a binary
		// installed from a package manager or downloaded on its own.
		return "", fmt.Errorf(
			"not signed in to the GitHub App, or the session has lapsed. Run `argus login`")
	}

	refreshed, err := s.client.Refresh(ctx, s.token.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("%w. Run `argus login` to sign in again", err)
	}
	if err := s.store.Save(refreshed); err != nil {
		return "", fmt.Errorf("storing the refreshed session: %w", err)
	}
	s.token = refreshed
	return s.token.AccessToken, nil
}

// Login runs the device flow and stores the result. announce is called
// with the code and URL so the caller decides how to show them.
func (s *Source) Login(ctx context.Context, announce func(DeviceCode)) error {
	code, err := s.client.RequestDeviceCode(ctx)
	if err != nil {
		return err
	}
	announce(code)

	token, err := s.client.WaitForToken(ctx, code)
	if err != nil {
		return err
	}
	if err := s.store.Save(token); err != nil {
		return err
	}
	s.mu.Lock()
	s.token = token
	s.mu.Unlock()
	return nil
}

// Logout forgets the session.
func (s *Source) Logout() error {
	s.mu.Lock()
	s.token = Token{}
	s.mu.Unlock()
	return s.store.Clear()
}

// SessionPath is where the session is kept - the keyring, or a file on
// a machine without one.
func (s *Source) SessionPath() string { return s.store.Location() }
