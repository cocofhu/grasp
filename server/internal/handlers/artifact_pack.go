package handlers

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

// PackRunArtifacts streams a zip of every artifact belonging to the run.
// GET /api/runs/:id/artifacts/pack — session auth; 404 when the run is missing.
func (h *Handlers) PackRunArtifacts(c *gin.Context) {
	runID := c.Param("id")
	run, ok := h.Runs.Get(runID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	dirLabel := strings.TrimSpace(run.Title)
	if dirLabel == "" {
		dirLabel = runID
	}
	dirName := services.SanitizeDownloadFilename(dirLabel)
	if dirName == "" || dirName == "folder" {
		dirName = services.SanitizeDownloadFilename(runID)
	}
	downloadName := dirName + "-artifacts.zip"

	arts := h.Arts.ByRunWithContent(runID)
	raw, err := buildRunArtifactsZIP(dirName, arts)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", contentDispositionAttachment(downloadName))
	c.Data(http.StatusOK, "application/zip", raw)
}

// buildRunArtifactsZIP writes one top-level directory with one file per artifact.
// Member bytes match DownloadArtifact (including image base64 decode). Paths are
// sanitized to block traversal while preserving original basenames/extensions.
func buildRunArtifactsZIP(dirName string, arts []models.Artifact) ([]byte, error) {
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	used := map[string]int{}

	for _, a := range arts {
		body, _ := decodeArtifactDownloadBody(a)
		base := zipMemberBaseName(a.Name)
		name := base
		if n := used[base]; n > 0 {
			name = fmt.Sprintf("%s_%d", base, n)
		}
		used[base]++

		entry := safeZipEntry(dirName, name)
		w, err := zw.Create(entry)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		if _, err := w.Write(body); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// zipMemberBaseName keeps the artifact basename (incl. extension) but strips
// directory components and null bytes so zip paths cannot escape the root dir.
func zipMemberBaseName(raw string) string {
	cleaned := strings.ReplaceAll(raw, "\\", "/")
	cleaned = strings.ReplaceAll(cleaned, "\x00", "_")
	base := path.Base(cleaned)
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == ".." {
		return "artifact"
	}
	base = strings.ReplaceAll(base, "/", "_")
	return base
}

func safeZipEntry(dirName, fileName string) string {
	dir := services.SanitizeDownloadFilename(dirName)
	if dir == "" || dir == "folder" {
		dir = "run"
	}
	file := zipMemberBaseName(fileName)
	entry := path.Join(dir, file)
	entry = path.Clean("/" + entry)
	entry = strings.TrimPrefix(entry, "/")
	if entry == "" || entry == "." || strings.Contains(entry, "..") {
		return path.Join(dir, "artifact")
	}
	if !strings.HasPrefix(entry, dir+"/") {
		return path.Join(dir, "artifact")
	}
	return entry
}
