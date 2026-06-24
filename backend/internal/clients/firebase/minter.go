package firebase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"firebase.google.com/go/v4/auth"
)

// The minter turns an email into a usable Firebase ID token — the "act as a seeded
// user without the OAuth dance" primitive behind the operator MintToken RPC
// (decision 0008). It ensures the Firebase user exists, mints a custom token, and
// exchanges it (via the Identity Toolkit signInWithCustomToken endpoint, which the
// Auth emulator also serves) for an ID token carrying the email claim — the same
// token the normal verify path accepts and provisions on.

const (
	// exchangeTimeout bounds the custom-token -> ID-token round trip.
	exchangeTimeout = 10 * time.Second
	// emulatorFallbackAPIKey is used when no web API key is configured but the
	// emulator is in play: the emulator accepts any non-empty key. Real Firebase
	// requires the project's real key (FIREBASE_WEB_API_KEY).
	emulatorFallbackAPIKey = "fake-api-key"
)

// Minter mints ID tokens for seeded users. Construct with NewMinter.
type Minter struct {
	auth       *auth.Client
	httpClient *http.Client
	endpoint   string
}

// NewMinter builds a Minter over a Firebase Auth client. emulatorHost routes the
// token exchange at the local emulator when set; webAPIKey is the Identity Toolkit
// key (a fallback is used against the emulator if empty).
func NewMinter(client *auth.Client, emulatorHost, webAPIKey string) *Minter {
	return &Minter{
		auth:       client,
		httpClient: &http.Client{Timeout: exchangeTimeout},
		endpoint:   exchangeEndpoint(emulatorHost, webAPIKey),
	}
}

// MintToken ensures a Firebase user exists for email, then mints and exchanges a
// token to act as them, returning the ID token.
func (m *Minter) MintToken(ctx context.Context, email string) (string, error) {
	uid, err := m.ensureUser(ctx, email)
	if err != nil {
		return "", fmt.Errorf("ensure firebase user: %w", err)
	}
	customToken, err := m.auth.CustomToken(ctx, uid)
	if err != nil {
		return "", fmt.Errorf("mint custom token: %w", err)
	}
	idToken, err := m.exchange(ctx, customToken)
	if err != nil {
		return "", fmt.Errorf("exchange custom token: %w", err)
	}
	return idToken, nil
}

// ensureUser returns the Firebase uid for email, creating the user (with the email
// set, so the ID token carries an email claim) if it does not exist yet.
func (m *Minter) ensureUser(ctx context.Context, email string) (string, error) {
	rec, err := m.auth.GetUserByEmail(ctx, email)
	if err == nil {
		return rec.UID, nil
	}
	if !auth.IsUserNotFound(err) {
		return "", err
	}
	created, err := m.auth.CreateUser(ctx, (&auth.UserToCreate{}).Email(email).EmailVerified(true))
	if err != nil {
		return "", err
	}
	return created.UID, nil
}

// exchange POSTs a custom token to the Identity Toolkit signInWithCustomToken
// endpoint and returns the resulting ID token.
func (m *Minter) exchange(ctx context.Context, customToken string) (string, error) {
	body, err := json.Marshal(map[string]any{"token": customToken, "returnSecureToken": true})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var decoded struct {
		IDToken string `json:"idToken"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK || decoded.IDToken == "" {
		return "", fmt.Errorf("identity toolkit returned %d: %s", resp.StatusCode, decoded.Error.Message)
	}
	return decoded.IDToken, nil
}

// exchangeEndpoint builds the signInWithCustomToken URL for the emulator or real
// Identity Toolkit.
func exchangeEndpoint(emulatorHost, apiKey string) string {
	if apiKey == "" {
		apiKey = emulatorFallbackAPIKey
	}
	const path = "/identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken"
	query := "?key=" + url.QueryEscape(apiKey)
	if strings.TrimSpace(emulatorHost) != "" {
		return "http://" + emulatorHost + path + query
	}
	return "https://identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken" + query
}
