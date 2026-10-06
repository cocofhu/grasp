package services

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/cocofhu/grasp/internal/envauth"
)

const (
	ProjectAgentsBundleKind          = "project-agents"
	ProjectAgentsBundleSchemaVersion = 1
	ProjectAgentsBundleMaxBytes      = 64 << 20 // 64 MiB
	projectAgentsManifestName        = "project.json"
)

var (
	ErrProjectBundleTooLarge        = errors.New("项目 Agent 包超过 64MiB 上限")
	ErrProjectBundleMissingManifest = errors.New("ZIP 缺少 project.json，无法识别为项目 Agent 包")
	ErrProjectBundleSingleAgent     = errors.New("这是单 Agent ZIP，项目级导入仅接受项目 Agent 包，请改用顶栏「导入」")
	ErrProjectBundleInvalidKind     = errors.New("project.json kind 无效，须为 project-agents")
	ErrProjectBundleBadSchema       = errors.New("不支持的 project.json schemaVersion")
	ErrProjectBundleInvalidZip      = errors.New("ZIP 格式非法或已损坏")
	ErrProjectBundleRootAgentJSON   = errors.New("项目 Agent 包根目录不得包含 agent.json")
	ErrProjectBundleNestedZip       = errors.New("项目 Agent 包不得包含嵌套 ZIP")
	ErrProjectBundleForeignAgent    = errors.New("同名 Agent 属于其他项目，无法覆盖")
)

// projectAgentsManifest is the root project.json inside a project package.
type projectAgentsManifest struct {
	Kind          string   `json:"kind"`
	SchemaVersion int      `json:"schemaVersion"`
	ExportedAt    string   `json:"exportedAt"`
	AgentNames    []string `json:"agentNames"`
}

// ImportProjectAgentsMode selects batch rename vs overwrite.
type ImportProjectAgentsMode string

const (
	ImportProjectAgentsRename    ImportProjectAgentsMode = "rename"
	ImportProjectAgentsOverwrite ImportProjectAgentsMode = "overwrite"
)

// ImportProjectAgentsResult is returned after a successful project import.
type ImportProjectAgentsResult struct {
	Created     []string          `json:"created,omitempty"`
	Overwritten []string          `json:"overwritten,omitempty"`
	Renamed     map[string]string `json:"renamed,omitempty"`
}

// projectAgentNames lists Agents whose home project is projectID, sorted.
func (s *AgentService) projectAgentNames(projectID string) []string {
	names := []string{}
	for _, a := range s.List() {
		if AgentProjectMatches(a, projectID) {
			names = append(names, a.Name)
		}
	}
	sort.Strings(names)
	return names
}

// ExportProjectAgentsZIP builds a project package with every Agent of projectID.
func (s *AgentService) ExportProjectAgentsZIP(projectID string) ([]byte, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrAgentProjectRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	names := s.projectAgentNames(projectID)
	manifest := projectAgentsManifest{
		Kind:          ProjectAgentsBundleKind,
		SchemaVersion: ProjectAgentsBundleSchemaVersion,
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		AgentNames:    names,
	}
	meta, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: projectAgentsManifestName, Method: zip.Store})
	if err != nil {
		_ = zw.Close()
		return nil, err
	}
	if _, err := w.Write(meta); err != nil {
		_ = zw.Close()
		return nil, err
	}
	for _, name := range names {
		if err := s.writeAgentToZip(zw, name, "agents/"+name+"/"); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if len(out) > ProjectAgentsBundleMaxBytes {
		return nil, ErrProjectBundleTooLarge
	}
	return out, nil
}

// ImportProjectAgentsZIP imports a project package; every Agent is written
// with projectId = projectID. The whole import rolls back on any failure.
func (s *AgentService) ImportProjectAgentsZIP(raw []byte, projectID string, mode ImportProjectAgentsMode) (result ImportProjectAgentsResult, err error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ImportProjectAgentsResult{}, ErrAgentProjectRequired
	}
	if int64(len(raw)) > ProjectAgentsBundleMaxBytes {
		return ImportProjectAgentsResult{}, ErrProjectBundleTooLarge
	}
	switch mode {
	case ImportProjectAgentsRename, ImportProjectAgentsOverwrite:
	default:
		return ImportProjectAgentsResult{}, fmt.Errorf("invalid import mode %q", mode)
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return ImportProjectAgentsResult{}, fmt.Errorf("%w: %v", ErrProjectBundleInvalidZip, err)
	}
	hasManifest, hasRootAgent := false, false
	for _, f := range zr.File {
		name := strings.TrimPrefix(filepath.ToSlash(f.Name), "./")
		if strings.HasSuffix(name, "/") {
			continue
		}
		switch name {
		case projectAgentsManifestName:
			hasManifest = true
		case "agent.json":
			hasRootAgent = true
		}
		if strings.HasSuffix(strings.ToLower(name), ".zip") && strings.HasPrefix(name, "agents/") {
			return ImportProjectAgentsResult{}, ErrProjectBundleNestedZip
		}
	}
	if !hasManifest {
		if hasRootAgent {
			return ImportProjectAgentsResult{}, ErrProjectBundleSingleAgent
		}
		return ImportProjectAgentsResult{}, ErrProjectBundleMissingManifest
	}
	if hasRootAgent {
		return ImportProjectAgentsResult{}, ErrProjectBundleRootAgentJSON
	}
	manifest, err := readProjectAgentsManifest(zr)
	if err != nil {
		return ImportProjectAgentsResult{}, err
	}
	exports, err := parseBundleAgents(raw, manifest.AgentNames)
	if err != nil {
		return ImportProjectAgentsResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing := map[string]Agent{}
	existingNames := map[string]struct{}{}
	for _, a := range s.List() {
		existing[a.Name] = a
		existingNames[a.Name] = struct{}{}
	}

	nameMap := map[string]string{}
	created := []string{}
	overwritten := []string{}
	renamed := map[string]string{}
	for _, origName := range manifest.AgentNames {
		origName = strings.TrimSpace(origName)
		if origName == "" {
			continue
		}
		finalName := origName
		if prev, exists := existing[origName]; exists {
			if mode == ImportProjectAgentsRename {
				normalized, nerr := NormalizeAndValidateAgentName(SuggestAgentRename(origName, existingNames))
				if nerr != nil {
					return ImportProjectAgentsResult{}, fmt.Errorf("无法为 %q 生成合法重命名：%w", origName, nerr)
				}
				finalName = normalized
				renamed[origName] = finalName
				created = append(created, finalName)
				existingNames[finalName] = struct{}{}
			} else {
				if !AgentProjectMatches(prev, projectID) {
					return ImportProjectAgentsResult{}, fmt.Errorf("%w：%s", ErrProjectBundleForeignAgent, origName)
				}
				overwritten = append(overwritten, origName)
			}
		} else {
			normalized, nerr := NormalizeAndValidateAgentName(origName)
			if nerr != nil {
				return ImportProjectAgentsResult{}, fmt.Errorf("包内 Agent 名称无效 %q：%w", origName, nerr)
			}
			finalName = normalized
			created = append(created, finalName)
			existingNames[finalName] = struct{}{}
		}
		nameMap[origName] = finalName
	}

	snapDir, err := os.MkdirTemp("", "project-agents-import-*")
	if err != nil {
		return ImportProjectAgentsResult{}, err
	}
	defer func() { _ = os.RemoveAll(snapDir) }()
	for _, name := range overwritten {
		if err := copyDir(filepath.Join(s.root, sanitize(name)), filepath.Join(snapDir, sanitize(name))); err != nil {
			return ImportProjectAgentsResult{}, fmt.Errorf("快照 Agent %q 失败：%w", name, err)
		}
	}

	var importErr error
	defer func() {
		if importErr == nil {
			return
		}
		for _, name := range created {
			_ = s.deleteUnlocked(name)
		}
		for _, name := range overwritten {
			dst := filepath.Join(s.root, sanitize(name))
			_ = os.RemoveAll(dst)
			_ = copyDir(filepath.Join(snapDir, sanitize(name)), dst)
		}
	}()

	for _, origName := range manifest.AgentNames {
		finalName, ok := nameMap[strings.TrimSpace(origName)]
		if !ok {
			continue
		}
		parsed, ok := exports[strings.TrimSpace(origName)]
		if !ok {
			importErr = fmt.Errorf("包内缺少 Agent %q 的快照", origName)
			return ImportProjectAgentsResult{}, fmt.Errorf("导入失败，已整次回滚：%w", importErr)
		}
		zipMode := ImportZIPCreate
		if mode == ImportProjectAgentsOverwrite && containsString(overwritten, finalName) {
			zipMode = ImportZIPOverwrite
		}
		if _, err := s.applyAgentExport(parsed.export, parsed.files, finalName, projectID, zipMode); err != nil {
			importErr = err
			return ImportProjectAgentsResult{}, fmt.Errorf("导入失败，已整次回滚：%w", err)
		}
	}

	return ImportProjectAgentsResult{
		Created:     created,
		Overwritten: overwritten,
		Renamed:     renamed,
	}, nil
}

// stripTokenKeysFromEnvMap drops secret keys so a packaged Agent template can
// be saved; secrets belong in project credentials.
func stripTokenKeysFromEnvMap(env map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range envauth.StripSecretEnvKeys(env) {
		if k = strings.TrimSpace(k); k != "" {
			out[k] = v
		}
	}
	return out
}

type parsedBundleAgent struct {
	export agentExportJSON
	files  []AgentFile
}

func readProjectAgentsManifest(zr *zip.Reader) (projectAgentsManifest, error) {
	var manifest projectAgentsManifest
	for _, f := range zr.File {
		if strings.TrimPrefix(filepath.ToSlash(f.Name), "./") != projectAgentsManifestName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return manifest, fmt.Errorf("无法读取 project.json：%v", err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return manifest, fmt.Errorf("无法读取 project.json：%v", err)
		}
		if err := json.Unmarshal(b, &manifest); err != nil {
			return manifest, fmt.Errorf("project.json 格式无效：%v", err)
		}
		if strings.TrimSpace(manifest.Kind) != ProjectAgentsBundleKind {
			return manifest, ErrProjectBundleInvalidKind
		}
		if manifest.SchemaVersion != ProjectAgentsBundleSchemaVersion {
			return manifest, fmt.Errorf("%w：%d", ErrProjectBundleBadSchema, manifest.SchemaVersion)
		}
		return manifest, nil
	}
	return manifest, ErrProjectBundleMissingManifest
}

func parseBundleAgents(raw []byte, names []string) (map[string]parsedBundleAgent, error) {
	out := map[string]parsedBundleAgent{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		export, files, err := parseZipAgent(bytes.NewReader(raw), int64(len(raw)), "agents/"+name+"/", WorkspaceFileMaxBytes)
		if err != nil {
			return nil, fmt.Errorf("Agent %q：%w", name, err)
		}
		out[name] = parsedBundleAgent{export: export, files: files}
	}
	return out, nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// SanitizeDownloadFilename mirrors web/src/lib/workflowIO.ts sanitizeFilename
// (ASCII word chars, CJK unified, hyphen, space) without the .json suffix;
// unsafe separators become '_'. Empty input yields "".
func SanitizeDownloadFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-' || r == ' ':
			b.WriteRune(r)
		case unicode.Is(unicode.Han, r):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.TrimSpace(b.String())
}
