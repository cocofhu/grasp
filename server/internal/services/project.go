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

// ProjectInput is the create / update shape of a project. Older clients may
// still send access, credential and environment fields; those keys are not
// part of this struct, so encoding/json drops them and they never persist.
type ProjectInput struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	RepoURL       string `json:"repoUrl"`
	DefaultBranch string `json:"defaultBranch"`
	RepoDirName   string `json:"repoDirName"`
}

// ProjectService manages Solo projects (one Git repository each).
type ProjectService struct {
	db *gorm.DB
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
	return in, nil
}

func applyProjectInput(p *models.Project, in ProjectInput) {
	p.Name = in.Name
	p.Description = in.Description
	p.RepoURL = in.RepoURL
	p.DefaultBranch = in.DefaultBranch
	p.RepoDirName = in.RepoDirName
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

// Touch records that a project was used to start a session.
func (s *ProjectService) Touch(id string) {
	now := time.Now()
	s.db.Model(&models.Project{}).Where("id = ?", id).UpdateColumn("last_used_at", &now)
}
