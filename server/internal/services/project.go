package services

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SoloError carries an HTTP status for Solo / project API failures.
// Params are interpolation values for localized clients (names, keys).
type SoloError struct {
	Status int
	Code   string
	Msg    string
	Params map[string]any
}

func (e *SoloError) Error() string { return e.Msg }

func soloErr(status int, code, format string, args ...any) error {
	return &SoloError{Status: status, Code: code, Msg: fmt.Sprintf(format, args...)}
}

func soloErrParams(status int, code, format string, params map[string]any, args ...any) error {
	return &SoloError{Status: status, Code: code, Msg: fmt.Sprintf(format, args...), Params: params}
}

// ProjectInput is the create / update shape of a project.
type ProjectInput struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	RepoURL       string `json:"repoUrl"`
	DefaultBranch string `json:"defaultBranch"`
	RepoDirName   string `json:"repoDirName"`
	CredentialID  string `json:"credentialId"`
	CommitName    string `json:"commitName"`
	CommitEmail   string `json:"commitEmail"`
}

// ProjectService manages repositories (one Git repository each) used by Solo
// sessions and workflows.
type ProjectService struct {
	db            *gorm.DB
	gitCredential runtime.GitCredentialFunc
}

// SetGitCredentialResolver lets saves check a repository's credential.
func (s *ProjectService) SetGitCredentialResolver(resolve runtime.GitCredentialFunc) {
	s.gitCredential = resolve
}

// NewProjectService builds the service.
func NewProjectService(db *gorm.DB) *ProjectService {
	return &ProjectService{db: db}
}

var repoDirPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

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
	in.CredentialID = strings.TrimSpace(in.CredentialID)
	in.CommitName = strings.TrimSpace(in.CommitName)
	in.CommitEmail = strings.TrimSpace(in.CommitEmail)

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
	if len([]rune(in.CommitName)) > 120 || strings.ContainsAny(in.CommitName, "<>\r\n\x00") {
		return in, soloErr(http.StatusBadRequest, "commit_name_invalid", "提交用户名不能包含 <、>、换行，且不超过 120 个字符")
	}
	if in.CommitEmail != "" && !commitEmailPattern.MatchString(in.CommitEmail) {
		return in, soloErr(http.StatusBadRequest, "commit_email_invalid", "提交邮箱格式不正确")
	}
	return in, nil
}

var commitEmailPattern = regexp.MustCompile(`^[^\s<>@]+@[^\s<>@]+$`)

// checkCredential rejects a credential that cannot authorize the repository's
// URL: unknown or revoked, a token on an SSH address or the reverse, or a
// token for another host.
func (s *ProjectService) checkCredential(in ProjectInput) error {
	if in.CredentialID == "" {
		return nil
	}
	if s.gitCredential == nil {
		return soloErr(http.StatusServiceUnavailable, "credential_unavailable", "凭据服务不可用")
	}
	if err := runtime.CheckRepoCredential(in.RepoURL, in.CredentialID, s.gitCredential); err != nil {
		return soloErr(http.StatusBadRequest, "credential_invalid", "仓库凭据不可用：%s", err.Error())
	}
	return nil
}

func applyProjectInput(p *models.Project, in ProjectInput) {
	p.Name = in.Name
	p.Description = in.Description
	p.RepoURL = in.RepoURL
	p.DefaultBranch = in.DefaultBranch
	p.RepoDirName = in.RepoDirName
	p.CredentialID = in.CredentialID
	p.CommitName = in.CommitName
	p.CommitEmail = in.CommitEmail
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
	if err := s.checkCredential(in); err != nil {
		return nil, err
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
	if err := s.checkCredential(in); err != nil {
		return nil, err
	}
	applyProjectInput(p, in)
	if err := s.db.Save(p).Error; err != nil {
		return nil, err
	}
	return p, nil
}

// Delete removes a project and unbinds it from every workflow. Live sessions
// and started runs keep their snapshot.
func (s *ProjectService) Delete(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var defs []models.WorkflowDef
		if err := tx.Select("id", "defaults").Find(&defs).Error; err != nil {
			return err
		}
		for _, def := range defs {
			if def.Defaults == nil {
				continue
			}
			kept := make([]models.RepoChoice, 0, len(def.Defaults.Repos))
			for _, r := range def.Defaults.Repos {
				if r.ProjectID != id {
					kept = append(kept, r)
				}
			}
			if len(kept) == len(def.Defaults.Repos) {
				continue
			}
			next := *def.Defaults
			next.Repos = kept
			if err := tx.Model(&models.WorkflowDef{}).Where("id = ?", def.ID).Select("Defaults").Updates(&models.WorkflowDef{Defaults: &next}).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&models.Project{}, "id = ?", id).Error
	})
}

// ActiveSessions lists sessions still holding a sandbox for the project.
// A session counts when the id is its legacy project_id or any frozen repo.
func (s *ProjectService) ActiveSessions(id string) ([]models.SoloSession, error) {
	id = strings.TrimSpace(id)
	var rows []models.SoloSession
	err := s.db.Where("status IN ?", []string{models.SoloStatusStarting, models.SoloStatusRunning}).
		Order("updated_at desc").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]models.SoloSession, 0)
	for i := range rows {
		if sessionUsesProject(&rows[i], id) {
			out = append(out, rows[i])
		}
	}
	return out, nil
}

func sessionUsesProject(row *models.SoloSession, id string) bool {
	if id == "" {
		return false
	}
	if row.ProjectID == id {
		return true
	}
	if row.ProjectSnapshot != nil && row.ProjectSnapshot.ID == id {
		return true
	}
	for _, repo := range row.Repos {
		if repo.ProjectID == id {
			return true
		}
	}
	return false
}

// Snapshot freezes the chosen repositories in order, each on its chosen
// branch or its default branch.
func (s *ProjectService) Snapshot(choices []models.RepoChoice) ([]models.SessionRepo, error) {
	out := make([]models.SessionRepo, 0, len(choices))
	for i, c := range choices {
		p, err := s.Get(c.ProjectID)
		if err != nil {
			return nil, err
		}
		out = append(out, SessionRepoFromProject(p, c.Branch, i))
	}
	if err := ValidateSessionRepos(out); err != nil {
		return nil, err
	}
	return out, nil
}

// Touch records that a project was used to start a session.
func (s *ProjectService) Touch(id string) {
	now := time.Now()
	s.db.Model(&models.Project{}).Where("id = ?", id).UpdateColumn("last_used_at", &now)
}
