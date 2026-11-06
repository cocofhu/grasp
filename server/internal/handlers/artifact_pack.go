package handlers

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
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

	// Spool the ZIP to a temporary file: memory remains bounded, and a missing
	// member can still return an error before sending a successful response.
	archive, err := os.CreateTemp("", "grasp-artifacts-*.zip")
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "artifact_pack_unavailable"})
		return
	}
	defer func() { _ = archive.Close(); _ = os.Remove(archive.Name()) }()
	arts := h.Arts.ByRun(runID)
	err = h.writeRunArtifactsZIP(c, archive, dirName, arts)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "artifact_pack_unavailable"})
		return
	}

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", contentDispositionAttachment(downloadName))
	if _, err = archive.Seek(0, io.SeekStart); err != nil {
		_ = c.Error(err)
		c.Status(http.StatusInternalServerError)
		return
	}
	http.ServeContent(c.Writer, c.Request, downloadName, run.StartedAt, archive)
}

func (h *Handlers) writeRunArtifactsZIP(c *gin.Context, dest io.Writer, dir string, arts []models.Artifact) error {
	zw := zip.NewWriter(dest)
	used := map[string]int{}
	for _, a := range arts {
		if err := c.Request.Context().Err(); err != nil {
			_ = zw.Close()
			return err
		}
		file, err := h.Arts.OpenFile(c.Request.Context(), a.ID, a.Revision)
		if err != nil {
			_ = zw.Close()
			return err
		}
		base := zipMemberBaseName(a.Name)
		name := base
		if used[base] > 0 {
			name = fmt.Sprintf("%s_%d", base, used[base])
		}
		used[base]++
		header := &zip.FileHeader{Name: safeZipEntry(dir, name), Method: zip.Deflate}
		if a.Kind == "video" || a.Kind == "audio" || a.Kind == "image" || a.Kind == "file" {
			header.Method = zip.Store
		}
		member, err := zw.CreateHeader(header)
		if err == nil {
			_, err = io.Copy(member, file.Reader)
		}
		closeErr := file.Reader.Close()
		if err != nil {
			_ = zw.Close()
			return err
		}
		if closeErr != nil {
			_ = zw.Close()
			return closeErr
		}
	}
	return zw.Close()
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
