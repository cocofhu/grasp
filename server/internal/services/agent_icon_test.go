package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func iconAgent(name string, icon *AgentIcon) Agent {
	return Agent{
		Name:         name,
		AcpBackend:   AcpBackendCursor,
		InheritModel: false,
		InheritGit:   false,
		Icon:         icon,
	}
}

func readAgentJSON(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, name, "agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGetLegacyAgentWithoutIcon(t *testing.T) {
	root := t.TempDir()
	s := NewAgentService(root)
	dir := filepath.Join(root, "legacy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const raw = `{"inheritModel":false,"inheritGit":false,"acpBackend":"cursor","env":{"KEEP":"yes"}}`
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("legacy")
	if !ok || got.Icon != nil {
		t.Fatalf("icon=%+v ok=%v", got.Icon, ok)
	}
	if got.Env["KEEP"] != "yes" {
		t.Fatalf("env=%v", got.Env)
	}
	got.Env["KEEP"] = "still"
	if err := s.Save(got); err != nil {
		t.Fatal(err)
	}
	stored := readAgentJSON(t, root, "legacy")
	if strings.Contains(stored, "icon") {
		t.Fatalf("save invented an icon: %s", stored)
	}
}

func TestSaveListAndClearIdentityIcon(t *testing.T) {
	root := t.TempDir()
	s := NewAgentService(root)
	icon := &AgentIcon{IconID: "flask", Color: "purple"}
	if err := s.Save(iconAgent("lab", icon)); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("lab")
	if !ok || got.Icon == nil || got.Icon.IconID != "flask" || got.Icon.Color != "purple" {
		t.Fatalf("get %+v ok=%v", got.Icon, ok)
	}
	listed := s.List()
	if len(listed) != 1 || listed[0].Icon == nil || listed[0].Icon.IconID != "flask" {
		t.Fatalf("list %+v", listed)
	}
	got.Icon.Color = "red"
	if listed[0].Icon.Color != "purple" {
		t.Fatal("list returned the stored icon pointer")
	}

	got.Icon = nil
	if err := s.Save(got); err != nil {
		t.Fatal(err)
	}
	cleared, _ := s.Get("lab")
	if cleared.Icon != nil {
		t.Fatalf("cleared %+v", cleared.Icon)
	}
	if strings.Contains(readAgentJSON(t, root, "lab"), `"icon"`) {
		t.Fatalf("cleared agent.json still has icon: %s", readAgentJSON(t, root, "lab"))
	}
}

func TestSaveRejectsInvalidIconWithoutWriting(t *testing.T) {
	root := t.TempDir()
	s := NewAgentService(root)
	if err := s.Save(iconAgent("lab", &AgentIcon{IconID: "flask", Color: "purple"})); err != nil {
		t.Fatal(err)
	}
	before := readAgentJSON(t, root, "lab")
	cases := []*AgentIcon{
		{IconID: "not-real", Color: "black"},
		{IconID: "flask", Color: "teal"},
		{IconID: "", Color: "black"},
		{IconID: "flask", Color: ""},
	}
	for _, icon := range cases {
		err := s.Save(iconAgent("lab", icon))
		if !errors.Is(err, ErrInvalidAgent) {
			t.Fatalf("icon %+v err=%v", icon, err)
		}
		if readAgentJSON(t, root, "lab") != before {
			t.Fatal("invalid save rewrote agent.json")
		}
		missing := s.Save(iconAgent("fresh", icon))
		if !errors.Is(missing, ErrInvalidAgent) {
			t.Fatalf("create icon %+v err=%v", icon, missing)
		}
		if s.Exists("fresh") {
			t.Fatal("invalid create left a directory")
		}
	}
}

func TestRenameKeepsIdentityIcon(t *testing.T) {
	s := NewAgentService(t.TempDir())
	if err := s.Save(iconAgent("old", &AgentIcon{IconID: "plane", Color: "blue"})); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("old", "new"); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("new")
	if !ok || got.Icon == nil || got.Icon.IconID != "plane" || got.Icon.Color != "blue" {
		t.Fatalf("%+v ok=%v", got.Icon, ok)
	}
}

func TestIdentityIconExportImportRoundTrip(t *testing.T) {
	src := NewAgentService(t.TempDir())
	if err := src.Save(iconAgent("lab", &AgentIcon{IconID: "flask", Color: "purple"})); err != nil {
		t.Fatal(err)
	}
	raw, err := src.ExportZIP("lab")
	if err != nil {
		t.Fatal(err)
	}
	dst := NewAgentService(t.TempDir())
	imported, err := dst.ImportZIP(raw, "lab-copy", ImportZIPCreate)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Icon == nil || imported.Icon.IconID != "flask" || imported.Icon.Color != "purple" {
		t.Fatalf("imported %+v", imported.Icon)
	}

	bundle, err := src.ExportWorkspaceAgentsZIP()
	if err != nil {
		t.Fatal(err)
	}
	packed := NewAgentService(t.TempDir())
	if _, err := packed.ImportWorkspaceAgentsZIP(bundle, ImportWorkspaceAgentsRename); err != nil {
		t.Fatal(err)
	}
	got, ok := packed.Get("lab")
	if !ok || got.Icon == nil || got.Icon.IconID != "flask" || got.Icon.Color != "purple" {
		t.Fatalf("bundle %+v ok=%v", got.Icon, ok)
	}
}

func TestImportLegacyZipHasNoIcon(t *testing.T) {
	s := NewAgentService(t.TempDir())
	meta := []byte(`{"name":"old","inheritModel":false,"inheritGit":false,"acpBackend":"cursor","schemaVersion":1}`)
	raw := buildTestZip(t, meta, nil)
	imported, err := s.ImportZIP(raw, "old", ImportZIPCreate)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Icon != nil {
		t.Fatalf("legacy import icon %+v", imported.Icon)
	}
	if strings.Contains(readAgentJSON(t, s.root, "old"), `"icon"`) {
		t.Fatal("legacy import wrote an icon")
	}
}

func TestImportRejectsBadIconAndEmojiWithoutRewriting(t *testing.T) {
	root := t.TempDir()
	s := NewAgentService(root)
	if err := s.Save(iconAgent("lab", &AgentIcon{IconID: "heart", Color: "pink"})); err != nil {
		t.Fatal(err)
	}
	before := readAgentJSON(t, root, "lab")
	cases := []string{
		`{"name":"lab","inheritModel":false,"inheritGit":false,"acpBackend":"cursor","schemaVersion":1,"icon":{"iconId":"nope","color":"black"}}`,
		`{"name":"lab","inheritModel":false,"inheritGit":false,"acpBackend":"cursor","schemaVersion":1,"icon":{"iconId":"flask","color":"teal"}}`,
		`{"name":"lab","inheritModel":false,"inheritGit":false,"acpBackend":"cursor","schemaVersion":1,"emoji":"x"}`,
		`{"name":"lab","inheritModel":false,"inheritGit":false,"acpBackend":"cursor","schemaVersion":1,"icon":{"iconId":"flask","color":"purple","emoji":"x"}}`,
		`{"name":"lab","inheritModel":false,"inheritGit":false,"acpBackend":"cursor","schemaVersion":1,"icon":{"kind":"emoji","iconId":"flask","color":"black"}}`,
		`{"name":"lab","inheritModel":false,"inheritGit":false,"acpBackend":"cursor","schemaVersion":1,"icon":{"iconId":"flask","color":"purple","svg":"<svg/>"}}`,
	}
	for _, meta := range cases {
		raw := buildTestZip(t, []byte(meta), nil)
		_, err := s.ImportZIP(raw, "lab", ImportZIPOverwrite)
		if !errors.Is(err, ErrInvalidAgent) {
			t.Fatalf("meta %s err=%v", meta, err)
		}
		if readAgentJSON(t, root, "lab") != before {
			t.Fatal("rejected import rewrote agent.json")
		}
		got, _ := s.Get("lab")
		if got.Icon == nil || got.Icon.IconID != "heart" || got.Icon.Color != "pink" {
			t.Fatalf("icon changed to %+v", got.Icon)
		}
	}
}

func TestWorkspaceRestoreKeepsIdentityIcon(t *testing.T) {
	root := t.TempDir()
	s := NewAgentService(root)
	if err := s.Save(iconAgent("lab", &AgentIcon{IconID: "code", Color: "green"})); err != nil {
		t.Fatal(err)
	}
	sha, err := s.WriteWorkspaceFileVcs("lab", "a.md", "v1", WorkspaceWriteOpts{
		Author: "u", Source: VcsSourcePmMCP, Reason: "seed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteWorkspaceFileVcs("lab", "a.md", "v2", WorkspaceWriteOpts{
		Author: "u", Source: VcsSourcePmMCP, Reason: "edit",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreWorkspaceVcs("lab", sha, "u", "rollback"); err != nil {
		t.Fatal(err)
	}
	body, err := s.ReadWorkspaceFile("lab", "a.md")
	if err != nil || body != "v1" {
		t.Fatalf("workspace=%q err=%v", body, err)
	}
	got, ok := s.Get("lab")
	if !ok || got.Icon == nil || got.Icon.IconID != "code" || got.Icon.Color != "green" {
		t.Fatalf("restore dropped icon %+v", got.Icon)
	}
	var cfg agentConfig
	if err := json.Unmarshal([]byte(readAgentJSON(t, root, "lab")), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Icon == nil || cfg.Icon.IconID != "code" {
		t.Fatalf("disk icon %+v", cfg.Icon)
	}
}

func TestEmojiIdentityScan(t *testing.T) {
	if err := RejectEmojiIdentity([]byte(`{"name":"a","modelBinding":{"kind":"configuration","configurationId":"cfg"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := RejectEmojiIdentity([]byte(`{"emojiId":"grin"}`)); !errors.Is(err, ErrInvalidAgent) {
		t.Fatalf("err=%v", err)
	}
	if err := RejectUnknownIconFields([]byte(`{"icon":{"iconId":"book","color":"black"}}`)); err != nil {
		t.Fatal(err)
	}
}
