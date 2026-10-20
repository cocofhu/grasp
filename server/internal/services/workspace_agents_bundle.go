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
	WorkspaceAgentsBundleKind          = "workspace-agents"
	WorkspaceAgentsBundleSchemaVersion = 1
	WorkspaceAgentsBundleMaxBytes      = 64 << 20 // 64 MiB
	workspaceAgentsManifestName        = "workspace.json"
)

var (
	ErrWorkspaceBundleTooLarge        = errors.New("工作区 Agent 包超过 64MiB 上限")
	ErrWorkspaceBundleMissingManifest = errors.New("ZIP 缺少 workspace.json，无法识别为工作区 Agent 包")
	ErrWorkspaceBundleSingleAgent     = errors.New("这是单 Agent ZIP，工作区导入仅接受工作区 Agent 包，请改用顶栏「导入」")
	ErrWorkspaceBundleInvalidKind     = errors.New("workspace.json kind 无效，须为 workspace-agents")
	ErrWorkspaceBundleBadSchema       = errors.New("不支持的 workspace.json schemaVersion")
	ErrWorkspaceBundleInvalidZip      = errors.New("ZIP 格式非法或已损坏")
	ErrWorkspaceBundleRootAgentJSON   = errors.New("工作区 Agent 包根目录不得包含 agent.json")
	ErrWorkspaceBundleNestedZip       = errors.New("工作区 Agent 包不得包含嵌套 ZIP")
)

// workspaceAgentsManifest is the root workspace.json inside a workspace package.
type workspaceAgentsManifest struct {
	Kind          string   `json:"kind"`
	SchemaVersion int      `json:"schemaVersion"`
	ExportedAt    string   `json:"exportedAt"`
	AgentNames    []string `json:"agentNames"`
}

// ImportWorkspaceAgentsMode selects batch rename vs overwrite.
type ImportWorkspaceAgentsMode string

const (
	ImportWorkspaceAgentsRename    ImportWorkspaceAgentsMode = "rename"
	ImportWorkspaceAgentsOverwrite ImportWorkspaceAgentsMode = "overwrite"
)

// ImportWorkspaceAgentsResult is returned after a successful workspace import.
type ImportWorkspaceAgentsResult struct {
	Created     []string          `json:"created,omitempty"`
	Overwritten []string          `json:"overwritten,omitempty"`
	Renamed     map[string]string `json:"renamed,omitempty"`
}

// agentNames lists every Agent name, sorted.
func (s *AgentService) agentNames() []string {
	names := []string{}
	for _, a := range s.List() {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	return names
}

// ExportWorkspaceAgentsZIP builds a workspace package with every Agent.
func (s *AgentService) ExportWorkspaceAgentsZIP() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := s.agentNames()
	manifest := workspaceAgentsManifest{
		Kind:          WorkspaceAgentsBundleKind,
		SchemaVersion: WorkspaceAgentsBundleSchemaVersion,
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		AgentNames:    names,
	}
	meta, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: workspaceAgentsManifestName, Method: zip.Store})
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
	if len(out) > WorkspaceAgentsBundleMaxBytes {
		return nil, ErrWorkspaceBundleTooLarge
	}
	return out, nil
}

// ImportWorkspaceAgentsZIP imports a workspace package. The whole import rolls
// back on any failure.
func (s *AgentService) ImportWorkspaceAgentsZIP(raw []byte, mode ImportWorkspaceAgentsMode) (result ImportWorkspaceAgentsResult, err error) {
	if int64(len(raw)) > WorkspaceAgentsBundleMaxBytes {
		return ImportWorkspaceAgentsResult{}, ErrWorkspaceBundleTooLarge
	}
	switch mode {
	case ImportWorkspaceAgentsRename, ImportWorkspaceAgentsOverwrite:
	default:
		return ImportWorkspaceAgentsResult{}, fmt.Errorf("invalid import mode %q", mode)
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return ImportWorkspaceAgentsResult{}, fmt.Errorf("%w: %v", ErrWorkspaceBundleInvalidZip, err)
	}
	hasManifest, hasRootAgent := false, false
	for _, f := range zr.File {
		name := strings.TrimPrefix(filepath.ToSlash(f.Name), "./")
		if strings.HasSuffix(name, "/") {
			continue
		}
		switch name {
		case workspaceAgentsManifestName:
			hasManifest = true
		case "agent.json":
			hasRootAgent = true
		}
		if strings.HasSuffix(strings.ToLower(name), ".zip") && strings.HasPrefix(name, "agents/") {
			return ImportWorkspaceAgentsResult{}, ErrWorkspaceBundleNestedZip
		}
	}
	if !hasManifest {
		if hasRootAgent {
			return ImportWorkspaceAgentsResult{}, ErrWorkspaceBundleSingleAgent
		}
		return ImportWorkspaceAgentsResult{}, ErrWorkspaceBundleMissingManifest
	}
	if hasRootAgent {
		return ImportWorkspaceAgentsResult{}, ErrWorkspaceBundleRootAgentJSON
	}
	manifest, err := readWorkspaceAgentsManifest(zr)
	if err != nil {
		return ImportWorkspaceAgentsResult{}, err
	}
	exports, err := parseBundleAgents(raw, manifest.AgentNames)
	if err != nil {
		return ImportWorkspaceAgentsResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existingNames := map[string]struct{}{}
	for _, a := range s.List() {
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
		if _, exists := existingNames[origName]; exists {
			if mode == ImportWorkspaceAgentsRename {
				normalized, nerr := NormalizeAndValidateAgentName(SuggestAgentRename(origName, existingNames))
				if nerr != nil {
					return ImportWorkspaceAgentsResult{}, fmt.Errorf("无法为 %q 生成合法重命名：%w", origName, nerr)
				}
				finalName = normalized
				renamed[origName] = finalName
				created = append(created, finalName)
				existingNames[finalName] = struct{}{}
			} else {
				overwritten = append(overwritten, origName)
			}
		} else {
			normalized, nerr := NormalizeAndValidateAgentName(origName)
			if nerr != nil {
				return ImportWorkspaceAgentsResult{}, fmt.Errorf("包内 Agent 名称无效 %q：%w", origName, nerr)
			}
			finalName = normalized
			created = append(created, finalName)
			existingNames[finalName] = struct{}{}
		}
		nameMap[origName] = finalName
	}

	snapDir, err := os.MkdirTemp("", "workspace-agents-import-*")
	if err != nil {
		return ImportWorkspaceAgentsResult{}, err
	}
	defer func() { _ = os.RemoveAll(snapDir) }()
	for _, name := range overwritten {
		if err := copyDir(filepath.Join(s.root, sanitize(name)), filepath.Join(snapDir, sanitize(name))); err != nil {
			return ImportWorkspaceAgentsResult{}, fmt.Errorf("快照 Agent %q 失败：%w", name, err)
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
			return ImportWorkspaceAgentsResult{}, fmt.Errorf("导入失败，已整次回滚：%w", importErr)
		}
		zipMode := ImportZIPCreate
		if mode == ImportWorkspaceAgentsOverwrite && containsString(overwritten, finalName) {
			zipMode = ImportZIPOverwrite
		}
		if _, err := s.applyAgentExport(parsed.export, parsed.files, finalName, zipMode); err != nil {
			importErr = err
			return ImportWorkspaceAgentsResult{}, fmt.Errorf("导入失败，已整次回滚：%w", err)
		}
	}

	return ImportWorkspaceAgentsResult{
		Created:     created,
		Overwritten: overwritten,
		Renamed:     renamed,
	}, nil
}

// stripTokenKeysFromEnvMap drops secret keys so a packaged Agent template can
// be saved; secrets belong in workspace credentials.
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

func readWorkspaceAgentsManifest(zr *zip.Reader) (workspaceAgentsManifest, error) {
	var manifest workspaceAgentsManifest
	for _, f := range zr.File {
		if strings.TrimPrefix(filepath.ToSlash(f.Name), "./") != workspaceAgentsManifestName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return manifest, fmt.Errorf("无法读取 workspace.json：%v", err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return manifest, fmt.Errorf("无法读取 workspace.json：%v", err)
		}
		if err := json.Unmarshal(b, &manifest); err != nil {
			return manifest, fmt.Errorf("workspace.json 格式无效：%v", err)
		}
		if strings.TrimSpace(manifest.Kind) != WorkspaceAgentsBundleKind {
			return manifest, ErrWorkspaceBundleInvalidKind
		}
		if manifest.SchemaVersion != WorkspaceAgentsBundleSchemaVersion {
			return manifest, fmt.Errorf("%w：%d", ErrWorkspaceBundleBadSchema, manifest.SchemaVersion)
		}
		return manifest, nil
	}
	return manifest, ErrWorkspaceBundleMissingManifest
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
