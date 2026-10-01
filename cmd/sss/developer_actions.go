package main

import (
	stdcontext "context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var httpServices = map[string]bool{
	"vite": true, "next.js": true, "sveltekit": true, "django": true,
	"uvicorn": true, "portainer": true,
}

func serverURL(s Server) string {
	if strings.ToLower(s.Protocol) != "tcp" || s.Port < 1 {
		return ""
	}
	name := strings.ToLower(s.ServiceName)
	knownPort := map[int]bool{80: true, 443: true, 3000: true, 4173: true, 5173: true, 8000: true, 8080: true, 8443: true, 9443: true}[s.Port]
	if !httpServices[name] && !knownPort {
		return ""
	}
	host := s.Address
	switch host {
	case "", "*", "0.0.0.0":
		host = "127.0.0.1"
	case "::", "[::]":
		host = "[::1]"
	default:
		if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
			host = "[" + host + "]"
		}
	}
	scheme := "http"
	if s.Port == 443 || s.Port == 8443 || s.Port == 9443 {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, s.Port)
}

func developerCopyValue(s Server) (string, string) {
	if url := serverURL(s); url != "" {
		return url, "URL"
	}
	if s.Cwd != "" {
		return s.Cwd, "dossier"
	}
	if s.Project != "" {
		return s.Project, "projet"
	}
	if s.PID > 0 {
		return strconv.Itoa(s.PID), "PID"
	}
	return s.ID, "identifiant"
}

type clipboardCommand struct {
	name string
	args []string
}

var clipboardCandidates = []clipboardCommand{
	{name: "wl-copy"},
	{name: "xclip", args: []string{"-selection", "clipboard"}},
	{name: "xsel", args: []string{"--clipboard", "--input"}},
}

func copyToClipboard(text string) (string, error) {
	for _, candidate := range clipboardCandidates {
		path, err := exec.LookPath(candidate.name)
		if err != nil {
			continue
		}
		ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 2*time.Second)
		cmd := exec.CommandContext(ctx, path, candidate.args...)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			cancel()
			continue
		}
		if err := cmd.Start(); err != nil {
			cancel()
			continue
		}
		_, writeErr := io.WriteString(stdin, text)
		closeErr := stdin.Close()
		waitErr := cmd.Wait()
		cancel()
		if writeErr == nil && closeErr == nil && waitErr == nil {
			return candidate.name, nil
		}
	}
	return "", errors.New("aucun presse-papiers compatible (wl-copy, xclip ou xsel)")
}

func openTarget(target string) error {
	path, err := exec.LookPath("xdg-open")
	if err != nil {
		return errors.New("xdg-open est indisponible")
	}
	cmd := exec.Command(path, target)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func projectDirectory(s Server) string {
	for _, candidate := range []string{s.Cwd, s.Project} {
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			absolute, err := filepath.Abs(candidate)
			if err == nil {
				return absolute
			}
		}
	}
	return ""
}

func copyServerValue(s Server) string {
	value, kind := developerCopyValue(s)
	tool, err := copyToClipboard(value)
	if err != nil {
		return "✗ " + err.Error()
	}
	return fmt.Sprintf("✓ %s copié avec %s", kind, tool)
}

func openServer(s Server) string {
	url := serverURL(s)
	if url == "" {
		return "✗ aucune URL HTTP reconnue pour ce service"
	}
	if err := openTarget(url); err != nil {
		return "✗ ouverture impossible : " + err.Error()
	}
	return "✓ Ouverture de " + url
}

func openProject(s Server) string {
	directory := projectDirectory(s)
	if directory == "" {
		return "✗ aucun dossier de projet accessible"
	}
	if err := openTarget(directory); err != nil {
		return "✗ ouverture impossible : " + err.Error()
	}
	return "✓ Ouverture de " + directory
}
