package appdns

import (
	"encoding/json"
	"os/exec"
	"sort"
	"strings"
	"syscall"
)

type RunningProcess struct {
	PID         int    `json:"pid"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Path        string `json:"path"`
	IsWindowApp bool   `json:"isWindowApp"`
}

// GetRunningApplications retrieves running desktop applications on Windows.
func GetRunningApplications() ([]RunningProcess, error) {
	// Query user-facing processes that have an executable path
	psScript := `Get-Process | Where-Object { $_.Path -ne $null -and $_.Name -ne 'Idle' -and $_.Name -ne 'System' } | Select-Object Id, ProcessName, MainWindowTitle, Path | ConvertTo-Json -Compress`

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(string(out))
	if len(trimmed) == 0 {
		return []RunningProcess{}, nil
	}

	type rawProc struct {
		Id              int    `json:"Id"`
		ProcessName     string `json:"ProcessName"`
		MainWindowTitle string `json:"MainWindowTitle"`
		Path            string `json:"Path"`
	}

	var rawList []rawProc
	if strings.HasPrefix(trimmed, "[") {
		_ = json.Unmarshal([]byte(trimmed), &rawList)
	} else if strings.HasPrefix(trimmed, "{") {
		var single rawProc
		if err := json.Unmarshal([]byte(trimmed), &single); err == nil {
			rawList = append(rawList, single)
		}
	}

	// Filter and deduplicate by process name, prioritizing ones with window titles
	procMap := make(map[string]RunningProcess)
	for _, p := range rawList {
		if p.Path == "" || strings.HasPrefix(strings.ToLower(p.Path), "c:\\windows\\system32\\") {
			continue
		}
		displayName := p.ProcessName
		isWin := false
		if strings.TrimSpace(p.MainWindowTitle) != "" {
			displayName = p.MainWindowTitle
			isWin = true
		}

		key := strings.ToLower(p.ProcessName)
		existing, exists := procMap[key]
		if !exists || (!existing.IsWindowApp && isWin) {
			procMap[key] = RunningProcess{
				PID:         p.Id,
				Name:        p.ProcessName + ".exe",
				DisplayName: displayName,
				Path:        p.Path,
				IsWindowApp: isWin,
			}
		}
	}

	var results []RunningProcess
	for _, p := range procMap {
		results = append(results, p)
	}

	// Sort window apps first, then alphabetical
	sort.Slice(results, func(i, j int) bool {
		if results[i].IsWindowApp != results[j].IsWindowApp {
			return results[i].IsWindowApp
		}
		return strings.ToLower(results[i].DisplayName) < strings.ToLower(results[j].DisplayName)
	})

	return results, nil
}
