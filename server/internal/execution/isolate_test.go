package execution

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestPrepareSharesLaunchAndDeletesOnEnd(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	t.Cleanup(ResetForTest)

	repo := initRepo(t)
	feature := gitOut(t, repo, "rev-parse", "refs/heads/feature")
	src := []Source{{Name: "app", URL: repo, Branch: "feature"}}

	first, err := Prepare("run-a", src)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Prepare("run-a", src)
	if err != nil {
		t.Fatal(err)
	}
	if first.Root != second.Root {
		t.Fatalf("same launch diverged: %s %s", first.Root, second.Root)
	}
	if len(first.Repos) != 1 || first.Repos[0].Kind != "worktree" {
		t.Fatalf("repos=%+v", first.Repos)
	}
	if gitOut(t, first.Repos[0].Path, "rev-parse", "HEAD") != feature {
		t.Fatal("worktree is not at the selected branch HEAD")
	}
	if err := exec.Command("git", "-C", first.Repos[0].Path, "symbolic-ref", "-q", "HEAD").Run(); err == nil {
		t.Fatal("worktree is still on a branch")
	}
	other, err := Prepare("run-b", src)
	if err != nil {
		t.Fatal(err)
	}
	if other.Root == first.Root {
		t.Fatal("parallel launches shared a directory")
	}
	if _, err := os.Stat(first.Root); err != nil {
		t.Fatal("directory removed while the launch is in progress")
	}
	Release("run-a", false)
	if _, err := os.Stat(first.Root); err != nil {
		t.Fatal("releasing one node removed the shared directory")
	}
	End("run-a")
	if _, err := os.Stat(first.Root); !os.IsNotExist(err) {
		t.Fatalf("ended launch still present: %v", err)
	}
	if _, err := os.Stat(other.Root); err != nil {
		t.Fatal("ending one launch removed the other")
	}
	mainHead := gitOut(t, repo, "rev-parse", "HEAD")
	if mainHead == "" {
		t.Fatal("source repo was damaged")
	}
	End("run-b")
	if _, err := Prepare("run-a", src); err == nil {
		t.Fatal("ended launch was recreated")
	}
}

func TestPrepareFolderWhenNotGit(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "note.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Prepare("solo-1", []Source{{Name: "notes", URL: src}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 1 || got.Repos[0].Kind != "folder" {
		t.Fatalf("repos=%+v", got.Repos)
	}
	if exec.Command("git", "-C", got.Repos[0].Path, "rev-parse", "--is-inside-work-tree").Run() == nil {
		t.Fatal("plain folder was turned into a worktree")
	}
	body, err := os.ReadFile(filepath.Join(got.Repos[0].Path, "note.txt"))
	if err != nil || string(body) != "keep" {
		t.Fatalf("copy=%q err=%v", body, err)
	}
	if err := os.WriteFile(filepath.Join(got.Repos[0].Path, "note.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig, err := os.ReadFile(filepath.Join(src, "note.txt"))
	if err != nil || string(orig) != "keep" {
		t.Fatalf("source changed: %q %v", orig, err)
	}
	Release("solo-1", true)
	if _, err := os.Stat(got.Root); !os.IsNotExist(err) {
		t.Fatal("solo directory survived release")
	}
}

func TestParallelLaunchesDiffer(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	var wg sync.WaitGroup
	roots := make([]string, 2)
	errs := make([]error, 2)
	for i, id := range []string{"p-1", "p-2"} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			got, err := Prepare(id, nil)
			errs[i] = err
			if err == nil {
				roots[i] = got.Root
			}
		}(i, id)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if roots[0] == "" || roots[0] == roots[1] {
		t.Fatalf("roots=%v", roots)
	}
	End("p-1")
	End("p-2")
}

func TestPrepareRollsBackRegisteredWorktree(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	t.Cleanup(ResetForTest)
	repo := initRepo(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	_, err := Prepare("multi", []Source{
		{Name: "app", URL: repo, Branch: "main"},
		{Name: "remote", URL: srv.URL + "/secret.git", Branch: "main"},
	})
	if err == nil {
		t.Fatal("expected the second repository to fail")
	}
	if _, statErr := os.Stat(filepath.Join(os.Getenv("GRASP_WORKTREE_ROOT"), "multi")); !os.IsNotExist(statErr) {
		t.Fatal("failed launch directory was kept")
	}
	list := gitOut(t, repo, "worktree", "list", "--porcelain")
	if strings.Contains(list, "prunable") || strings.Count(list, "worktree ") != 1 {
		t.Fatalf("source repo kept a deleted worktree:\n%s", list)
	}
}

func TestPrepareAuthClonesPrivateHTTP(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	t.Cleanup(ResetForTest)
	isolateGitConfig(t)
	bare, feature := bareFeatureRepo(t)
	const token = "secret-token"
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "x-access-token" || pass != token {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		sawAuth = true
		serveGitHTTP(w, r, filepath.Dir(bare))
	}))
	t.Cleanup(srv.Close)
	cloneURL := srv.URL + "/repo.git"

	got, err := PrepareAuth("priv", []Source{{Name: "app", URL: cloneURL, Branch: "feature"}}, Auth{
		Configured: true,
		Credentials: []Credential{{
			Kind: credGitHubHTTPS, Scope: cloneURL, Secret: token,
		}},
		GitHubToken: "ambient-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawAuth {
		t.Fatal("clone did not send the catalog token")
	}
	if len(got.Repos) != 1 || got.Repos[0].Kind != "worktree" {
		t.Fatalf("repos=%+v", got.Repos)
	}
	if gitOut(t, got.Repos[0].Path, "rev-parse", "HEAD") != feature {
		t.Fatal("worktree is not at the selected branch HEAD")
	}
	if exec.Command("git", "-C", got.Repos[0].Path, "symbolic-ref", "-q", "HEAD").Run() == nil {
		t.Fatal("worktree is still on a branch")
	}
	mirror := filepath.Join(os.Getenv("GRASP_WORKTREE_ROOT"), ".mirrors", "priv", "app")
	if info, err := os.Stat(mirror); err != nil || !info.IsDir() {
		t.Fatalf("mirror directory: %v", err)
	}
	if body, err := os.ReadFile(os.Getenv("GIT_CONFIG_GLOBAL")); err == nil && strings.Contains(string(body), "credential") {
		t.Fatal("credential helper was written to process git config")
	}
	End("priv")
}

func TestPrepareAuthRejectsPrivateHTTPWithoutCredential(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	t.Cleanup(ResetForTest)
	isolateGitConfig(t)
	bare, _ := bareFeatureRepo(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, _, ok := r.BasicAuth(); ok && user != "" {
			t.Errorf("unexpected authorization %q", user)
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	cloneURL := srv.URL + "/repo.git"
	_, err := PrepareAuth("priv-no", []Source{{Name: "app", URL: cloneURL, Branch: "feature"}}, Auth{
		Configured:  true,
		GitHubToken: "ambient-token",
	})
	if err == nil {
		t.Fatal("expected clone to fail without a selected credential")
	}
	if _, statErr := os.Stat(filepath.Join(os.Getenv("GRASP_WORKTREE_ROOT"), "priv-no")); !os.IsNotExist(statErr) {
		t.Fatal("failed launch directory was kept")
	}
	if _, statErr := os.Stat(filepath.Join(os.Getenv("GRASP_WORKTREE_ROOT"), ".mirrors", "priv-no")); !os.IsNotExist(statErr) {
		t.Fatal("failed clone cache was kept")
	}
	if _, statErr := os.Stat(bare); statErr != nil {
		t.Fatal(statErr)
	}
}

func TestPrepareBranchWithSlash(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	t.Cleanup(ResetForTest)
	repo := initRepo(t)
	gitRun(t, repo, "checkout", "-b", "topic/slash")
	want := gitOut(t, repo, "rev-parse", "refs/heads/topic/slash")
	got, err := Prepare("slash", []Source{{Name: "app", URL: repo, Branch: "topic/slash"}})
	if err != nil {
		t.Fatal(err)
	}
	if gitOut(t, got.Repos[0].Path, "rev-parse", "HEAD") != want {
		t.Fatal("slash branch was not checked out")
	}
	End("slash")
}

func TestPrepareEmptyBranchUsesHEAD(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	t.Cleanup(ResetForTest)
	repo := initRepo(t)
	want := gitOut(t, repo, "rev-parse", "HEAD")
	got, err := Prepare("empty-branch", []Source{{Name: "app", URL: repo}})
	if err != nil {
		t.Fatal(err)
	}
	if gitOut(t, got.Repos[0].Path, "rev-parse", "HEAD") != want {
		t.Fatal("empty branch did not use HEAD")
	}
	End("empty-branch")
}

func TestPrepareCopiesNamesWithTwoDots(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	t.Cleanup(ResetForTest)
	src := t.TempDir()
	if err := os.Mkdir(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "foo..bar.txt"), []byte("dots"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "a..b"), []byte("inner"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Prepare("dots", []Source{{Name: "notes", URL: src}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(got.Repos[0].Path, "foo..bar.txt"))
	if err != nil || string(body) != "dots" {
		t.Fatalf("copy=%q err=%v", body, err)
	}
	inner, err := os.ReadFile(filepath.Join(got.Repos[0].Path, "sub", "a..b"))
	if err != nil || string(inner) != "inner" {
		t.Fatalf("nested=%q err=%v", inner, err)
	}
	End("dots")
}

func TestPrepareRejectsLocalPathWithDotDot(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	t.Cleanup(ResetForTest)
	base := t.TempDir()
	sentinel := filepath.Join(base, "sentinel")
	if err := os.Mkdir(sentinel, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sentinel, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Join would clean ".." away; the address must still contain the segment.
	url := filepath.Join(base, "missing") + "/../sentinel"
	if _, err := Prepare("dotdot", []Source{{Name: "app", URL: url}}); err == nil {
		t.Fatal("expected local path with .. to fail")
	}
	body, err := os.ReadFile(filepath.Join(sentinel, "keep"))
	if err != nil || string(body) != "x" {
		t.Fatalf("sentinel changed: %q %v", body, err)
	}
	if _, statErr := os.Stat(filepath.Join(os.Getenv("GRASP_WORKTREE_ROOT"), "dotdot")); !os.IsNotExist(statErr) {
		t.Fatal("rejected launch directory was kept")
	}
}

func TestPrepareRejectsBadRemoteAndBranch(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	t.Cleanup(ResetForTest)
	repo := initRepo(t)
	cases := []Source{
		{Name: "app", URL: "git://example.com/repo.git", Branch: "main"},
		{Name: "app", URL: "http://example.com/a b.git", Branch: "main"},
		{Name: "app", URL: "-not-a-url", Branch: "main"},
		{Name: "app", URL: repo, Branch: "feature/../main"},
		{Name: "app", URL: repo, Branch: "-rf"},
	}
	for _, src := range cases {
		if _, err := Prepare("bad", []Source{src}); err == nil {
			t.Fatalf("expected rejection for %+v", src)
		}
		if _, statErr := os.Stat(filepath.Join(os.Getenv("GRASP_WORKTREE_ROOT"), "bad")); !os.IsNotExist(statErr) {
			t.Fatalf("rejected launch kept for %+v", src)
		}
	}
	if _, err := validateRemote("git@github.com:org/repo.git"); err != nil {
		t.Fatal(err)
	}
	if _, err := validateRemote("ssh://git@github.com/org/repo.git"); err != nil {
		t.Fatal(err)
	}
	if _, err := validateRemote("https://example.com/a/../b.git"); err == nil {
		t.Fatal("expected .. in a remote URL to fail")
	}
}

func TestCopyTargetRejectsEscape(t *testing.T) {
	dst := t.TempDir()
	outside := filepath.Join(filepath.Dir(dst), "outside-copy")
	if _, err := copyTarget(dst, ".."); err == nil {
		t.Fatal("relative .. was accepted")
	}
	if _, err := copyTarget(dst, "sub/../../"+filepath.Base(outside)); err == nil {
		t.Fatal("relative escape was accepted")
	}
	got, err := copyTarget(dst, "foo..bar.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dst, "foo..bar.txt") {
		t.Fatalf("two-dot name: %s", got)
	}
	if _, err := pathWithin(dst, dst+"evil"); err == nil {
		t.Fatal("adjacent directory was accepted")
	}
	if _, err := pathWithin(dst, filepath.Join(dst, "..", "outside-copy")); err == nil {
		t.Fatal("cleaned escape was accepted")
	}
}

func TestRemoveWithinDoesNotEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside-rm")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(outside, "keep")
	if err := os.WriteFile(keep, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	removeWithin(root, outside)
	removeWithin(root, filepath.Join(root, "..", filepath.Base(outside)))
	body, err := os.ReadFile(keep)
	if err != nil || string(body) != "z" {
		t.Fatalf("outside file removed: %q %v", body, err)
	}
}

func TestFailedPrepareDoesNotRemoveOutsideMirror(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRASP_WORKTREE_ROOT", root)
	ResetForTest()
	t.Cleanup(ResetForTest)
	sentinel := filepath.Join(root, "sentinel")
	if err := os.Mkdir(sentinel, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sentinel, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Prepare("bad-mirror", []Source{{Name: "app", URL: "http://127.0.0.1:1/nope.git", Branch: "main"}})
	if err == nil {
		t.Fatal("expected clone to fail")
	}
	body, readErr := os.ReadFile(filepath.Join(sentinel, "keep"))
	if readErr != nil || string(body) != "x" {
		t.Fatalf("sentinel removed: %q %v", body, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "bad-mirror")); !os.IsNotExist(statErr) {
		t.Fatal("failed launch directory was kept")
	}
	if _, statErr := os.Stat(filepath.Join(root, ".mirrors", "bad-mirror")); !os.IsNotExist(statErr) {
		t.Fatal("failed mirror directory was kept")
	}
}

func TestPrepareSeparateMirrorsPerName(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	t.Cleanup(ResetForTest)
	bare, feature := bareFeatureRepo(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveGitHTTP(w, r, filepath.Dir(bare))
	}))
	t.Cleanup(srv.Close)
	cloneURL := "http://alice@" + strings.TrimPrefix(srv.URL, "http://") + "/repo.git"
	got, err := Prepare("two", []Source{
		{Name: "alpha", URL: cloneURL, Branch: "feature"},
		{Name: "beta", URL: cloneURL, Branch: "feature"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mirrors := filepath.Join(os.Getenv("GRASP_WORKTREE_ROOT"), ".mirrors", "two")
	for _, name := range []string{"alpha", "beta"} {
		info, statErr := os.Stat(filepath.Join(mirrors, name))
		if statErr != nil || !info.IsDir() {
			t.Fatalf("mirror %s: %v", name, statErr)
		}
	}
	entries, err := os.ReadDir(mirrors)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name() == entries[1].Name() {
		t.Fatalf("mirrors=%v", entries)
	}
	if strings.Contains(mirrors, "alice") {
		t.Fatalf("userinfo leaked into mirror path %s", mirrors)
	}
	for _, repo := range got.Repos {
		if gitOut(t, repo.Path, "rev-parse", "HEAD") != feature {
			t.Fatalf("repo %s is not at feature", repo.Name)
		}
	}
	End("two")
}

func TestHTTPSHelperStaysInProcessEnv(t *testing.T) {
	env, prefix, cleanup, err := httpsHelper(gitScope{scheme: "https", host: "github.com", path: "org/repo"}, identity{kind: credGitHubHTTPS, secret: "example-token"})
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(prefix) != 0 {
		t.Fatalf("helper returned argv: %v", prefix)
	}
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "GIT_CONFIG_COUNT=3") || !strings.Contains(joined, "GIT_CONFIG_KEY_1=credential.helper") {
		t.Fatalf("missing process config: %s", joined)
	}
	if !strings.Contains(joined, "!") || !strings.Contains(joined, " ") {
		t.Fatal("helper value was not passed through process config")
	}
	for _, arg := range prefix {
		if strings.Contains(arg, "!") {
			t.Fatal("credential helper remained in argv")
		}
	}
}

func TestEndWithoutLaunchDoesNotStick(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	t.Cleanup(ResetForTest)
	End("sandbox-run")
	if _, err := Prepare("sandbox-run", nil); err != nil {
		t.Fatal(err)
	}
	End("sandbox-run")
	if _, err := Prepare("sandbox-run", nil); err == nil {
		t.Fatal("ended launch was recreated")
	}
}

func TestRepoCredentialBeatsCatalog(t *testing.T) {
	remote, ok := parseScope("https://git.example/team/app.git")
	if !ok {
		t.Fatal("parse")
	}
	auth := Auth{
		Configured: true,
		Credentials: []Credential{{
			Kind: credGitHubHTTPS, Scope: "https://git.example/team/app", Secret: "catalog",
		}},
		Repos: []RepoCredential{{
			URL: "https://git.example/team/app.git", Kind: credGitLabHTTPS, Secret: "repo-token",
		}},
		GitHubToken: "legacy",
	}
	id, ok := auth.pick(remote)
	if !ok || id.secret != "repo-token" || id.kind != credGitLabHTTPS {
		t.Fatalf("identity=%+v ok=%v", id, ok)
	}
	miss := Auth{Configured: true, GitHubToken: "legacy"}
	id, ok = miss.pick(remote)
	if !ok || !id.failClosed || id.secret != "" {
		t.Fatalf("missing catalog fell through: %+v ok=%v", id, ok)
	}
	legacy := Auth{GitHubURL: "https://git.example", GitHubToken: "legacy"}
	id, ok = legacy.pick(remote)
	if !ok || id.secret != "legacy" || id.kind != credGitHubHTTPS {
		t.Fatalf("legacy=%+v ok=%v", id, ok)
	}
	sshRemote, ok := parseScope("git@git.example:team/app.git")
	if !ok {
		t.Fatal("ssh parse")
	}
	sshAuth := Auth{Configured: true, Credentials: []Credential{{
		Kind: credSSH, Scope: "git@git.example:team/app.git", Secret: "PRIVATE\nKEY\n",
		KnownHosts: "git.example ssh-ed25519 AAA",
	}}}
	id, ok = sshAuth.pick(sshRemote)
	if !ok || id.kind != credSSH || !strings.Contains(id.secret, "PRIVATE") {
		t.Fatalf("ssh identity=%+v ok=%v", id, ok)
	}
	_, _, _, err := (Auth{SSHPrivateKey: "KEY"}).gitPrefix("ssh://git.example/team/app.git")
	if err == nil || !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("legacy ssh err=%v", err)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "t@example.com")
	gitRun(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-m", "main")
	gitRun(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "feature")
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func bareFeatureRepo(t *testing.T) (string, string) {
	t.Helper()
	src := initRepo(t)
	feature := gitOut(t, src, "rev-parse", "refs/heads/feature")
	bare := filepath.Join(t.TempDir(), "repo.git")
	cmd := exec.Command("git", "clone", "--bare", src, bare)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bare clone: %v: %s", err, out)
	}
	gitRun(t, bare, "update-server-info")
	return bare, feature
}

func serveGitHTTP(w http.ResponseWriter, r *http.Request, projectRoot string) {
	cmd := exec.Command("git", "http-backend")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GIT_PROJECT_ROOT=" + projectRoot,
		"GIT_HTTP_EXPORT_ALL=1",
		"REQUEST_METHOD=" + r.Method,
		"PATH_INFO=" + r.URL.Path,
		"QUERY_STRING=" + r.URL.RawQuery,
		"CONTENT_TYPE=" + r.Header.Get("Content-Type"),
		"CONTENT_LENGTH=" + strconv.FormatInt(r.ContentLength, 10),
	}
	cmd.Stdin = r.Body
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		http.Error(w, "git http-backend", http.StatusBadGateway)
		return
	}
	raw := stdout.Bytes()
	head, body, ok := bytes.Cut(raw, []byte("\r\n\r\n"))
	if !ok {
		head, body, ok = bytes.Cut(raw, []byte("\n\n"))
	}
	if !ok {
		http.Error(w, "bad cgi", http.StatusBadGateway)
		return
	}
	status := http.StatusOK
	for _, line := range bytes.Split(head, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		key, val, found := bytes.Cut(line, []byte(":"))
		if !found {
			continue
		}
		name := string(key)
		value := strings.TrimSpace(string(val))
		if strings.EqualFold(name, "Status") {
			if code, err := strconv.Atoi(strings.Fields(value)[0]); err == nil {
				status = code
			}
			continue
		}
		w.Header().Add(name, value)
	}
	w.WriteHeader(status)
	_, _ = io.Copy(w, bytes.NewReader(body))
}

func isolateGitConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "none"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(dir, "none-system"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
