// Package ghauth signs Argus in as a GitHub App user, using the device
// flow.
//
// An installation token cannot work here. GET /user and GET /user/teams -
// "who am I, which teams am I on" - are documented as user-access-token
// only, and every rule is built on them: without them there is no
// "waiting on your review" and no team queries. So Argus must act as the
// person, which means user-to-server OAuth.
//
// The device flow is the right variant for a tool that runs on loopback
// with no public callback URL. It has one other property that matters for
// an open-source tool: GitHub does not require a client secret for a
// token obtained this way, nor for refreshing it. Argus therefore ships
// only a public client id and holds no secret it would have to hide.
package ghauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	deviceCodeURL  = "https://github.com/login/device/code"
	accessTokenURL = "https://github.com/login/oauth/access_token"
)

// Errors the poll loop has to distinguish, because they mean different
// things to the person staring at a code.
var (
	ErrDenied  = errors.New("authorisation was declined")
	ErrExpired = errors.New("the code expired before it was entered")
)

// DeviceCode is what a person needs to complete a sign-in.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// Token is a user access token and the means to renew it.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`

	// RefreshExpiresAt is when re-authorising becomes unavoidable. GitHub
	// gives six months, and a tool left unused over a long break will hit
	// it, so it is worth saying clearly rather than failing obscurely.
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

// Expired reports whether the access token needs renewing. The margin
// means a sweep that starts just before expiry does not fail midway.
func (t Token) Expired() bool {
	return t.AccessToken == "" || time.Now().After(t.ExpiresAt.Add(-5*time.Minute))
}

// RefreshUsable reports whether renewal is still possible.
func (t Token) RefreshUsable() bool {
	return t.RefreshToken != "" &&
		(t.RefreshExpiresAt.IsZero() || time.Now().Before(t.RefreshExpiresAt))
}

// Client talks to GitHub's OAuth endpoints for one App.
type Client struct {
	ClientID string
	HTTP     *http.Client
}

func New(clientID string) *Client {
	return &Client{ClientID: clientID, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// RequestDeviceCode starts a sign-in.
func (c *Client) RequestDeviceCode(ctx context.Context) (DeviceCode, error) {
	var out DeviceCode
	err := c.form(ctx, deviceCodeURL, url.Values{
		"client_id": {c.ClientID},
	}, &out)
	if err != nil {
		return DeviceCode{}, err
	}
	if out.Interval <= 0 {
		out.Interval = 5
	}
	return out, nil
}

// WaitForToken polls until the person finishes, declines, or the code
// expires.
//
// GitHub's poll interval is not advisory: polling faster earns a
// slow_down and a longer required wait, so the interval is honoured and
// increased whenever asked.
func (c *Client) WaitForToken(ctx context.Context, code DeviceCode) (Token, error) {
	interval := time.Duration(code.Interval) * time.Second
	deadline := time.Now().Add(time.Duration(code.ExpiresIn) * time.Second)

	for {
		select {
		case <-ctx.Done():
			return Token{}, ctx.Err()
		case <-time.After(interval):
		}
		if time.Now().After(deadline) {
			return Token{}, ErrExpired
		}

		var res tokenResponse
		err := c.form(ctx, accessTokenURL, url.Values{
			"client_id":   {c.ClientID},
			"device_code": {code.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}, &res)
		if err != nil {
			return Token{}, err
		}

		switch res.Error {
		case "":
			return res.token(), nil
		case "authorization_pending":
			// Expected: nobody has finished in the browser yet.
		case "slow_down":
			interval += 5 * time.Second
		case "expired_token":
			return Token{}, ErrExpired
		case "access_denied":
			return Token{}, ErrDenied
		default:
			return Token{}, fmt.Errorf("github: %s: %s", res.Error, res.ErrorDescription)
		}
	}
}

// Refresh exchanges a refresh token for a new pair.
//
// GitHub rotates refresh tokens: using one invalidates both it and the
// access token it replaced. A refreshed pair that is not stored is a
// session lost, so callers must persist the result before relying on it.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	var res tokenResponse
	err := c.form(ctx, accessTokenURL, url.Values{
		"client_id":     {c.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}, &res)
	if err != nil {
		return Token{}, err
	}
	if res.Error != "" {
		return Token{}, fmt.Errorf("refreshing: %s: %s", res.Error, res.ErrorDescription)
	}
	return res.token(), nil
}

type tokenResponse struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	Error                 string `json:"error"`
	ErrorDescription      string `json:"error_description"`
}

func (r tokenResponse) token() Token {
	t := Token{AccessToken: r.AccessToken, RefreshToken: r.RefreshToken}
	now := time.Now()
	if r.ExpiresIn > 0 {
		t.ExpiresAt = now.Add(time.Duration(r.ExpiresIn) * time.Second)
	} else {
		// A non-expiring token: the App has expiry switched off.
		t.ExpiresAt = now.AddDate(10, 0, 0)
	}
	if r.RefreshTokenExpiresIn > 0 {
		t.RefreshExpiresAt = now.Add(time.Duration(r.RefreshTokenExpiresIn) * time.Second)
	}
	return t
}

// form posts a form and decodes JSON. GitHub answers these endpoints in
// form encoding unless asked otherwise, so the Accept header matters.
func (c *Client) form(ctx context.Context, endpoint string, values url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("reaching github: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		detail := string(body)
		if len(detail) > 200 {
			detail = detail[:200]
		}
		return fmt.Errorf("github returned %d: %s", resp.StatusCode, detail)
	}
	return json.Unmarshal(body, out)
}
