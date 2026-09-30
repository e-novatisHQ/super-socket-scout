package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type association struct {
	PID       int       `json:"pid"`
	StartTime string    `json:"startTime"`
	Name      string    `json:"name"`
	Directory string    `json:"directory"`
	StartedAt time.Time `json:"startedAt"`
}

type associationFile struct {
	Version      int           `json:"version"`
	Associations []association `json:"associations"`
}

func associationsPath() (string, error) {
	if explicit := os.Getenv("SERVER_WATCH_ASSOCIATIONS"); explicit != "" {
		return explicit, nil
	}
	stateDirectory := os.Getenv("XDG_STATE_HOME")
	if stateDirectory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		stateDirectory = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateDirectory, "server-watch", "associations.json"), nil
}

func readAssociations(path string) (associationFile, error) {
	state := associationFile{Version: contractVersion, Associations: []association{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(b, &state); err != nil {
		return associationFile{}, err
	}
	if state.Version != contractVersion {
		return associationFile{}, errors.New("version du registre incompatible")
	}
	return state, nil
}

func updateAssociations(path string, mutate func([]association) []association) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	state, err := readAssociations(path)
	if err != nil {
		return err
	}
	state.Associations = mutate(state.Associations)
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".associations-*.json")
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

func registerAssociation(path string, value association) error {
	return updateAssociations(path, func(values []association) []association {
		result := make([]association, 0, len(values)+1)
		for _, existing := range values {
			if existing.PID != value.PID {
				result = append(result, existing)
			}
		}
		return append(result, value)
	})
}

func unregisterAssociation(path string, pid int, startTime string) error {
	return updateAssociations(path, func(values []association) []association {
		result := make([]association, 0, len(values))
		for _, existing := range values {
			if existing.PID != pid || existing.StartTime != startTime {
				result = append(result, existing)
			}
		}
		return result
	})
}

func parentPID(pid int) int {
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "PPid:" {
			parent, _ := strconv.Atoi(fields[1])
			return parent
		}
	}
	return 0
}

func descendantOf(pid, root int, parent func(int) int) bool {
	for depth := 0; pid > 1 && depth < 32; depth++ {
		if pid == root {
			return true
		}
		pid = parent(pid)
	}
	return false
}

func applyAssociationRecords(items []Server, associations []association, parent func(int) int) {
	for i := range items {
		for _, association := range associations {
			if !descendantOf(items[i].PID, association.PID, parent) {
				continue
			}
			items[i].ServiceName = association.Name
			items[i].Manager = "server-watch run"
			items[i].Confidence = "certain"
			items[i].UnknownReason = ""
			if items[i].Runtime == "" {
				items[i].Runtime = valueOr(items[i].ProcessName, "processus")
			}
			items[i].Evidence = append(items[i].Evidence, "lancé explicitement avec server-watch run")
			if items[i].Cwd == "" {
				items[i].Cwd = association.Directory
			}
			break
		}
	}
}

func activeAssociations(path string) ([]association, error) {
	state, err := readAssociations(path)
	if err != nil {
		return nil, err
	}
	active := make([]association, 0, len(state.Associations))
	for _, association := range state.Associations {
		if processStart(association.PID) == association.StartTime {
			active = append(active, association)
		}
	}
	if len(active) != len(state.Associations) {
		_ = updateAssociations(path, func([]association) []association { return active })
	}
	return active, nil
}

func applyAssociations(items []Server) {
	path, err := associationsPath()
	if err != nil {
		return
	}
	values, err := activeAssociations(path)
	if err != nil {
		return
	}
	applyAssociationRecords(items, values, parentPID)
}

func runAssociated(name string, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "run exige une commande après --")
		return 3
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Commande introuvable:", args[0])
		return 1
	}
	directory, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if name == "" {
		name = filepath.Base(directory)
	}
	cmd := exec.Command(path, args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "Démarrage impossible:", err)
		return 1
	}
	startTime := processStart(cmd.Process.Pid)
	registry, registryErr := associationsPath()
	if registryErr == nil && startTime != "" {
		registryErr = registerAssociation(registry, association{PID: cmd.Process.Pid, StartTime: startTime, Name: name, Directory: directory, StartedAt: time.Now()})
	}
	if registryErr != nil {
		fmt.Fprintln(os.Stderr, "Association non enregistrée:", registryErr)
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
		return 1
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	receivedSignal := syscall.Signal(0)
	select {
	case err = <-done:
	case value := <-signals:
		if typed, ok := value.(syscall.Signal); ok {
			receivedSignal = typed
			_ = syscall.Kill(-cmd.Process.Pid, typed)
		}
		err = <-done
	}
	_ = unregisterAssociation(registry, cmd.Process.Pid, startTime)
	if receivedSignal != 0 {
		return 128 + int(receivedSignal)
	}
	if err == nil {
		return 0
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		return exitError.ExitCode()
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}
