package modelconnection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/provider"
)

func writeProjectModels(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, ".codebuddy", "models.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCodeBuddyModelInspectionStopsAtFilesystemRoot(t *testing.T) {
	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	cwd := filepath.Join(workspace, "project", "nested")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	// If the walk reaches this file, it fails JSON parsing. It is outside the
	// session's filesystem boundary and must not affect the selected model.
	outside := writeProjectModels(t, parent, "outside-project-config")
	c := connection(Chat)
	p := Prepared{Connection: &c}
	opts := provider.OpenOptions{Cwd: cwd, FSRoot: workspace}
	for _, tool := range []provider.Name{provider.CodeBuddy, provider.CodeBuddyACP} {
		if err := p.ValidateOptions(tool, opts); err != nil {
			t.Fatalf("inspected an ancestor outside the filesystem root: %v", err)
		}
	}
	content := `{"models":[{"id":"vendor/model/with/slashes"}]}`
	for _, dir := range []string{workspace, filepath.Dir(cwd), cwd} {
		path := writeProjectModels(t, dir, content)
		if err := p.ValidateOptions(provider.CodeBuddy, opts); err == nil || !strings.Contains(err.Error(), "overrides a selected connection model") {
			t.Fatalf("missed a model override inside the filesystem root: %v", err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if body, err := os.ReadFile(outside); err != nil || string(body) != "outside-project-config" {
		t.Fatal("inspection changed an outside project file")
	}
}

func TestCodeBuddyModelInspectionDefaultsRootToWorkingDirectory(t *testing.T) {
	parent := t.TempDir()
	cwd := filepath.Join(parent, "workspace")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	writeProjectModels(t, parent, "outside-project-config")
	c := connection(Chat)
	p := Prepared{Connection: &c}
	if err := p.ValidateOptions(provider.CodeBuddy, provider.OpenOptions{Cwd: cwd}); err != nil {
		t.Fatalf("an omitted filesystem root must stop at cwd: %v", err)
	}
	t.Chdir(cwd)
	if err := p.ValidateOptions(provider.CodeBuddy, provider.OpenOptions{}); err != nil {
		t.Fatalf("empty cwd must inspect only the process working directory: %v", err)
	}
}

func TestCodeBuddyModelInspectionRejectsInvalidWorkingDirectory(t *testing.T) {
	parent := t.TempDir()
	workspace, outside := filepath.Join(parent, "workspace"), filepath.Join(parent, "workspace-other")
	for _, dir := range []string{workspace, outside} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(workspace, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(workspace, "not-a-directory")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := connection(Chat)
	p := Prepared{Connection: &c}
	for _, cwd := range []string{outside, workspace + "/../workspace-other", link, file, filepath.Join(workspace, "missing")} {
		if err := p.ValidateOptions(provider.CodeBuddy, provider.OpenOptions{Cwd: cwd, FSRoot: workspace}); err == nil {
			t.Fatalf("accepted a working directory outside the session boundary or not a directory: %s", cwd)
		}
	}
}

func TestCodeBuddyModelInspectionRejectsEscapingProjectSymlinks(t *testing.T) {
	for _, directoryLink := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directoryLink], func(t *testing.T) {
			workspace, outside := t.TempDir(), t.TempDir()
			outsidePath := writeProjectModels(t, outside, "outside-secret-do-not-copy")
			if directoryLink {
				if err := os.Symlink(filepath.Dir(outsidePath), filepath.Join(workspace, ".codebuddy")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(filepath.Join(workspace, ".codebuddy"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outsidePath, filepath.Join(workspace, ".codebuddy", "models.json")); err != nil {
					t.Fatal(err)
				}
			}
			c := connection(Chat)
			p := Prepared{Connection: &c}
			err := p.ValidateOptions(provider.CodeBuddy, provider.OpenOptions{Cwd: workspace, FSRoot: workspace})
			if err == nil || !strings.Contains(err.Error(), "cannot inspect") || strings.Contains(err.Error(), "outside-secret") {
				t.Fatalf("a project symlink escaped the session boundary: %v", err)
			}
		})
	}
}

func TestCodeBuddyModelInspectionFollowsConfinedProjectSymlinks(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, ".codebuddy"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(workspace, "project-models.json")
	if err := os.WriteFile(config, []byte(`{"models":[{"id":"another-model"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../project-models.json", filepath.Join(workspace, ".codebuddy", "models.json")); err != nil {
		t.Fatal(err)
	}
	c := connection(Chat)
	p := Prepared{Connection: &c}
	opts := provider.OpenOptions{Cwd: workspace, FSRoot: workspace}
	if err := p.ValidateOptions(provider.CodeBuddy, opts); err != nil {
		t.Fatalf("a project symlink inside the filesystem root was rejected: %v", err)
	}
	if err := os.WriteFile(config, []byte(`{"models":[{"id":"vendor/model/with/slashes"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateOptions(provider.CodeBuddy, opts); err == nil || !strings.Contains(err.Error(), "overrides a selected connection model") {
		t.Fatalf("missed a conflicting project model through a confined symlink: %v", err)
	}
}
