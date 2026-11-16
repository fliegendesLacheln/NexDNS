package appdns

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nexdns/internal/settings"
)

type AppRule struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	ProcessName  string    `json:"processName"`
	ExePath      string    `json:"exePath"`
	ProviderID   string    `json:"providerId"`
	ProviderName string    `json:"providerName"`
	DNSPrimary   string    `json:"dnsPrimary"`
	DNSSecondary string    `json:"dnsSecondary"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"createdAt"`
}

var (
	rulesMutex sync.RWMutex
	cacheRules []AppRule
)

func getRulesPath() string {
	return filepath.Join(settings.GetConfigDir(), "app_rules.json")
}

func LoadRules() []AppRule {
	rulesMutex.Lock()
	defer rulesMutex.Unlock()

	data, err := os.ReadFile(getRulesPath())
	if err != nil {
		cacheRules = []AppRule{}
		return cacheRules
	}

	var list []AppRule
	if err := json.Unmarshal(data, &list); err != nil {
		cacheRules = []AppRule{}
		return cacheRules
	}

	cacheRules = list
	return cacheRules
}

func SaveRules(rules []AppRule) error {
	rulesMutex.Lock()
	defer rulesMutex.Unlock()

	cacheRules = rules
	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(getRulesPath(), data, 0644)
}

func AddOrUpdateRule(rule AppRule) error {
	rules := LoadRules()
	rule.ProcessName = strings.ToLower(rule.ProcessName)
	if !strings.HasSuffix(rule.ProcessName, ".exe") {
		rule.ProcessName += ".exe"
	}
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = time.Now()
	}

	found := false
	for i, r := range rules {
		if r.ID == rule.ID || strings.EqualFold(r.ProcessName, rule.ProcessName) {
			rules[i] = rule
			found = true
			break
		}
	}
	if !found {
		rules = append(rules, rule)
	}
	return SaveRules(rules)
}

func DeleteRule(id string) error {
	rules := LoadRules()
	var updated []AppRule
	for _, r := range rules {
		if r.ID != id {
			updated = append(updated, r)
		}
	}
	return SaveRules(updated)
}

func ToggleRule(id string, enabled bool) error {
	rules := LoadRules()
	for i, r := range rules {
		if r.ID == id {
			rules[i].Enabled = enabled
			break
		}
	}
	return SaveRules(rules)
}

// FindRuleForProcess checks if there is an active rule for a given process executable name.
func FindRuleForProcess(procName string) *AppRule {
	rulesMutex.RLock()
	defer rulesMutex.RUnlock()

	cleanName := strings.ToLower(filepath.Base(procName))
	for _, r := range cacheRules {
		if r.Enabled && strings.EqualFold(r.ProcessName, cleanName) {
			ruleCopy := r
			return &ruleCopy
		}
	}
	return nil
}
