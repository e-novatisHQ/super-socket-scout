package main

import (
	"bufio"
	stdcontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"
)

var version = "dev"

type Endpoint struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Exposed  bool   `json:"exposed"`
}
type Server struct {
	ID             string     `json:"id"`
	Label          string     `json:"label"`
	Protocol       string     `json:"protocol"`
	Address        string     `json:"address"`
	ProcessName    string     `json:"processName,omitempty"`
	Command        string     `json:"command,omitempty"`
	Cwd            string     `json:"cwd,omitempty"`
	Project        string     `json:"project,omitempty"`
	Branch         string     `json:"branch,omitempty"`
	ServiceName    string     `json:"serviceName"`
	Runtime        string     `json:"runtime,omitempty"`
	Manager        string     `json:"manager"`
	Confidence     string     `json:"confidence"`
	UnknownReason  string     `json:"unknownReason,omitempty"`
	Port           int        `json:"port"`
	PID            int        `json:"pid,omitempty"`
	UID            *int       `json:"uid,omitempty"`
	PPID           int        `json:"ppid,omitempty"`
	Cgroup         string     `json:"cgroup,omitempty"`
	StartTime      string     `json:"startTime,omitempty"`
	Exposed        bool       `json:"exposed"`
	System         bool       `json:"system"`
	Worktree       bool       `json:"worktree"`
	Orphan         bool       `json:"orphan"`
	Endpoints      []Endpoint `json:"endpoints"`
	Evidence       []string   `json:"evidence"`
	Status         string     `json:"-"`
	Elevated       bool       `json:"elevated"`
	DisabledReason string     `json:"-"`
}
type dockerContainer struct {
	Name  string
	Image string
	Ports []Endpoint
}
type socketRecord struct {
	Protocol    string `json:"protocol"`
	Address     string `json:"address"`
	ProcessName string `json:"processName,omitempty"`
	Port        int    `json:"port"`
	PID         int    `json:"pid,omitempty"`
}

var dockerPort = regexp.MustCompile(`(?:(?:\d+(?:\.\d+){3}|\[[^]]+\]):)?(\d+)->(\d+)/(tcp|udp)`)

type filters struct {
	scope, query, notice string
	udp, elevated, watch bool
	sortMode             string
}

var scopes = []string{"Attention", "Projets", "Système", "Tous"}
var ssProcess = regexp.MustCompile(`\(\("([^"]+)",pid=(\d+)`)
var systemNames = map[string]bool{"systemd": true, "systemd-resolve": true, "sshd": true, "cupsd": true, "avahi-daemon": true, "NetworkManager": true, "rootlesskit": true, "containerd": true, "dockerd": true, "kdeconnectd": true}
var knownPorts = map[string][2]string{
	"tcp:22": {"SSH", "OpenSSH"}, "tcp:53": {"DNS", "résolveur système"}, "udp:53": {"DNS", "résolveur système"},
	"tcp:631": {"CUPS", "service d'impression"}, "tcp:1716": {"KDE Connect", "service de bureau"}, "udp:1716": {"KDE Connect", "service de bureau"},
	"tcp:5355": {"LLMNR", "résolution locale"}, "udp:5355": {"LLMNR", "résolution locale"},
	"udp:546": {"Client DHCPv6", "réseau système"},
}

type serviceSignature struct {
	pattern                *regexp.Regexp
	name, runtime, manager string
}

var serviceSignatures = []serviceSignature{
	{regexp.MustCompile(`(?i)admin-tools-orchestrator-test.*fake-npm|fake-npm.*admin-tools-orchestrator-test`), "Test orchestrateur", "Node.js", "test local"},
	{regexp.MustCompile(`(?i)(?:^|/)vite(?:\s|$)|vite dev`), "Vite", "Node.js", "serveur de développement"},
	{regexp.MustCompile(`(?i)next dev`), "Next.js", "Node.js", "serveur de développement"},
	{regexp.MustCompile(`(?i)svelte-kit|sveltekit`), "SvelteKit", "Node.js", "serveur de développement"},
	{regexp.MustCompile(`(?i)\btsx\s+watch\b`), "Watcher TypeScript", "Node.js/tsx", "serveur de développement"},
	{regexp.MustCompile(`(?i)\brootlesskit\b.*\bdocker`), "Docker rootless", "Docker", "service utilisateur"},
	{regexp.MustCompile(`(?i)\buvicorn\b`), "Uvicorn", "Python", "serveur applicatif"},
	{regexp.MustCompile(`(?i)manage\.py\s+runserver`), "Django", "Python", "serveur de développement"},
	{regexp.MustCompile(`(?i)\badb\s+-L\s+tcp:`), "ADB server", "Android Debug Bridge", "processus utilisateur"},
	{regexp.MustCompile(`(?i)\bkdeconnectd\b`), "KDE Connect", "service de bureau", "session utilisateur"},
	{regexp.MustCompile(`(?i)systemd-resolved`), "DNS / LLMNR", "résolution système", "systemd"},
	{regexp.MustCompile(`(?i)(?:^|/)sshd(?:\s|$)`), "SSH", "OpenSSH", "systemd"},
	{regexp.MustCompile(`(?i)(?:^|/)cupsd(?:\s|$)`), "CUPS", "service d'impression", "systemd"},
}

func run(name string, args ...string) (string, error) {
	return runTimeout(5*time.Second, name, args...)
}
func runTimeout(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()
	b, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if ctx.Err() == stdcontext.DeadlineExceeded {
		return string(b), fmt.Errorf("commande %s expirée après %s", name, timeout)
	}
	return string(b), err
}
func exposed(a string) bool {
	return a != "127.0.0.1" && a != "127.0.0.53%lo" && a != "127.0.0.54" && a != "::1"
}
func splitAddr(v string) (string, int) {
	i := strings.LastIndex(v, ":")
	if i < 0 {
		return v, 0
	}
	p, _ := strconv.Atoi(v[i+1:])
	return strings.Trim(v[:i], "[]"), p
}
func dockerContainers() []dockerContainer {
	out, e := runTimeout(1500*time.Millisecond, "docker", "ps", "--format", "{{json .}}")
	if e != nil {
		return nil
	}
	return parseDockerContainers(out)
}
func parseDockerContainers(out string) []dockerContainer {
	r := []dockerContainer{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		var v struct{ Names, Image, Ports string }
		if json.Unmarshal([]byte(line), &v) != nil {
			continue
		}
		c := dockerContainer{Name: v.Names, Image: v.Image}
		for _, m := range dockerPort.FindAllStringSubmatch(v.Ports, -1) {
			host, _ := strconv.Atoi(m[1])
			container, _ := strconv.Atoi(m[2])
			c.Ports = append(c.Ports, Endpoint{Protocol: m[3], Port: host, Address: strconv.Itoa(container)})
		}
		r = append(r, c)
	}
	return r
}

func parseSS(text string) []socketRecord {
	records := []socketRecord{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || (f[0] != "tcp" && f[0] != "udp") {
			continue
		}
		if f[0] == "tcp" && f[1] != "LISTEN" || f[0] == "udp" && f[1] != "UNCONN" {
			continue
		}
		address, port := splitAddr(f[4])
		if port == 0 {
			continue
		}
		r := socketRecord{Protocol: f[0], Address: address, Port: port}
		m := ssProcess.FindStringSubmatch(strings.Join(f[min(6, len(f)):], " "))
		if len(m) > 0 {
			r.ProcessName = m[1]
			r.PID, _ = strconv.Atoi(m[2])
		}
		records = append(records, r)
	}
	return records
}

func discover(elevated bool) ([]Server, error) {
	containers := dockerContainers()
	gitCache := map[string]gitProjectMetadata{}
	var out string
	var err error
	if elevated {
		out, err = run("/usr/bin/sudo", "-n", "/usr/bin/ss", "-H", "-lntup")
	} else {
		out, err = run("ss", "-H", "-lntup")
	}
	if err != nil {
		return nil, fmt.Errorf("inventaire des sockets: %w", err)
	}
	byKey := map[string]*Server{}
	for _, socket := range parseSS(out) {
		if socket.Protocol == "udp" && socket.Address != "0.0.0.0" && socket.Address != "::" && socket.Address != "*" && socket.Port >= 1024 {
			continue
		}
		key := fmt.Sprintf("unknown:%s:%d", socket.Protocol, socket.Port)
		if socket.PID > 0 {
			key = fmt.Sprintf("process:%d", socket.PID)
		}
		s := byKey[key]
		if s == nil {
			s = &Server{ID: key, Protocol: socket.Protocol, Address: socket.Address, Port: socket.Port, PID: socket.PID, ProcessName: socket.ProcessName, Elevated: elevated}
			byKey[key] = s
			if socket.PID > 0 {
				readProcCached(s, elevated, gitCache)
				s.ID = fmt.Sprintf("process:%d:%s", socket.PID, valueOr(s.StartTime, "unknown"))
			}
		}
		e := Endpoint{socket.Protocol, socket.Address, socket.Port, exposed(socket.Address)}
		s.Endpoints = append(s.Endpoints, e)
		s.Exposed = s.Exposed || e.Exposed
	}
	items := make([]Server, 0, len(byKey))
	for _, s := range byKey {
		finalizeServer(s)
		if s.PID > 0 {
			s.Status = "available"
		} else {
			s.Status = "disabled"
			if elevated {
				s.DisabledReason = "PID non visible même avec l'identification élevée"
			} else {
				s.DisabledReason = "PID non visible avec les permissions actuelles"
			}
		}
		identify(s, containers)
		items = append(items, *s)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Exposed != items[j].Exposed {
			return items[i].Exposed
		}
		return items[i].Port < items[j].Port
	})
	applyAssociations(items)
	return items, nil
}

func finalizeServer(s *Server) {
	sort.SliceStable(s.Endpoints, func(i, j int) bool {
		if s.Endpoints[i].Exposed != s.Endpoints[j].Exposed {
			return s.Endpoints[i].Exposed
		}
		return s.Endpoints[i].Port < s.Endpoints[j].Port
	})
	if len(s.Endpoints) == 0 {
		return
	}
	primary := s.Endpoints[0]
	s.Protocol, s.Address, s.Port = primary.Protocol, primary.Address, primary.Port
	s.Label = fmt.Sprintf("%s %s:%d", strings.ToUpper(primary.Protocol), primary.Address, primary.Port)
	if len(s.Endpoints) > 1 {
		s.Label += fmt.Sprintf(" (+%d)", len(s.Endpoints)-1)
	}
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func procValue(pid int, file string, elevated bool) string {
	p := fmt.Sprintf("/proc/%d/%s", pid, file)
	var b []byte
	if elevated {
		out, e := run("/usr/bin/sudo", "-n", "/usr/bin/cat", p)
		if e == nil {
			return out
		}
		return ""
	}
	b, _ = os.ReadFile(p)
	return string(b)
}
func readProc(s *Server, elevated bool) {
	readProcCached(s, elevated, nil)
}

type gitProjectMetadata struct {
	project, branch string
	worktree        bool
	valid           bool
}

func readProcCached(s *Server, elevated bool, gitCache map[string]gitProjectMetadata) {
	cmd := redactProcCommand(procValue(s.PID, "cmdline", elevated))
	s.Command = cmd
	status := procValue(s.PID, "status", elevated)
	uidKnown := false
	for _, line := range strings.Split(status, "\n") {
		f := strings.Fields(line)
		if len(f) > 1 && f[0] == "Uid:" {
			uid, _ := strconv.Atoi(f[1])
			s.UID = &uid
			uidKnown = true
		}
		if len(f) > 1 && f[0] == "PPid:" {
			s.PPID, _ = strconv.Atoi(f[1])
		}
	}
	s.Cgroup = strings.TrimSpace(procValue(s.PID, "cgroup", elevated))
	s.StartTime = statStartTime(procValue(s.PID, "stat", elevated))
	if elevated {
		s.Cwd, _ = run("/usr/bin/sudo", "-n", "/usr/bin/readlink", fmt.Sprintf("/proc/%d/cwd", s.PID))
		s.Cwd = strings.TrimSpace(s.Cwd)
	} else {
		s.Cwd, _ = os.Readlink(fmt.Sprintf("/proc/%d/cwd", s.PID))
	}
	s.Orphan = strings.HasSuffix(s.Cwd, " (deleted)")
	s.Cwd = strings.TrimSuffix(s.Cwd, " (deleted)")
	executable := ""
	if fields := strings.Fields(cmd); len(fields) > 0 {
		executable = filepath.Base(fields[0])
	}
	s.System = uidKnown && s.UID != nil && *s.UID == 0 || systemNames[executable]
	root := s.Cwd
	for root != "" && root != "/" {
		if _, e := os.Stat(filepath.Join(root, ".git")); e == nil {
			if metadata, exists := gitCache[root]; exists {
				if metadata.valid {
					s.Project, s.Branch, s.Worktree = metadata.project, metadata.branch, metadata.worktree
				}
			} else {
				valid := attachGitProject(s, root)
				if gitCache != nil {
					gitCache[root] = gitProjectMetadata{project: s.Project, branch: s.Branch, worktree: s.Worktree, valid: valid}
				}
			}
			break
		}
		root = filepath.Dir(root)
	}
	markCodexWorktree(s)
}

func attachGitProject(s *Server, root string) bool {
	checkoutRaw, checkoutErr := run("git", "-C", root, "rev-parse", "--show-toplevel")
	commonRaw, commonErr := run("git", "-C", root, "rev-parse", "--git-common-dir")
	if checkoutErr != nil || commonErr != nil {
		return false
	}
	checkout := strings.TrimSpace(checkoutRaw)
	common := resolveGitPath(root, strings.TrimSpace(commonRaw))
	s.Project, s.Worktree = classifyGitProject(checkout, common)
	if branchRaw, branchErr := run("git", "-C", root, "branch", "--show-current"); branchErr == nil {
		s.Branch = valueOr(strings.TrimSpace(branchRaw), "HEAD détachée")
	}
	return true
}

func resolveGitPath(root, value string) string {
	if value == "" {
		return filepath.Join(root, ".git")
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(root, value))
}
func classifyGitProject(checkout, common string) (string, bool) {
	worktree := common != "" && common != filepath.Join(checkout, ".git")
	if worktree {
		return filepath.Dir(common), true
	}
	return checkout, false
}
func markCodexWorktree(s *Server) {
	if strings.Contains(s.Cwd, "/.codex/worktrees/") {
		s.Worktree = true
		if s.Project == "" {
			s.Orphan = true
		}
	}
}

func identify(s *Server, containers []dockerContainer) {
	for _, c := range containers {
		for _, p := range c.Ports {
			for _, e := range s.Endpoints {
				if p.Port == e.Port && p.Protocol == e.Protocol {
					s.ServiceName = valueOr(c.Name, "Conteneur Docker")
					s.Runtime = valueOr(c.Image, "Docker")
					s.Manager = "Docker"
					s.Confidence = "certain"
					s.Evidence = []string{"port publié par le conteneur " + c.Name, "image " + c.Image}
					return
				}
			}
		}
	}
	for _, signature := range serviceSignatures {
		if signature.pattern.MatchString(s.Command) {
			s.ServiceName = signature.name
			s.Runtime = signature.runtime
			s.Manager = signature.manager
			s.Confidence = "certain"
			s.Evidence = []string{"signature de commande : " + signature.name}
			return
		}
	}
	unit := ""
	for _, part := range strings.FieldsFunc(s.Cgroup, func(r rune) bool { return r == '/' || r == '\n' }) {
		if strings.HasSuffix(part, ".service") {
			unit = part
			break
		}
	}
	for _, e := range s.Endpoints {
		if v, ok := knownPorts[fmt.Sprintf("%s:%d", e.Protocol, e.Port)]; ok {
			s.ServiceName = v[0]
			s.Runtime = v[1]
			if unit != "" {
				s.Manager = "systemd · " + unit
				s.Evidence = append(s.Evidence, "cgroup systemd : "+unit)
			} else if s.System {
				s.Manager = "service système"
			} else {
				s.Manager = "non déterminé"
			}
			s.Confidence = "probable"
			s.Evidence = append(s.Evidence, fmt.Sprintf("port standard %d/%s", e.Port, e.Protocol))
			if s.PID == 0 {
				s.UnknownReason = "PID masqué par les permissions actuelles"
			}
			return
		}
	}
	if s.ProcessName != "" {
		s.ServiceName = s.ProcessName
		if s.ProcessName == "node" {
			s.ServiceName = "Application Node.js"
			s.Runtime = "Node.js"
		}
		if unit != "" {
			s.Manager = "systemd · " + unit
		} else {
			s.Manager = "processus direct"
		}
		s.Confidence = "partial"
		s.Evidence = []string{}
		if unit != "" {
			s.Evidence = append(s.Evidence, "cgroup systemd : "+unit)
		}
		s.Evidence = append(s.Evidence, "nom du processus : "+s.ProcessName)
		s.UnknownReason = "aucune signature de service reconnue"
		return
	}
	s.ServiceName = "Service non identifié"
	s.Manager = "propriétaire masqué"
	s.Confidence = "unknown"
	s.UnknownReason = "PID masqué par les permissions actuelles"
	s.System = true
	s.Evidence = []string{"socket observé, PID non accessible"}
}

func diagnostic(s Server) string {
	if s.Orphan {
		return "action"
	}
	if s.Exposed && (s.PID == 0 || s.Confidence == "unknown" || s.Confidence == "partial") {
		return "review"
	}
	return "normal"
}
func visible(s Server, f filters) bool {
	if !f.udp && s.Protocol == "udp" {
		return false
	}
	if f.scope == "Attention" && diagnostic(s) == "normal" {
		return false
	}
	if f.scope == "Projets" && s.Project == "" {
		return false
	}
	if f.scope == "Système" && !s.System {
		return false
	}
	q := strings.ToLower(f.query)
	return q == "" || strings.Contains(strings.ToLower(searchHaystack(s)), q)
}
func searchHaystack(s Server) string {
	parts := []string{s.ID, strconv.Itoa(s.Port), s.ServiceName, s.Runtime, s.Manager, s.Project, s.Branch, s.Cwd, s.Command, s.ProcessName}
	if s.PID > 0 {
		parts = append(parts, strconv.Itoa(s.PID))
	}
	parts = append(parts, s.Evidence...)
	return strings.Join(parts, " ")
}
func filtered(items []Server, f filters) []Server {
	r := []Server{}
	for _, s := range items {
		if visible(s, f) {
			r = append(r, s)
		}
	}
	rank := map[string]int{"action": 0, "review": 1, "normal": 2}
	sort.SliceStable(r, func(i, j int) bool {
		left, right := rank[diagnostic(r[i])], rank[diagnostic(r[j])]
		if left != right {
			return left < right
		}
		switch f.sortMode {
		case "port":
			return r[i].Port < r[j].Port
		case "project":
			return strings.ToLower(context(r[i])) < strings.ToLower(context(r[j]))
		default:
			return false
		}
	})
	return r
}
func compactName(s Server) string {
	n := s.ServiceName
	if n == "Application Node.js" {
		return "App Node.js"
	}
	if n == "Service non identifié" {
		return "Non identifié"
	}
	return n
}
func context(s Server) string {
	if s.Orphan && s.Worktree {
		return "Worktree disparu"
	}
	if s.Orphan {
		return "Dossier disparu"
	}
	if s.Worktree {
		return filepath.Base(s.Project) + " · worktree"
	}
	if s.Project != "" {
		return filepath.Base(s.Project)
	}
	if s.System {
		return "Système"
	}
	return "Hors projet"
}
func listen(s Server) string {
	where := "loc"
	if s.Exposed {
		where = "ext"
	}
	extra := ""
	if len(s.Endpoints) > 1 {
		extra = fmt.Sprintf(" +%d", len(s.Endpoints)-1)
	}
	return fmt.Sprintf("%d%s %s", s.Port, extra, where)
}
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

type layout struct{ service, context, listening, runtime int }

func tableLayout(w int) layout {
	content := w - 2
	if w < 80 {
		return layout{service: max(18, content-19), listening: 18}
	}
	if w < 120 {
		return layout{service: 23, context: max(21, content-42), listening: 17}
	}
	return layout{service: 26, context: max(28, content-67), listening: 18, runtime: 20}
}
func tableLine(s Server, w int) string {
	l := tableLayout(w)
	service := fmt.Sprintf("%-*s", l.service, clip(compactName(s), l.service))
	listen := fmt.Sprintf("%-*s", l.listening, clip(listen(s), l.listening))
	if l.context == 0 {
		return service + " " + listen
	}
	base := service + " " + fmt.Sprintf("%-*s", l.context, clip(context(s), l.context)) + " " + listen
	if l.runtime > 0 {
		return base + " " + clip(valueOr(s.Runtime, s.Manager), l.runtime)
	}
	return base
}
func tableHeader(w int) string {
	l := tableLayout(w)
	if l.context == 0 {
		return fmt.Sprintf("%-*s %-*s", l.service, "SERVICE", l.listening, "ÉCOUTE")
	}
	base := fmt.Sprintf("%-*s %-*s %-*s", l.service, "SERVICE", l.context, "CONTEXTE", l.listening, "ÉCOUTE")
	if l.runtime > 0 {
		return base + " RUNTIME"
	}
	return base
}
func guidance(s Server) string {
	actions := []string{"Entrée détails"}
	if serverURL(s) != "" {
		actions = append(actions, "o ouvrir", "c copier")
	} else {
		_, kind := developerCopyValue(s)
		actions = append(actions, "c copier "+kind)
	}
	if projectDirectory(s) != "" {
		actions = append(actions, "p projet")
	}
	actionText := strings.Join(actions, " · ")
	if s.Orphan {
		return "Dossier de travail disparu · " + actionText
	}
	if diagnostic(s) == "review" && s.PID == 0 {
		return "Propriétaire masqué · e mieux identifier · " + actionText
	}
	if diagnostic(s) == "review" {
		return "Identification incertaine · " + actionText
	}
	return actionText
}
func help(w int) string {
	groups := []string{
		"↑↓ serveur · Entrée détails",
		"←→ vue · 1–4 accès direct",
		"/ recherche · u UDP · t tri",
		"e sudo · w suivi",
		"h historique · r rafraîchir · ? aide · q quitter",
	}
	lines := []string{}
	line := ""
	for _, group := range groups {
		candidate := group
		if line != "" {
			candidate = line + "      " + group
		}
		if utf8.RuneCountInString(candidate) <= w {
			line = candidate
			continue
		}
		if line != "" {
			lines = append(lines, line)
		}
		line = group
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func helpForSize(w, h int) string {
	if h <= 16 {
		return "↑↓ sélection · Entrée détails · ←→ vue · 1–4\n/ cherche · u UDP · e sudo · r actualise · ? aide · q quitte"
	}
	return help(w)
}

func helpScreenText(h int) string {
	if h <= 18 {
		return "\x1b[2J\x1b[H\x1b[1mAide Super Socket Scout (sss)\x1b[0m\n\n" +
			"↑/↓ sélectionner · ←/→ ou 1–4 changer de vue\nEntrée détails · Échap/b retour\n\n" +
			"/ rechercher · u UDP · t trier · w suivi\ne sudo · h historique · r rafraîchir\n" +
			"c copier · o ouvrir · p projet\nq quitter · ? fermer l’aide\n"
	}
	return "\x1b[2J\x1b[H\x1b[1mAide Super Socket Scout (sss)\x1b[0m\n\n" +
		"NAVIGATION\n  ↑/↓       sélectionner un serveur\n  ←/→, 1–4 changer de vue\n  Entrée    afficher les détails\n\n" +
		"FILTRES ET SUIVI\n  /         rechercher par port, projet, PID ou commande\n  u         afficher ou masquer UDP\n  t         trier par priorité, port ou projet\n  w         activer le suivi automatique toutes les 2 s\n  e         améliorer l’identification avec sudo\n\n" +
		"SESSION\n  h         consulter l’historique local\n  r         rafraîchir maintenant\n  q         quitter\n\nÉchap, b ou ? pour revenir\n"
}

func helpScreen() {
	fmt.Print(helpScreenText(height()))
}

func historyScreen(path string) string {
	state, err := loadHistory(path)
	if err != nil {
		return "Historique indisponible : " + err.Error() + "\n"
	}
	return renderHistory(state, false) + "\nÉchap, b ou h pour revenir\n"
}

func status(items []Server, jsonOut bool) {
	if jsonOut {
		b, _ := json.MarshalIndent(items, "", "  ")
		fmt.Println(string(b))
		return
	}
	fmt.Print(renderStatusText(items, width()))
}

func renderStatusText(items []Server, w int) string {
	var out strings.Builder
	a, review, normal, projects, systems := 0, 0, 0, 0, 0
	for _, s := range items {
		if s.Project != "" && !s.System {
			projects++
		}
		if s.System {
			systems++
		}
		switch diagnostic(s) {
		case "action":
			a++
		case "review":
			review++
		default:
			normal++
		}
	}
	fmt.Fprintf(&out, "\x1b[2J\x1b[H\x1b[1mSupervision réseau\x1b[0m  actualisé %s\n\n", time.Now().Format("15:04:05"))
	if a+review == 0 {
		out.WriteString("\x1b[32m✓ Rien ne nécessite votre attention\x1b[0m\n")
	} else {
		fmt.Fprintf(&out, "%d action requise   \x1b[33m? %d à vérifier\x1b[0m   \x1b[32m✓ %d sans anomalie\x1b[0m\n", a, review, normal)
	}
	fmt.Fprintf(&out, "%d serveurs de projet · %d services système · %d services au total\n", projects, systems, len(items))
	out.WriteString("\n" + tabLines(filters{scope: "Tous", udp: true}, w))
	if len(items) > 0 {
		fmt.Fprintf(&out, "   %d/%d affichés · UDP affiché · ident. standard\n", len(items), len(items))
	} else {
		out.WriteString("   UDP affiché · ident. standard\n")
	}
	ordered := filtered(items, filters{scope: "Tous", udp: true})
	labels := map[string]string{"action": "ORPHELINS — ACTION REQUISE", "review": "À VÉRIFIER", "normal": "SANS ANOMALIE"}
	colors := map[string]string{"action": "31", "review": "33", "normal": "32"}
	if len(ordered) == 0 {
		out.WriteString("\n  Aucun serveur ne correspond aux filtres.\n")
	}
	for _, kind := range []string{"action", "review", "normal"} {
		group := []Server{}
		for _, s := range ordered {
			if diagnostic(s) == kind {
				group = append(group, s)
			}
		}
		if len(group) == 0 {
			continue
		}
		fmt.Fprintf(&out, "\n\x1b[1m\x1b[%sm%s  (%d)\x1b[0m\x1b[0m\n\x1b[2m  %s\x1b[0m\n", colors[kind], labels[kind], len(group), tableHeader(w))
		for _, s := range group {
			fmt.Fprintf(&out, "  %s\n", tableLine(s, w))
		}
	}
	return out.String()
}

type result struct {
	ID      string `json:"id"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
	Reason  string `json:"reason,omitempty"`
}
type conflict struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}
type action struct {
	ID          string `json:"id"`
	PID         int    `json:"pid"`
	StartTime   string `json:"startTime"`
	Target      Server `json:"target"`
	Description string `json:"description"`
}
type plan struct {
	Risk              string     `json:"risk"`
	ConfirmationToken string     `json:"confirmationToken"`
	Actions           []action   `json:"actions"`
	Conflicts         []conflict `json:"conflicts"`
}
type outcome struct {
	Code    int      `json:"code"`
	Plan    *plan    `json:"plan"`
	Results []result `json:"results"`
	Error   string   `json:"error,omitempty"`
}

func buildPlan(items []Server, ids []string, includeSystem bool) plan {
	byID := map[string]Server{}
	for _, s := range items {
		byID[s.ID] = s
	}
	seen := map[string]bool{}
	p := plan{Risk: "destructive", Actions: []action{}, Conflicts: []conflict{}}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		s, ok := byID[id]
		if !ok {
			p.Conflicts = append(p.Conflicts, conflict{id, "Serveur absent de l'inventaire"})
			continue
		}
		if s.PID == 0 {
			p.Conflicts = append(p.Conflicts, conflict{id, "PID non visible"})
			continue
		}
		if s.StartTime == "" {
			p.Conflicts = append(p.Conflicts, conflict{id, "Identité du processus incomplète"})
			continue
		}
		if s.System && !includeSystem {
			p.Conflicts = append(p.Conflicts, conflict{id, "Processus système protégé (utiliser --include-system)"})
			continue
		}
		command := s.Command
		if command == "" {
			command = valueOr(s.ProcessName, "inconnu")
		}
		p.Actions = append(p.Actions, action{ID: id, PID: s.PID, StartTime: s.StartTime, Target: s, Description: fmt.Sprintf("Arrêter PID %d — %s — %s", s.PID, valueOr(s.Label, listen(s)), command)})
	}
	pids := []int{}
	for _, a := range p.Actions {
		pids = append(pids, a.PID)
	}
	sort.Ints(pids)
	parts := []string{}
	for _, v := range pids {
		parts = append(parts, strconv.Itoa(v))
	}
	p.ConfirmationToken = "STOP:" + strings.Join(parts, ",")
	return p
}
func processStart(pid int) string {
	return statStartTime(procValue(pid, "stat", false))
}
func statStartTime(line string) string {
	i := strings.LastIndex(line, ")")
	if i < 0 {
		return ""
	}
	f := strings.Fields(line[i+1:])
	if len(f) > 19 {
		return f[19]
	}
	return ""
}
func descendants(pid int) []int {
	out, e := run("ps", "-eo", "pid=,ppid=")
	if e != nil {
		return nil
	}
	children := map[int][]int{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		c, _ := strconv.Atoi(f[0])
		p, _ := strconv.Atoi(f[1])
		children[p] = append(children[p], c)
	}
	r := []int{}
	var visit func(int)
	visit = func(p int) {
		for _, c := range children[p] {
			visit(c)
			r = append(r, c)
		}
	}
	visit(pid)
	return r
}

var launcherNames = map[string]bool{"node": true, "npm": true, "npx": true, "pnpm": true, "yarn": true, "bun": true, "sh": true, "bash": true, "tsx": true}

func stopRoot(target Server) int {
	root := target
	for depth := 0; depth < 8 && root.PPID > 1; depth++ {
		parent := Server{PID: root.PPID}
		readProc(&parent, false)
		fields := strings.Fields(parent.Command)
		name := ""
		if len(fields) > 0 {
			name = filepath.Base(fields[0])
		}
		if parent.Cwd == "" || parent.Cwd != root.Cwd || !sameUID(parent.UID, root.UID) || !launcherNames[name] {
			break
		}
		root = parent
	}
	return root.PID
}
func sameUID(a, b *int) bool {
	return a != nil && b != nil && *a == *b
}

type stopDependencies struct {
	processStart func(int) string
	stopRoot     func(Server) int
	descendants  func(int) []int
	kill         func(int, syscall.Signal) error
}

func stopTarget(s Server) result {
	return stopTargetWith(s, stopDependencies{processStart, stopRoot, descendants, syscall.Kill})
}
func stopTargetWith(s Server, deps stopDependencies) result {
	currentStart := deps.processStart(s.PID)
	if s.StartTime == "" || currentStart == "" || currentStart != s.StartTime {
		return result{ID: s.ID, Status: "conflict", Message: "Le PID a changé depuis l'inventaire"}
	}
	root := deps.stopRoot(s)
	pids := append(deps.descendants(root), root)
	for _, pid := range pids {
		if e := deps.kill(pid, syscall.SIGTERM); e != nil && e != syscall.ESRCH {
			return result{ID: s.ID, Status: "failed", Message: e.Error()}
		}
	}
	return result{ID: s.ID, Status: "success", Message: fmt.Sprintf("SIGTERM envoyé à l'arbre %d (%d processus)", root, len(pids))}
}
func executePlan(p plan) []result {
	r := make([]result, 0, len(p.Conflicts)+len(p.Actions))
	for _, c := range p.Conflicts {
		r = append(r, result{ID: c.ID, Status: "conflict", Reason: c.Reason})
	}
	for _, a := range p.Actions {
		r = append(r, stopTarget(a.Target))
	}
	return r
}

func makeStopOutcome(items []Server, ids []string, includeSystem, yes bool, token string, executor func(plan) []result) outcome {
	if len(ids) == 0 {
		return outcome{Code: 3, Plan: nil, Results: []result{}, Error: "stop exige --server ID ou --all"}
	}
	known := map[string]bool{}
	for _, s := range items {
		known[s.ID] = true
	}
	unknown, seen := []string{}, map[string]bool{}
	for _, id := range ids {
		if !known[id] && !seen[id] {
			unknown = append(unknown, id)
			seen[id] = true
		}
	}
	if len(unknown) > 0 {
		return outcome{Code: 3, Plan: nil, Results: []result{}, Error: "Identifiant inconnu : " + strings.Join(unknown, ", ")}
	}
	p := buildPlan(items, ids, includeSystem)
	out := outcome{Plan: &p, Results: []result{}}
	if len(p.Actions) == 0 {
		out.Code = 2
		for _, c := range p.Conflicts {
			out.Results = append(out.Results, result{ID: c.ID, Reason: c.Reason})
		}
		return out
	}
	if !yes || token != p.ConfirmationToken {
		out.Code = 4
		out.Error = "Confirmation requise ou incorrecte"
		return out
	}
	out.Results = executor(p)
	for _, r := range out.Results {
		if r.Status == "failed" {
			out.Code = 1
		} else if r.Status == "conflict" && out.Code == 0 {
			out.Code = 2
		}
	}
	return out
}

type options struct {
	command                                                string
	ids                                                    []string
	ports                                                  []int
	runArgs                                                []string
	all, yes, json, sudo, includeSystem, help, showVersion bool
	token, name, completionShell                           string
}

func parseArgs(args []string) (options, error) {
	o := options{command: "tui"}
	i := 0
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		o.command = args[0]
		i = 1
	}
	if o.command == "completion" {
		if len(args) != 2 {
			return o, errors.New("completion exige bash, zsh ou fish")
		}
		o.completionShell = args[1]
		if _, err := completionScript(o.completionShell); err != nil {
			return o, err
		}
		return o, nil
	}
	for ; i < len(args); i++ {
		switch args[i] {
		case "--":
			if o.command != "run" {
				return o, errors.New("-- est réservé à run")
			}
			o.runArgs = append(o.runArgs, args[i+1:]...)
			i = len(args)
		case "--name":
			if i+1 >= len(args) {
				return o, errors.New("--name exige un nom")
			}
			i++
			o.name = strings.TrimSpace(args[i])
			if o.name == "" {
				return o, errors.New("--name ne peut pas être vide")
			}
		case "--server":
			if i+1 >= len(args) {
				return o, errors.New("--server exige un ID")
			}
			i++
			o.ids = append(o.ids, args[i])
		case "--port":
			if i+1 >= len(args) {
				return o, errors.New("--port exige un numéro")
			}
			i++
			port, err := strconv.Atoi(args[i])
			if err != nil || port < 1 || port > 65535 {
				return o, fmt.Errorf("port invalide : %s", args[i])
			}
			found := false
			for _, existing := range o.ports {
				found = found || existing == port
			}
			if !found {
				o.ports = append(o.ports, port)
			}
		case "--all":
			o.all = true
		case "--yes":
			o.yes = true
		case "--json":
			o.json = true
		case "--sudo":
			o.sudo = true
		case "--include-system":
			o.includeSystem = true
		case "--confirm":
			if i+1 >= len(args) {
				return o, errors.New("--confirm exige un jeton")
			}
			i++
			o.token = args[i]
		case "-h", "--help":
			o.help = true
		case "--version":
			o.showVersion = true
		default:
			return o, fmt.Errorf("option inconnue : %s", args[i])
		}
	}
	if o.command != "tui" && o.command != "status" && o.command != "stop" && o.command != "history" && o.command != "check" && o.command != "run" {
		return o, fmt.Errorf("commande inconnue : %s", o.command)
	}
	if o.sudo && o.command != "status" && o.command != "check" {
		return o, errors.New("--sudo est réservé à status et check")
	}
	if o.command == "check" && len(o.ports) == 0 {
		return o, errors.New("check exige au moins un --port")
	}
	if o.command == "run" && len(o.runArgs) == 0 {
		return o, errors.New("run exige une commande après --")
	}
	if o.command != "check" && len(o.ports) > 0 {
		return o, errors.New("--port est réservé à check")
	}
	if o.command != "run" && o.name != "" {
		return o, errors.New("--name est réservé à run")
	}
	if o.command != "stop" && (len(o.ids) > 0 || o.all || o.yes || o.includeSystem || o.token != "") {
		return o, errors.New("les options d’arrêt sont réservées à stop")
	}
	if o.command == "history" && o.sudo {
		return o, errors.New("history ne nécessite pas sudo")
	}
	if o.command == "run" && (o.json || o.sudo) {
		return o, errors.New("run n’accepte pas --json ou --sudo")
	}
	return o, nil
}

type termios syscall.Termios

var errCancelled = errors.New("interrompu")
var stdin = bufio.NewReader(os.Stdin)

func raw(fd int) (*termios, error) {
	var old termios
	_, _, e := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&old)), 0, 0, 0)
	if e != 0 {
		return nil, e
	}
	n := old
	n.Lflag &^= syscall.ICANON | syscall.ECHO
	n.Cc[syscall.VMIN] = 1
	n.Cc[syscall.VTIME] = 0
	_, _, e = syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&n)), 0, 0, 0)
	if e != 0 {
		return nil, e
	}
	return &old, nil
}
func isTerminal(fd int) bool {
	var state termios
	_, _, e := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&state)), 0, 0, 0)
	return e == 0
}
func restore(fd int, t *termios) {
	syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(t)), 0, 0, 0)
}
func key() string {
	b, e := stdin.ReadByte()
	if e != nil {
		return "eof"
	}
	if b == 27 {
		if !inputReady(35 * time.Millisecond) {
			return "escape"
		}
		b2, e2 := stdin.ReadByte()
		if e2 != nil {
			return "escape"
		}
		if b2 != '[' {
			_ = stdin.UnreadByte()
			return "escape"
		}
		if !inputReady(35 * time.Millisecond) {
			return "escape"
		}
		b3, e3 := stdin.ReadByte()
		if e3 != nil {
			return "escape"
		}
		if b3 == 65 {
			return "up"
		}
		if b3 == 66 {
			return "down"
		}
		if b3 == 67 {
			return "right"
		}
		if b3 == 68 {
			return "left"
		}
	}
	return string([]byte{b})
}

func keyWithTimeout(timeout time.Duration) string {
	if timeout > 0 && !inputReady(timeout) {
		return "tick"
	}
	return key()
}

func inputReady(timeout time.Duration) bool {
	if stdin.Buffered() > 0 {
		return true
	}
	fd := int(os.Stdin.Fd())
	var set syscall.FdSet
	set.Bits[fd/64] |= 1 << uint(fd%64)
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	n, _ := syscall.Select(fd+1, &set, nil, nil, &tv)
	return n > 0
}

func restoreOnSignals(old *termios) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		s := <-ch
		signal.Stop(ch)
		handleTerminalSignal(s, func() { restore(int(os.Stdin.Fd()), old) }, os.Exit)
	}()
	return func() {
		signal.Stop(ch)
	}
}

func handleTerminalSignal(s os.Signal, restoreTerminal func(), exit func(int)) {
	restoreTerminal()
	code := 1
	if value, ok := s.(syscall.Signal); ok {
		code = 128 + int(value)
	}
	exit(code)
}
func width() int {
	w := 100
	var z struct{ R, C, X, Y uint16 }
	_, _, e := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(os.Stdout.Fd()), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&z)), 0, 0, 0)
	if e == 0 && z.C > 0 {
		w = int(z.C)
	}
	if w > 130 {
		w = 130
	}
	if w < 50 {
		w = 50
	}
	return w
}
func height() int {
	h := 24
	var z struct{ R, C, X, Y uint16 }
	_, _, e := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(os.Stdout.Fd()), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&z)), 0, 0, 0)
	if e == 0 && z.R > 0 {
		h = int(z.R)
	}
	if h < 12 {
		h = 12
	}
	return h
}
func tabs(f filters, w int) {
	fmt.Print(tabLines(f, w))
}
func tabLines(f filters, w int) string {
	names := make([]string, 4)
	displayNames := make([]string, 4)
	active := 0
	for i, n := range scopes {
		names[i] = fmt.Sprintf("%d %s", i+1, n)
		displayNames[i] = names[i]
		if n == f.scope {
			names[i] = "|" + names[i] + "|"
			displayNames[i] = "\x1b[36m|\x1b[0m" + displayNames[i] + "\x1b[36m|\x1b[0m"
			active = i
		}
	}
	line := "  " + strings.Join(displayNames, "   ")
	start := 2
	for i := 0; i < active; i++ {
		start += utf8.RuneCountInString(names[i]) + 3
	}
	activeWidth := utf8.RuneCountInString(names[active])
	bar := strings.Repeat("─", start) + strings.Repeat(" ", activeWidth) + strings.Repeat("─", max(0, w-start-activeWidth))
	return line + "\n\x1b[36m" + bar + "\x1b[0m\n"
}
func selectionIndex(items []Server, selectedID string) int {
	for i, item := range items {
		if item.ID == selectedID {
			return i
		}
	}
	return 0
}

func viewportCost(items []Server) int {
	cost, previous := 0, ""
	for _, item := range items {
		kind := diagnostic(item)
		if kind != previous {
			cost += 2 // titre de section et en-tête de colonnes
			previous = kind
		}
		cost++
	}
	return cost
}

func viewport(items []Server, selected, budget int) ([]Server, int, int) {
	if len(items) == 0 {
		return nil, 0, 0
	}
	budget = max(3, budget)
	selected = max(0, min(selected, len(items)-1))
	maxItems := max(1, budget-2)
	start := max(0, selected-maxItems/2)
	end := min(len(items), start+maxItems)
	start = max(0, end-maxItems)
	for viewportCost(items[start:end]) > budget && end-start > 1 {
		if selected-start >= end-selected-1 {
			start++
		} else {
			end--
		}
	}
	return items[start:end], selected - start, start
}

func listBudget(h, w int) int {
	helpLines := strings.Count(helpForSize(w, h), "\n") + 1
	return max(3, h-(13+helpLines))
}

func renderLoading(message string) {
	fmt.Printf("\x1b[2J\x1b[H\x1b[1mSupervision réseau\x1b[0m\n\n\x1b[36m⟳ %s\x1b[0m\n", message)
}

func render(items []Server, f filters, selected int) {
	fmt.Print(renderScreen(items, f, selected, width(), height(), time.Now()))
}

func renderScreen(items []Server, f filters, selected, w, h int, now time.Time) string {
	var out strings.Builder
	fmt.Fprintf(&out, "\x1b[2J\x1b[H\x1b[1mSupervision réseau\x1b[0m  actualisé %s", now.Format("15:04:05"))
	if f.watch {
		out.WriteString(" · \x1b[36msuivi 2 s\x1b[0m")
	}
	out.WriteString("\n\n")
	a, v, n, projects, systems := 0, 0, 0, 0, 0
	for _, s := range items {
		if s.Project != "" && !s.System {
			projects++
		}
		if s.System {
			systems++
		}
		switch diagnostic(s) {
		case "action":
			a++
		case "review":
			v++
		default:
			n++
		}
	}
	if a+v == 0 {
		out.WriteString("\x1b[32m✓ Rien ne nécessite votre attention\x1b[0m\n")
	} else {
		fmt.Fprintf(&out, "%d action requise   \x1b[33m? %d à vérifier\x1b[0m   \x1b[32m✓ %d sans anomalie\x1b[0m\n", a, v, n)
	}
	fmt.Fprintf(&out, "%d serveurs de projet · %d services système · %d services au total\n\n", projects, systems, len(items))
	out.WriteString(tabLines(f, w))
	list := filtered(items, f)
	visibleList, visibleSelected, visibleStart := viewport(list, selected, listBudget(h, w))
	total := len(filtered(items, filters{scope: f.scope, udp: true, elevated: f.elevated}))
	count := ""
	if total > 0 {
		count = fmt.Sprintf("%d/%d affichés · ", len(list), total)
	}
	search := ""
	if f.query != "" {
		search = " · recherche « " + f.query + " »"
	}
	sortLabel := map[string]string{"port": "port", "project": "projet"}[f.sortMode]
	if sortLabel == "" {
		sortLabel = "priorité"
	}
	fmt.Fprintf(&out, "   %sUDP %s · ident. %s · tri %s%s\n", count, map[bool]string{true: "affiché", false: "masqué"}[f.udp], map[bool]string{true: "élevée", false: "standard"}[f.elevated], sortLabel, search)
	if len(list) == 0 {
		if f.scope == "Attention" {
			out.WriteString("\n  4 Tous les services · u Inclure UDP · / Rechercher\n")
		} else {
			out.WriteString("\n  Aucun serveur dans cette vue · / Modifier la recherche\n")
		}
	}
	labels := map[string]string{"action": "ORPHELINS — ACTION REQUISE", "review": "À VÉRIFIER", "normal": "SANS ANOMALIE"}
	colors := map[string]string{"action": "31", "review": "33", "normal": "32"}
	index := 0
	for _, kind := range []string{"action", "review", "normal"} {
		count, visibleCount := 0, 0
		for _, s := range list {
			if diagnostic(s) == kind {
				count++
			}
		}
		for _, s := range visibleList {
			if diagnostic(s) == kind {
				visibleCount++
			}
		}
		if visibleCount == 0 {
			continue
		}
		fmt.Fprintf(&out, "\x1b[%sm%s (%d)\x1b[0m\n  %s\n", colors[kind], labels[kind], count, tableHeader(w))
		for _, s := range visibleList {
			if diagnostic(s) != kind {
				continue
			}
			p, suffix := "  ", ""
			if index == visibleSelected {
				p, suffix = "\x1b[1;36m› ", "\x1b[0m"
			}
			fmt.Fprintf(&out, "%s%s%s\n", p, tableLine(s, w), suffix)
			index++
		}
	}
	if len(visibleList) < len(list) {
		fmt.Fprintf(&out, "  %d–%d / %d · ↑↓ pour parcourir\n", visibleStart+1, visibleStart+len(visibleList), len(list))
	}
	out.WriteString(strings.Repeat("─", w) + "\n")
	if len(list) > 0 {
		message := guidance(list[selected])
		if f.notice != "" {
			message = f.notice
		}
		out.WriteString(clip(message, w) + "\n")
	}
	out.WriteString(helpForSize(w, h) + "\n")
	return out.String()
}
func promptLine(old *termios, label string) string {
	restore(int(os.Stdin.Fd()), old)
	fmt.Print("\n", label)
	v, _ := stdin.ReadString('\n')
	raw(int(os.Stdin.Fd()))
	return strings.TrimSpace(v)
}
func wrapDetailLine(line string, width int) []string {
	width = max(1, width)
	runes := []rune(line)
	if len(runes) <= width {
		return []string{line}
	}
	lines := []string{}
	for len(runes) > 0 {
		end := min(width, len(runes))
		lines = append(lines, string(runes[:end]))
		runes = runes[end:]
		if len(runes) > 0 {
			runes = append([]rune("  "), runes...)
		}
	}
	return lines
}

func detailContent(s Server, notice string, width int) []string {
	lines := []string{fmt.Sprintf("%s — %s %s:%d", s.ServiceName, strings.ToUpper(s.Protocol), s.Address, s.Port)}
	pid := "masqué"
	if s.PID > 0 {
		pid = strconv.Itoa(s.PID)
	}
	lines = append(lines,
		"Identification: "+s.Confidence,
		"Gestionnaire: "+valueOr(s.Manager, "inconnu"),
		"Runtime: "+valueOr(s.Runtime, "inconnu"),
		"PID: "+pid,
		"Commande: "+valueOr(s.Command, "inaccessible"),
		"Projet: "+valueOr(s.Project, "aucun"),
	)
	endpoints := []string{}
	for _, e := range s.Endpoints {
		endpoints = append(endpoints, fmt.Sprintf("%s://%s:%d", e.Protocol, e.Address, e.Port))
	}
	lines = append(lines,
		"Écoutes: "+strings.Join(endpoints, ", "),
		"Branche: "+valueOr(s.Branch, "—"),
		"Dossier: "+valueOr(s.Cwd, "—"),
		"État: "+detailFlags(s),
	)
	if url := serverURL(s); url != "" {
		lines = append(lines, "URL: "+url)
	}
	if s.Orphan {
		cause := "dossier de travail disparu"
		if s.Worktree {
			cause = "worktree ou dossier de travail disparu"
		}
		lines = append(lines, "Cause: "+cause)
	}
	if s.UnknownReason != "" {
		lines = append(lines, "Limite: "+s.UnknownReason)
	}
	lines = append(lines, "Preuves:")
	for _, e := range s.Evidence {
		lines = append(lines, " • "+e)
	}
	if notice != "" {
		lines = append(lines, "", notice)
	}
	wrapped := []string{}
	for _, line := range lines {
		wrapped = append(wrapped, wrapDetailLine(line, width)...)
	}
	return wrapped
}

func detailActions(s Server) string {
	actions := []string{"Esc/b ←", "c copie"}
	if serverURL(s) != "" {
		actions = append(actions, "o ouvre")
	}
	if projectDirectory(s) != "" {
		actions = append(actions, "p projet")
	}
	if canStop(s) {
		actions = append(actions, "s arrêt")
	}
	return strings.Join(actions, " · ")
}

func detailScreen(s Server, notice string, width, height, offset int) (string, int) {
	content := detailContent(s, notice, width)
	available := max(1, height-1)
	needsScroll := len(content) > available
	if needsScroll {
		available = max(1, available-1)
	}
	maxOffset := max(0, len(content)-available)
	offset = max(0, min(offset, maxOffset))
	end := min(len(content), offset+available)
	var out strings.Builder
	out.WriteString("\x1b[2J\x1b[H")
	for index, line := range content[offset:end] {
		if offset+index == 0 {
			out.WriteString("\x1b[1m" + line + "\x1b[0m\n")
		} else {
			out.WriteString(line + "\n")
		}
	}
	if needsScroll {
		fmt.Fprintf(&out, "%d–%d / %d · ↑↓ parcourir\n", offset+1, end, len(content))
	}
	out.WriteString(clip(detailActions(s), width) + "\n")
	return out.String(), maxOffset
}

func details(s Server, notice string, offset int) int {
	screen, maxOffset := detailScreen(s, notice, width(), height(), offset)
	fmt.Print(screen)
	return maxOffset
}

func detailFlags(s Server) string {
	flags := []string{map[bool]string{true: "EXPOSÉ", false: "LOCAL"}[s.Exposed]}
	if s.System {
		flags = append(flags, "SYSTÈME")
	}
	if s.Worktree {
		flags = append(flags, "WORKTREE")
	}
	if s.Orphan {
		flags = append(flags, "ORPHELIN")
	}
	return strings.Join(flags, " ")
}
func confirmInteractive(old *termios, s Server) bool {
	restore(int(os.Stdin.Fd()), old)
	fmt.Printf("\nPlan d'arrêt :\n - Arrêter PID %d — %s\nConfirmer l'arrêt du PID %d ? [y/N] ", s.PID, s.Command, s.PID)
	v, _ := stdin.ReadString('\n')
	raw(int(os.Stdin.Fd()))
	return strings.EqualFold(strings.TrimSpace(v), "y") || strings.EqualFold(strings.TrimSpace(v), "yes") || strings.EqualFold(strings.TrimSpace(v), "o") || strings.EqualFold(strings.TrimSpace(v), "oui")
}
func tui() error {
	return tuiWith(newServerOrchestrator(linuxServerAdapter{}))
}

func tuiWith(orchestrator serverOrchestrator) error {
	if !isTerminal(int(os.Stdin.Fd())) || !isTerminal(int(os.Stdout.Fd())) {
		return errors.New("la TUI exige un terminal")
	}
	old, e := raw(int(os.Stdin.Fd()))
	if e != nil {
		return errors.New("la TUI exige un terminal")
	}
	defer restore(int(os.Stdin.Fd()), old)
	stopSignals := restoreOnSignals(old)
	defer stopSignals()
	preferenceFile, preferencePathErr := preferencesPath()
	historyFilePath, historyPathErr := historyPath()
	prefs := preferences{Scope: "Attention"}
	preferenceLoadErr := preferencePathErr
	if preferencePathErr == nil {
		prefs, preferenceLoadErr = loadPreferences(preferenceFile)
	}
	f := filters{scope: prefs.Scope, udp: prefs.ShowUDP, sortMode: prefs.SortMode, watch: prefs.Watch}
	if preferenceLoadErr != nil {
		f.notice = "✗ Préférences ignorées : " + preferenceLoadErr.Error()
	}
	sel := 0
	selectedID := ""
	renderLoading("Inventaire en cours…")
	items, e := orchestrator.Discover(f.elevated)
	if e != nil {
		return e
	}
	if historyPathErr == nil {
		_, _ = recordInventory(historyFilePath, items, time.Now())
	}
	recordChanges := func() {
		if historyPathErr != nil {
			return
		}
		delta, historyErr := recordInventory(historyFilePath, items, time.Now())
		if historyErr != nil {
			f.notice = "✗ Historique non mis à jour : " + historyErr.Error()
		} else if message := historyDeltaMessage(delta); message != "" {
			f.notice = message
		}
	}
	for {
		list := filtered(items, f)
		if selectedID != "" {
			sel = selectionIndex(list, selectedID)
		}
		if len(list) == 0 {
			sel, selectedID = 0, ""
		} else {
			selectedID = list[sel].ID
		}
		render(items, f, sel)
		wait := time.Duration(0)
		if f.watch {
			wait = 2 * time.Second
		}
		k := keyWithTimeout(wait)
		f.notice = ""
		switch k {
		case "eof":
			return errCancelled
		case "\x03":
			return errCancelled
		case "q":
			return nil
		case "r":
			renderLoading("Actualisation en cours…")
			items, e = orchestrator.Discover(f.elevated)
			if e != nil {
				return e
			}
			recordChanges()
		case "tick":
			items, e = orchestrator.Discover(f.elevated)
			if e != nil {
				return e
			}
			recordChanges()
		case "w":
			f.watch = !f.watch
			persistPreferences(preferenceFile, &f)
		case "u":
			f.udp = !f.udp
			persistPreferences(preferenceFile, &f)
		case "t":
			switch f.sortMode {
			case "":
				f.sortMode = "port"
			case "port":
				f.sortMode = "project"
			default:
				f.sortMode = ""
			}
			persistPreferences(preferenceFile, &f)
		case "c":
			if len(list) > 0 {
				f.notice = copyServerValue(list[sel])
			}
		case "o":
			if len(list) > 0 {
				f.notice = openServer(list[sel])
			}
		case "p":
			if len(list) > 0 {
				f.notice = openProject(list[sel])
			}
		case "?":
			for {
				helpScreen()
				helpKey := key()
				if helpKey == "\x03" || helpKey == "eof" {
					return errCancelled
				}
				if helpKey == "escape" || helpKey == "b" || helpKey == "?" {
					break
				}
			}
		case "h":
			for {
				fmt.Print("\x1b[2J\x1b[H" + historyScreen(historyFilePath))
				historyKey := key()
				if historyKey == "\x03" || historyKey == "eof" {
					return errCancelled
				}
				if historyKey == "escape" || historyKey == "b" || historyKey == "h" {
					break
				}
			}
		case "/":
			f.query = promptLine(old, "Recherche (vide pour effacer) : ")
		case "left":
			for i, n := range scopes {
				if n == f.scope {
					f.scope = scopes[(i+3)%4]
					break
				}
			}
			persistPreferences(preferenceFile, &f)
		case "right":
			for i, n := range scopes {
				if n == f.scope {
					f.scope = scopes[(i+1)%4]
					break
				}
			}
			persistPreferences(preferenceFile, &f)
		case "1", "2", "3", "4":
			i, _ := strconv.Atoi(k)
			f.scope = scopes[i-1]
			persistPreferences(preferenceFile, &f)
		case "e":
			if f.elevated {
				f.elevated = false
			} else {
				restore(int(os.Stdin.Fd()), old)
				fmt.Println("\nAuthentification sudo pour une collecte en lecture seule…")
				_, e = runTimeout(120*time.Second, "/usr/bin/sudo", "-v")
				if e == nil {
					fmt.Println("✓ Identification élevée activée pour cette session")
				} else {
					fmt.Println("✗ Authentification sudo refusée")
				}
				var rawErr error
				old, rawErr = raw(int(os.Stdin.Fd()))
				if rawErr != nil {
					return errors.New("impossible de réactiver le mode terminal")
				}
				f.elevated = e == nil
			}
			renderLoading("Nouvel inventaire en cours…")
			items, e = orchestrator.Discover(f.elevated)
			if e != nil {
				return e
			}
			recordChanges()
		case "up":
			if sel > 0 {
				sel--
				selectedID = list[sel].ID
			}
		case "down":
			if sel+1 < len(list) {
				sel++
				selectedID = list[sel].ID
			}
		case "\r", "\n":
			if len(list) > 0 {
				detailNotice := ""
				detailOffset := 0
				for {
					maxDetailOffset := details(list[sel], detailNotice, detailOffset)
					detailKey := key()
					if detailKey == "\x03" || detailKey == "eof" {
						return errCancelled
					}
					if detailKey == "escape" || detailKey == "b" || detailKey == "left" || detailKey == "\r" || detailKey == "\n" {
						break
					}
					if detailKey == "up" {
						detailOffset = max(0, detailOffset-1)
						continue
					}
					if detailKey == "down" {
						detailOffset = min(maxDetailOffset, detailOffset+1)
						continue
					}
					switch detailKey {
					case "c":
						detailNotice = copyServerValue(list[sel])
						continue
					case "o":
						detailNotice = openServer(list[sel])
						continue
					case "p":
						detailNotice = openProject(list[sel])
						continue
					}
					if detailKey == "s" && canStop(list[sel]) && confirmInteractive(old, list[sel]) {
						r := orchestrator.StopConfirmed(list[sel])
						stopNotice := r.Status + " — " + r.Message
						renderLoading("Vérification après arrêt…")
						items, e = orchestrator.Discover(f.elevated)
						if e != nil {
							return e
						}
						recordChanges()
						if f.notice != "" {
							stopNotice += " · " + f.notice
						}
						f.notice = stopNotice
						break
					}
				}
			}
		}
	}
}
func main() {
	o, e := parseArgs(os.Args[1:])
	if e != nil {
		fmt.Fprintln(os.Stderr, "Erreur :", e)
		os.Exit(3)
	}
	orchestrator := newServerOrchestrator(linuxServerAdapter{})
	if o.showVersion {
		fmt.Println("sss", version)
		return
	}
	if o.help {
		fmt.Println("Usage:\n  sss\n  sss status [--sudo] [--json]\n  sss check --port PORT [--port PORT ...] [--sudo] [--json]\n  sss history [--json]\n  sss run [--name NOM] -- COMMANDE [ARG ...]\n  sss stop (--server ID ... | --all) --yes --confirm STOP:PID[,PID] [--include-system] [--json]\n  sss completion bash|zsh|fish\n  sss --version")
		return
	}
	if o.command == "completion" {
		script, _ := completionScript(o.completionShell)
		fmt.Print(script)
		return
	}
	if o.command == "run" {
		os.Exit(runAssociated(o.name, o.runArgs))
	}
	if o.command == "history" {
		path, err := historyPath()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Historique indisponible:", err)
			os.Exit(1)
		}
		state, err := loadHistory(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Historique indisponible:", err)
			os.Exit(1)
		}
		fmt.Print(renderHistory(state, o.json))
		return
	}
	if o.command == "check" {
		if o.sudo {
			if _, err := runTimeout(120*time.Second, "/usr/bin/sudo", "-v"); err != nil {
				report := unavailablePortChecks(o.ports, errors.New("élévation refusée"))
				fmt.Print(renderPortChecks(report, o.json))
				os.Exit(1)
			}
		}
		items, err := orchestrator.Discover(o.sudo)
		if err != nil {
			report := unavailablePortChecks(o.ports, err)
			fmt.Print(renderPortChecks(report, o.json))
			os.Exit(1)
		}
		report := checkPorts(items, o.ports)
		fmt.Print(renderPortChecks(report, o.json))
		if code := portCheckCode(report); code != 0 {
			os.Exit(code)
		}
		return
	}
	if o.command == "tui" {
		if e := tui(); e != nil {
			if errors.Is(e, errCancelled) {
				os.Exit(130)
			}
			fmt.Fprintln(os.Stderr, "Erreur:", e)
			os.Exit(3)
		}
		return
	}
	if o.command == "status" {
		if o.sudo {
			if _, e := runTimeout(120*time.Second, "/usr/bin/sudo", "-v"); e != nil {
				fmt.Fprintln(os.Stderr, "Élévation refusée")
				os.Exit(1)
			}
		}
		items, e := orchestrator.Discover(o.sudo)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		if path, pathErr := historyPath(); pathErr == nil {
			_, _ = recordInventory(path, items, time.Now())
		}
		status(items, o.json)
		return
	}
	items, e := orchestrator.Discover(false)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	ids := o.ids
	if o.all {
		ids = nil
		for _, s := range items {
			if s.PID > 0 && (o.includeSystem || !s.System) {
				ids = append(ids, s.ID)
			}
		}
	}
	outcome := orchestrator.Stop(items, ids, o.includeSystem, o.yes, o.token)
	if o.json {
		b, _ := json.MarshalIndent(outcome, "", "  ")
		fmt.Println(string(b))
	} else {
		if outcome.Plan != nil {
			fmt.Println("Plan :")
			for _, a := range outcome.Plan.Actions {
				fmt.Println(" -", a.Description)
			}
			fmt.Println("Jeton requis :", outcome.Plan.ConfirmationToken)
		}
		if outcome.Error != "" {
			fmt.Fprintln(os.Stderr, outcome.Error)
		}
		for _, r := range outcome.Results {
			fmt.Printf(" - %s: %s — %s\n", r.ID, r.Status, r.Message)
		}
	}
	os.Exit(outcome.Code)
}
