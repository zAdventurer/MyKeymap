package sync

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// SyncSettings are the only user-configurable values stored for sync. They
// intentionally exclude credentials, SSH keys, and filesystem implementation
// paths.
type SyncSettings struct {
	RepositoryURL string `json:"repositoryUrl"`
	Branch        string `json:"branch"`
}

// HTTPConfig supplies the local paths and optional restart callback needed by
// the HTTP adapter. RepositoryURL and Branch are used only when no saved
// settings exist.
type HTTPConfig struct {
	Config       Config
	SettingsPath string
	Restart      func() error
}

// HTTPHandler exposes Repository operations through a Gin router.
type HTTPHandler struct {
	config HTTPConfig
}

// NewHTTPHandler creates a handler with safe local defaults. Production code
// should provide Config paths explicitly; defaults make the settings server
// usable from its normal config-server working directory.
func NewHTTPHandler(config HTTPConfig) *HTTPHandler {
	if config.Config.RepositoryURL == "" {
		config.Config.RepositoryURL = DefaultRepositoryURL
	}
	if config.Config.Branch == "" {
		config.Config.Branch = DefaultBranch
	}
	if config.Config.ConfigPath == "" {
		config.Config.ConfigPath = filepath.FromSlash("../data/config.json")
	}
	storageDirectory := filepath.Join(filepath.Dir(config.Config.ConfigPath), ".mykeymap-sync")
	if config.Config.MirrorPath == "" {
		config.Config.MirrorPath = filepath.Join(storageDirectory, "mirror")
	}
	if config.Config.StatePath == "" {
		config.Config.StatePath = filepath.Join(storageDirectory, "state.json")
	}
	if config.Config.BackupPath == "" {
		config.Config.BackupPath = filepath.Join(storageDirectory, "backups")
	}
	if config.SettingsPath == "" {
		config.SettingsPath = filepath.Join(storageDirectory, "settings.json")
	}
	return &HTTPHandler{config: config}
}

// Register adds the complete, versioned set of sync endpoints to router.
func (handler *HTTPHandler) Register(router gin.IRoutes) {
	router.GET("/sync/status", handler.status)
	router.PUT("/sync/settings", handler.saveSettings)
	router.POST("/sync/pull", handler.pull)
	router.POST("/sync/push", handler.push)
	router.GET("/sync/diff", handler.diff)
	router.POST("/sync/resolve", handler.resolve)
}

func (handler *HTTPHandler) status(c *gin.Context) {
	repository, settings, err := handler.repository()
	if err != nil {
		writeSyncError(c, err)
		return
	}
	status, err := repository.Status(c.Request.Context())
	if err != nil {
		writeSyncError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": status, "settings": settings})
}

func (handler *HTTPHandler) saveSettings(c *gin.Context) {
	var settings SyncSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		writeSyncClientError(c, "invalid_settings", "sync settings must be valid JSON")
		return
	}
	settings, err := normalizeSettings(settings)
	if err != nil {
		writeSyncClientError(c, "invalid_settings", "sync settings are invalid")
		return
	}
	if err := writeSettingsAtomically(handler.config.SettingsPath, settings); err != nil {
		writeSyncError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": settings})
}

func (handler *HTTPHandler) pull(c *gin.Context) {
	repository, _, err := handler.repository()
	if err != nil {
		writeSyncError(c, err)
		return
	}
	status, err := repository.Status(c.Request.Context())
	if err != nil {
		writeSyncError(c, err)
		return
	}
	if err := repository.Pull(c.Request.Context()); err != nil {
		writeSyncError(c, err)
		return
	}
	if status == StatusRemoteOnly && handler.config.Restart != nil {
		if err := handler.config.Restart(); err != nil {
			writeSyncError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "action": "pull", "status": StatusSynchronized})
}

func (handler *HTTPHandler) push(c *gin.Context) {
	repository, _, err := handler.repository()
	if err != nil {
		writeSyncError(c, err)
		return
	}
	if err := repository.Push(c.Request.Context()); err != nil {
		writeSyncError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "action": "push", "status": StatusSynchronized})
}

func (handler *HTTPHandler) diff(c *gin.Context) {
	repository, _, err := handler.repository()
	if err != nil {
		writeSyncError(c, err)
		return
	}
	status, err := repository.Status(c.Request.Context())
	if err != nil {
		writeSyncError(c, err)
		return
	}
	if status == StatusLocalOnly {
		c.JSON(http.StatusOK, gin.H{"status": status, "diff": ""})
		return
	}

	localPath, err := absolutePath(repository.Config.ConfigPath)
	if err != nil {
		writeSyncError(c, err)
		return
	}
	remotePath, err := absolutePath(repository.mirrorConfigPath())
	if err != nil {
		writeSyncError(c, err)
		return
	}
	localConfig, err := readRedactedConfig(localPath)
	if err != nil {
		writeSyncError(c, err)
		return
	}
	remoteConfig, err := readRedactedConfig(remotePath)
	if err != nil {
		writeSyncError(c, err)
		return
	}
	diffDirectory, err := os.MkdirTemp("", "mykeymap-sync-diff-")
	if err != nil {
		writeSyncError(c, err)
		return
	}
	defer os.RemoveAll(diffDirectory)
	localDiffPath := filepath.Join(diffDirectory, "local-config.json")
	remoteDiffPath := filepath.Join(diffDirectory, "remote-config.json")
	if err := os.WriteFile(localDiffPath, localConfig, 0o600); err != nil {
		writeSyncError(c, err)
		return
	}
	if err := os.WriteFile(remoteDiffPath, remoteConfig, 0o600); err != nil {
		writeSyncError(c, err)
		return
	}
	command := repository.Git.Command(c.Request.Context(), diffDirectory,
		"diff", "--no-index", "--", localDiffPath, remoteDiffPath)
	output, err := command.CombinedOutput()
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
			writeSyncError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": status, "diff": redactSensitiveDiff(string(output))})
}

func (handler *HTTPHandler) resolve(c *gin.Context) {
	var request struct {
		Resolution ConflictResolution `json:"resolution"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeSyncClientError(c, "invalid_resolution", "a conflict resolution is required")
		return
	}
	if request.Resolution != ResolutionKeepLocal && request.Resolution != ResolutionUseRemote {
		writeSyncClientError(c, "invalid_resolution", "resolution must be keep-local or use-remote")
		return
	}
	repository, _, err := handler.repository()
	if err != nil {
		writeSyncError(c, err)
		return
	}
	if err := repository.ResolveConflict(c.Request.Context(), request.Resolution); err != nil {
		writeSyncError(c, err)
		return
	}
	if request.Resolution == ResolutionUseRemote && handler.config.Restart != nil {
		if err := handler.config.Restart(); err != nil {
			writeSyncError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "action": "resolve", "status": StatusSynchronized})
}

func (handler *HTTPHandler) repository() (*Repository, SyncSettings, error) {
	settings, err := handler.readSettings()
	if err != nil {
		return nil, SyncSettings{}, err
	}
	config := handler.config.Config
	config.RepositoryURL = settings.RepositoryURL
	config.Branch = settings.Branch
	return &Repository{Config: config, Git: GitExecutor{}}, settings, nil
}

func (handler *HTTPHandler) readSettings() (SyncSettings, error) {
	data, err := os.ReadFile(handler.config.SettingsPath)
	if errors.Is(err, os.ErrNotExist) {
		return normalizeSettings(SyncSettings{
			RepositoryURL: handler.config.Config.RepositoryURL,
			Branch:        handler.config.Config.Branch,
		})
	}
	if err != nil {
		return SyncSettings{}, err
	}
	var settings SyncSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return SyncSettings{}, err
	}
	return normalizeSettings(settings)
}

func normalizeSettings(settings SyncSettings) (SyncSettings, error) {
	settings.RepositoryURL = strings.TrimSpace(settings.RepositoryURL)
	settings.Branch = strings.TrimSpace(settings.Branch)
	if settings.RepositoryURL == "" {
		settings.RepositoryURL = DefaultRepositoryURL
	}
	if settings.Branch == "" {
		settings.Branch = DefaultBranch
	}
	if strings.HasPrefix(settings.RepositoryURL, "-") || strings.HasPrefix(settings.Branch, "-") {
		return SyncSettings{}, errors.New("unsafe Git argument")
	}
	if strings.ContainsAny(settings.RepositoryURL, "?#") {
		return SyncSettings{}, errors.New("repository URL contains credentials or is malformed")
	}
	if strings.Contains(settings.RepositoryURL, "://") {
		parsed, err := url.Parse(settings.RepositoryURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return SyncSettings{}, errors.New("repository URL contains credentials or is malformed")
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return SyncSettings{}, errors.New("repository URL contains credentials or is malformed")
		}
		if parsed.User != nil {
			_, hasPassword := parsed.User.Password()
			if hasPassword || parsed.Scheme != "ssh" {
				return SyncSettings{}, errors.New("repository URL contains credentials or is malformed")
			}
		}
	}
	return settings, nil
}

func writeSettingsAtomically(path string, settings SyncSettings) (err error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFileAtomically(temporaryPath, path)
}

func absolutePath(path string) (string, error) {
	return filepath.Abs(path)
}

func readRedactedConfig(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return redactConfigJSON(data)
}

func redactConfigJSON(data []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	redactJSONValue(value)
	redacted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(redacted, '\n'), nil
}

func redactSensitiveDiff(diff string) string {
	var redacted []string
	for _, line := range strings.Split(diff, "\n") {
		if (strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++")) ||
			(strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---")) ||
			strings.HasPrefix(line, " ") {
			var value any
			if err := json.Unmarshal([]byte(line[1:]), &value); err != nil {
				return "[REDACTED: configuration diff contains malformed JSON]"
			}
			redactJSONValue(value)
			encoded, err := json.Marshal(value)
			if err != nil {
				return "[REDACTED: configuration diff could not be safely rendered]"
			}
			redacted = append(redacted, line[:1]+string(encoded))
		} else {
			redacted = append(redacted, line)
		}
	}
	return strings.Join(redacted, "\n")
}

func redactJSONValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if isSensitiveConfigKey(key) {
				typed[key] = "[REDACTED]"
				continue
			}
			redactJSONValue(child)
		}
	case []any:
		for _, child := range typed {
			redactJSONValue(child)
		}
	}
}

func isSensitiveConfigKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(key))
	for _, marker := range []string{"token", "secret", "password", "apikey", "privatekey", "accesskey", "passphrase", "cookie", "authorization", "credential"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func writeSyncClientError(c *gin.Context, code, message string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": code, "message": message}})
}

func writeSyncError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrConflict), errors.Is(err, ErrLocalChanged), errors.Is(err, ErrRemoteChanged):
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "conflict", "message": "synchronization requires an explicit conflict resolution"}})
	default:
		// Git and filesystem errors can include credentials or local paths. Keep
		// details in the server log rather than returning them to the browser.
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "sync_failed", "message": "synchronization failed; check Git and SSH configuration"}})
	}
}
