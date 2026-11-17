package github

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testAppID          = 12345
	testInstallationID = 678
	testRSABits        = 2048
	testInstallToken   = "ghs_fabricated-installation-token"
	testMintPath       = "/app/installations/678/access_tokens"
)

// testKey is generated once: RSA key generation is slow.
var testKey = sync.OnceValue(func() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, testRSABits)
	if err != nil {
		panic(err)
	}
	return k
})

func pkcs1PEM(k *rsa.PrivateKey) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: pemRSA, Bytes: x509.MarshalPKCS1PrivateKey(k)}))
}

func pkcs8PEM(t *testing.T, k *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: pemPKCS8, Bytes: der}))
}

// appServer answers the token mint with tokens expiring ttl after the
// fake clock, and every GET with an empty PR; it records the mint
// requests' JWTs and the GETs' authorization headers.
type appServer struct {
	mu    sync.Mutex
	jwts  []string
	auths []string
	mints int
	ttl   time.Duration
	// mintStatus, when not 0, answers the mint with it.
	mintStatus int
}

func (s *appServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method == http.MethodPost && r.URL.Path == testMintPath {
		s.mints++
		s.jwts = append(s.jwts, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if s.mintStatus != 0 {
			w.WriteHeader(s.mintStatus)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(mintResponse{
			Token:     fmt.Sprintf("%s-%d", testInstallToken, s.mints),
			ExpiresAt: testObservedAt.Add(s.ttl),
		})
		return
	}
	s.auths = append(s.auths, r.Header.Get("Authorization"))
	okPR(w)
}

func newAppClient(t *testing.T, s *appServer, key string) *Client {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	cfg := Config{APIURL: srv.URL, Instance: testInstance,
		App: &AppCredentials{ID: testAppID, InstallationID: testInstallationID, PrivateKey: PrivateKey(key)}}
	c, err := NewClient(cfg, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return testObservedAt }
	return c
}

func TestMintSignsJWTAndAuthorizesGETs(t *testing.T) {
	s := &appServer{ttl: time.Hour}
	c := newAppClient(t, s, pkcs1PEM(testKey()))
	var pr PullRequest
	if err := c.getOne(t.Context(), "/x", &pr); err != nil {
		t.Fatal(err)
	}
	if s.mints != 1 {
		t.Fatalf("mints = %d, want 1", s.mints)
	}
	parts := strings.Split(s.jwts[0], ".")
	if len(parts) != 3 {
		t.Fatalf("jwt = %q, want three parts", s.jwts[0])
	}
	enc := base64.RawURLEncoding
	header, _ := enc.DecodeString(parts[0])
	if string(header) != jwtHeader {
		t.Errorf("header = %s", header)
	}
	raw, _ := enc.DecodeString(parts[1])
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["iss"] != "12345" ||
		int64(claims["iat"].(float64)) != testObservedAt.Add(-jwtBackdate).Unix() ||
		int64(claims["exp"].(float64)) != testObservedAt.Add(jwtLifetime).Unix() {
		t.Errorf("claims = %v", claims)
	}
	sig, _ := enc.DecodeString(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&testKey().PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Errorf("signature: %v", err)
	}
	if len(s.auths) == 0 || s.auths[0] != "Bearer "+testInstallToken+"-1" {
		t.Errorf("GET authorization = %v, want the installation token", s.auths)
	}
	if got := c.requests.Load(); got != int64(len(s.auths)) {
		t.Errorf("requests = %d, want the %d GETs only", got, len(s.auths))
	}
}

func TestInstallationTokenReusedUntilMargin(t *testing.T) {
	s := &appServer{ttl: time.Hour}
	c := newAppClient(t, s, pkcs8PEM(t, testKey()))
	now := testObservedAt
	c.now = func() time.Time { return now }
	get := func() {
		t.Helper()
		var pr PullRequest
		if err := c.getOne(t.Context(), "/x", &pr); err != nil {
			t.Fatal(err)
		}
	}
	get()
	now = testObservedAt.Add(time.Hour - installationTokenRefreshMargin - time.Second)
	get()
	if s.mints != 1 {
		t.Fatalf("mints = %d before the margin, want 1", s.mints)
	}
	now = testObservedAt.Add(time.Hour - installationTokenRefreshMargin)
	get()
	if s.mints != 2 || s.auths[2] != "Bearer "+testInstallToken+"-2" {
		t.Errorf("mints = %d, authorizations %v; want a new token at the margin", s.mints, s.auths)
	}
}

func TestMintFailureIsAGETErrorWithoutSecrets(t *testing.T) {
	s := &appServer{mintStatus: http.StatusUnauthorized}
	c := newAppClient(t, s, pkcs1PEM(testKey()))
	var pr PullRequest
	err := c.getOne(t.Context(), "/x", &pr)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"github: GET /x", "POST " + testMintPath, "401"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), s.jwts[0]) || strings.Contains(err.Error(), "PRIVATE KEY") {
		t.Errorf("error %q leaks a credential", err)
	}
	if len(s.auths) != 0 {
		t.Errorf("GETs sent after a failed mint: %v", s.auths)
	}
}

func TestConfigCredentials(t *testing.T) {
	k := testKey()
	app := map[string]string{
		EnvAppID: "12345", EnvAppInstallationID: "678", EnvAppPrivateKey: pkcs1PEM(k),
	}
	with := func(base map[string]string, kv ...string) map[string]string {
		out := map[string]string{}
		for key, v := range base {
			out[key] = v
		}
		for i := 0; i < len(kv); i += 2 {
			if kv[i+1] == "" {
				delete(out, kv[i])
			} else {
				out[kv[i]] = kv[i+1]
			}
		}
		return out
	}
	tests := []struct {
		name    string
		env     map[string]string
		wantApp bool
		wantErr string
	}{
		{"token only", map[string]string{EnvToken: testToken}, false, ""},
		{"app only, PKCS#1", app, true, ""},
		{"app only, PKCS#8", with(app, EnvAppPrivateKey, pkcs8PEM(t, k)), true, ""},
		{"app with escaped newlines", with(app, EnvAppPrivateKey,
			strings.ReplaceAll(strings.TrimSpace(pkcs1PEM(k)), "\n", `\n`)), true, ""},
		{"both", with(app, EnvToken, testToken), false, "not both"},
		{"neither", map[string]string{}, false, EnvToken + " is not set"},
		{"no installation", with(app, EnvAppInstallationID, ""), false, EnvAppInstallationID + " is not set"},
		{"no id nor key", map[string]string{EnvAppInstallationID: "678"}, false, EnvAppID + ", " + EnvAppPrivateKey},
		{"bad id", with(app, EnvAppID, "abc"), false, EnvAppID + " must be a positive integer"},
		{"zero installation", with(app, EnvAppInstallationID, "0"), false, EnvAppInstallationID + " must be"},
		{"not PEM", with(app, EnvAppPrivateKey, "secret-not-a-pem"), false, "is not PEM"},
		{"bad PEM body", with(app, EnvAppPrivateKey,
			"-----BEGIN RSA PRIVATE KEY-----\nAAAA\n-----END RSA PRIVATE KEY-----"), false, "not a valid PKCS#1"},
		{"other PEM type", with(app, EnvAppPrivateKey,
			"-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----"), false, "PEM type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ConfigFromEnv(envOf(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				for _, secret := range []string{testToken, "secret-not-a-pem", "AAAA"} {
					if strings.Contains(err.Error(), secret) {
						t.Errorf("error %q leaks a credential", err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (cfg.App != nil) != tt.wantApp {
				t.Errorf("App set = %v, want %v", cfg.App != nil, tt.wantApp)
			}
		})
	}
}

func TestConfigRedactsPrivateKey(t *testing.T) {
	cfg := Config{App: &AppCredentials{ID: 1, InstallationID: 2, PrivateKey: PrivateKey(pkcs1PEM(testKey()))}}
	for _, got := range []string{fmt.Sprintf("%v", cfg.App), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg.App)} {
		if strings.Contains(got, "PRIVATE KEY") || strings.Contains(got, "MII") {
			t.Errorf("printing leaks the key: %.80s", got)
		}
	}
	text, _ := json.Marshal(cfg.App)
	if strings.Contains(string(text), "MII") {
		t.Errorf("JSON leaks the key: %.80s", text)
	}
}
