package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

const contractVersion = 1

type portOwner struct {
	ID          string `json:"id"`
	ServiceName string `json:"serviceName"`
	Project     string `json:"project,omitempty"`
	PID         int    `json:"pid,omitempty"`
	Exposed     bool   `json:"exposed"`
}

type portCheck struct {
	Port   int         `json:"port"`
	State  string      `json:"state"`
	Owners []portOwner `json:"owners"`
	Error  string      `json:"error,omitempty"`
}

func unavailablePortChecks(ports []int, err error) portCheckReport {
	report := portCheckReport{SchemaVersion: contractVersion, Checks: make([]portCheck, 0, len(ports))}
	for _, port := range ports {
		report.Checks = append(report.Checks, portCheck{Port: port, State: "unavailable", Owners: []portOwner{}, Error: err.Error()})
	}
	return report
}

type portCheckReport struct {
	SchemaVersion int         `json:"schemaVersion"`
	Checks        []portCheck `json:"checks"`
}

func checkPorts(items []Server, ports []int) portCheckReport {
	report := portCheckReport{SchemaVersion: contractVersion, Checks: make([]portCheck, 0, len(ports))}
	for _, port := range ports {
		check := portCheck{Port: port, State: "free", Owners: []portOwner{}}
		seen := map[string]bool{}
		for _, server := range items {
			matches := server.Port == port
			for _, endpoint := range server.Endpoints {
				matches = matches || endpoint.Port == port
			}
			if !matches || seen[server.ID] {
				continue
			}
			seen[server.ID] = true
			check.State = "occupied"
			check.Owners = append(check.Owners, portOwner{ID: server.ID, ServiceName: server.ServiceName, Project: server.Project, PID: server.PID, Exposed: server.Exposed})
		}
		report.Checks = append(report.Checks, check)
	}
	return report
}

func renderPortChecks(report portCheckReport, jsonOutput bool) string {
	if jsonOutput {
		b, _ := json.MarshalIndent(report, "", "  ")
		return string(b) + "\n"
	}
	var out strings.Builder
	for _, check := range report.Checks {
		if check.State == "unavailable" {
			fmt.Fprintf(&out, "? Port %d · détection indisponible : %s\n", check.Port, check.Error)
			continue
		}
		if check.State == "free" {
			fmt.Fprintf(&out, "✓ Port %d libre\n", check.Port)
			continue
		}
		fmt.Fprintf(&out, "✗ Port %d occupé\n", check.Port)
		for _, owner := range check.Owners {
			context := owner.Project
			if context == "" {
				context = "hors projet"
			}
			fmt.Fprintf(&out, "  - %s · %s · PID %d\n", owner.ServiceName, context, owner.PID)
		}
	}
	return out.String()
}

func portCheckCode(report portCheckReport) int {
	for _, check := range report.Checks {
		if check.State == "occupied" {
			return 2
		}
	}
	return 0
}
