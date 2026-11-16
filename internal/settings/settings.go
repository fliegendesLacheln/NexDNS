package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type CustomDNSConfig struct {
	Name          string `json:"name"`
	IPv4Primary   string `json:"ipv4Primary"`
	IPv4Secondary string `json:"ipv4Secondary"`
	IPv6Primary   string `json:"ipv6Primary"`
	IPv6Secondary string `json:"ipv6Secondary"`
}

type Config struct {
	Theme           string            `json:"theme"`
	Mode            string            `json:"mode"` // "system" or "app"
	SelectedAdapter string            `json:"selectedAdapter"`
	ActiveProvider  string            `json:"activeProvider"`
	AutoStart       bool              `json:"autoStart"`
	AutoStartSet    bool              `json:"autoStartSet,omitempty"`
	Language        string            `json:"language"`
	CustomDNS       []CustomDNSConfig `json:"customDNS"`
}

var (
	currentConfig *Config
	configMutex   sync.RWMutex
)

func GetConfigDir() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = "."
	}
	dir := filepath.Join(appData, "NEXDNS")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

func GetConfigPath() string {
	return filepath.Join(GetConfigDir(), "settings.json")
}

func DefaultConfig() *Config {
	return &Config{
		Theme:           "midnight-purple",
		Mode:            "system",
		SelectedAdapter: "auto",
		ActiveProvider:  "cloudflare",
		AutoStart:       true,
		AutoStartSet:    true,
		Language:        "tr",
		CustomDNS:       []CustomDNSConfig{},
	}
}

func Load() *Config {
	configMutex.Lock()
	defer configMutex.Unlock()

	cfgPath := GetConfigPath()
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		currentConfig = DefaultConfig()
		saveUnlocked(currentConfig)
		return currentConfig
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		currentConfig = DefaultConfig()
		saveUnlocked(currentConfig)
		return currentConfig
	}

	if cfg.Theme == "" {
		cfg.Theme = "midnight-purple"
	}
	if cfg.Mode == "" {
		cfg.Mode = "system"
	}
	if cfg.Language == "" {
		cfg.Language = "tr"
	}
	if cfg.ActiveProvider == "" {
		cfg.ActiveProvider = "cloudflare"
	}
	// Older settings files had no way to opt out of startup. Treat the old
	// default false value as unconfigured and honor the user's startup request.
	if !cfg.AutoStartSet {
		cfg.AutoStart = true
		cfg.AutoStartSet = true
		saveUnlocked(&cfg)
	}

	currentConfig = &cfg
	return currentConfig
}

func Get() *Config {
	configMutex.RLock()
	cfg := currentConfig
	configMutex.RUnlock()

	if cfg == nil {
		cfg = Load()
	}
	return cloneConfig(cfg)
}

// cloneConfig returns a deep copy so callers can mutate the result without
// racing against the shared internal config pointer.
func cloneConfig(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	if cfg.CustomDNS != nil {
		clone.CustomDNS = make([]CustomDNSConfig, len(cfg.CustomDNS))
		copy(clone.CustomDNS, cfg.CustomDNS)
	}
	return &clone
}

func Save(cfg *Config) error {
	configMutex.Lock()
	defer configMutex.Unlock()
	cfg.AutoStartSet = true
	return saveUnlocked(cfg)
}

func saveUnlocked(cfg *Config) error {
	currentConfig = cfg
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(GetConfigPath(), data, 0644)
}
