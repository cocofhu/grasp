package services

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

const artifactReferencePageSize = 20

type artifactReferenceItem struct {
	Kind     string `json:"kind"`
	Target   string `json:"target"`
	Title    string `json:"title"`
	FileKind string `json:"fileKind"`
	Source   string `json:"source,omitempty"`
}

func (s *ArtifactService) searchArtifactReferences(ctx context.Context, q ReferenceSearchQuery) (ReferenceSearchResult, error) {
	// Use the same visible products and search fields as the workspace Artifacts page.
	base := s.allQuery(ArtifactFilter{Q: q.Query}).WithContext(ctx)
	var count int64
	if err := base.Count(&count).Error; err != nil {
		return ReferenceSearchResult{}, err
	}
	items := []artifactReferenceItem{}
	total := int(count)
	page := max(q.Page, 1)
	res := ReferenceSearchResult{Lane: "artifact", Items: items, Total: &total, Source: "search"}
	if total == 0 || page > (total-1)/artifactReferencePageSize+1 {
		return res, nil
	}
	offset := (page - 1) * artifactReferencePageSize
	var rows []models.Artifact
	if err := base.Order("artifacts.updated_at DESC, artifacts.id DESC").Offset(offset).Limit(artifactReferencePageSize).Scan(&rows).Error; err != nil {
		return ReferenceSearchResult{}, err
	}
	if total-offset > artifactReferencePageSize {
		res.NextPage = page + 1
	}
	for _, a := range rows {
		items = append(items, artifactReferenceItem{Kind: models.ReferenceKindArtifact, Target: a.ID, Title: a.Name, FileKind: a.Kind,
			Source: cmp.Or(a.RunTitle, a.WorkflowName, a.RunID)})
	}
	res.Items = items
	return res, nil
}

func (s *ArtifactService) artifactReferenceRecord(ctx context.Context, id string) (models.Artifact, error) {
	var a models.Artifact
	err := excludeHiddenArtifacts(s.db.WithContext(ctx).Select("id", "name", "revision"), "name").Take(&a, "id = ?", id).Error
	return a, err
}

func (s *ArtifactService) prepareArtifactReference(ctx context.Context, id string) (models.MessageReference, error) {
	a, err := s.artifactReferenceRecord(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.MessageReference{}, soloErr(http.StatusNotFound, "reference_not_found", "引用的产物不存在")
	}
	if err != nil {
		return models.MessageReference{}, err
	}
	return models.MessageReference{Kind: models.ReferenceKindArtifact, Target: a.ID, Title: a.Name, Anchor: strconv.Itoa(max(a.Revision, 1))}, nil
}

func renderArtifactReference(ref models.MessageReference) string {
	if ref.Error == models.ReferenceErrorDeleted {
		return fmt.Sprintf("- %s  产物「%s」⚠ 已被删除，无法读取\n", ref.ID, ref.Title)
	}
	if ref.Error != "" {
		return fmt.Sprintf("- %s  产物「%s」⚠ 暂时无法读取\n", ref.ID, ref.Title)
	}
	return fmt.Sprintf("- %s  产物「%s」(v%s)：%s\n", ref.ID, ref.Title, ref.Anchor, referenceReadTool(ref.ID))
}

func artifactReferenceRevision(ref models.MessageReference) (int, error) {
	revision, err := strconv.Atoi(ref.Anchor)
	if ref.Kind != models.ReferenceKindArtifact || ref.Target == "" || ref.UserID == "" || err != nil || revision < 1 {
		return 0, errors.New("引用的产物不可用")
	}
	return revision, nil
}

func (s *ConversationHistoryService) readArtifactReference(ref models.MessageReference) (mcp.ConversationHistoryPage, error) {
	page := mcp.ConversationHistoryPage{Ref: ref.ID, HistoryVersion: ref.Anchor, Messages: []mcp.ConversationHistoryMessage{}}
	revision, err := artifactReferenceRevision(ref)
	if err != nil {
		return page, err
	}
	file, err := (&ArtifactService{db: s.db}).OpenFile(context.Background(), ref.Target, revision)
	if err != nil {
		return page, errors.New("引用的产物或版本已删除或暂时不可用")
	}
	defer func() { _ = file.Reader.Close() }()
	page.Artifact = &mcp.ReferenceArtifact{Name: file.Artifact.Name, Kind: file.Artifact.Kind, MIMEType: file.MimeType, Revision: revision,
		Download: artifactReferenceDownload(ref.ID)}
	switch file.Artifact.Kind {
	case "text", "markdown", "json", "yaml", "html":
		const excerptBytes = 8 << 10
		data, err := io.ReadAll(io.LimitReader(file.Reader, excerptBytes+1))
		if err != nil {
			return page, errors.New("引用的产物暂时无法读取")
		}
		page.Artifact.Truncated = len(data) > excerptBytes
		page.Artifact.Content = strings.ToValidUTF8(string(data[:min(len(data), excerptBytes)]), "�")
	}
	return page, nil
}

func artifactReferenceDownload(refID string) string {
	// Solo configures grasp-tool in mcp.json even when it does not set the
	// workflow environment variables. Use either source without printing credentials.
	return fmt.Sprintf(`python3 - <<'PY'
import json, os, shutil, tempfile, urllib.request
from pathlib import Path

base = os.environ.get("GRASP_ARTIFACT_URL")
token = os.environ.get("GRASP_ARTIFACT_TOKEN")
if base and token:
    headers = {"Authorization": "Bearer " + token}
else:
    config = Path(os.environ.get("CONFIG_ROOT") or "/root/.cursor") / "mcp.json"
    server = json.loads(config.read_text())["mcpServers"]["grasp-tool"]
    base, headers = server["url"], server["headers"]
request = urllib.request.Request(base.rstrip("/") + %q, headers=headers)
with urllib.request.urlopen(request, timeout=60) as response:
    with tempfile.NamedTemporaryFile(prefix="grasp-reference-", delete=False) as file:
        shutil.copyfileobj(response, file)
        print(file.name)
PY`, "/references/"+url.PathEscape(refID))
}
