package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fixture() []Server {
	return []Server{
		{ID: "process:42:99", Label: "TCP 0.0.0.0:5173", PID: 42, StartTime: "99", ServiceName: "Application Node.js", Protocol: "tcp", Port: 5173, Exposed: true, Project: "/repo/app", Confidence: "certain"},
		{ID: "process:1:1", Label: "TCP 0.0.0.0:22", PID: 1, StartTime: "1", ServiceName: "SSH", Protocol: "tcp", Port: 22, Exposed: true, System: true, Confidence: "probable"},
		{ID: "unknown:tcp:7070", Label: "TCP 0.0.0.0:7070", Protocol: "tcp", Port: 7070, Exposed: true, System: true, Confidence: "unknown"},
		{ID: "process:2:2", Label: "UDP 127.0.0.1:5355", Protocol: "udp", Address: "127.0.0.1", Port: 5355, PID: 2, StartTime: "2", ServiceName: "LLMNR", System: true, Confidence: "probable"},
	}
}

type fakeServerAdapter struct {
	items    []Server
	err      error
	discover int
	execute  int
}

func (f *fakeServerAdapter) Discover(bool) ([]Server, error) {
	f.discover++
	return append([]Server(nil), f.items...), f.err
}

func TestOrchestratorPropagatesDiscoveryFailureWithoutExecution(t *testing.T) {
	adapter := &fakeServerAdapter{err: errors.New("ss indisponible")}
	orchestrator := newServerOrchestrator(adapter)
	if _, err := orchestrator.Discover(false); err == nil || adapter.execute != 0 {
		t.Fatalf("erreur de découverte masquée ou mutation inattendue: %v %#v", err, adapter)
	}
}
func (f *fakeServerAdapter) BuildPlan(items []Server, ids []string, includeSystem bool) plan {
	return buildPlan(items, ids, includeSystem)
}
func (f *fakeServerAdapter) Execute(p plan) []result {
	f.execute++
	return []result{{ID: p.Actions[0].ID, Status: "success"}}
}

func TestOrchestratorKeepsDiscoveryPlanningAndExecutionSeparate(t *testing.T) {
	adapter := &fakeServerAdapter{items: fixture()}
	orchestrator := newServerOrchestrator(adapter)
	items, err := orchestrator.Discover(false)
	if err != nil || adapter.discover != 1 {
		t.Fatalf("découverte non déléguée: %v %#v", err, adapter)
	}
	out := orchestrator.Stop(items, []string{"process:42:99"}, false, false, "")
	if out.Code != 4 || adapter.execute != 0 {
		t.Fatalf("confirmation contournée: %#v exécutions=%d", out, adapter.execute)
	}
	out = orchestrator.Stop(items, []string{"process:42:99"}, false, true, "STOP:42")
	if out.Code != 0 || adapter.execute != 1 {
		t.Fatalf("plan confirmé non exécuté: %#v exécutions=%d", out, adapter.execute)
	}
	if result := orchestrator.StopConfirmed(items[1]); result.Status != "conflict" || adapter.execute != 1 {
		t.Fatalf("service système interactif non protégé: %#v exécutions=%d", result, adapter.execute)
	}
}

func TestParseArgsStop(t *testing.T) {
	o, err := parseArgs([]string{"stop", "--server", "process:42:99", "--yes", "--confirm", "STOP:42"})
	if err != nil || o.command != "stop" || !o.yes || o.token != "STOP:42" || len(o.ids) != 1 {
		t.Fatalf("options invalides: %#v %v", o, err)
	}
	if _, err := parseArgs([]string{"stop", "--sudo"}); err == nil {
		t.Fatal("--sudo doit être refusé pour stop")
	}
}

func TestParseArgsCheckValidatesAndDeduplicatesPorts(t *testing.T) {
	o, err := parseArgs([]string{"check", "--port", "5173", "--port", "5173", "--port", "8000", "--json"})
	if err != nil || !reflect.DeepEqual(o.ports, []int{5173, 8000}) || !o.json {
		t.Fatalf("options check invalides: %#v %v", o, err)
	}
	for _, args := range [][]string{{"check"}, {"check", "--port", "0"}, {"check", "--port", "70000"}, {"check", "--port", "abc"}} {
		if _, err := parseArgs(args); err == nil {
			t.Fatalf("arguments invalides acceptés: %v", args)
		}
	}
}

func TestParseArgsRunKeepsCommandArgumentsSeparate(t *testing.T) {
	o, err := parseArgs([]string{"run", "--name", "API locale", "--", "npm", "run", "dev", "--", "--port", "5173"})
	if err != nil || o.name != "API locale" || !reflect.DeepEqual(o.runArgs, []string{"npm", "run", "dev", "--", "--port", "5173"}) {
		t.Fatalf("commande run altérée: %#v %v", o, err)
	}
	if _, err := parseArgs([]string{"run", "--name", "API"}); err == nil {
		t.Fatal("run sans commande doit être refusé")
	}
}

func TestCompletionAndVersionOptions(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		o, err := parseArgs([]string{"completion", shell})
		if err != nil || o.completionShell != shell {
			t.Fatalf("completion %s invalide: %#v %v", shell, o, err)
		}
		script, err := completionScript(shell)
		if err != nil || !strings.Contains(script, "server-watch") {
			t.Fatalf("script %s invalide: %q %v", shell, script, err)
		}
	}
	if _, err := parseArgs([]string{"completion", "powershell"}); err == nil {
		t.Fatal("un shell inconnu doit être refusé")
	}
	o, err := parseArgs([]string{"--version"})
	if err != nil || !o.showVersion {
		t.Fatalf("--version non reconnu: %#v %v", o, err)
	}
}

func TestBuildPlanProtectsSystemAndBuildsExactToken(t *testing.T) {
	p := buildPlan(fixture(), []string{"process:42:99", "process:1:1", "process:42:99"}, false)
	if len(p.Actions) != 1 || len(p.Conflicts) != 1 || p.ConfirmationToken != "STOP:42" {
		t.Fatalf("plan inattendu: %#v", p)
	}
}

func TestFiltersAndLabels(t *testing.T) {
	items := fixture()
	if len(filtered(items, filters{scope: "Attention"})) != 1 {
		t.Fatal("la vue Attention doit isoler l'inconnu exposé")
	}
	if len(filtered(items, filters{scope: "Projets"})) != 1 {
		t.Fatal("la vue Projets doit isoler le projet")
	}
	if compactName(items[0]) != "App Node.js" || listen(items[0]) != "5173 ext" {
		t.Fatal("libellés compacts incorrects")
	}
}

func TestSelectionIsRestoredByStableID(t *testing.T) {
	items := fixture()
	selected := items[2].ID
	reordered := []Server{items[2], items[0], items[1]}
	if got := selectionIndex(reordered, selected); got != 0 {
		t.Fatalf("sélection restaurée au mauvais index: %d", got)
	}
	if got := selectionIndex(reordered, "disparu"); got != 0 {
		t.Fatalf("le repli doit être déterministe: %d", got)
	}
}

func TestViewportKeepsSelectionVisible(t *testing.T) {
	items := make([]Server, 20)
	for i := range items {
		items[i] = Server{ID: fmt.Sprintf("server:%d", i)}
	}
	visible, selected, start := viewport(items, 17, 7)
	if viewportCost(visible) > 7 || visible[selected].ID != "server:17" {
		t.Fatalf("sélection hors viewport: start=%d selected=%d items=%#v", start, selected, visible)
	}
	visible, selected, start = viewport(items, 0, 7)
	if start != 0 || selected != 0 || viewportCost(visible) > 7 {
		t.Fatalf("viewport initial incorrect: start=%d selected=%d len=%d", start, selected, len(visible))
	}
}

func TestListBudgetAlwaysLeavesANavigableRow(t *testing.T) {
	for _, tc := range []struct{ height, width int }{{12, 50}, {16, 79}, {24, 80}, {40, 130}} {
		if got := listBudget(tc.height, tc.width); got < 1 {
			t.Fatalf("budget nul à %dx%d: %d", tc.width, tc.height, got)
		}
	}
}

func TestHelpNeverExceedsTerminalWidth(t *testing.T) {
	for _, width := range []int{50, 63, 64, 79, 80, 119, 120, 130} {
		for _, line := range strings.Split(help(width), "\n") {
			if len([]rune(line)) > width {
				t.Fatalf("aide trop large à %d colonnes: %q", width, line)
			}
		}
	}
}

func TestHelpScreenFitsConstrainedHeight(t *testing.T) {
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	for _, height := range []int{12, 16, 18, 24} {
		plain := ansi.ReplaceAllString(helpScreenText(height), "")
		lines := strings.Split(strings.TrimSuffix(plain, "\n"), "\n")
		if len(lines) > height {
			t.Fatalf("aide trop haute pour %d lignes (%d):\n%s", height, len(lines), plain)
		}
	}
}

func TestLoadedScreenHonorsWidthHeightAndKeepsSelectionVisible(t *testing.T) {
	items := make([]Server, 0, 30)
	for i := 0; i < 30; i++ {
		items = append(items, Server{
			ID: fmt.Sprintf("process:%d:%d", 1000+i, i), ServiceName: "Application Node.js avec un libellé long",
			Protocol: "tcp", Address: "127.0.0.1", Port: 3000 + i, PID: 1000 + i,
			Project: "/tmp/projet avec espaces et caractères-é", Worktree: i%2 == 0,
		})
	}
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	for _, size := range []struct{ width, height int }{{79, 16}, {80, 17}, {119, 24}, {120, 25}, {130, 40}} {
		screen := renderScreen(items, filters{scope: "Tous"}, 20, size.width, size.height, time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC))
		plain := ansi.ReplaceAllString(screen, "")
		lines := strings.Split(strings.TrimSuffix(plain, "\n"), "\n")
		if len(lines) > size.height {
			t.Fatalf("écran %dx%d trop haut: %d lignes\n%s", size.width, size.height, len(lines), plain)
		}
		if !strings.Contains(plain, "›") || !strings.Contains(plain, " / 30 · ↑↓") {
			t.Fatalf("sélection ou position absente à %dx%d:\n%s", size.width, size.height, plain)
		}
		if !strings.Contains(plain, "SANS ANOMALIE (30)") {
			t.Fatalf("le compteur de section dépend du viewport à %dx%d:\n%s", size.width, size.height, plain)
		}
		for _, line := range lines {
			if len([]rune(line)) > size.width {
				t.Fatalf("ligne trop large à %dx%d (%d): %q", size.width, size.height, len([]rune(line)), line)
			}
		}
	}
}

func TestConstrainedViewportNeverPrintsAnEmptySectionHeader(t *testing.T) {
	items := fixture()
	items[0].Orphan = true
	screen := renderScreen(items, filters{scope: "Tous", udp: true}, 0, 79, 16, time.Now())
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	plain := ansi.ReplaceAllString(screen, "")
	lines := strings.Split(strings.TrimSuffix(plain, "\n"), "\n")
	if len(lines) > 16 {
		t.Fatalf("viewport multi-section trop haut: %d lignes\n%s", len(lines), plain)
	}
	if strings.Contains(plain, "À VÉRIFIER") || strings.Contains(plain, "SANS ANOMALIE") {
		t.Fatalf("en-tête sans ligne visible:\n%s", plain)
	}
}

func TestEmptyAttentionScreenIsExplicitAndActionable(t *testing.T) {
	screen := renderScreen(nil, filters{scope: "Attention"}, 0, 80, 20, time.Now())
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	plain := ansi.ReplaceAllString(screen, "")
	for _, expected := range []string{"Rien ne nécessite votre attention", "4 Tous les services", "u Inclure UDP", "/ Rechercher", "q quitter"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("état vide non actionnable, %q absent:\n%s", expected, plain)
		}
	}
}

func TestDetailScreenScrollsAndKeepsActionsVisible(t *testing.T) {
	s := fixture()[0]
	s.Command = strings.Repeat("commande-très-longue ", 12)
	s.Cwd = t.TempDir()
	s.Evidence = []string{strings.Repeat("preuve ", 20), "seconde preuve"}
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	for _, size := range []struct{ width, height int }{{50, 12}, {79, 16}, {120, 24}} {
		screen, maxOffset := detailScreen(s, "", size.width, size.height, 0)
		plain := ansi.ReplaceAllString(screen, "")
		lines := strings.Split(strings.TrimSuffix(plain, "\n"), "\n")
		if len(lines) > size.height || (size.height <= 16 && maxOffset == 0) {
			t.Fatalf("détail non scrollable à %dx%d: lignes=%d offset=%d\n%s", size.width, size.height, len(lines), maxOffset, plain)
		}
		footer := lines[len(lines)-1]
		for _, action := range []string{"Esc/b", "c copie", "o ouvre", "p projet", "s arrêt"} {
			if !strings.Contains(footer, action) {
				t.Fatalf("action %q absente du footer %dx%d: %q", action, size.width, size.height, footer)
			}
		}
		if maxOffset > 0 {
			scrolled, _ := detailScreen(s, "", size.width, size.height, maxOffset)
			if scrolled == screen || !strings.Contains(ansi.ReplaceAllString(scrolled, ""), "seconde preuve") {
				t.Fatalf("le défilement n’atteint pas les preuves à %dx%d", size.width, size.height)
			}
		}
	}
}

func TestSortModesPreserveDiagnosticPriority(t *testing.T) {
	items := fixture()
	byPort := filtered(items, filters{scope: "Tous", udp: true, sortMode: "port"})
	if diagnostic(byPort[0]) != "review" || byPort[1].Port > byPort[2].Port {
		t.Fatalf("tri par port ou priorité incorrect: %#v", byPort)
	}
	byProject := filtered(items, filters{scope: "Tous", udp: true, sortMode: "project"})
	if diagnostic(byProject[0]) != "review" {
		t.Fatalf("le diagnostic doit rester prioritaire: %#v", byProject)
	}
}

func TestServerURLOnlyTargetsRecognizedHTTPServices(t *testing.T) {
	cases := []struct {
		server Server
		want   string
	}{
		{Server{Protocol: "tcp", Address: "0.0.0.0", Port: 5173, ServiceName: "Vite"}, "http://127.0.0.1:5173"},
		{Server{Protocol: "tcp", Address: "::", Port: 9443, ServiceName: "portainer"}, "https://[::1]:9443"},
		{Server{Protocol: "tcp", Address: "127.0.0.1", Port: 22, ServiceName: "SSH"}, ""},
		{Server{Protocol: "tcp", Address: "0.0.0.0", Port: 2399, ServiceName: "Application Node.js"}, ""},
		{Server{Protocol: "udp", Address: "0.0.0.0", Port: 5173, ServiceName: "Vite"}, ""},
	}
	for _, tc := range cases {
		if got := serverURL(tc.server); got != tc.want {
			t.Errorf("URL incorrecte pour %#v: obtenu %q, attendu %q", tc.server, got, tc.want)
		}
	}
}

func TestDeveloperCopyValueUsesUsefulFallbacks(t *testing.T) {
	if value, kind := developerCopyValue(Server{Protocol: "tcp", Port: 3000, ServiceName: "App Node.js"}); value != "http://127.0.0.1:3000" || kind != "URL" {
		t.Fatalf("URL non prioritaire: %q %q", value, kind)
	}
	if value, kind := developerCopyValue(Server{Cwd: "/repo/app", PID: 42}); value != "/repo/app" || kind != "dossier" {
		t.Fatalf("dossier non prioritaire: %q %q", value, kind)
	}
	if value, kind := developerCopyValue(Server{PID: 42}); value != "42" || kind != "PID" {
		t.Fatalf("PID non utilisé en repli: %q %q", value, kind)
	}
}

func TestProjectDirectoryRequiresAnExistingDirectory(t *testing.T) {
	directory := t.TempDir()
	if got := projectDirectory(Server{Cwd: directory}); got != directory {
		t.Fatalf("dossier existant non reconnu: %q", got)
	}
	if got := projectDirectory(Server{Cwd: directory + "/absent", Project: directory}); got != directory {
		t.Fatalf("projet de repli non reconnu: %q", got)
	}
}

func TestPreferencesRoundTripAndValidation(t *testing.T) {
	path := t.TempDir() + "/nested/config.json"
	want := preferences{Scope: "Projets", ShowUDP: true, SortMode: "project", Watch: true}
	if err := savePreferences(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadPreferences(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("préférences non restaurées: %#v %v", got, err)
	}
	if err := savePreferences(path, preferences{Scope: "inconnue"}); err == nil {
		t.Fatal("une vue invalide ne doit pas être persistée")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions de configuration incorrectes: %#v %v", info, err)
	}
}

func TestMissingPreferencesUseAttentionDefaults(t *testing.T) {
	got, err := loadPreferences(t.TempDir() + "/missing.json")
	if err != nil || got.Scope != "Attention" || got.ShowUDP || got.Watch {
		t.Fatalf("préférences par défaut incorrectes: %#v %v", got, err)
	}
}

func TestHistoryTracksAppearancesAndDisappearancesWithoutCommands(t *testing.T) {
	path := t.TempDir() + "/history.json"
	first := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	items := []Server{
		{Protocol: "tcp", Address: "127.0.0.1", Port: 3000, ServiceName: "Vite", Project: "/repo/app", Command: "SECRET_SENTINEL"},
		{Protocol: "tcp", Address: "0.0.0.0", Port: 8000, ServiceName: "Uvicorn"},
	}
	delta, err := recordInventory(path, items, first)
	if err != nil || delta.Appeared != 2 || delta.Disappeared != 0 {
		t.Fatalf("premier inventaire incorrect: %#v %v", delta, err)
	}
	second := first.Add(time.Minute)
	delta, err = recordInventory(path, []Server{items[0], {Protocol: "tcp", Address: "127.0.0.1", Port: 5173, ServiceName: "Vite"}}, second)
	if err != nil || delta.Appeared != 1 || delta.Disappeared != 1 {
		t.Fatalf("delta incorrect: %#v %v", delta, err)
	}
	state, err := loadHistory(path)
	if err != nil || len(state.Records) != 3 {
		t.Fatalf("historique incorrect: %#v %v", state, err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "SECRET_SENTINEL") || strings.Contains(string(b), `"command"`) {
		t.Fatalf("la commande a fui dans l’historique: %s", b)
	}
}

func TestHistoryRenderingSupportsJSONAndText(t *testing.T) {
	state := historyFile{Version: historyVersion, Records: []historyRecord{{Key: "one", ServiceName: "Vite", Protocol: "tcp", Address: "127.0.0.1", Port: 5173, Active: true, LastSeen: time.Now()}}}
	if text := renderHistory(state, false); !strings.Contains(text, "Historique des écoutes") || !strings.Contains(text, "actif") {
		t.Fatalf("historique texte incomplet: %s", text)
	}
	if jsonText := renderHistory(state, true); !strings.Contains(jsonText, `"version": 1`) || !strings.Contains(jsonText, `"records"`) {
		t.Fatalf("historique JSON incomplet: %s", jsonText)
	}
}

func TestPortCheckReportsFreeOccupiedAndSecondaryEndpoints(t *testing.T) {
	items := []Server{{ID: "process:42:1", ServiceName: "Vite", Project: "/repo/app", PID: 42, Port: 3000, Endpoints: []Endpoint{{Protocol: "tcp", Port: 3000}, {Protocol: "tcp", Port: 5173}}}}
	report := checkPorts(items, []int{5173, 8000})
	if len(report.Checks) != 2 || report.Checks[0].State != "occupied" || len(report.Checks[0].Owners) != 1 || report.Checks[1].State != "free" {
		t.Fatalf("rapport de ports incorrect: %#v", report)
	}
	if portCheckCode(report) != 2 {
		t.Fatal("un port occupé doit produire un conflit")
	}
	jsonText := renderPortChecks(report, true)
	for _, expected := range []string{`"schemaVersion": 1`, `"state": "occupied"`, `"owners"`} {
		if !strings.Contains(jsonText, expected) {
			t.Fatalf("contrat JSON incomplet (%s): %s", expected, jsonText)
		}
	}
}

func TestUnavailablePortCheckIsNotReportedAsFree(t *testing.T) {
	report := unavailablePortChecks([]int{5173}, errors.New("ss indisponible"))
	if report.Checks[0].State != "unavailable" || !strings.Contains(renderPortChecks(report, false), "détection indisponible") {
		t.Fatalf("indisponibilité masquée: %#v", report)
	}
}

func TestExplicitAssociationNamesDescendantServer(t *testing.T) {
	items := []Server{{PID: 42, ServiceName: "App Node.js", Manager: "serveur de développement", Confidence: "partial"}}
	parents := map[int]int{42: 40, 40: 10, 10: 1}
	applyAssociationRecords(items, []association{{PID: 10, Name: "Storefront", Directory: "/repo/storefront"}}, func(pid int) int { return parents[pid] })
	if items[0].ServiceName != "Storefront" || items[0].Manager != "server-watch run" || items[0].Confidence != "certain" || !strings.Contains(strings.Join(items[0].Evidence, " "), "explicitement") {
		t.Fatalf("association non appliquée: %#v", items[0])
	}
}

func TestAssociationRegistryDoesNotPersistCommandArguments(t *testing.T) {
	path := t.TempDir() + "/associations.json"
	value := association{PID: 42, StartTime: "100", Name: "API", Directory: "/repo/api", StartedAt: time.Now()}
	if err := registerAssociation(path, value); err != nil {
		t.Fatal(err)
	}
	state, err := readAssociations(path)
	if err != nil || len(state.Associations) != 1 || state.Associations[0].Name != "API" {
		t.Fatalf("registre incorrect: %#v %v", state, err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "args") || strings.Contains(string(b), "command") {
		t.Fatalf("le registre contient la commande: %s", b)
	}
	if err := unregisterAssociation(path, 42, "100"); err != nil {
		t.Fatal(err)
	}
	state, _ = readAssociations(path)
	if len(state.Associations) != 0 {
		t.Fatalf("association non retirée: %#v", state)
	}
}

func TestUnknownTargetsNeverBecomeActions(t *testing.T) {
	p := buildPlan(fixture(), []string{"absent", "unknown:tcp:7070"}, true)
	if len(p.Actions) != 0 || len(p.Conflicts) != 2 {
		t.Fatalf("cibles dangereuses acceptées: %#v", p)
	}
}

func TestStatStartTimeHandlesSpacesInProcessName(t *testing.T) {
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[19] = "98765"
	if got := statStartTime("42 (process name) " + strings.Join(fields, " ")); got != "98765" {
		t.Fatalf("startTime incorrect: %q", got)
	}
}

func TestStopJSONUsesPublicContractNames(t *testing.T) {
	p := buildPlan(fixture(), []string{"process:42:99"}, false)
	b, err := json.Marshal(outcome{Code: 4, Plan: &p, Results: []result{}, Error: "confirmation"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, expected := range []string{`"confirmationToken"`, `"actions"`, `"startTime"`, `"results"`} {
		if !strings.Contains(s, expected) {
			t.Fatalf("champ JSON absent %s dans %s", expected, s)
		}
	}
	if strings.Contains(s, `"ConfirmationToken"`) || strings.Contains(s, `"Targets"`) {
		t.Fatalf("noms Go exposés dans le JSON: %s", s)
	}
}

func TestStatusJSONMatchesNodePublicShape(t *testing.T) {
	s := fixture()[0]
	rootUID := 0
	s.UID = &rootUID
	s.Label = "TCP 0.0.0.0:5173"
	s.Status = "available"
	s.DisabledReason = "interne"
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, expected := range []string{`"label"`, `"serviceName"`, `"startTime"`, `"elevated"`, `"uid":0`} {
		if !strings.Contains(got, expected) {
			t.Fatalf("champ public absent %s dans %s", expected, got)
		}
	}
	if strings.Contains(got, `"status"`) || strings.Contains(got, `"disabledReason"`) {
		t.Fatalf("champ interne exposé dans %s", got)
	}
}

func TestConfirmationOutcomeSerializesEmptyResultsArray(t *testing.T) {
	p := buildPlan(fixture(), []string{"process:42:99"}, false)
	b, err := json.Marshal(outcome{Code: 4, Plan: &p, Results: []result{}, Error: "confirmation"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"results":[]`) || !strings.Contains(string(b), `"conflicts":[]`) {
		t.Fatalf("les collections JSON doivent être des tableaux: %s", b)
	}
}

func TestStopRefusesMissingProcessIdentity(t *testing.T) {
	r := stopTarget(Server{ID: "process:999999:unknown", PID: 999999})
	if r.Status != "conflict" {
		t.Fatalf("un arrêt sans startTime doit être refusé: %#v", r)
	}
}

func TestStopRevalidatesIdentityAndSignalsWholeTree(t *testing.T) {
	killed := []int{}
	deps := stopDependencies{
		processStart: func(int) string { return "100" },
		stopRoot:     func(Server) int { return 40 },
		descendants:  func(int) []int { return []int{43, 42} },
		kill: func(pid int, _ syscall.Signal) error {
			killed = append(killed, pid)
			return nil
		},
	}
	r := stopTargetWith(Server{ID: "process:42:100", PID: 42, StartTime: "100"}, deps)
	if r.Status != "success" || !reflect.DeepEqual(killed, []int{43, 42, 40}) {
		t.Fatalf("arrêt incomplet: résultat=%#v signaux=%v", r, killed)
	}
	killed = nil
	deps.processStart = func(int) string { return "101" }
	r = stopTargetWith(Server{ID: "process:42:100", PID: 42, StartTime: "100"}, deps)
	if r.Status != "conflict" || len(killed) != 0 {
		t.Fatalf("un PID remplacé a reçu un signal: résultat=%#v signaux=%v", r, killed)
	}
}

func TestTerminalSignalRestoresBeforeExit(t *testing.T) {
	sequence := []string{}
	handleTerminalSignal(syscall.SIGINT, func() { sequence = append(sequence, "restore") }, func(code int) {
		sequence = append(sequence, fmt.Sprintf("exit:%d", code))
	})
	if !reflect.DeepEqual(sequence, []string{"restore", "exit:130"}) {
		t.Fatalf("ordre de sortie dangereux: %v", sequence)
	}
}

func TestExternalCommandsHaveATimeout(t *testing.T) {
	started := time.Now()
	if _, err := runTimeout(20*time.Millisecond, "sleep", "2"); err == nil {
		t.Fatal("la commande lente aurait dû expirer")
	}
	if time.Since(started) > time.Second {
		t.Fatal("le timeout n'a pas interrompu la commande")
	}
}

func TestSensitiveCommandValuesAreRedactedBeforeRendering(t *testing.T) {
	secret := "SECRET_SENTINEL_42"
	command := "API_TOKEN=" + secret + " tool --password " + secret + " --api-key=" + secret + " https://user:" + secret + "@example.test/path"
	redacted := redactCommand(command)
	if strings.Contains(redacted, secret) {
		t.Fatalf("secret non masqué: %s", redacted)
	}
	if strings.Count(redacted, "[REDACTED]") != 4 {
		t.Fatalf("masquage incomplet: %s", redacted)
	}
	s := fixture()[0]
	s.Command = redacted
	b, _ := json.Marshal(s)
	plan := buildPlan([]Server{s}, []string{s.ID}, false)
	combined := string(b) + fmt.Sprint(plan)
	if strings.Contains(combined, secret) {
		t.Fatalf("secret présent dans une sortie publique: %s", combined)
	}
}

func TestProcArgumentBoundariesProtectSecretsContainingSpaces(t *testing.T) {
	secret := "SECRET SENTINEL WITH SPACES"
	raw := "tool\x00--token\x00" + secret + "\x00--port\x005173\x00"
	redacted := redactProcCommand(raw)
	if strings.Contains(redacted, secret) || strings.Contains(redacted, "SENTINEL") || redacted != "tool --token [REDACTED] --port 5173" {
		t.Fatalf("argument sensible mal masqué: %q", redacted)
	}
}

func TestFinalizeServerSelectsExposedPrimaryEndpoint(t *testing.T) {
	s := Server{Endpoints: []Endpoint{
		{Protocol: "tcp", Address: "127.0.0.1", Port: 3000, Exposed: false},
		{Protocol: "tcp", Address: "0.0.0.0", Port: 5173, Exposed: true},
	}}
	finalizeServer(&s)
	if s.Address != "0.0.0.0" || s.Port != 5173 || s.Label != "TCP 0.0.0.0:5173 (+1)" {
		t.Fatalf("endpoint principal incorrect: %#v", s)
	}
}

func TestResolveGitPathSupportsAbsoluteAndRelativeCommonDir(t *testing.T) {
	if got := resolveGitPath("/worktrees/feature", "/projects/app/.git"); got != "/projects/app/.git" {
		t.Fatalf("chemin absolu altéré: %s", got)
	}
	if got := resolveGitPath("/projects/app", ".git"); got != "/projects/app/.git" {
		t.Fatalf("chemin relatif incorrect: %s", got)
	}
	if project, worktree := classifyGitProject("/worktrees/feature", "/projects/app/.git"); project != "/projects/app" || !worktree {
		t.Fatalf("rattachement worktree incorrect: projet=%s worktree=%v", project, worktree)
	}
	if project, worktree := classifyGitProject("/projects/app", "/projects/app/.git"); project != "/projects/app" || worktree {
		t.Fatalf("dépôt principal mal classé: projet=%s worktree=%v", project, worktree)
	}
	orphan := Server{Cwd: "/home/user/.codex/worktrees/deleted-feature"}
	markCodexWorktree(&orphan)
	if !orphan.Worktree || !orphan.Orphan {
		t.Fatalf("worktree Codex sans projet non marqué orphelin: %#v", orphan)
	}
}

func TestInvalidGitMarkerNeverBecomesProjectMetadata(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(directory+"/.git", []byte("not a gitdir"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Server{}
	if attachGitProject(&s, directory) || s.Project != "" || s.Branch != "" || s.Worktree {
		t.Fatalf("une erreur Git a été utilisée comme métadonnée: %#v", s)
	}
}

func TestIdentificationSignaturesAvoidSubstringFalsePositives(t *testing.T) {
	cases := []struct{ command, want string }{
		{"node /tmp/admin-tools-orchestrator-test/fake-npm.mjs", "Test orchestrateur"},
		{"/repo/node_modules/.bin/vite dev", "Vite"},
		{"node next dev", "Next.js"},
		{"node svelte-kit dev", "SvelteKit"},
		{"tsx watch src/main.ts", "Watcher TypeScript"},
		{"rootlesskit --state-dir=x dockerd", "Docker rootless"},
		{"python -m uvicorn app:main", "Uvicorn"},
		{"python manage.py runserver", "Django"},
		{"adb -L tcp:5037 fork-server server", "ADB server"},
		{"/usr/bin/kdeconnectd", "KDE Connect"},
		{"/usr/lib/systemd/systemd-resolved", "DNS / LLMNR"},
		{"/usr/sbin/sshd -D", "SSH"},
		{"/usr/sbin/cupsd -l", "CUPS"},
	}
	for _, tc := range cases {
		s := Server{Command: tc.command, ProcessName: "process"}
		identify(&s, nil)
		if s.ServiceName != tc.want || s.Confidence != "certain" {
			t.Errorf("signature %q: obtenu %#v", tc.command, s)
		}
	}
	for _, command := range []string{"/usr/bin/invite-worker", "/opt/svelter/service", "rootlesskit helper", "python runserver-helper"} {
		s := Server{Command: command, ProcessName: "worker"}
		identify(&s, nil)
		if s.Confidence == "certain" {
			t.Errorf("faux positif pour %q: %#v", command, s)
		}
	}
}

func TestDockerAndKnownPortIdentification(t *testing.T) {
	if !systemNames["rootlesskit"] {
		t.Fatal("rootlesskit doit rester classé comme service, comme dans la référence Node")
	}
	containers := parseDockerContainers("{\"Names\":\"portainer\",\"Image\":\"portainer/portainer-ce:latest\",\"Ports\":\"0.0.0.0:9443->9443/tcp\"}\nligne invalide")
	if len(containers) != 1 || len(containers[0].Ports) != 1 {
		t.Fatalf("mapping Docker non parsé: %#v", containers)
	}
	docker := Server{Endpoints: []Endpoint{{Protocol: "tcp", Port: 9443}}}
	identify(&docker, containers)
	if docker.ServiceName != "portainer" || docker.Manager != "Docker" || docker.Confidence != "certain" {
		t.Fatalf("conteneur non identifié: %#v", docker)
	}
	known := Server{PID: 10, System: true, Cgroup: "0::/system.slice/ssh.service", Endpoints: []Endpoint{{Protocol: "tcp", Port: 22}}}
	identify(&known, nil)
	if known.ServiceName != "SSH" || known.Manager != "systemd · ssh.service" || known.Confidence != "probable" {
		t.Fatalf("port connu/systemd non identifié: %#v", known)
	}
}

func TestStatusTextUsesSameHierarchyAsTUI(t *testing.T) {
	text := renderStatusText(fixture(), 100)
	for _, expected := range []string{"Supervision réseau", "À VÉRIFIER", "SANS ANOMALIE", "SERVICE", "CONTEXTE", "ÉCOUTE"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("élément de hiérarchie absent %q", expected)
		}
	}
}

func TestViewParityWithNodeReference(t *testing.T) {
	items := fixture()
	payload, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	script := `import { filterServers, diagnosticOf, groupServers } from './src/ui/view-model.mjs';
import { createServerAdapter } from './src/domain/server-adapter.mjs';
import { stopServers } from './src/orchestrator.mjs';
const chunks=[]; for await (const chunk of process.stdin) chunks.push(chunk);
const items=JSON.parse(Buffer.concat(chunks));
const scopes=['attention','projects','system','all'];
const result={diagnostics:Object.fromEntries(items.map(x=>[x.id,diagnosticOf(x)])),views:{}};
for (const scope of scopes) result.views[scope]=groupServers(filterServers(items,{scope,query:'',showUdp:false})).flatMap(g=>g.items).map(x=>x.id);
result.variants={
 udp:filterServers(items,{scope:'all',query:'',showUdp:true}).map(x=>x.id),
 query:filterServers(items,{scope:'all',query:'5173',showUdp:true}).map(x=>x.id),
};
const adapter=createServerAdapter({system:{discover:async()=>items,stop:async target=>({id:target.id,status:'success'})}});
await adapter.discover();
const plan=await adapter.buildPlan(['process:42:99','process:1:1'],{includeSystem:false});
result.plan={token:plan.confirmationToken,actions:plan.actions.map(x=>x.id),conflicts:plan.conflicts.map(x=>x.id)};
result.outcomes={
 invalid:await stopServers({adapter,ids:['absent']}),
 protected:await stopServers({adapter,ids:['process:1:1'],yes:true,confirmToken:'STOP:1'}),
 confirmation:await stopServers({adapter,ids:['process:42:99']}),
 success:await stopServers({adapter,ids:['process:42:99'],yes:true,confirmToken:'STOP:42'}),
};
result.codes=Object.fromEntries(Object.entries(result.outcomes).map(([key,value])=>[key,value.code]));
process.stdout.write(JSON.stringify(result));`
	cmd := exec.Command("node", "--input-type=module", "-e", script)
	cmd.Dir = "../.."
	cmd.Stdin = strings.NewReader(string(payload))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("référence Node indisponible: %v", err)
	}
	var node struct {
		Diagnostics map[string]string   `json:"diagnostics"`
		Views       map[string][]string `json:"views"`
		Plan        struct {
			Token     string   `json:"token"`
			Actions   []string `json:"actions"`
			Conflicts []string `json:"conflicts"`
		} `json:"plan"`
		Codes    map[string]int             `json:"codes"`
		Outcomes map[string]json.RawMessage `json:"outcomes"`
		Variants map[string][]string        `json:"variants"`
	}
	if err := json.Unmarshal(out, &node); err != nil {
		t.Fatal(err)
	}
	goScopes := map[string]string{"attention": "Attention", "projects": "Projets", "system": "Système", "all": "Tous"}
	for key, scope := range goScopes {
		got := []string{}
		for _, s := range filtered(items, filters{scope: scope}) {
			got = append(got, s.ID)
		}
		if !reflect.DeepEqual(got, node.Views[key]) {
			t.Fatalf("vue %s différente: Go=%v Node=%v", key, got, node.Views[key])
		}
	}
	for _, s := range items {
		if diagnostic(s) != node.Diagnostics[s.ID] {
			t.Fatalf("diagnostic différent pour %s: Go=%s Node=%s", s.ID, diagnostic(s), node.Diagnostics[s.ID])
		}
	}
	goVariants := map[string][]string{}
	for key, f := range map[string]filters{"udp": {scope: "Tous", udp: true}, "query": {scope: "Tous", udp: true, query: "5173"}} {
		ids := []string{}
		for _, s := range items {
			if visible(s, f) {
				ids = append(ids, s.ID)
			}
		}
		goVariants[key] = ids
	}
	if !reflect.DeepEqual(goVariants, node.Variants) {
		t.Fatalf("variantes de filtres différentes: Go=%v Node=%v", goVariants, node.Variants)
	}
	p := buildPlan(items, []string{"process:42:99", "process:1:1"}, false)
	goActions, goConflicts := []string{}, []string{}
	for _, a := range p.Actions {
		goActions = append(goActions, a.ID)
	}
	for _, c := range p.Conflicts {
		goConflicts = append(goConflicts, c.ID)
	}
	if p.ConfirmationToken != node.Plan.Token || !reflect.DeepEqual(goActions, node.Plan.Actions) || !reflect.DeepEqual(goConflicts, node.Plan.Conflicts) {
		t.Fatalf("plan différent: Go=%#v Node=%#v", p, node.Plan)
	}
	successExecutor := func(p plan) []result { return []result{{ID: p.Actions[0].ID, Status: "success"}} }
	goCodes := map[string]int{
		"invalid":      makeStopOutcome(items, []string{"absent"}, false, false, "", successExecutor).Code,
		"protected":    makeStopOutcome(items, []string{"process:1:1"}, false, true, "STOP:1", successExecutor).Code,
		"confirmation": makeStopOutcome(items, []string{"process:42:99"}, false, false, "", successExecutor).Code,
		"success":      makeStopOutcome(items, []string{"process:42:99"}, false, true, "STOP:42", successExecutor).Code,
	}
	if !reflect.DeepEqual(goCodes, node.Codes) {
		t.Fatalf("codes différents: Go=%v Node=%v", goCodes, node.Codes)
	}
	goOutcomes := map[string]outcome{
		"invalid":      makeStopOutcome(items, []string{"absent"}, false, false, "", successExecutor),
		"protected":    makeStopOutcome(items, []string{"process:1:1"}, false, true, "STOP:1", successExecutor),
		"confirmation": makeStopOutcome(items, []string{"process:42:99"}, false, false, "", successExecutor),
		"success":      makeStopOutcome(items, []string{"process:42:99"}, false, true, "STOP:42", successExecutor),
	}
	for key, goOutcome := range goOutcomes {
		goJSON, _ := json.Marshal(goOutcome)
		var goValue, nodeValue any
		_ = json.Unmarshal(goJSON, &goValue)
		_ = json.Unmarshal(node.Outcomes[key], &nodeValue)
		if !reflect.DeepEqual(goValue, nodeValue) {
			t.Fatalf("JSON %s différent:\nGo   %s\nNode %s", key, goJSON, node.Outcomes[key])
		}
	}
}

func TestSocketInventoryParsingParityWithNode(t *testing.T) {
	input := strings.Join([]string{
		`tcp LISTEN 0 4096 0.0.0.0:5173 0.0.0.0:* users:(("node",pid=42,fd=20))`,
		`udp UNCONN 0 0 [::]:5355 [::]:*`,
		`tcp ESTAB 0 0 127.0.0.1:1234 127.0.0.1:80`,
	}, "\n")
	script := `import { parseSs } from './src/system/linux-adapter.mjs'; const chunks=[]; for await (const c of process.stdin) chunks.push(c); process.stdout.write(JSON.stringify(parseSs(Buffer.concat(chunks).toString())));`
	cmd := exec.Command("node", "--input-type=module", "-e", script)
	cmd.Dir = "../.."
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var node []struct {
		Protocol, Address string
		Port              int
		PID               *int
		ProcessName       *string
	}
	if err := json.Unmarshal(out, &node); err != nil {
		t.Fatal(err)
	}
	got := parseSS(input)
	if len(got) != len(node) {
		t.Fatalf("nombre de sockets différent: Go=%#v Node=%#v", got, node)
	}
	for i := range got {
		pid, name := 0, ""
		if node[i].PID != nil {
			pid = *node[i].PID
		}
		if node[i].ProcessName != nil {
			name = *node[i].ProcessName
		}
		if got[i].Protocol != node[i].Protocol || got[i].Address != node[i].Address || got[i].Port != node[i].Port || got[i].PID != pid || got[i].ProcessName != name {
			t.Fatalf("socket %d différent: Go=%#v Node=%#v", i, got[i], node[i])
		}
	}
}

func TestStopOutcomeCodesAndJSONCollections(t *testing.T) {
	items := fixture()
	never := func(plan) []result { t.Fatal("exécution inattendue"); return nil }
	cases := []struct {
		name     string
		ids      []string
		inc, yes bool
		token    string
		want     int
	}{
		{"sans cible", nil, false, false, "", 3},
		{"inconnue", []string{"absent"}, false, false, "", 3},
		{"système protégé", []string{"process:1:1"}, false, true, "STOP:1", 2},
		{"confirmation", []string{"process:42:99"}, false, false, "", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := makeStopOutcome(items, tc.ids, tc.inc, tc.yes, tc.token, never)
			if got.Code != tc.want || got.Results == nil {
				t.Fatalf("issue incorrecte: %#v", got)
			}
		})
	}
	success := makeStopOutcome(items, []string{"process:42:99"}, false, true, "STOP:42", func(p plan) []result {
		return []result{{ID: p.Actions[0].ID, Status: "success"}}
	})
	if success.Code != 0 {
		t.Fatalf("succès classé en erreur: %#v", success)
	}
	failed := makeStopOutcome(items, []string{"process:42:99"}, false, true, "STOP:42", func(p plan) []result {
		return []result{{ID: p.Actions[0].ID, Status: "failed"}}
	})
	if failed.Code != 1 {
		t.Fatalf("échec mal classé: %#v", failed)
	}
}
