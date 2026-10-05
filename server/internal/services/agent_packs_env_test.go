package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentPacksUseGraspEnv(t *testing.T) {
	roots := []string{
		filepath.Join("..", "..", "..", "agents"),
		"team_embed",
	}
	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.EqualFold(d.Name(), "agent.json") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(files) == 0 {
		t.Fatal("no agent.json files found")
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "APPROVING_") {
			t.Errorf("%s still references APPROVING_", path)
		}
	}
}
