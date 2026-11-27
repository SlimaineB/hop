package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/SlimaineB/hop/internal/config"
)

type linkTestState struct {
	shortcuts []config.ShortcutConfig
	opened    string
	copied    string
}

func testLinkModel(t *testing.T) (LinkModel, *linkTestState) {
	t.Helper()
	state := &linkTestState{shortcuts: []config.ShortcutConfig{
		{Name: "grafana", URL: "https://grafana.example.com", Keywords: []string{"metrics", "dashboards"}, Description: "observability"},
		{Name: "jira", URL: "https://jira.example.com", Keywords: []string{"ticket", "bug"}, Description: "issues"},
		{Name: "swagger", URL: "https://api.example.com/swagger", Keywords: []string{"api", "openapi"}},
	}}
	model := NewLinks(LinkOptions{
		Shortcuts: state.shortcuts,
		OpenURL: func(url string) error {
			state.opened = url
			return nil
		},
		CopyText: func(value string) error {
			state.copied = value
			return nil
		},
		Save: func(shortcut config.ShortcutConfig) error {
			for i := range state.shortcuts {
				if state.shortcuts[i].Name == shortcut.Name {
					state.shortcuts[i] = shortcut
					return nil
				}
			}
			state.shortcuts = append(state.shortcuts, shortcut)
			return nil
		},
		Remove: func(name string) error {
			for i := range state.shortcuts {
				if state.shortcuts[i].Name == name {
					state.shortcuts = append(state.shortcuts[:i], state.shortcuts[i+1:]...)
					return nil
				}
			}
			return nil
		},
		Reload: func() ([]config.ShortcutConfig, error) {
			return append([]config.ShortcutConfig(nil), state.shortcuts...), nil
		},
	})
	return model, state
}

func keyRunes(value string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

func TestLinkViewModelRenders(t *testing.T) {
	model, _ := testLinkModel(t)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := updated.View()
	for _, needle := range []string{"hop", "browser shortcuts", "SHORTCUTS", "grafana", "https://grafana.example.com"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("view missing %q: %q", needle, view)
		}
	}
}

func TestLinkViewEmptyAndHelp(t *testing.T) {
	model := NewLinks(LinkOptions{})
	view := model.View()
	if !strings.Contains(view, "press a to add one") {
		t.Fatalf("empty view missing add hint: %q", view)
	}
	updated, _ := model.Update(keyRunes("?"))
	view = updated.View()
	if !strings.Contains(view, "edit the selected shortcut") || !strings.Contains(view, "add a shortcut") {
		t.Fatalf("help view incomplete: %q", view)
	}
}

func TestLinkFilter(t *testing.T) {
	model, _ := testLinkModel(t)
	model.query = "ticket"
	model.applyFilter("")
	if len(model.hits) != 1 || model.hits[0].Shortcut.Name != "jira" {
		t.Fatalf("hits = %+v", model.hits)
	}
	model.query = "missing"
	model.applyFilter("")
	if len(model.hits) != 0 {
		t.Fatalf("unexpected hits = %+v", model.hits)
	}
}

func TestLinkOpenAndCopy(t *testing.T) {
	model, state := testLinkModel(t)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("open returned nil command")
	}
	msg, ok := cmd().(statusMsg)
	if !ok || msg.err != nil || !strings.Contains(msg.text, "grafana") {
		t.Fatalf("open msg = %#v", msg)
	}
	if state.opened != "https://grafana.example.com" {
		t.Fatalf("opened = %q", state.opened)
	}

	_, cmd = model.Update(keyRunes("y"))
	if cmd == nil {
		t.Fatal("copy returned nil command")
	}
	msg, ok = cmd().(statusMsg)
	if !ok || msg.err != nil {
		t.Fatalf("copy msg = %#v", msg)
	}
	if state.copied != "https://grafana.example.com" {
		t.Fatalf("copied = %q", state.copied)
	}
}

func TestLinkAddForm(t *testing.T) {
	model, state := testLinkModel(t)
	updated, _ := model.Update(keyRunes("a"))
	form := updated.(LinkModel)
	if form.form == nil {
		t.Fatal("add did not open a form")
	}
	form.form.inputs[0].SetValue("docs")
	form.form.inputs[1].SetValue("https://docs.example.com")
	form.form.inputs[2].SetValue("documentation, docs docs")
	form.form.inputs[3].SetValue("internal docs")
	updated, _ = form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	form = updated.(LinkModel)
	if form.form != nil {
		t.Fatalf("form still open: %#v", form.form)
	}
	if len(state.shortcuts) != 4 || state.shortcuts[3].Name != "docs" {
		t.Fatalf("shortcuts = %+v", state.shortcuts)
	}
	if got := strings.Join(state.shortcuts[3].Keywords, ","); got != "documentation,docs" {
		t.Fatalf("keywords = %q", got)
	}
	if form.status != "saved docs" {
		t.Fatalf("status = %q", form.status)
	}
}

func TestLinkFormRejectsInvalidURL(t *testing.T) {
	model, _ := testLinkModel(t)
	updated, _ := model.Update(keyRunes("a"))
	form := updated.(LinkModel)
	form.form.inputs[0].SetValue("bad")
	form.form.inputs[1].SetValue("javascript:alert(1)")
	updated, _ = form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	form = updated.(LinkModel)
	if form.form == nil || !strings.Contains(form.form.err, "http(s)") {
		t.Fatalf("expected URL error, got %#v", form.form)
	}
}

func TestLinkEditRename(t *testing.T) {
	model, state := testLinkModel(t)
	updated, _ := model.Update(keyRunes("e"))
	form := updated.(LinkModel)
	form.form.inputs[0].SetValue("grafana-prod")
	updated, _ = form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	form = updated.(LinkModel)
	if form.form != nil {
		t.Fatalf("form still open: %#v", form.form)
	}
	if len(state.shortcuts) != 3 {
		t.Fatalf("shortcuts = %+v", state.shortcuts)
	}
	names := map[string]bool{}
	for _, shortcut := range state.shortcuts {
		names[shortcut.Name] = true
	}
	if names["grafana"] || !names["grafana-prod"] {
		t.Fatalf("rename failed: %+v", state.shortcuts)
	}
}

func TestLinkDeleteConfirmation(t *testing.T) {
	model, state := testLinkModel(t)
	updated, _ := model.Update(keyRunes("d"))
	model = updated.(LinkModel)
	if !model.confirmDelete {
		t.Fatal("delete did not ask for confirmation")
	}
	updated, _ = model.Update(keyRunes("y"))
	model = updated.(LinkModel)
	if model.confirmDelete {
		t.Fatal("delete confirmation still active")
	}
	if len(state.shortcuts) != 2 || len(model.all) != 2 {
		t.Fatalf("shortcuts = %+v model = %+v", state.shortcuts, model.all)
	}
}
