package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// EnvAppID names the environment variable holding the numeric ID of
	// the GitHub App sillage authenticates as.
	EnvAppID = "SILLAGE_GITHUB_APP_ID"
	// EnvAppInstallationID names the environment variable holding the
	// numeric ID of the App's installation on the account to read.
	EnvAppInstallationID = "SILLAGE_GITHUB_APP_INSTALLATION_ID"
	// EnvAppPrivateKey names the environment variable holding the App's
	// private key, the PEM itself (PKCS#1 "RSA PRIVATE KEY", which is what
	// GitHub generates, or PKCS#8 "PRIVATE KEY"). Literal \n sequences
	// stand for newlines, for a variable that cannot hold several lines.
	EnvAppPrivateKey = "SILLAGE_GITHUB_APP_PRIVATE_KEY"

	// pemRSA and pemPKCS8 are the PEM block types of the two accepted key
	// encodings.
	pemRSA   = "RSA PRIVATE KEY"
	pemPKCS8 = "PRIVATE KEY"
	// escapedNewline is how a one-line variable spells a newline of a PEM.
	escapedNewline = `\n`

	// jwtBackdate is how far before now a JWT's iat is set, against clock
	// drift between this host and GitHub.
	jwtBackdate = 60 * time.Second
	// jwtLifetime is how long after now a JWT expires; GitHub's maximum is
	// ten minutes.
	jwtLifetime = 9 * time.Minute
	// jwtHeader is the fixed header of the JWTs: RS256.
	jwtHeader = `{"alg":"RS256","typ":"JWT"}`
	// installationTokenRefreshMargin is how long before an installation
	// token expires a new one is minted.
	installationTokenRefreshMargin = 5 * time.Minute
	// maxMintBytes bounds the body read from a token mint's response.
	maxMintBytes = 64 << 10
)

// PrivateKey is a GitHub App's private key, as PEM. It prints as
// [redacted] through fmt and log/slog, so a Config can be logged without
// leaking it.
type PrivateKey string

// String implements fmt.Stringer.
func (PrivateKey) String() string { return redacted }

// GoString implements fmt.GoStringer, for %#v.
func (PrivateKey) GoString() string { return redacted }

// LogValue implements slog.LogValuer.
func (PrivateKey) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText implements encoding.TextMarshaler, so JSON and other text
// encodings print the placeholder too.
func (PrivateKey) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// AppCredentials identify a GitHub App installation: sillage signs a
// short-lived JWT with the App's key and exchanges it for an installation
// token, which has the App's own rate limit and, unlike a fine-grained
// personal access token, can be granted Checks read.
type AppCredentials struct {
	// ID is the App's numeric ID; InstallationID the installation's.
	ID, InstallationID int64
	// PrivateKey is the App's RSA private key, PEM encoded.
	PrivateKey PrivateKey
}

// appFromEnv reads the App settings through getenv. It returns nil when
// none is set, and an error naming each missing or malformed variable
// when some are, never a value.
func appFromEnv(getenv func(string) string) (*AppCredentials, error) {
	id, installation := strings.TrimSpace(getenv(EnvAppID)), strings.TrimSpace(getenv(EnvAppInstallationID))
	key := strings.TrimSpace(getenv(EnvAppPrivateKey))
	if id == "" && installation == "" && key == "" {
		return nil, nil
	}
	var missing []string
	for _, v := range []struct{ name, value string }{
		{EnvAppID, id}, {EnvAppInstallationID, installation}, {EnvAppPrivateKey, key},
	} {
		if v.value == "" {
			missing = append(missing, v.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("github: %s is not set, but the other GitHub App settings are", strings.Join(missing, ", "))
	}
	app := &AppCredentials{PrivateKey: PrivateKey(unescapeNewlines(key))}
	var err error
	if app.ID, err = positiveID(EnvAppID, id); err != nil {
		return nil, err
	}
	if app.InstallationID, err = positiveID(EnvAppInstallationID, installation); err != nil {
		return nil, err
	}
	return app, nil
}

// unescapeNewlines turns literal \n sequences of a one-line PEM into
// newlines; a PEM that already has lines is left alone.
func unescapeNewlines(pemText string) string {
	if strings.Contains(pemText, "\n") {
		return pemText
	}
	return strings.ReplaceAll(pemText, escapedNewline, "\n")
}

// positiveID parses the value of variable name as a positive integer.
func positiveID(name, value string) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("github: %s must be a positive integer", name)
	}
	return n, nil
}

// validate reports whether a is complete: positive IDs and an RSA key.
func (a AppCredentials) validate() error {
	if a.ID <= 0 {
		return fmt.Errorf("github: %s must be a positive integer", EnvAppID)
	}
	if a.InstallationID <= 0 {
		return fmt.Errorf("github: %s must be a positive integer", EnvAppInstallationID)
	}
	_, err := parseRSAKey(a.PrivateKey)
	return err
}

// parseRSAKey decodes a PKCS#1 or PKCS#8 PEM RSA private key. Its errors
// say what is wrong, never repeat the key.
func parseRSAKey(key PrivateKey) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(key))
	if block == nil {
		return nil, fmt.Errorf("github: %s is not PEM", EnvAppPrivateKey)
	}
	switch block.Type {
	case pemRSA:
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("github: %s is not a valid PKCS#1 RSA key", EnvAppPrivateKey)
		}
		return k, nil
	case pemPKCS8:
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("github: %s is not a valid PKCS#8 key", EnvAppPrivateKey)
		}
		if k, ok := parsed.(*rsa.PrivateKey); ok {
			return k, nil
		}
		return nil, fmt.Errorf("github: %s is not an RSA key", EnvAppPrivateKey)
	}
	return nil, fmt.Errorf("github: %s: PEM type %q is not %q or %q", EnvAppPrivateKey, block.Type, pemRSA, pemPKCS8)
}

// installationTokens mints installation tokens for one App installation
// and caches the current one. It is safe for concurrent use; one mint
// runs at a time.
type installationTokens struct {
	apiURL   string
	appID    string
	path     string
	key      *rsa.PrivateKey
	http     *http.Client
	now      func() time.Time
	maxBody  int64
	margin   time.Duration
	mu       sync.Mutex
	token    string
	expireAt time.Time
}

// newInstallationTokens returns the minter of app's installation tokens
// from the API at apiURL (no trailing slash). now is read at each use.
func newInstallationTokens(apiURL string, app AppCredentials, hc *http.Client,
	now func() time.Time) (*installationTokens, error) {
	key, err := parseRSAKey(app.PrivateKey)
	if err != nil {
		return nil, err
	}
	return &installationTokens{
		apiURL:  apiURL,
		appID:   strconv.FormatInt(app.ID, 10),
		path:    "/app/installations/" + strconv.FormatInt(app.InstallationID, 10) + "/access_tokens",
		key:     key,
		http:    hc,
		now:     now,
		maxBody: maxMintBytes,
		margin:  installationTokenRefreshMargin,
	}, nil
}

// authorize sets r's bearer token to a valid installation token, minting
// one when the cached token expires within the refresh margin.
func (m *installationTokens) authorize(ctx context.Context, r *http.Request) error {
	token, err := m.current(ctx)
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	return nil
}

// current returns the cached installation token, minted again first when
// it is missing or about to expire.
func (m *installationTokens) current(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.token != "" && m.now().Add(m.margin).Before(m.expireAt) {
		return m.token, nil
	}
	token, expireAt, err := m.mint(ctx)
	if err != nil {
		return "", err
	}
	m.token, m.expireAt = token, expireAt
	return token, nil
}

// mintResponse is the part of the access-token answer used.
type mintResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// mint exchanges a fresh JWT for an installation token. It is not counted
// as an API read, and its response carries no rate-limit budget of the
// client's. Errors name the path and status, never a credential.
func (m *installationTokens) mint(ctx context.Context) (string, time.Time, error) {
	jwt, err := m.signJWT()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("POST %s: signing: %w", m.path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.apiURL+m.path, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("POST %s: %w", m.path, err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", mediaType)
	req.Header.Set(headerAPIVersion, apiVersion)
	req.Header.Set("User-Agent", userAgent)
	resp, err := m.http.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("POST %s: %w", m.path, unwrapURLError(err))
	}
	defer resp.Body.Close()
	defer func() { _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes)) }()
	if resp.StatusCode != http.StatusCreated {
		return "", time.Time{}, fmt.Errorf("POST %s: %s", m.path, resp.Status)
	}
	var out mintResponse
	body := http.MaxBytesReader(nil, resp.Body, m.maxBody)
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return "", time.Time{}, fmt.Errorf("POST %s: response larger than %d bytes", m.path, m.maxBody)
		}
		return "", time.Time{}, fmt.Errorf("POST %s: decoding the response", m.path)
	}
	if out.Token == "" || out.ExpiresAt.IsZero() {
		return "", time.Time{}, fmt.Errorf("POST %s: the response has no token or expiry", m.path)
	}
	return out.Token, out.ExpiresAt, nil
}

// jwtClaims are the claims GitHub requires of an App's JWT.
type jwtClaims struct {
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	Issuer    string `json:"iss"`
}

// signJWT returns an RS256 JWT identifying the App, valid for jwtLifetime.
func (m *installationTokens) signJWT() (string, error) {
	now := m.now()
	claims, err := json.Marshal(jwtClaims{
		IssuedAt:  now.Add(-jwtBackdate).Unix(),
		ExpiresAt: now.Add(jwtLifetime).Unix(),
		Issuer:    m.appID,
	})
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString([]byte(jwtHeader)) + "." + enc.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, m.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signing + "." + enc.EncodeToString(sig), nil
}
