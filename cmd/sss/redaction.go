package main

import (
	"regexp"
	"strings"
)

var (
	sensitiveOption = regexp.MustCompile(`(?i)(--?(?:password|passwd|token|secret|api[-_]?key|access[-_]?token|auth)(?:=|\s+))([^\s]+)`)
	sensitiveEnv    = regexp.MustCompile(`(?i)(^|\s)([A-Z0-9_]*(?:PASSWORD|PASSWD|TOKEN|SECRET|API_KEY|ACCESS_KEY)[A-Z0-9_]*)=([^\s]+)`)
	urlCredentials  = regexp.MustCompile(`(://[^:/\s]+:)([^@/\s]+)(@)`)
	sensitiveFlag   = regexp.MustCompile(`(?i)^--?(?:password|passwd|token|secret|api[-_]?key|access[-_]?token|auth)$`)
)

func redactCommand(command string) string {
	command = sensitiveOption.ReplaceAllString(command, `${1}[REDACTED]`)
	command = sensitiveEnv.ReplaceAllString(command, `${1}${2}=[REDACTED]`)
	return urlCredentials.ReplaceAllString(command, `${1}[REDACTED]${3}`)
}

func redactProcCommand(raw string) string {
	parts := strings.Split(strings.TrimRight(raw, "\x00"), "\x00")
	for index := 0; index < len(parts); index++ {
		if sensitiveFlag.MatchString(parts[index]) && index+1 < len(parts) {
			parts[index+1] = "[REDACTED]"
			index++
			continue
		}
		parts[index] = redactCommand(parts[index])
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}
