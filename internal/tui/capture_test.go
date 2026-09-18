package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/internal/project"
	tea "github.com/charmbracelet/bubbletea"
)

// harmlessTemplate is the default template with its program swapped for
// `true`, so every quoting layer is the real one and nothing opens a window.
func harmlessTemplate() string {
	return strings.Replace(defaultSpawnTemplate, "osascript", "true", 1)
}

func mustBuildCaptureCommand(t *testing.T, template, dir, project, idea string) string {
	t.Helper()
	cmd, err := buildCaptureCommand(template, dir, project, idea)
	if err != nil {
		t.Fatalf("buildCaptureCommand(%q): %v", idea, err)
	}
	return cmd
}

func TestCaptureKeyOpensPromptInListAndEscCancels(t *testing.T) {
	f := newGlobalFixture(t)
	a := f.app(t, "warp", "true {command}")

	model, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	a = model.(App)
	if !a.captureActive {
		t.Fatal("c in the list view did not open the capture prompt")
	}
	if a.captureNS != "warp" {
		t.Errorf("captureNS = %q, want the board's project", a.captureNS)
	}
	if !a.captureBar.Focused() {
		t.Error("the capture prompt is open but not focused")
	}
	if a.overlay != overlayNone {
		t.Errorf("c opened overlay %v, want the prompt on the board", a.overlay)
	}
	if cmd == nil {
		t.Error("focusing the prompt returned no command")
	}
	if out := a.View(); !strings.Contains(out, "capture:") {
		t.Errorf("the footer does not show the capture prompt:\n%s", out)
	}

	model, cmd = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	a = model.(App)
	if a.captureBar.Value() != "x" {
		t.Errorf("typed rune did not reach the prompt, value %q", a.captureBar.Value())
	}
	if a.overlay != overlayNone {
		t.Errorf("a typed rune fell through to the board and opened overlay %v", a.overlay)
	}

	model, cmd = a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	a = model.(App)
	if a.captureActive {
		t.Fatal("esc did not close the capture prompt")
	}
	if cmd != nil {
		t.Errorf("esc returned a command %T, want none — nothing may spawn", cmd())
	}
	if a.captureBar.Value() != "" {
		t.Errorf("esc left %q in the prompt", a.captureBar.Value())
	}
}

func TestCaptureKeyOpensPromptFromDetailWithTheTicketsNamespace(t *testing.T) {
	f := newGlobalFixture(t)
	// A loom child reached through warp's epic: the capture must follow the
	// detail's own namespace, not the board's.
	a := onTab(t, f.app(t, "warp", "true {command}"), tabDone, "a-0002")
	a = press(t, a, "u")
	a = press(t, a, "enter")
	a = pickChild(t, a, "loom/b-0003")

	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	a = model.(App)
	if !a.captureActive {
		t.Fatal("c on a detail did not open the capture prompt")
	}
	if a.captureNS != "loom" {
		t.Errorf("captureNS = %q, want the detail's namespace loom", a.captureNS)
	}
	if a.overlay != overlayDetail {
		t.Errorf("the prompt closed the detail, overlay %v", a.overlay)
	}
	if out := a.View(); !strings.Contains(out, "capture:") {
		t.Errorf("the overlay frame does not show the capture prompt:\n%s", out)
	}

	model, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	a = model.(App)
	if a.captureActive {
		t.Fatal("esc did not close the capture prompt")
	}
	if cmd != nil {
		t.Errorf("esc returned a command, want none")
	}
	if a.overlay != overlayDetail {
		t.Errorf("esc on the prompt left the detail too, overlay %v", a.overlay)
	}
}

func TestCaptureNewKeyOpensCreateForm(t *testing.T) {
	f := newGlobalFixture(t)
	a := f.app(t, "warp", "")

	a = press(t, a, "n")
	if a.overlay != overlayForm {
		t.Fatalf("n opened overlay %v, want the create form", a.overlay)
	}
	if a.form.editID != "" {
		t.Errorf("n opened the edit form for %q, want a create form", a.form.editID)
	}
	if a.captureActive {
		t.Error("n opened the capture prompt as well")
	}
}

func TestCaptureEnterOnEmptyIdeaCancels(t *testing.T) {
	f := newGlobalFixture(t)
	a := f.app(t, "warp", "true {command}")

	a = press(t, a, "c")
	for _, r := range "   " {
		model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		a = model.(App)
	}
	model, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	a = model.(App)
	if a.captureActive {
		t.Fatal("enter left the capture prompt open")
	}
	if cmd != nil {
		t.Errorf("enter on a blank idea returned a command %T, want none", cmd())
	}
}

func TestCaptureEnterSpawnsTheIdea(t *testing.T) {
	f := newGlobalFixture(t)
	a := f.app(t, "warp", "true {command}")

	a = press(t, a, "c")
	for _, r := range "an idea" {
		model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		a = model.(App)
	}
	model, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	a = model.(App)
	if a.captureActive {
		t.Fatal("enter left the capture prompt open")
	}
	if cmd == nil {
		t.Fatal("enter on an idea returned no command")
	}
	status, ok := cmd().(statusMsg)
	if !ok {
		t.Fatalf("spawn returned %T, want statusMsg", status)
	}
	if !strings.HasPrefix(string(status), "Launching /brainstorm") {
		t.Errorf("expected a launch, got %q", string(status))
	}
	if a.captureBar.Value() != "" {
		t.Errorf("the idea %q is still in the prompt after the spawn", a.captureBar.Value())
	}
}

func TestCaptureRefusedOnRootBoard(t *testing.T) {
	f := newGlobalFixture(t)
	a := f.app(t, project.RootNamespace, "true {command}")

	// Refused at the keypress, like `w`: the prompt never opens, so the idea
	// is not typed and then discarded on the same refusal.
	model, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	a = model.(App)
	if a.captureActive {
		t.Fatal("capture prompt opened on the Root board, want a refusal")
	}
	if cmd == nil {
		t.Fatal("c returned no command, want the refusal")
	}
	status, ok := cmd().(statusMsg)
	if !ok || !strings.Contains(string(status), "refusing to spawn") {
		t.Errorf("Root has no checkout, want a refusal, got %q", status)
	}
}

func TestCaptureBuildSubstitutesCommand(t *testing.T) {
	got := mustBuildCaptureCommand(t, "true {dir} {project} {command}", "/some/dir", "proj", "an idea")
	want := "true /some/dir proj /brainstorm an idea"
	if got != want {
		t.Errorf("buildCaptureCommand = %q, want %q", got, want)
	}
}

func TestCaptureBuildSanitizesIdea(t *testing.T) {
	// The idea is free text: what would break a quoting layer or expand in the
	// interactive shell becomes a space, in {command} and in {title} alike.
	got := mustBuildCaptureCommand(t, "{command}|{title}|{wtitle}|{id}", "/some/dir", "proj", "a'b\"c$d`e!f")
	want := "/brainstorm a b c d e f|a b c d e f|PROJ -- capture -- a b c d e f|"
	if got != want {
		t.Errorf("buildCaptureCommand = %q, want %q", got, want)
	}
}

func TestCaptureWindowTitleTruncatesTo20Runes(t *testing.T) {
	got := mustBuildCaptureCommand(t, "{command} {wtitle}", "/some/dir", "proj", "0123456789abcdefghijKLMNOP")
	want := "/brainstorm 0123456789abcdefghijKLMNOP PROJ -- capture -- 0123456789abcdefghij"
	if got != want {
		t.Errorf("capture window title = %q, want %q", got, want)
	}
}

func TestCaptureBuildDefault(t *testing.T) {
	got := mustBuildCaptureCommand(t, "", "/some/dir", "proj", "an idea")
	for _, want := range []string{"/some/dir", "/brainstorm an idea", "iTerm", "claude", "write text", "PROJ -- capture -- an idea"} {
		if !strings.Contains(got, want) {
			t.Errorf("default capture command %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "/work") {
		t.Errorf("default capture command still carries /work: %s", got)
	}
	check := exec.Command("sh", "-n")
	check.Stdin = strings.NewReader(got)
	if out, err := check.CombinedOutput(); err != nil {
		t.Errorf("default capture command is not valid shell syntax: %v\n%s\ncmd: %s", err, out, got)
	}
}

func TestCaptureBuildRefusesDirAndProject(t *testing.T) {
	cmd, err := buildCaptureCommand("", `/Users/me/it's`, "proj", "idea")
	if err == nil || !strings.Contains(err.Error(), "working directory") {
		t.Errorf("dir with a quote: err %v cmd %q, want a refusal naming the working directory", err, cmd)
	}
	cmd, err = buildCaptureCommand("", "/some/dir", "it's", "idea")
	if err == nil || !strings.Contains(err.Error(), "project name") {
		t.Errorf("project with a quote: err %v cmd %q, want a refusal naming the project name", err, cmd)
	}
}

func TestCaptureBuildRefusesTemplateWithoutCommand(t *testing.T) {
	cmd, err := buildCaptureCommand(`tmux new-window -c {dir} "claude \"/work {id}\""`, "/some/dir", "proj", "idea")
	if err == nil {
		t.Fatalf("a template with no {command} was accepted: %s", cmd)
	}
	if !strings.Contains(err.Error(), "{command}") {
		t.Errorf("refusal must name the missing placeholder, got %q", err.Error())
	}
	if cmd != "" {
		t.Errorf("refusal returned a command alongside its error: %s", cmd)
	}
}

func TestCaptureWorkTemplateWithoutCommandIsUnchanged(t *testing.T) {
	// A custom template written before {command} existed hard-codes /work and
	// must keep serving w byte for byte.
	template := `tmux new-window -c {dir} "claude \"/work {id}\""`
	got := mustBuildSpawnCommand(t, template, "/some/dir", "proj/tk-x", "proj", "Title")
	want := `tmux new-window -c /some/dir "claude \"/work proj/tk-x\""`
	if got != want {
		t.Errorf("buildSpawnCommand = %q, want %q", got, want)
	}
}

func TestCaptureWorkDefaultCarriesWorkCommand(t *testing.T) {
	got := mustBuildSpawnCommand(t, "", "/some/dir", "proj/tk-x", "proj", "Title")
	if !strings.Contains(got, `claude \"/work proj/tk-x\"`) {
		t.Errorf("default work command does not open /work on the id: %s", got)
	}
	if strings.Contains(got, "{command}") {
		t.Errorf("{command} survived interpolation: %s", got)
	}
}

// TestCaptureInjectionThroughIdeaDoesNotRun types the {id}-style injection
// into the idea and runs the real default quoting (program swapped for
// `true`): sanitizing strips the quote that would close the layer, so the
// sentinel is never touched.
func TestCaptureInjectionThroughIdeaDoesNotRun(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "PWNED")
	idea := `x"; touch ` + sentinel + `; echo "`

	cmd := mustBuildCaptureCommand(t, harmlessTemplate(), dir, "proj", idea)
	exec.Command("sh", "-c", cmd).Run()
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatalf("injected command executed: %s exists", sentinel)
	}
	if strings.Contains(cmd, `x"; touch`) {
		t.Errorf("the idea's quote reached the command: %s", cmd)
	}

	a := New(filepath.Join(dir, ".tickets"), "proj", "v0", harmlessTemplate(), dir, false, fixedExecDir(dir))
	status, ok := a.spawnCapture("proj", idea)().(statusMsg)
	if !ok || !strings.HasPrefix(string(status), "Launching /brainstorm") {
		t.Errorf("spawnCapture with the sanitized idea should launch, got %q", status)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatalf("injected command executed through spawnCapture: %s exists", sentinel)
	}
}

func TestCaptureSpawnRefusesWhenExecDirFails(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "PWNED")
	a := New(filepath.Join(dir, ".tickets"), "proj", "v0", "touch "+sentinel+" {command}", dir, false, nil)

	status, ok := a.spawnCapture("proj", "idea")().(statusMsg)
	if !ok {
		t.Fatalf("spawnCapture returned %T, want statusMsg", status)
	}
	if !strings.Contains(string(status), "refusing to spawn") {
		t.Errorf("want a refusal, got %q", string(status))
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatalf("a refused spawn still ran the template: %s exists", sentinel)
	}
}

func TestCaptureSpawnRefusesDirWithQuotingCharacters(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "PWNED")
	workDir := dir + "'; touch " + sentinel + "; echo '"
	a := New(filepath.Join(dir, ".tickets"), "proj", "v0", harmlessTemplate(), workDir, false, fixedExecDir(workDir))

	status, ok := a.spawnCapture("proj", "idea")().(statusMsg)
	if !ok || !strings.Contains(string(status), "refusing to spawn") || !strings.Contains(string(status), "working directory") {
		t.Errorf("refusal must name the working directory, got %q", status)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatalf("injected command executed: %s exists", sentinel)
	}
}

func TestCaptureSpawnRefusesTemplateWithoutCommand(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "PWNED")
	a := New(filepath.Join(dir, ".tickets"), "proj", "v0", "touch "+sentinel+" {id}", dir, false, fixedExecDir(dir))

	status, ok := a.spawnCapture("proj", "idea")().(statusMsg)
	if !ok || !strings.Contains(string(status), "{command}") {
		t.Errorf("refusal must name the missing placeholder, got %q", status)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatalf("a template with no {command} still ran: %s exists", sentinel)
	}
}

func TestCaptureHelpAdvertisesKeys(t *testing.T) {
	a := App{activeTab: tabInbox}
	a.dashboard.activeTab = tabInbox
	help := a.helpText()
	for _, k := range []string{"(n)ew", "(c)apture"} {
		if !strings.Contains(help, k) {
			t.Errorf("list help should advertise %q, got:\n%s", k, help)
		}
	}
	if strings.Contains(help, "(c)reate") {
		t.Errorf("list help still advertises (c)reate:\n%s", help)
	}
	d := detailModel{width: 200}
	detailHelp := strings.Join(d.helpLines(), " ")
	if !strings.Contains(detailHelp, "(c)apture") {
		t.Errorf("detail help should advertise (c)apture, got:\n%s", detailHelp)
	}
}
