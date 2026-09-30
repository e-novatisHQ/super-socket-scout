package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type preferences struct {
	Scope    string `json:"scope"`
	ShowUDP  bool   `json:"showUdp"`
	SortMode string `json:"sortMode"`
	Watch    bool   `json:"watch"`
}

func preferencesPath() (string, error) {
	if explicit := os.Getenv("SERVER_WATCH_CONFIG"); explicit != "" {
		return explicit, nil
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "server-watch", "config.json"), nil
}

func validScope(scope string) bool {
	for _, candidate := range scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

func loadPreferences(path string) (preferences, error) {
	p := preferences{Scope: "Attention"}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return preferences{Scope: "Attention"}, err
	}
	if !validScope(p.Scope) {
		p.Scope = "Attention"
	}
	if p.SortMode != "" && p.SortMode != "port" && p.SortMode != "project" {
		p.SortMode = ""
	}
	return p, nil
}

func savePreferences(path string, p preferences) error {
	if !validScope(p.Scope) {
		return errors.New("vue invalide")
	}
	if p.SortMode != "" && p.SortMode != "port" && p.SortMode != "project" {
		return errors.New("tri invalide")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
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

func preferencesFor(f filters) preferences {
	return preferences{Scope: f.scope, ShowUDP: f.udp, SortMode: f.sortMode, Watch: f.watch}
}

func persistPreferences(path string, f *filters) {
	if path == "" {
		return
	}
	if err := savePreferences(path, preferencesFor(*f)); err != nil {
		f.notice = "✗ Préférences non enregistrées : " + err.Error()
	}
}
