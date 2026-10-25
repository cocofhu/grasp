package services

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SoloError carries an HTTP status for Solo / project API failures.
type SoloError struct {
	Status int
	Code   string
	Msg    string
}

func (e *SoloError) Error() string { return e.Msg }

func soloErr(status int, code, format string, args ...any) error {
	return &SoloError{Status: status, Code: code, Msg: fmt.Sprintf(format, args...)}
}

// ProjectInput is the create / update shape of a project.
type ProjectInput struct {
	Name                 string             `json:"name"`
	Description          string             `json:"description"`
	RepoURL              string             `json:"repoUrl"`
	DefaultBranch        string             `json:"defaultBranch"`
	RepoDirName          string             `json:"repoDirName"`
	GitCredentialType    string             `json:"gitCredentialType"`
	GitCredentialID      string             `json:"gitCredentialId"`
	SshHostsCredentialID string             `json:"sshHostsCredentialId"`
	Env                  map[string]string  `json:"env"`
	CredentialBindings   map[string]*string `json:"credentialBindings"`
}

// ProjectService manages Solo projects (one Git repository each).
type ProjectService struct {
	db *gorm.DB
}

// NewProjectService builds the service.
func NewProjectService(db *gorm.DB) *ProjectService {
	return &ProjectService{db: db}
}

var (
	envKeyPattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	repoDirPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	// sensitiveEnvMarkers flag env names that look like secrets; those must be
	// supplied through workspace credentials, not stored on the project.
	sensitiveEnvMarkers = []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "APIKEY", "PRIVATE_KEY", "ACCESS_KEY", "CREDENTIAL"}
	// soloReservedEnvKeys are written by the platform when the sandbox starts.
	soloReservedEnvKeys = map[string]bool{
		"GIT_REPOS": true, "GRASP_THREAD_ID": true, "CODEX_HOME": true,
		"ACP_BRIDGE_MODEL": true, "GRASP_CONFIGURED_MODELS": true, "GRASP_INITIAL_CHAT_MODEL": true,
	}
)

// IsSensitiveEnvName reports env names that must come from credentials.
func IsSensitiveEnvName(k string) bool {
	k = strings.ToUpper(strings.TrimSpace(k))
	if envauth.IsSecretEnvKey(k) || envauth.IsPlatformAuthEnvKey(k) {
		return true
	}
	for _, m := range sensitiveEnvMarkers {
		if strings.Contains(k, m) {
			return true
		}
	}
	return false
}

func validRepoURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, "https://"), strings.HasPrefix(raw, "http://"),
		strings.HasPrefix(raw, "ssh://"), strings.HasPrefix(raw, "git://"):
		return len(raw) > len("https://")
	case strings.HasPrefix(raw, "git@"):
		return strings.Contains(raw, ":")
	}
	return false
}

func normalizeProjectInput(in ProjectInput) (ProjectInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.RepoURL = strings.TrimSpace(in.RepoURL)
	in.DefaultBranch = strings.TrimSpace(in.DefaultBranch)
	in.RepoDirName = strings.TrimSpace(in.RepoDirName)
	in.GitCredentialType = strings.TrimSpace(in.GitCredentialType)
	in.GitCredentialID = strings.TrimSpace(in.GitCredentialID)
	in.SshHostsCredentialID = strings.TrimSpace(in.SshHostsCredentialID)

	if in.Name == "" {
		return in, soloErr(http.StatusBadRequest, "name_required", "仓库名称不能为空")
	}
	if len([]rune(in.Name)) > 80 {
		return in, soloErr(http.StatusBadRequest, "name_too_long", "仓库名称不能超过 80 个字符")
	}
	if !validRepoURL(in.RepoURL) {
		return in, soloErr(http.StatusBadRequest, "repo_url_invalid", "仓库地址需以 https://、ssh:// 或 git@ 开头")
	}
	if strings.ContainsAny(in.DefaultBranch, " |,") {
		return in, soloErr(http.StatusBadRequest, "branch_invalid", "分支名不能包含空格、| 或 ,")
	}
	if in.RepoDirName == "" {
		in.RepoDirName = sandbox.RepoNameFromURL(in.RepoURL)
	}
	if in.RepoDirName != "" && (!repoDirPattern.MatchString(in.RepoDirName) || in.RepoDirName == "." || in.RepoDirName == "..") {
		return in, soloErr(http.StatusBadRequest, "repo_dir_invalid", "目录名只能包含字母、数字、. _ -")
	}
	switch in.GitCredentialType {
	case "":
		in.GitCredentialID, in.SshHostsCredentialID = "", ""
	case "github_https", "gitlab_https":
		in.SshHostsCredentialID = ""
	case "ssh":
	default:
		return in, soloErr(http.StatusBadRequest, "git_type_invalid", "不支持的访问方式 %q", in.GitCredentialType)
	}
	env := map[string]string{}
	for k, v := range in.Env {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if !envKeyPattern.MatchString(k) {
			return in, soloErr(http.StatusBadRequest, "env_key_invalid", "环境变量名 %q 不合法", k)
		}
		if envauth.IsPlatformReservedEnvKey(k) || soloReservedEnvKeys[k] {
			return in, soloErr(http.StatusBadRequest, "env_key_reserved", "环境变量 %s 由平台注入，不能在仓库配置里设置", k)
		}
		if IsSensitiveEnvName(k) {
			return in, soloErr(http.StatusBadRequest, "env_key_sensitive", "%s 看起来是敏感值，请放到凭据中并通过凭据绑定引用", k)
		}
		env[k] = v
	}
	in.Env = env
	bindings := map[string]*string{}
	for k, id := range in.CredentialBindings {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if !envKeyPattern.MatchString(k) || envauth.IsPlatformReservedEnvKey(k) || soloReservedEnvKeys[k] {
			return in, soloErr(http.StatusBadRequest, "binding_key_invalid", "凭据绑定的变量名 %q 不可用", k)
		}
		if id == nil || strings.TrimSpace(*id) == "" {
			continue
		}
		v := strings.TrimSpace(*id)
		bindings[k] = &v
	}
	in.CredentialBindings = bindings
	return in, nil
}

func applyProjectInput(p *models.Project, in ProjectInput) {
	p.Name = in.Name
	p.Description = in.Description
	p.RepoURL = in.RepoURL
	p.DefaultBranch = in.DefaultBranch
	p.RepoDirName = in.RepoDirName
	p.GitCredentialType = in.GitCredentialType
	p.GitCredentialID = in.GitCredentialID
	p.SshHostsCredentialID = in.SshHostsCredentialID
	p.Env = in.Env
	p.CredentialBindings = in.CredentialBindings
}

// List returns projects, most recently used first.
func (s *ProjectService) List() ([]models.Project, error) {
	var rows []models.Project
	if err := s.db.Find(&rows).Error; err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].LastUsedAt, rows[j].LastUsedAt
		switch {
		case a != nil && b != nil && !a.Equal(*b):
			return a.After(*b)
		case a != nil && b == nil:
			return true
		case a == nil && b != nil:
			return false
		}
		return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
	})
	return rows, nil
}

// Get returns one project.
func (s *ProjectService) Get(id string) (*models.Project, error) {
	var p models.Project
	if err := s.db.Where("id = ?", strings.TrimSpace(id)).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, soloErr(http.StatusNotFound, "project_not_found", "仓库不存在")
		}
		return nil, err
	}
	return &p, nil
}

func (s *ProjectService) nameTaken(name, exceptID string) bool {
	var n int64
	s.db.Model(&models.Project{}).Where("name = ? AND id <> ?", name, exceptID).Count(&n)
	return n > 0
}

// Create stores a new project.
func (s *ProjectService) Create(in ProjectInput, user string) (*models.Project, error) {
	in, err := normalizeProjectInput(in)
	if err != nil {
		return nil, err
	}
	if s.nameTaken(in.Name, "") {
		return nil, soloErr(http.StatusConflict, "name_taken", "已存在同名仓库")
	}
	p := &models.Project{ID: uuid.NewString(), CreatedBy: user}
	applyProjectInput(p, in)
	if err := s.db.Create(p).Error; err != nil {
		return nil, err
	}
	return p, nil
}

// Update replaces a project's editable fields.
func (s *ProjectService) Update(id string, in ProjectInput) (*models.Project, error) {
	p, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	in, err = normalizeProjectInput(in)
	if err != nil {
		return nil, err
	}
	if s.nameTaken(in.Name, p.ID) {
		return nil, soloErr(http.StatusConflict, "name_taken", "已存在同名仓库")
	}
	applyProjectInput(p, in)
	if err := s.db.Save(p).Error; err != nil {
		return nil, err
	}
	return p, nil
}

// Delete removes a project. Live sessions keep their snapshot.
func (s *ProjectService) Delete(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.db.Delete(&models.Project{}, "id = ?", id).Error
}

// ActiveSessions lists sessions still holding a sandbox for the project.
func (s *ProjectService) ActiveSessions(id string) ([]models.SoloSession, error) {
	var rows []models.SoloSession
	err := s.db.Where("project_id = ? AND status IN ?", id,
		[]string{models.SoloStatusStarting, models.SoloStatusRunning}).
		Order("updated_at desc").Find(&rows).Error
	return rows, err
}

// Touch records that a project was used to start a session.
func (s *ProjectService) Touch(id string) {
	now := time.Now()
	s.db.Model(&models.Project{}).Where("id = ?", id).UpdateColumn("last_used_at", &now)
}
