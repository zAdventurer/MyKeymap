package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHTTPStatusReturnsStructuredStatus(t *testing.T) {
	repository, config := newRepositoryTestFixture(t, `{"name":"local"}`)
	handler := NewHTTPHandler(HTTPConfig{Config: config, SettingsPath: filepath.Join(t.TempDir(), "settings.json")})
	router := newHTTPRouter(handler)

	response := performHTTPRequest(t, router, http.MethodGet, "/sync/status", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /sync/status status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Status   Status       `json:"status"`
		Settings SyncSettings `json:"settings"`
	}
	decodeHTTPResponse(t, response, &body)
	if body.Status != StatusLocalOnly {
		t.Fatalf("status = %q, want %q", body.Status, StatusLocalOnly)
	}
	if body.Settings.RepositoryURL != repository.Config.RepositoryURL || body.Settings.Branch != repository.Config.Branch {
		t.Fatalf("settings = %#v, want repository %q and branch %q", body.Settings, repository.Config.RepositoryURL, repository.Config.Branch)
	}
}

func TestHTTPSettingsPersistsOnlyNonSecretFields(t *testing.T) {
	_, config := newRepositoryTestFixture(t, `{"name":"local"}`)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	handler := NewHTTPHandler(HTTPConfig{Config: config, SettingsPath: settingsPath})
	router := newHTTPRouter(handler)

	response := performHTTPRequest(t, router, http.MethodPut, "/sync/settings", SyncSettings{
		RepositoryURL: config.RepositoryURL,
		Branch:        config.Branch,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("PUT /sync/settings status = %d, body = %s", response.Code, response.Body.String())
	}
	contents, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(contents, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || fields["repositoryUrl"] == nil || fields["branch"] == nil {
		t.Fatalf("persisted settings contain fields other than repositoryUrl and branch: %s", contents)
	}
	var settings SyncSettings
	if err := json.Unmarshal(contents, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.RepositoryURL != config.RepositoryURL || settings.Branch != config.Branch {
		t.Fatalf("persisted settings = %#v", settings)
	}
}

func TestHTTPSettingsRejectsCredentialedRepositoryURLWithoutEchoingSecret(t *testing.T) {
	_, config := newRepositoryTestFixture(t, `{"name":"local"}`)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	handler := NewHTTPHandler(HTTPConfig{Config: config, SettingsPath: settingsPath})
	router := newHTTPRouter(handler)

	response := performHTTPRequest(t, router, http.MethodPut, "/sync/settings", SyncSettings{
		RepositoryURL: "https://account:very-secret-token@example.invalid/config.git",
		Branch:        "main",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("PUT /sync/settings status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "very-secret-token") {
		t.Fatalf("credential leaked in response: %s", response.Body.String())
	}
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Fatalf("settings were persisted after invalid request: %v", err)
	}
}

func TestHTTPSettingsRejectsRepositoryURLWithCredentialQuery(t *testing.T) {
	_, config := newRepositoryTestFixture(t, `{"name":"local"}`)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	handler := NewHTTPHandler(HTTPConfig{Config: config, SettingsPath: settingsPath})
	router := newHTTPRouter(handler)

	response := performHTTPRequest(t, router, http.MethodPut, "/sync/settings", SyncSettings{
		RepositoryURL: "https://github.com/zAdventurer/MyKeymap.git?access_token=very-secret-token",
		Branch:        "main",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("PUT /sync/settings status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "very-secret-token") {
		t.Fatalf("credential leaked in response: %s", response.Body.String())
	}
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Fatalf("settings were persisted after invalid request: %v", err)
	}
}

func TestHTTPSettingsRejectsSCPStyleRepositoryURLWithCredentialQuery(t *testing.T) {
	_, config := newRepositoryTestFixture(t, `{"name":"local"}`)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	handler := NewHTTPHandler(HTTPConfig{Config: config, SettingsPath: settingsPath})
	router := newHTTPRouter(handler)

	response := performHTTPRequest(t, router, http.MethodPut, "/sync/settings", SyncSettings{
		RepositoryURL: "git@github.com:zAdventurer/MyKeymap.git?access_token=very-secret-token",
		Branch:        "main",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("PUT /sync/settings status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "very-secret-token") {
		t.Fatalf("credential leaked in response: %s", response.Body.String())
	}
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Fatalf("settings were persisted after invalid request: %v", err)
	}
}

func TestHTTPSettingsAllowsSSHRepositoryUserWithoutPersistingCredentials(t *testing.T) {
	_, config := newRepositoryTestFixture(t, `{"name":"local"}`)
	handler := NewHTTPHandler(HTTPConfig{Config: config, SettingsPath: filepath.Join(t.TempDir(), "settings.json")})
	router := newHTTPRouter(handler)

	response := performHTTPRequest(t, router, http.MethodPut, "/sync/settings", SyncSettings{
		RepositoryURL: "ssh://git@github.com/zAdventurer/MyKeymap.git",
		Branch:        "main",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("PUT /sync/settings status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHTTPPullAppliesRemoteChangeAndRestartsOnce(t *testing.T) {
	repository, config := newRepositoryTestFixture(t, `{"name":"initial"}`)
	if err := repository.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeRemoteRepositoryConfig(t, config.RepositoryURL, config.Branch, `{"name":"remote"}`)
	restarts := 0
	handler := NewHTTPHandler(HTTPConfig{
		Config:       config,
		SettingsPath: filepath.Join(t.TempDir(), "settings.json"),
		Restart: func() error {
			restarts++
			return nil
		},
	})
	router := newHTTPRouter(handler)

	response := performHTTPRequest(t, router, http.MethodPost, "/sync/pull", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("POST /sync/pull status = %d, body = %s", response.Code, response.Body.String())
	}
	assertFileContents(t, config.ConfigPath, `{"name":"remote"}`)
	if restarts != 1 {
		t.Fatalf("restart count = %d, want 1", restarts)
	}
}

func TestHTTPPushReturnsStructuredConflictWithoutOverwritingEitherCopy(t *testing.T) {
	repository, config := newRepositoryTestFixture(t, `{"name":"initial"}`)
	if err := repository.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ConfigPath, []byte(`{"name":"local"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRemoteRepositoryConfig(t, config.RepositoryURL, config.Branch, `{"name":"remote"}`)
	handler := NewHTTPHandler(HTTPConfig{Config: config, SettingsPath: filepath.Join(t.TempDir(), "settings.json")})
	router := newHTTPRouter(handler)

	response := performHTTPRequest(t, router, http.MethodPost, "/sync/push", nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("POST /sync/push status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeHTTPResponse(t, response, &body)
	if body.Error.Code != "conflict" {
		t.Fatalf("error code = %q, want conflict", body.Error.Code)
	}
	assertFileContents(t, config.ConfigPath, `{"name":"local"}`)
	if got := readBareRepositoryConfig(t, config.RepositoryURL, config.Branch); got != `{"name":"remote"}` {
		t.Fatalf("remote config = %q, want remote version", got)
	}
}

func TestHTTPDiffRedactsSensitiveConfigurationValues(t *testing.T) {
	repository, config := newRepositoryTestFixture(t, `{"token":"local-secret"}`)
	if err := repository.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ConfigPath, []byte(`{"token":"local-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRemoteRepositoryConfig(t, config.RepositoryURL, config.Branch, `{"token":"remote-secret"}`)
	handler := NewHTTPHandler(HTTPConfig{Config: config, SettingsPath: filepath.Join(t.TempDir(), "settings.json")})
	router := newHTTPRouter(handler)

	response := performHTTPRequest(t, router, http.MethodGet, "/sync/diff", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /sync/diff status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "local-secret") || strings.Contains(response.Body.String(), "remote-secret") {
		t.Fatalf("diff exposed a sensitive value: %s", response.Body.String())
	}
}

func TestRedactSensitiveDiffRecursivelyRedactsParsedJSON(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/config.json b/config.json",
		"--- a/config.json",
		"+++ b/config.json",
		"@@ -1 +1 @@",
		`-{"enabled":true,"nested":{"api-key":"old-api-key","Private_Key":"old-private-key"},"items":[{"passphrase":"old-passphrase","cookie":"old-cookie"}],"token":"old-token","secret":"old-secret","password":"old-password","api_key":"old-api-key-underscore","apiKey":"old-api-key-camel","privateKey":"old-private-key-camel","access_key":"old-access-key-underscore","accessKey":"old-access-key-camel","authorization":"old-authorization","credential":"old-credential"}`,
		`+{"enabled":false,"nested":{"api-key":"new-api-key","Private_Key":"new-private-key"},"items":[{"passphrase":"new-passphrase","cookie":"new-cookie"}],"token":"new-token","secret":"new-secret","password":"new-password","api_key":"new-api-key-underscore","apiKey":"new-api-key-camel","privateKey":"new-private-key-camel","access_key":"new-access-key-underscore","accessKey":"new-access-key-camel","authorization":"new-authorization","credential":"new-credential"}`,
	}, "\n")

	redacted := redactSensitiveDiff(diff)
	for _, secret := range []string{
		"old-api-key", "new-api-key", "old-private-key", "new-private-key", "old-passphrase", "new-passphrase",
		"old-cookie", "new-cookie", "old-token", "new-token", "old-secret", "new-secret", "old-password", "new-password",
		"old-access-key", "new-access-key", "old-authorization", "new-authorization", "old-credential", "new-credential",
	} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted diff exposed %q: %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, `"enabled":true`) || !strings.Contains(redacted, `"enabled":false`) {
		t.Fatalf("redacted diff did not preserve non-sensitive configuration: %s", redacted)
	}
	if strings.Count(redacted, "[REDACTED]") != 28 {
		t.Fatalf("redacted diff = %s", redacted)
	}
}

func TestRedactSensitiveDiffReturnsSafeNoticeForMalformedJSON(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/config.json b/config.json",
		"--- a/config.json",
		"+++ b/config.json",
		"@@ -1 +1 @@",
		`-{"token":"local-secret"`,
		`+{"token":"remote-secret"}`,
	}, "\n")

	redacted := redactSensitiveDiff(diff)
	if strings.Contains(redacted, "local-secret") || strings.Contains(redacted, "remote-secret") {
		t.Fatalf("malformed diff exposed a sensitive value: %s", redacted)
	}
	if redacted != "[REDACTED: configuration diff contains malformed JSON]" {
		t.Fatalf("malformed diff = %q", redacted)
	}
}

func TestRedactSensitiveDiffRedactsSensitiveJSONOnContextLines(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/config.json b/config.json",
		"--- a/config.json",
		"+++ b/config.json",
		"@@ -1,2 +1,2 @@",
		` {"apiKey":"context-secret"}`,
		`-null`,
		`+true`,
	}, "\n")

	redacted := redactSensitiveDiff(diff)
	if strings.Contains(redacted, "context-secret") {
		t.Fatalf("redacted diff exposed a context secret: %s", redacted)
	}
	if !strings.Contains(redacted, `"apiKey":"[REDACTED]"`) {
		t.Fatalf("redacted diff did not retain a safe context line: %s", redacted)
	}
}

func TestAbsolutePathResolvesServerRelativeConfigurationPath(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(bin); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	path, err := absolutePath(filepath.FromSlash("../data/config.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "data", "config.json")
	if path != want {
		t.Fatalf("absolutePath() = %q, want %q", path, want)
	}
}

func TestHTTPPullReturnsFailureWhenRestartFails(t *testing.T) {
	repository, config := newRepositoryTestFixture(t, `{"name":"initial"}`)
	if err := repository.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeRemoteRepositoryConfig(t, config.RepositoryURL, config.Branch, `{"name":"remote"}`)
	handler := NewHTTPHandler(HTTPConfig{
		Config:       config,
		SettingsPath: filepath.Join(t.TempDir(), "settings.json"),
		Restart: func() error {
			return errors.New("launch failed")
		},
	})

	response := performHTTPRequest(t, newHTTPRouter(handler), http.MethodPost, "/sync/pull", nil)
	if response.Code == http.StatusOK {
		t.Fatalf("POST /sync/pull status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHTTPResolveUseRemoteReturnsFailureWhenRestartFails(t *testing.T) {
	repository, config := newRepositoryTestFixture(t, `{"name":"initial"}`)
	if err := repository.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ConfigPath, []byte(`{"name":"local"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRemoteRepositoryConfig(t, config.RepositoryURL, config.Branch, `{"name":"remote"}`)
	handler := NewHTTPHandler(HTTPConfig{
		Config:       config,
		SettingsPath: filepath.Join(t.TempDir(), "settings.json"),
		Restart: func() error {
			return errors.New("launch failed")
		},
	})

	response := performHTTPRequest(t, newHTTPRouter(handler), http.MethodPost, "/sync/resolve", map[string]string{"resolution": string(ResolutionUseRemote)})
	if response.Code == http.StatusOK {
		t.Fatalf("POST /sync/resolve status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHTTPResolveUseRemoteAppliesConfigurationThenRestarts(t *testing.T) {
	repository, config := newRepositoryTestFixture(t, `{"name":"initial"}`)
	if err := repository.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ConfigPath, []byte(`{"name":"local"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRemoteRepositoryConfig(t, config.RepositoryURL, config.Branch, `{"name":"remote"}`)
	restarts := 0
	handler := NewHTTPHandler(HTTPConfig{
		Config:       config,
		SettingsPath: filepath.Join(t.TempDir(), "settings.json"),
		Restart:      func() error { restarts++; return nil },
	})
	router := newHTTPRouter(handler)

	response := performHTTPRequest(t, router, http.MethodPost, "/sync/resolve", map[string]string{"resolution": string(ResolutionUseRemote)})
	if response.Code != http.StatusOK {
		t.Fatalf("POST /sync/resolve status = %d, body = %s", response.Code, response.Body.String())
	}
	assertFileContents(t, config.ConfigPath, `{"name":"remote"}`)
	if restarts != 1 {
		t.Fatalf("restart count = %d, want 1", restarts)
	}
}

func newHTTPRouter(handler *HTTPHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler.Register(router)
	return router
}

func performHTTPRequest(t *testing.T, router http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	request := httptest.NewRequest(method, target, reader)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeHTTPResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}
