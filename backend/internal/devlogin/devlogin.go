// Package devlogin implements the staging/local-only "act as a seeded user without
// the Google OAuth dance" path (PLAN Phase 8). Given an email, it ensures a Firebase
// user exists, mints a custom token, and exchanges it (via the Identity Toolkit REST
// endpoint, which the Auth emulator also serves) for a real ID token the caller pastes
// straight into Authorization: Bearer — the same token the normal verify path accepts.
//
// It is mounted ONLY when dev auth is enabled (never in production — decision 0007);
// the server refuses to boot if asked to enable it in production. It is the single
// most dangerous surface if it ever ships to prod, so its availability is gated at
// wiring time, not by a runtime check here.
package devlogin

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

	"github.com/tallam99/qlab/backend/internal/httpmw"
)

// exchangeTimeout bounds the custom-token -> ID-token round trip to the Identity
// Toolkit (or the local emulator).
const exchangeTimeout = 10 * time.Second

// emulatorFallbackAPIKey is used when no web API key is configured but the emulator
// is in play: the emulator accepts any non-empty key. Real Firebase requires a real
// key (configured via FIREBASE_WEB_API_KEY).
const emulatorFallbackAPIKey = "fake-api-key"

// Options configures the handler.
type Options struct {
	// Auth is the Firebase Auth client (pointed at the emulator locally) used to
	// ensure the user exists and mint the custom token.
	Auth *auth.Client
	// EmulatorHost, when set, routes the token exchange at the local Auth emulator
	// instead of Google's servers. Mirrors FIREBASE_AUTH_EMULATOR_HOST.
	EmulatorHost string
	// WebAPIKey is the Identity Toolkit key for the exchange. Against the emulator any
	// non-empty value works (a fallback is used if empty); real Firebase needs the
	// project's real key.
	WebAPIKey string
}

// handler serves the dev-login endpoint.
type handler struct {
	auth       *auth.Client
	httpClient *http.Client
	exchangeFn func(ctx context.Context, customToken string) (string, error)
}

// request is the dev-login request body.
type request struct {
	// Email identifies the seeded/invited user to act as.
	Email string `json:"email"`
	// LabID is the lab to act in; echoed back so the caller has both header values in
	// one response. Optional (dev-login does not validate it).
	LabID string `json:"labId"`
}

// response is the dev-login response: the ID token (Authorization: Bearer) and the
// lab (X-QLab-Lab) the caller then sends on real API calls.
type response struct {
	IDToken string `json:"idToken"`
	Email   string `json:"email"`
	LabID   string `json:"labId"`
}

// Handler builds the dev-login HTTP handler. It panics if the Firebase client is
// missing — a wiring bug should fail loudly.
func Handler(opts Options) http.Handler {
	if opts.Auth == nil {
		panic("devlogin: Handler requires a Firebase Auth client")
	}
	h := &handler{
		auth:       opts.Auth,
		httpClient: &http.Client{Timeout: exchangeTimeout},
	}
	h.exchangeFn = h.exchangeCustomToken(opts.EmulatorHost, opts.WebAPIKey)
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger := httpmw.LoggerFromContext(r.Context())
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}

	idToken, err := h.mint(r.Context(), email)
	if err != nil {
		logger.Error("dev-login failed", "error", err, "email", email)
		http.Error(w, "dev-login failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response{IDToken: idToken, Email: email, LabID: req.LabID})
}

// mint ensures the Firebase user exists, mints a custom token, and exchanges it for
// an ID token carrying the email claim (which the normal verify path provisions on).
func (h *handler) mint(ctx context.Context, email string) (string, error) {
	uid, err := h.ensureUser(ctx, email)
	if err != nil {
		return "", fmt.Errorf("ensure user: %w", err)
	}
	customToken, err := h.auth.CustomToken(ctx, uid)
	if err != nil {
		return "", fmt.Errorf("mint custom token: %w", err)
	}
	idToken, err := h.exchangeFn(ctx, customToken)
	if err != nil {
		return "", fmt.Errorf("exchange custom token: %w", err)
	}
	return idToken, nil
}

// ensureUser returns the Firebase uid for email, creating the user (with the email
// set, so the ID token carries an email claim) if it does not exist yet.
func (h *handler) ensureUser(ctx context.Context, email string) (string, error) {
	rec, err := h.auth.GetUserByEmail(ctx, email)
	if err == nil {
		return rec.UID, nil
	}
	if !auth.IsUserNotFound(err) {
		return "", err
	}
	created, err := h.auth.CreateUser(ctx, (&auth.UserToCreate{}).Email(email).EmailVerified(true))
	if err != nil {
		return "", err
	}
	return created.UID, nil
}

// exchangeCustomToken returns a function that POSTs a custom token to the Identity
// Toolkit signInWithCustomToken endpoint and returns the resulting ID token. The
// endpoint is the emulator's when emulatorHost is set, else Google's.
func (h *handler) exchangeCustomToken(emulatorHost, apiKey string) func(context.Context, string) (string, error) {
	endpoint := exchangeEndpoint(emulatorHost, apiKey)
	return func(ctx context.Context, customToken string) (string, error) {
		body, err := json.Marshal(map[string]any{"token": customToken, "returnSecureToken": true})
		if err != nil {
			return "", err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := h.httpClient.Do(httpReq)
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
}

// exchangeEndpoint builds the signInWithCustomToken URL for the emulator or real
// Identity Toolkit.
func exchangeEndpoint(emulatorHost, apiKey string) string {
	if apiKey == "" {
		apiKey = emulatorFallbackAPIKey
	}
	const path = "/identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken"
	query := "?key=" + url.QueryEscape(apiKey)
	if emulatorHost != "" {
		return "http://" + emulatorHost + path + query
	}
	return "https://identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken" + query
}
