package sandbox

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func selectedGitCatalog() []GitCredential {
	return []GitCredential{
		{ID: "team", Kind: RepoCredentialGitHubHTTPS, Secret: "synthetic-team-token", Preferred: true},
		{ID: "project", Kind: RepoCredentialGitHubHTTPS, Scope: "https://github.com/example/project.git", Secret: "synthetic-project-token"},
		{ID: "gitlab", Kind: RepoCredentialGitLabHTTPS, Scope: "git.example.com", Secret: "synthetic-gitlab-token"},
		{ID: "ssh", Kind: RepoCredentialSSH, Scope: "ssh.example.com", Secret: "synthetic-private-key\n", KnownHosts: "ssh.example.com ssh-ed25519 SYNTHETIC\n"},
	}
}

func TestSelectedGitCatalogStagesEveryAccountWithoutGlobalIdentity(t *testing.T) {
	credentials := selectedGitCatalog()
	data, err := packGitInjectBundle("unselected-private-key", "unselected-hosts", true, credentials)
	if err != nil {
		t.Fatal(err)
	}
	files, modes := untar(t, data)
	var manifest []struct {
		ID        string `json:"id"`
		Scope     string `json:"scope"`
		Preferred bool   `json:"preferred"`
	}
	if err := json.Unmarshal([]byte(files["git/manifest.json"]), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest) != len(credentials) {
		t.Fatal("some selected accounts were not staged")
	}
	for i, credential := range credentials {
		if manifest[i].ID != credential.ID || manifest[i].Scope != credential.Scope || manifest[i].Preferred != credential.Preferred {
			t.Fatal("catalog lost the selected identity, scope, order or preference")
		}
		if strings.Contains(files["git/manifest.json"], credential.Secret) {
			t.Fatal("secret was included in the metadata manifest")
		}
	}
	for name, want := range map[string]string{
		"git/0.secret":      credentials[0].Secret + "\n",
		"git/1.secret":      credentials[1].Secret + "\n",
		"git/2.secret":      credentials[2].Secret + "\n",
		"git/3.secret":      credentials[3].Secret,
		"git/3.known_hosts": credentials[3].KnownHosts,
	} {
		if files[name] != want || modes[name] != 0o600 {
			t.Fatalf("selected credential file %s was omitted, changed, or exposed", name)
		}
	}
	if modes["git/manifest.json"] != 0o600 {
		t.Fatal("catalog manifest must be private")
	}
	for _, name := range []string{"id_rsa", "known_hosts"} {
		if _, ok := files[name]; ok {
			t.Fatal("explicit selection retained the inherited global SSH identity")
		}
	}
	for name := range files {
		if strings.HasPrefix(name, "repos/") {
			t.Fatal("explicit per-repository grant was staged beside the Agent catalog")
		}
	}
}

func TestGitCatalogExplicitEmptySurvivesTransport(t *testing.T) {
	legacy, err := packGitInjectBundle("", "", false, nil)
	if err != nil || legacy != nil {
		t.Fatal("unset selection must retain the empty legacy transport")
	}
	explicit, err := packGitInjectBundle("legacy-key", "legacy-hosts", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := untar(t, explicit)
	if len(files) != 1 || strings.TrimSpace(files["git/manifest.json"]) != "[]" {
		t.Fatal("explicit empty selection must send a clearing catalog without old keys")
	}
	for _, credential := range []GitCredential{
		{ID: "bad", Kind: "unknown", Secret: "synthetic"},
		{ID: "empty", Kind: RepoCredentialGitHubHTTPS, Secret: " \n"},
	} {
		if _, err := packGitInjectBundle("", "", true, []GitCredential{credential}); err == nil {
			t.Fatal("invalid selected credential must not be silently dropped")
		}
	}
}

func TestGitCatalogMetadataCannotChangeFilePaths(t *testing.T) {
	credentials := []GitCredential{
		{ID: "../../outside", Name: "../account", Kind: RepoCredentialGitHubHTTPS, Secret: "  selected-token \n"},
		{ID: "ssh", Kind: RepoCredentialSSH, Secret: "private-key", KnownHosts: "selected-host ssh-ed25519 SYNTHETIC"},
	}
	files, err := gitCatalogFiles(credentials)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := packGitInjectBundle("", "", true, credentials)
	if err != nil {
		t.Fatal(err)
	}
	transport, modes := untar(t, bundle)
	want := map[string]string{
		"0.secret":      "selected-token\n",
		"1.secret":      "private-key\n",
		"1.known_hosts": "selected-host ssh-ed25519 SYNTHETIC\n",
	}
	if len(files) != len(want)+1 || len(transport) != len(files) {
		t.Fatal("catalog file set changed between local installation and transport")
	}
	for _, file := range files {
		if file.name == "manifest.json" {
			var manifest []struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal([]byte(file.body), &manifest); err != nil || len(manifest) != 2 || manifest[0].ID != credentials[0].ID {
				t.Fatal("catalog lost the account metadata")
			}
		} else if body, ok := want[file.name]; !ok || file.body != body {
			t.Fatal("catalog metadata changed a generated filename or secret normalization")
		}
		if transport["git/"+file.name] != file.body || modes["git/"+file.name] != 0o600 || !strings.HasSuffix(file.body, "\n") {
			t.Fatal("catalog transport changed a private file's body or permissions")
		}
	}
}

func TestReprimeSelectedGitCatalogReplacesLegacyEnvironment(t *testing.T) {
	credentials := selectedGitCatalog()
	spec := Spec{GitBindingsConfigured: true, GitCredentials: credentials, Env: map[string]string{
		"GITHUB_TOKEN": "unselected-gh", "GITLAB_TOKEN": "unselected-gl", "GH_TOKEN": "unselected-cli",
		"GIT_SSH_PRIVATE_KEY": "unselected-ssh", "GIT_SSH_KNOWN_HOSTS": "unselected-hosts", "SSH_AUTH_SOCK": "old-agent.sock",
		"GIT_REPOS": "project|https://github.com/example/project.git|main", "AGENT_PROVIDER": "cursor",
	}}
	data, err := packReprimeBundle(spec, []string{"GITHUB_TOKEN", "GITLAB_TOKEN", "GH_TOKEN", "GIT_SSH_PRIVATE_KEY", "GIT_SSH_KNOWN_HOSTS", "SSH_AUTH_SOCK", "AGENT_PROVIDER"})
	if err != nil {
		t.Fatal(err)
	}
	files := untarReprime(t, data)
	env := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(files["env"]), "\n") {
		key, encoded, _ := strings.Cut(line, "=")
		value, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		env[key] = string(value)
	}
	if !reflect.DeepEqual(env, map[string]string{"GIT_REPOS": spec.Env["GIT_REPOS"], "AGENT_PROVIDER": "cursor"}) {
		t.Fatal("reprime reintroduced global Git credentials or lost the clone list")
	}
	for _, name := range []string{"GITHUB_TOKEN", "GITLAB_TOKEN", "GH_TOKEN", "GIT_SSH_PRIVATE_KEY", "GIT_SSH_KNOWN_HOSTS", "SSH_AUTH_SOCK"} {
		if !strings.Contains("\n"+files["unset"], "\n"+name+"\n") {
			t.Fatalf("reprime did not clear previous node's %s", name)
		}
	}
	if strings.TrimSpace(files["ssh/git/1.secret"]) != credentials[1].Secret || files["ssh/git/3.secret"] != credentials[3].Secret {
		t.Fatal("reprime did not transport every selected account")
	}
}

func TestSharedSandboxFingerprintIncludesEveryGitAuthorizationChoice(t *testing.T) {
	base := Spec{GitBindingsConfigured: true, GitCredentials: selectedGitCatalog()}
	want := SpecFingerprint(base)
	if want == "" || want != SpecFingerprint(base) {
		t.Fatal("unchanged Git selection should reuse its initialized sandbox")
	}
	cases := map[string]func(*Spec){
		"non-preferred token rotation": func(s *Spec) { s.GitCredentials[1].Secret = "synthetic-rotated" },
		"private key rotation":         func(s *Spec) { s.GitCredentials[3].Secret = "synthetic-new-key" },
		"known hosts":                  func(s *Spec) { s.GitCredentials[3].KnownHosts = "new-host-key" },
		"repository scope":             func(s *Spec) { s.GitCredentials[1].Scope = "https://github.com/example/other.git" },
		"preferred account":            func(s *Spec) { s.GitCredentials[0].Preferred = false; s.GitCredentials[1].Preferred = true },
		"selection order":              func(s *Spec) { s.GitCredentials[1], s.GitCredentials[2] = s.GitCredentials[2], s.GitCredentials[1] },
		"removed account":              func(s *Spec) { s.GitCredentials = s.GitCredentials[:3] },
		"explicit no authorization":    func(s *Spec) { s.GitCredentials = nil },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			spec := Spec{GitBindingsConfigured: true, GitCredentials: append([]GitCredential(nil), base.GitCredentials...)}
			change(&spec)
			if SpecFingerprint(spec) == want {
				t.Fatal("authorization change must trigger sandbox reinitialization")
			}
		})
	}
	if SpecFingerprint(Spec{}) == SpecFingerprint(Spec{GitBindingsConfigured: true}) {
		t.Fatal("explicit no-authorization must not reuse legacy Git authentication")
	}
}
