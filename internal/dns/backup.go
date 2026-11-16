package dns

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"nexdns/internal/settings"
)

type DNSBackup struct {
	AdapterName   string    `json:"adapterName"`
	IsDHCP        bool      `json:"isDhcp"`
	IPv4Servers   []string  `json:"ipv4Servers"`
	IPv6Servers   []string  `json:"ipv6Servers"`
	SavedAt       time.Time `json:"savedAt"`
	PriorProvider string    `json:"priorProvider"`
}

func getBackupDir() string {
	dir := filepath.Join(settings.GetConfigDir(), "backups")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

func getBackupFilePath() string {
	return filepath.Join(getBackupDir(), "dns_backup.json")
}

// SaveBackup persists adapter's current DNS state.
func SaveBackup(backup DNSBackup) error {
	backup.SavedAt = time.Now()
	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(getBackupFilePath(), data, 0644)
}

// LoadLatestBackup retrieves the most recently saved backup.
func LoadLatestBackup() (*DNSBackup, error) {
	data, err := os.ReadFile(getBackupFilePath())
	if err != nil {
		return nil, fmt.Errorf("kayıtlı DNS yedeği bulunamadı: %w", err)
	}
	var backup DNSBackup
	if err := json.Unmarshal(data, &backup); err != nil {
		return nil, fmt.Errorf("yedek dosyası okunamadı: %w", err)
	}
	return &backup, nil
}

// HasBackup checks if a valid backup exists.
func HasBackup() bool {
	_, err := os.Stat(getBackupFilePath())
	return err == nil
}
