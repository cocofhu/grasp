package services

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupFolderOrg(t *testing.T) (*AgentService, *OrgService) {
	t.Helper()
	root := t.TempDir()
	skill := NewAgentService(root)
	for _, name := range []string{"alice", "bob", "carol", "outside"} {
		if err := skill.Save(Agent{
			Name:       name,
			AcpBackend: AcpBackendClaudeCode,
			Env:        map[string]string{"TOKEN": name + "-secret"},
			Files:      []AgentFile{{Path: "rules/" + name + ".md", Content: "# " + name}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	orgSvc := NewOrgService(root, skill)
	gRoot := OrgGroup{ID: "g_root", Name: "Grasp项目组"}
	gSub := OrgGroup{ID: "g_pipe", Name: "工作流(GitHub)", ParentGroupID: "g_root"}
	gEmpty := OrgGroup{ID: "g_empty", Name: "空组", ParentGroupID: "g_root"}
	gOther := OrgGroup{ID: "g_other", Name: "其他组"}
	if _, err := orgSvc.Put(AgentOrg{
		Groups: []OrgGroup{gRoot, gSub, gEmpty, gOther},
		Agents: map[string]OrgAgentMembership{
			"alice":   {GroupIDs: []string{"g_root", "g_other"}},
			"bob":     {GroupIDs: []string{"g_pipe", "g_root"}},
			"carol":   {GroupIDs: []string{"g_pipe"}},
			"outside": {GroupIDs: []string{"g_other"}},
		},
	}, 0); err != nil {
		t.Fatal(err)
	}
	return skill, orgSvc
}

func TestCollectGroupSubtree_emptyGroupsDedupAndClip(t *testing.T) {
	_, orgSvc := setupFolderOrg(t)
	org, err := orgSvc.Get()
	if err != nil {
		t.Fatal(err)
	}
	sub, err := CollectGroupSubtree(org, "g_root")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Groups) != 3 {
		t.Fatalf("groups=%d %+v", len(sub.Groups), sub.Groups)
	}
	ids := map[string]struct{}{}
	for _, g := range sub.Groups {
		ids[g.ID] = struct{}{}
	}
	if _, ok := ids["g_empty"]; !ok {
		t.Fatal("empty group missing")
	}
	if _, ok := ids["g_other"]; ok {
		t.Fatal("outside group should not be in subtree")
	}
	if len(sub.AgentNames) != 3 {
		t.Fatalf("agents=%v", sub.AgentNames)
	}
	alice := sub.Memberships["alice"]
	if len(alice.GroupIDs) != 1 || alice.GroupIDs[0] != "g_root" {
		t.Fatalf("alice groupIds clipped: %+v", alice.GroupIDs)
	}
}

func TestExportFolderZIP_emptyGroupAndFilename(t *testing.T) {
	_, orgSvc := setupFolderOrg(t)
	raw, filename, err := orgSvc.ExportFolderZIP("g_empty")
	if err != nil {
		t.Fatal(err)
	}
	if filename != "空组.zip" {
		t.Fatalf("filename=%q", filename)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var folderStore bool
	var hasRootAgent bool
	for _, f := range zr.File {
		if f.Name == "folder.json" {
			if f.Method != zip.Store {
				t.Fatalf("folder.json method=%d want Store", f.Method)
			}
			folderStore = true
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			var man orgFolderJSON
			if err := json.Unmarshal(b, &man); err != nil {
				t.Fatal(err)
			}
			if man.Kind != OrgFolderKind || man.SchemaVersion != 1 {
				t.Fatalf("manifest %+v", man)
			}
			if len(man.AgentNames) != 0 {
				t.Fatalf("empty group agents=%v", man.AgentNames)
			}
			if len(man.Groups) != 1 || man.Groups[0].Name != "空组" {
				t.Fatalf("groups %+v", man.Groups)
			}
		}
		if f.Name == "agent.json" {
			hasRootAgent = true
		}
	}
	if !folderStore {
		t.Fatal("missing folder.json")
	}
	if hasRootAgent {
		t.Fatal("root agent.json must not appear")
	}
}

func TestExportImportFolder_remapMountRenameOverwriteRollback(t *testing.T) {
	skill, orgSvc := setupFolderOrg(t)

	raw, _, err := orgSvc.ExportFolderZIP("g_root")
	if err != nil {
		t.Fatal(err)
	}

	// Top-bar import → new root group, remap ids, drop outside parent.
	dstRoot := t.TempDir()
	dstSkill := NewAgentService(dstRoot)
	dstOrg := NewOrgService(dstRoot, dstSkill)
	if err := dstSkill.Save(Agent{Name: "alice", Files: []AgentFile{{Path: "rules/old.md", Content: "old"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := dstOrg.Put(AgentOrg{
		Groups: []OrgGroup{{ID: "g_local", Name: "本地组"}},
		Agents: map[string]OrgAgentMembership{"alice": {GroupIDs: []string{"g_local"}}},
	}, 0); err != nil {
		t.Fatal(err)
	}

	renamed, err := dstOrg.ImportFolderZIP(raw, "", ImportFolderRename)
	if err != nil {
		t.Fatalf("rename import: %v", err)
	}
	if renamed.Renamed["alice"] != "alice_v2" {
		t.Fatalf("rename map %+v", renamed.Renamed)
	}
	if _, ok := dstSkill.Get("alice_v2"); !ok {
		t.Fatal("alice_v2 missing")
	}
	oldAlice, _ := dstSkill.Get("alice")
	if len(oldAlice.Files) != 1 || oldAlice.Files[0].Path != "rules/old.md" {
		t.Fatalf("original alice must be untouched: %+v", oldAlice.Files)
	}
	// New ids must not reuse source UUIDs.
	for _, g := range renamed.Org.Groups {
		if g.ID == "g_root" || g.ID == "g_pipe" || g.ID == "g_empty" {
			t.Fatalf("reused source group id %s", g.ID)
		}
	}
	var newRoot *OrgGroup
	for i := range renamed.Org.Groups {
		if renamed.Org.Groups[i].Name == "Grasp项目组" && renamed.Org.Groups[i].ParentGroupID == "" {
			newRoot = &renamed.Org.Groups[i]
			break
		}
	}
	if newRoot == nil {
		t.Fatalf("new root missing: %+v", renamed.Org.Groups)
	}
	importedAlice := renamed.Org.Agents["alice_v2"]
	if !containsString(importedAlice.GroupIDs, newRoot.ID) {
		t.Fatalf("alice_v2 not in new root: %+v", importedAlice.GroupIDs)
	}
	// Original alice still in g_local.
	if got := renamed.Org.Agents["alice"].GroupIDs; len(got) != 1 || got[0] != "g_local" {
		t.Fatalf("untouched alice membership %+v", renamed.Org.Agents["alice"])
	}

	// Overwrite import under target group: alice workspace cleared, only package membership.
	over, err := dstOrg.ImportFolderZIP(raw, "g_local", ImportFolderOverwrite)
	if err != nil {
		t.Fatalf("overwrite import: %v", err)
	}
	alice2, ok := dstSkill.Get("alice")
	if !ok {
		t.Fatal("alice missing after overwrite")
	}
	if alice2.AcpBackend != AcpBackendClaudeCode {
		t.Fatalf("overwrite acpBackend=%q", alice2.AcpBackend)
	}
	hasOld := false
	for _, f := range alice2.Files {
		if f.Path == "rules/old.md" {
			hasOld = true
		}
	}
	if hasOld {
		t.Fatalf("overwrite must clear old workspace: %+v", alice2.Files)
	}
	m := over.Org.Agents["alice"]
	if containsString(m.GroupIDs, "g_local") {
		t.Fatalf("overwrite alice must drop pre-import g_local membership, got %+v", m)
	}
	var mountedRoot *OrgGroup
	for i := range over.Org.Groups {
		g := over.Org.Groups[i]
		if g.Name == "Grasp项目组" && g.ParentGroupID == "g_local" {
			mountedRoot = &g
			break
		}
	}
	if mountedRoot == nil {
		t.Fatalf("package root not mounted under target: %+v", over.Org.Groups)
	}
	if !containsString(m.GroupIDs, mountedRoot.ID) {
		t.Fatalf("overwrite alice membership %+v want %s", m.GroupIDs, mountedRoot.ID)
	}

	// Rollback: invalid group name after agents would be written.
	bad := buildCorruptFolderZip(t, raw)
	before, _ := dstOrg.Get()
	beforeAlice, _ := dstSkill.Get("alice")
	if _, err := dstOrg.ImportFolderZIP(bad, "", ImportFolderRename); err == nil {
		t.Fatal("expected rollback error")
	} else if !strings.Contains(err.Error(), "整次回滚") {
		t.Fatalf("rollback message: %v", err)
	}
	after, _ := dstOrg.Get()
	if after.Revision != before.Revision {
		t.Fatalf("org revision changed after rollback: %d -> %d", before.Revision, after.Revision)
	}
	afterAlice, _ := dstSkill.Get("alice")
	if len(afterAlice.Files) != len(beforeAlice.Files) {
		t.Fatalf("alice files changed after rollback")
	}
	// No extra alice_vN from failed import beyond what rename+overwrite already created.
	_ = skill
}

func buildCorruptFolderZip(t *testing.T, good []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(good), int64(len(good)))
	if err != nil {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(rc)
		_ = rc.Close()
		if f.Name == "folder.json" {
			var man orgFolderJSON
			_ = json.Unmarshal(body, &man)
			man.Groups = append(man.Groups, OrgGroup{ID: "g_bad", Name: ""})
			body, _ = json.Marshal(man)
			hdr := &zip.FileHeader{Name: "folder.json", Method: zip.Store}
			w, _ := zw.CreateHeader(hdr)
			_, _ = w.Write(body)
			continue
		}
		w, _ := zw.Create(f.Name)
		_, _ = w.Write(body)
	}
	_ = zw.Close()
	return buf.Bytes()
}

func TestImportFolderZIP_singleAgentRejectedAndOversize(t *testing.T) {
	_, orgSvc := setupFolderOrg(t)
	meta := []byte(`{"name":"solo","schemaVersion":1,"exportedAt":"2026-01-01T00:00:00Z"}`)
	single := buildTestZip(t, meta, map[string][]byte{"rules/a.md": []byte("a")})
	if _, err := orgSvc.ImportFolderZIP(single, "", ImportFolderRename); err == nil || !strings.Contains(err.Error(), "单 Agent ZIP") {
		t.Fatalf("single agent: %v", err)
	}
	if _, err := orgSvc.Get(); err != nil {
		t.Fatal(err)
	}
	big := make([]byte, OrgFolderMaxBytes+1)
	if _, err := orgSvc.ImportFolderZIP(big, "", ImportFolderRename); err == nil || !strings.Contains(err.Error(), "64") {
		t.Fatalf("oversize: %v", err)
	}
}

func TestSanitizeDownloadFilename_cjkAndUnsafe(t *testing.T) {
	if got := sanitizeDownloadFilename("Grasp项目组"); got != "Grasp项目组" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeDownloadFilename("工作流(GitHub)"); got != "工作流_GitHub_" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeDownloadFilename("a/b\\c"); got != "a_b_c" {
		t.Fatalf("got %q", got)
	}
}

func TestExportFolderZIP_groupNotFound(t *testing.T) {
	_, orgSvc := setupFolderOrg(t)
	if _, _, err := orgSvc.ExportFolderZIP("g_missing"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestCopyDirRoundTrip(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir() + "/out"
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	if err != nil || string(got) != "hi" {
		t.Fatalf("copy: %s %v", got, err)
	}
}
