package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	// ErrConfigNotFound is returned when no configuration file is found.
	ErrConfigNotFound = errors.New("config file not found")

	// ErrNoProfiles is returned when no CLI profiles are configured.
	ErrNoProfiles = errors.New("no CLI profiles configured")

	// ErrInsecureAuthConfig is returned when authentication uses unsafe defaults.
	ErrInsecureAuthConfig = errors.New("insecure auth config")
)

// AppConfig is the runtime configuration composed from YAML and env overrides.
type AppConfig struct {
	Port                    string
	DataDir                 string
	DefaultCLICommand       string
	CLIOutputProcessorsMode string
	CLIOutputArchiveDir     string
	CLIOutputArchiveLimit   int
	ConfigPath              string
	Environment             string
	ProfileRegistry         *CLIProfileRegistry
	Auth                    AuthConfig
	Automation              AutomationConfig
}

// AutomationConfig holds requirement automation runtime controls.
type AutomationConfig struct {
	MaxRequirementRetryAttempts int
	ReconnectBaseSeconds        int
	ReconnectMaxSeconds         int
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	Enabled       bool
	Username      string
	Password      string
	SessionSecret []byte
	SessionMaxAge int
}

type appYAMLConfig struct {
	CLIProfiles map[string][]CLIProfile `yaml:"cli_profiles"`
	Server      struct {
		Port    string `yaml:"port"`
		DataDir string `yaml:"data_dir"`
	} `yaml:"server"`
	CLIOutput struct {
		ArchiveDir   string `yaml:"archive_dir"`
		ArchiveLimit int    `yaml:"archive_limit"`
	} `yaml:"cli_output"`
	CLI struct {
		DefaultCommand       string `yaml:"default_command"`
		OutputProcessorsMode string `yaml:"output_processors_mode"`
	} `yaml:"cli"`
	Auth struct {
		Enabled       bool   `yaml:"enabled"`
		Username      string `yaml:"username"`
		Password      string `yaml:"password"`
		SessionSecret string `yaml:"session_secret"`
		SessionMaxAge int    `yaml:"session_max_age"`
	} `yaml:"auth"`
	Automation struct {
		MaxRequirementRetryAttempts int `yaml:"max_requirement_retry_attempts"`
		ReconnectBaseSeconds        int `yaml:"reconnect_base_seconds"`
		ReconnectMaxSeconds         int `yaml:"reconnect_max_seconds"`
	} `yaml:"automation"`
}

// CLIProfile describes one configured account profile in app.yaml.
type CLIProfile struct {
	ID            string            `yaml:"id" json:"id"`
	Name          string            `yaml:"name" json:"name"`
	PreScript     string            `yaml:"pre_script" json:"pre_script,omitempty"`
	ScriptCommand string            `yaml:"script_command" json:"script_command"`
	Env           map[string]string `yaml:"env" json:"env,omitempty"`
	Description   string            `yaml:"description" json:"description,omitempty"`
}

// CLIProfileRegistry provides validated read-only access to profile definitions.
type CLIProfileRegistry struct {
	byType map[string][]CLIProfile
	byKey  map[string]CLIProfile
}

// LoadAppConfig loads and validates runtime config from app.yaml with env variable overrides.
// Returns error if no configuration file exists.
func LoadAppConfig() (AppConfig, error) {
	yamlPath, yamlCfg, err := loadYAMLConfigFile()
	if err != nil {
		return AppConfig{}, err
	}

	if yamlCfg == nil {
		return AppConfig{}, ErrConfigNotFound
	}

	cfg := AppConfig{
		ConfigPath:  yamlPath,
		Environment: detectAppEnvironment(),
	}
	isProduction := isProductionEnvironment(cfg.Environment)

	// Apply YAML config values
	if strings.TrimSpace(yamlCfg.Server.Port) != "" {
		cfg.Port = strings.TrimSpace(yamlCfg.Server.Port)
	}
	if strings.TrimSpace(yamlCfg.Server.DataDir) != "" {
		cfg.DataDir = strings.TrimSpace(yamlCfg.Server.DataDir)
	}
	if strings.TrimSpace(yamlCfg.CLI.DefaultCommand) != "" {
		cfg.DefaultCLICommand = strings.TrimSpace(yamlCfg.CLI.DefaultCommand)
	}
	if strings.TrimSpace(yamlCfg.CLI.OutputProcessorsMode) != "" {
		cfg.CLIOutputProcessorsMode = strings.TrimSpace(yamlCfg.CLI.OutputProcessorsMode)
	}
	if strings.TrimSpace(yamlCfg.CLIOutput.ArchiveDir) != "" {
		cfg.CLIOutputArchiveDir = strings.TrimSpace(yamlCfg.CLIOutput.ArchiveDir)
	}
	if yamlCfg.CLIOutput.ArchiveLimit > 0 {
		cfg.CLIOutputArchiveLimit = yamlCfg.CLIOutput.ArchiveLimit
	}

	// Apply auth config from YAML
	if yamlCfg.Auth.Enabled {
		cfg.Auth.Enabled = true
		cfg.Auth.Username = strings.TrimSpace(yamlCfg.Auth.Username)
		cfg.Auth.Password = strings.TrimSpace(yamlCfg.Auth.Password)
		cfg.Auth.SessionSecret = []byte(yamlCfg.Auth.SessionSecret)
		cfg.Auth.SessionMaxAge = yamlCfg.Auth.SessionMaxAge
	}
	if yamlCfg.Automation.MaxRequirementRetryAttempts > 0 {
		cfg.Automation.MaxRequirementRetryAttempts = yamlCfg.Automation.MaxRequirementRetryAttempts
	}
	if yamlCfg.Automation.ReconnectBaseSeconds > 0 {
		cfg.Automation.ReconnectBaseSeconds = yamlCfg.Automation.ReconnectBaseSeconds
	}
	if yamlCfg.Automation.ReconnectMaxSeconds > 0 {
		cfg.Automation.ReconnectMaxSeconds = yamlCfg.Automation.ReconnectMaxSeconds
	}

	// Apply environment variable overrides
	if port := os.Getenv("PORT"); port != "" {
		cfg.Port = strings.TrimSpace(port)
	}
	if dataDir := os.Getenv("DATA_DIR"); dataDir != "" {
		cfg.DataDir = strings.TrimSpace(dataDir)
	}
	if defaultCLI := os.Getenv("DEFAULT_CLI_COMMAND"); defaultCLI != "" {
		cfg.DefaultCLICommand = strings.TrimSpace(defaultCLI)
	}
	if processorsMode := os.Getenv("CLI_OUTPUT_PROCESSORS_MODE"); processorsMode != "" {
		cfg.CLIOutputProcessorsMode = strings.TrimSpace(processorsMode)
	}
	if archiveDir := os.Getenv("CLI_OUTPUT_ARCHIVE_DIR"); archiveDir != "" {
		cfg.CLIOutputArchiveDir = strings.TrimSpace(archiveDir)
	}
	if archiveLimit := os.Getenv("CLI_OUTPUT_ARCHIVE_LIMIT"); archiveLimit != "" {
		if n, err := strconv.Atoi(archiveLimit); err == nil && n > 0 {
			cfg.CLIOutputArchiveLimit = n
		}
	}
	if value := os.Getenv("AUTOMATION_MAX_REQUIREMENT_RETRY_ATTEMPTS"); value != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > 0 {
			cfg.Automation.MaxRequirementRetryAttempts = n
		}
	}
	if value := os.Getenv("AUTOMATION_RECONNECT_BASE_SECONDS"); value != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > 0 {
			cfg.Automation.ReconnectBaseSeconds = n
		}
	}
	if value := os.Getenv("AUTOMATION_RECONNECT_MAX_SECONDS"); value != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > 0 {
			cfg.Automation.ReconnectMaxSeconds = n
		}
	}

	// Apply auth environment variable overrides
	authUsernameFromEnv := false
	authPasswordFromEnv := false
	authSessionSecretFromEnv := false
	if authEnabled, ok := os.LookupEnv("AUTH_ENABLED"); ok {
		cfg.Auth.Enabled = strings.ToLower(strings.TrimSpace(authEnabled)) == "true"
	}
	if authUsername, ok := os.LookupEnv("AUTH_USERNAME"); ok {
		cfg.Auth.Username = strings.TrimSpace(authUsername)
		authUsernameFromEnv = true
	}
	if authPassword, ok := os.LookupEnv("AUTH_PASSWORD"); ok {
		cfg.Auth.Password = strings.TrimSpace(authPassword)
		authPasswordFromEnv = true
	}
	if authSessionSecret, ok := os.LookupEnv("AUTH_SESSION_SECRET"); ok {
		cfg.Auth.SessionSecret = []byte(authSessionSecret)
		authSessionSecretFromEnv = true
	}
	if authSessionMaxAge, ok := os.LookupEnv("AUTH_SESSION_MAX_AGE"); ok && strings.TrimSpace(authSessionMaxAge) != "" {
		if n, err := strconv.Atoi(authSessionMaxAge); err == nil && n > 0 {
			cfg.Auth.SessionMaxAge = n
		}
	}

	// Apply default values if not set
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	if cfg.DefaultCLICommand == "" {
		cfg.DefaultCLICommand = "claude"
	}
	if cfg.CLIOutputArchiveDir == "" {
		cfg.CLIOutputArchiveDir = "/tmp/auto-code-cli-output"
	}
	if cfg.CLIOutputArchiveLimit == 0 {
		cfg.CLIOutputArchiveLimit = 500
	}
	if cfg.Automation.MaxRequirementRetryAttempts <= 0 {
		cfg.Automation.MaxRequirementRetryAttempts = 5
	}
	if cfg.Automation.ReconnectBaseSeconds <= 0 {
		cfg.Automation.ReconnectBaseSeconds = 15
	}
	if cfg.Automation.ReconnectMaxSeconds <= 0 {
		cfg.Automation.ReconnectMaxSeconds = 600
	}

	// Apply auth default values
	if cfg.Auth.Enabled && cfg.Auth.SessionMaxAge == 0 {
		cfg.Auth.SessionMaxAge = 86400 // 24 hours
	}
	if err := validateAuthSourcePolicy(cfg.Auth, isProduction, authUsernameFromEnv, authPasswordFromEnv, authSessionSecretFromEnv); err != nil {
		return AppConfig{}, err
	}
	if err := validateAuthConfig(cfg.Auth, isProduction); err != nil {
		return AppConfig{}, err
	}

	// Build profile registry from YAML config
	registry, err := buildProfileRegistry(yamlCfg)
	if err != nil {
		return AppConfig{}, fmt.Errorf("build profile registry: %w", err)
	}
	cfg.ProfileRegistry = registry

	return cfg, nil
}

func validateAuthSourcePolicy(cfg AuthConfig, production, usernameFromEnv, passwordFromEnv, sessionSecretFromEnv bool) error {
	if !cfg.Enabled || !production {
		return nil
	}
	missing := make([]string, 0, 3)
	if !usernameFromEnv {
		missing = append(missing, "AUTH_USERNAME")
	}
	if !passwordFromEnv {
		missing = append(missing, "AUTH_PASSWORD")
	}
	if !sessionSecretFromEnv {
		missing = append(missing, "AUTH_SESSION_SECRET")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: production requires auth credentials from environment variables: %s", ErrInsecureAuthConfig, strings.Join(missing, ", "))
	}
	return nil
}

func validateAuthConfig(cfg AuthConfig, strict bool) error {
	if !cfg.Enabled {
		return nil
	}

	username := strings.TrimSpace(cfg.Username)
	password := strings.TrimSpace(cfg.Password)
	sessionSecret := strings.TrimSpace(string(cfg.SessionSecret))

	if username == "" {
		return fmt.Errorf("%w: auth username is required when auth is enabled", ErrInsecureAuthConfig)
	}
	if password == "" {
		return fmt.Errorf("%w: auth password is required when auth is enabled", ErrInsecureAuthConfig)
	}
	if len(cfg.SessionSecret) < 32 {
		return fmt.Errorf("%w: auth session_secret must be at least 32 bytes", ErrInsecureAuthConfig)
	}
	if strict {
		if sessionSecret == "change-this-to-random-32byte-key" {
			return fmt.Errorf("%w: auth session_secret still uses the placeholder value", ErrInsecureAuthConfig)
		}
		if username == "admin" && password == "e10adc057f20f883e" {
			return fmt.Errorf("%w: auth credentials still use the default demo values", ErrInsecureAuthConfig)
		}
	}
	return nil
}

func detectAppEnvironment() string {
	for _, key := range []string{"APP_ENV", "ENV", "GO_ENV"} {
		if value, ok := os.LookupEnv(key); ok {
			trimmed := strings.TrimSpace(strings.ToLower(value))
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return "local"
}

func isProductionEnvironment(env string) bool {
	switch strings.TrimSpace(strings.ToLower(env)) {
	case "prod", "production":
		return true
	default:
		return false
	}
}

// loadYAMLConfigFile loads config from ./app.yaml.
// Returns error if file does not exist.
func loadYAMLConfigFile() (string, *appYAMLConfig, error) {
	const configPath = "./app.yaml"
	cfg, err := readYAMLConfig(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf("config file not found: %s", configPath)
		}
		return "", nil, fmt.Errorf("load app.yaml: %w", err)
	}
	return configPath, &cfg, nil
}

// readYAMLConfig parses a yaml configuration file.
func readYAMLConfig(path string) (appYAMLConfig, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return appYAMLConfig{}, err
	}
	if len(bytes) == 0 {
		return appYAMLConfig{}, errors.New("config file is empty")
	}
	var cfg appYAMLConfig
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return appYAMLConfig{}, fmt.Errorf("parse YAML: %w", err)
	}
	return cfg, nil
}

// buildProfileRegistry creates a validated profile registry from YAML config.
func buildProfileRegistry(yamlCfg *appYAMLConfig) (*CLIProfileRegistry, error) {
	if yamlCfg == nil || len(yamlCfg.CLIProfiles) == 0 {
		return nil, ErrNoProfiles
	}

	profiles := make(map[string][]CLIProfile)
	for cliType, list := range yamlCfg.CLIProfiles {
		normalizedType := strings.TrimSpace(strings.ToLower(cliType))
		if normalizedType == "" {
			continue
		}
		cleaned := make([]CLIProfile, 0, len(list))
		for _, item := range list {
			profile := CLIProfile{
				ID:            strings.TrimSpace(item.ID),
				Name:          strings.TrimSpace(item.Name),
				PreScript:     strings.TrimSpace(item.PreScript),
				ScriptCommand: strings.TrimSpace(item.ScriptCommand),
				Env:           normalizeProfileEnv(item.Env),
				Description:   strings.TrimSpace(item.Description),
			}
			if profile.ID == "" && profile.Name == "" && profile.PreScript == "" && profile.ScriptCommand == "" && len(profile.Env) == 0 {
				continue
			}
			cleaned = append(cleaned, profile)
		}
		if len(cleaned) > 0 {
			profiles[normalizedType] = cleaned
		}
	}

	if len(profiles) == 0 {
		return nil, ErrNoProfiles
	}
	return NewCLIProfileRegistry(profiles)
}

// NewCLIProfileRegistry validates and constructs a profile registry.
func NewCLIProfileRegistry(input map[string][]CLIProfile) (*CLIProfileRegistry, error) {
	registry := &CLIProfileRegistry{
		byType: make(map[string][]CLIProfile),
		byKey:  make(map[string]CLIProfile),
	}

	for cliType, list := range input {
		normalizedType := strings.TrimSpace(strings.ToLower(cliType))
		if normalizedType == "" {
			continue
		}
		// Track IDs within this CLI type only (same ID can exist in different types)
		seenIDsInType := make(map[string]bool)

		items := make([]CLIProfile, 0, len(list))
		for _, profile := range list {
			profile.ID = strings.TrimSpace(profile.ID)
			profile.Name = strings.TrimSpace(profile.Name)
			profile.PreScript = strings.TrimSpace(profile.PreScript)
			profile.ScriptCommand = strings.TrimSpace(profile.ScriptCommand)
			profile.Env = normalizeProfileEnv(profile.Env)
			profile.Description = strings.TrimSpace(profile.Description)

			if profile.ID == "" {
				return nil, fmt.Errorf("profile id is required for type %s", normalizedType)
			}
			if profile.Name == "" {
				return nil, fmt.Errorf("profile name is required for type %s id %s", normalizedType, profile.ID)
			}
			// pre_script and script_command are optional. pre_script is profile init hook.
			if seenIDsInType[profile.ID] {
				return nil, fmt.Errorf("profile id %s is duplicated in type %s", profile.ID, normalizedType)
			}
			seenIDsInType[profile.ID] = true
			items = append(items, profile)
			registry.byKey[normalizedType+":"+profile.ID] = profile
		}
		sort.Slice(items, func(i, j int) bool {
			return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		})
		registry.byType[normalizedType] = items
	}

	if len(registry.byType) == 0 {
		return nil, ErrNoProfiles
	}
	return registry, nil
}

// Types returns all configured CLI types sorted alphabetically.
func (r *CLIProfileRegistry) Types() []string {
	if r == nil {
		return nil
	}
	types := make([]string, 0, len(r.byType))
	for cliType := range r.byType {
		types = append(types, cliType)
	}
	sort.Strings(types)
	return types
}

// All returns a copy of profile map grouped by CLI type.
func (r *CLIProfileRegistry) All() map[string][]CLIProfile {
	if r == nil {
		return nil
	}
	out := make(map[string][]CLIProfile, len(r.byType))
	for cliType, list := range r.byType {
		copied := make([]CLIProfile, 0, len(list))
		for _, item := range list {
			item.Env = normalizeProfileEnv(item.Env)
			copied = append(copied, item)
		}
		out[cliType] = copied
	}
	return out
}

// ProfilesByType returns a copied profile list of one CLI type.
func (r *CLIProfileRegistry) ProfilesByType(cliType string) []CLIProfile {
	if r == nil {
		return nil
	}
	normalized := strings.TrimSpace(strings.ToLower(cliType))
	list := r.byType[normalized]
	out := make([]CLIProfile, 0, len(list))
	for _, item := range list {
		item.Env = normalizeProfileEnv(item.Env)
		out = append(out, item)
	}
	return out
}

// Find returns profile details for one cli_type + profile id pair.
func (r *CLIProfileRegistry) Find(cliType, profileID string) (CLIProfile, bool) {
	if r == nil {
		return CLIProfile{}, false
	}
	normalizedType := strings.TrimSpace(strings.ToLower(cliType))
	normalizedID := strings.TrimSpace(profileID)
	profile, ok := r.byKey[normalizedType+":"+normalizedID]
	if ok {
		profile.Env = normalizeProfileEnv(profile.Env)
	}
	return profile, ok
}

func normalizeProfileEnv(raw map[string]string) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		out[name] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SupportsMultipleAccounts returns true if the CLI type supports multiple account profiles.
func (r *CLIProfileRegistry) SupportsMultipleAccounts(cliType string) bool {
	if r == nil {
		return false
	}
	normalized := strings.TrimSpace(strings.ToLower(cliType))
	return len(r.byType[normalized]) > 1
}

// GetDefaultProfileID returns the default profile ID for a given CLI type.
func (r *CLIProfileRegistry) GetDefaultProfileID(cliType string) string {
	normalized := strings.TrimSpace(strings.ToLower(cliType))
	profiles := r.byType[normalized]
	if len(profiles) > 0 {
		return profiles[0].ID
	}
	return ""
}

// String returns a summary of the configuration for debugging.
func (c *AppConfig) String() string {
	if c == nil {
		return "AppConfig(nil)"
	}
	profileTypes := "none"
	if c.ProfileRegistry != nil {
		types := c.ProfileRegistry.Types()
		profileTypes = strings.Join(types, ", ")
	}
	authStatus := "disabled"
	if c.Auth.Enabled {
		authStatus = fmt.Sprintf("enabled(user=%s)", c.Auth.Username)
	}
	return fmt.Sprintf("AppConfig{Port=%s, DataDir=%s, ConfigPath=%s, ProfileTypes=[%s], Auth=%s}",
		c.Port, c.DataDir, c.ConfigPath, profileTypes, authStatus)
}
