package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const historyVersion = 1

type historyRecord struct {
	Key         string    `json:"key"`
	ServiceName string    `json:"serviceName"`
	Project     string    `json:"project,omitempty"`
	Protocol    string    `json:"protocol"`
	Address     string    `json:"address"`
	Port        int       `json:"port"`
	FirstSeen   time.Time `json:"firstSeen"`
	LastSeen    time.Time `json:"lastSeen"`
	Active      bool      `json:"active"`
}

type historyFile struct {
	Version int             `json:"version"`
	Records []historyRecord `json:"records"`
}

type historyDelta struct {
	Appeared    int
	Disappeared int
}

func historyPath() (string, error) {
	if explicit := os.Getenv("SERVER_WATCH_HISTORY"); explicit != "" {
		return explicit, nil
	}
	directory, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "server-watch", "history.json"), nil
}

func historyKey(s Server) string {
	return strings.Join([]string{strings.ToLower(s.Protocol), s.Address, fmt.Sprint(s.Port), s.Project, s.ServiceName}, "\x00")
}

func loadHistory(path string) (historyFile, error) {
	state := historyFile{Version: historyVersion, Records: []historyRecord{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(b, &state); err != nil {
		return historyFile{Version: historyVersion, Records: []historyRecord{}}, err
	}
	if state.Version != historyVersion {
		return historyFile{Version: historyVersion, Records: []historyRecord{}}, errors.New("version d’historique incompatible")
	}
	if state.Records == nil {
		state.Records = []historyRecord{}
	}
	return state, nil
}

func writeHistory(path string, state historyFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".history-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(b, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func recordInventory(path string, items []Server, now time.Time) (historyDelta, error) {
	state, err := loadHistory(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return historyDelta{}, err
	}
	previous := map[string]historyRecord{}
	for _, record := range state.Records {
		previous[record.Key] = record
	}
	seen := map[string]bool{}
	delta := historyDelta{}
	for _, item := range items {
		key := historyKey(item)
		seen[key] = true
		record, exists := previous[key]
		if !exists {
			record = historyRecord{Key: key, FirstSeen: now}
			delta.Appeared++
		} else if !record.Active {
			delta.Appeared++
		}
		record.ServiceName, record.Project = item.ServiceName, item.Project
		record.Protocol, record.Address, record.Port = item.Protocol, item.Address, item.Port
		record.LastSeen, record.Active = now, true
		previous[key] = record
	}
	for key, record := range previous {
		if record.Active && !seen[key] {
			record.Active = false
			previous[key] = record
			delta.Disappeared++
		}
	}
	state.Records = state.Records[:0]
	for _, record := range previous {
		state.Records = append(state.Records, record)
	}
	sort.Slice(state.Records, func(i, j int) bool { return state.Records[i].LastSeen.After(state.Records[j].LastSeen) })
	if len(state.Records) > 500 {
		state.Records = state.Records[:500]
	}
	return delta, writeHistory(path, state)
}

func historyDeltaMessage(delta historyDelta) string {
	parts := []string{}
	if delta.Appeared > 0 {
		parts = append(parts, fmt.Sprintf("+%d nouvelle(s) écoute(s)", delta.Appeared))
	}
	if delta.Disappeared > 0 {
		parts = append(parts, fmt.Sprintf("−%d écoute(s) disparue(s)", delta.Disappeared))
	}
	return strings.Join(parts, " · ")
}

func renderHistory(state historyFile, jsonOutput bool) string {
	if jsonOutput {
		b, _ := json.MarshalIndent(state, "", "  ")
		return string(b) + "\n"
	}
	var out strings.Builder
	out.WriteString("Historique des écoutes\n\n")
	if len(state.Records) == 0 {
		out.WriteString("Aucune écoute enregistrée.\n")
		return out.String()
	}
	for _, record := range state.Records {
		status := "arrêté"
		if record.Active {
			status = "actif"
		}
		context := filepath.Base(record.Project)
		if context == "." || context == "" {
			context = "hors projet"
		}
		fmt.Fprintf(&out, "%-7s %-22s %-18s %s/%s:%d · vu %s\n", status, clip(record.ServiceName, 22), clip(context, 18), record.Protocol, record.Address, record.Port, record.LastSeen.Local().Format("2006-01-02 15:04:05"))
	}
	return out.String()
}
