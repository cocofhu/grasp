#!/usr/bin/env python3
"""Offline tests for the sandbox's selected Git identities; no API/network calls."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


SCRIPT = Path(__file__).resolve().parents[1] / "sandbox/scripts/git-credentials.py"
sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location("grasp_git_credentials", SCRIPT)
HELPER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(HELPER)


class CredentialsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.home = self.root / "home"
        self.home.mkdir()
        self.source = self.root / "staging"
        self.source.mkdir()
        self.directory = self.home / ".grasp/git-credentials"
        self.environment = {"PATH": os.environ["PATH"], "HOME": str(self.home),
                            "GIT_CONFIG_GLOBAL": str(self.home / ".gitconfig"),
                            "GIT_CONFIG_NOSYSTEM": "1", "GIT_TERMINAL_PROMPT": "0"}
        self.env_patch = patch.dict(os.environ, self.environment, clear=True)
        self.env_patch.start()
        self.addCleanup(self.env_patch.stop)

    def stage(self, entries):
        records = []
        for index, entry in enumerate(entries):
            record = {"index": index, "id": f"id-{index}", "kind": "github_https",
                      "scope": "", "preferred": index == 0}
            record.update({k: v for k, v in entry.items() if k not in {"token", "hosts"}})
            (self.source / f"{index}.secret").write_text(entry.get("token", f"fake-token-{index}") + "\n")
            if "hosts" in entry:
                (self.source / f"{index}.known_hosts").write_text(entry["hosts"])
            records.append(record)
        (self.source / "manifest.json").write_text(json.dumps(records))
        return records

    def install(self, entries):
        self.stage(entries)
        HELPER.install(self.source, self.home)

    def git(self, *args, body=None, check=True):
        return subprocess.run(["git", *args], input=body, text=True,
                              capture_output=True, check=check)

    def fill(self, url, check=True):
        return self.git("credential", "fill", body=f"url={url}\n\n", check=check)

    def test_real_git_exact_repo_overrides_host_preferred_and_preserves_path(self):
        self.install([{}, {"scope": "https://github.com/acme/private.git", "token": "repo-token"}])
        for url in ("https://github.com/acme/private.git", "https://github.com/acme/private"):
            self.assertIn("password=repo-token", self.fill(url).stdout)
        for url in ("https://github.com/acme/private-other.git", "https://github.com/acme/public.git"):
            self.assertIn("password=fake-token-0", self.fill(url).stdout)
        self.assertNotIn("repo-token", (self.home / ".gitconfig").read_text())
        self.assertFalse(self.source.exists())

    def test_reject_does_not_cycle_tokens(self):
        self.install([{}, {"token": "other-same-host-token"}])
        self.git("credential", "reject", body="url=https://github.com/team/repo.git\n\n")
        self.assertIn("password=fake-token-0", self.fill("https://github.com/team/repo.git").stdout)
        self.assertEqual((self.directory / "1.secret").read_text().strip(), "other-same-host-token")

    def test_default_host_and_stable_first_when_preferred_is_another_host(self):
        self.install([{"kind": "gitlab_https"}, {"scope": "github.com"}, {"scope": "github.com"}])
        self.assertIn("password=fake-token-1", self.fill("https://github.com/team/app").stdout)
        self.assertIn("username=oauth2", self.fill("https://gitlab.com/team/app").stdout)
        self.assertNotEqual(self.fill("https://other.example/team/app", check=False).returncode, 0)

    def test_url_scheme_and_port_are_part_of_scope(self):
        self.install([{"scope": "http://git.example:8080/team/app"}])
        self.assertIn("password=fake-token-0", self.fill("http://git.example:8080/team/app.git").stdout)
        for url in ("https://git.example:8080/team/app", "http://git.example/team/app"):
            self.assertNotEqual(self.fill(url, check=False).returncode, 0)

    def test_url_username_does_not_override_scope(self):
        self.install([{}, {"scope": "https://github.com/team/app", "token": "exact"}])
        self.assertIn("password=exact", self.fill("https://unrelated@github.com/team/app").stdout)

    def test_empty_selection_resets_inherited_helpers_and_cli_configs(self):
        self.git("config", "--global", "credential.helper", "!echo password=legacy")
        for name in (".config/gh/hosts.yml", ".config/glab-cli/config.yml"):
            path = self.home / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("old-token")
        self.install([])
        result = self.fill("https://github.com/team/app", check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("legacy", result.stdout)
        self.assertFalse((self.home / ".config/gh/hosts.yml").exists())
        self.assertFalse((self.home / ".config/glab-cli/config.yml").exists())

    def test_permissions_and_cli_defaults_are_per_host(self):
        self.install([{}, {"scope": "github.com", "token": "not-default"},
                      {"kind": "gitlab_https", "scope": "https://git.example/team/app", "token": "gl-only"}])
        self.assertEqual(self.directory.stat().st_mode & 0o777, 0o700)
        for path in self.directory.iterdir():
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        gh = (self.home / ".config/gh/hosts.yml").read_text()
        gl = (self.home / ".config/glab-cli/config.yml").read_text()
        self.assertIn("fake-token-0", gh)
        self.assertNotIn("not-default", gh)
        self.assertNotIn("gl-only", gh)
        self.assertIn('"git.example":', gl)
        self.assertNotIn("fake-token-0", gl)
        self.assertEqual((self.home / ".config/gh/hosts.yml").stat().st_mode & 0o777, 0o600)
        if shutil.which("gh"):
            output = subprocess.run(["gh", "auth", "token", "--hostname", "github.com"],
                                    text=True, capture_output=True, check=True).stdout
            self.assertEqual(output.strip(), "fake-token-0")
        if shutil.which("glab"):
            output = subprocess.run(["glab", "config", "get", "token", "--host", "git.example"],
                                    text=True, capture_output=True, check=True).stdout
            self.assertEqual(output.strip(), "gl-only")

    def test_invalid_selected_record_fails_whole_catalog(self):
        for mutation in ("missing", "symlink", "newline", "duplicate-id", "duplicate-index",
                         "invalid-scope", "no-preferred", "two-preferred", "bad-kind", "hosts-symlink"):
            with self.subTest(mutation=mutation):
                for path in self.source.iterdir():
                    path.unlink()
                records = self.stage([{}, {}])
                if mutation == "missing":
                    (self.source / "0.secret").unlink()
                elif mutation == "symlink":
                    (self.source / "0.secret").unlink()
                    (self.source / "0.secret").symlink_to(self.source / "1.secret")
                elif mutation == "newline":
                    (self.source / "0.secret").write_text("fake\npassword=injected\n")
                elif mutation == "duplicate-id":
                    records[1]["id"] = records[0]["id"]
                elif mutation == "duplicate-index":
                    records[1]["index"] = 0
                elif mutation == "invalid-scope":
                    records[0]["scope"] = "github.com\nhelper=malicious"
                elif mutation == "no-preferred":
                    records[0]["preferred"] = False
                elif mutation == "two-preferred":
                    records[1]["preferred"] = True
                elif mutation == "bad-kind":
                    records[0]["kind"] = "unknown"
                else:
                    (self.source / "0.known_hosts").symlink_to(self.source / "1.secret")
                (self.source / "manifest.json").write_text(json.dumps(records))
                with self.assertRaises((ValueError, OSError)):
                    HELPER.catalog(self.source)

    def test_invalid_install_does_not_replace_valid_installed_catalog(self):
        self.install([{}])
        self.source.mkdir()
        self.stage([{}])
        (self.source / "0.secret").unlink()
        with self.assertRaises(ValueError):
            HELPER.install(self.source, self.home)
        self.assertIn("password=fake-token-0", self.fill("https://github.com/team/app").stdout)

    def test_damaged_installed_selection_never_falls_back_to_other_account(self):
        self.install([{}, {}])
        (self.directory / "0.secret").unlink()
        result = self.fill("https://github.com/team/app", check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("fake-token-1", result.stdout + result.stderr)

    def test_ssh_uses_only_exact_selected_key_and_known_hosts(self):
        self.install([{"kind": "ssh", "token": "global-key"},
                      {"kind": "ssh", "scope": "git@github.com:team/app.git", "token": "repo-key",
                       "hosts": "github.com ssh-ed25519 FAKE\n"}])
        with patch.object(HELPER.os, "execvp") as execute:
            HELPER.ssh(self.directory, ["git@github.com", "git-upload-pack 'team/app.git'"])
        command = execute.call_args.args[1]
        self.assertEqual(command.count("-i"), 1)
        self.assertEqual(command[command.index("-i") + 1], str(self.directory / "1.secret"))
        self.assertIn("IdentityAgent=none", command)
        self.assertIn("StrictHostKeyChecking=yes", command)
        self.assertEqual(command[command.index("-F") + 1], "/dev/null")

    def test_ssh_scope_preferred_other_host_does_not_override_matching_key(self):
        self.install([{"kind": "ssh", "scope": "first.example"},
                      {"kind": "ssh", "scope": "second.example"},
                      {"kind": "ssh", "scope": "second.example"}])
        with patch.object(HELPER.os, "execvp") as execute:
            HELPER.ssh(self.directory, ["git@second.example", "git-receive-pack 'team/app.git'"])
        self.assertIn(str(self.directory / "1.secret"), execute.call_args.args[1])
        with self.assertRaises(ValueError):
            HELPER.ssh(self.directory, ["git@unselected.example", "git-upload-pack 'team/app'"])

    def test_ssh_port_username_probe_and_remote_command_validation(self):
        self.install([{"kind": "ssh", "scope": "ssh://git@git.example:2222/team/app"}])
        with patch.object(HELPER.os, "execvp") as execute:
            self.assertEqual(HELPER.ssh(self.directory, ["-G", "git.example"]), 0)
            execute.assert_not_called()
            HELPER.ssh(self.directory, ["-o", "SendEnv=GIT_PROTOCOL", "-p", "2222", "-l", "deploy",
                                        "git.example", "git-upload-pack 'team/app.git'"])
        command = execute.call_args.args[1]
        self.assertIn("2222", command)
        self.assertEqual(command[command.index("-l") + 1], "deploy")
        with self.assertRaises(ValueError):
            HELPER.ssh(self.directory, ["git.example", "git-upload-pack 'team/app'; injected"])

    def test_ipv6_scopes_match_with_ports(self):
        self.assertEqual(HELPER.location("ssh://git@[::1]:2222/team/app.git", "ssh"),
                         ("ssh", "[::1]:2222", "team/app"))
        self.assertEqual(HELPER.location("git@[::1]:team/app", "ssh"), ("ssh", "[::1]", "team/app"))
        self.install([{"scope": "http://[::1]:8080/team/app"}])
        self.assertIn("password=fake-token-0", self.fill("http://[::1]:8080/team/app.git").stdout)

    def test_ssh_explicit_standard_port_and_missing_final_key_newline(self):
        key = self.root / "generated-key"
        subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(key)], check=True)
        self.stage([{"kind": "ssh", "scope": "ssh://git@git.example:22/team/app"}])
        (self.source / "0.secret").write_text(key.read_text().rstrip("\n"))
        HELPER.install(self.source, self.home)
        subprocess.run(["ssh-keygen", "-y", "-f", str(self.directory / "0.secret")],
                       check=True, capture_output=True)
        with patch.object(HELPER.os, "execvp") as execute:
            HELPER.ssh(self.directory, ["-p", "22", "git@git.example", "git-upload-pack 'team/app.git'"])
        self.assertIn(str(self.directory / "0.secret"), execute.call_args.args[1])

    def test_real_git_ssh_transport_invokes_single_selected_key(self):
        self.install([{"kind": "ssh", "scope": "ssh://git@git.example:22/team/app"},
                      {"kind": "ssh", "scope": "ssh://git@[::1]:2222/team/app"}])
        binary = self.root / "bin"
        binary.mkdir()
        ssh = binary / "ssh"
        trace = self.root / "ssh.json"
        ssh.write_text("#!/usr/bin/env python3\nimport json,sys\nfrom pathlib import Path\n"
                       f"Path({str(trace)!r}).write_text(json.dumps(sys.argv[1:]))\nsys.exit(1)\n")
        ssh.chmod(0o700)
        os.environ["PATH"] = str(binary) + ":" + os.environ["PATH"]
        for index, url in enumerate(("ssh://git@git.example:22/team/app.git",
                                     "ssh://git@[::1]:2222/team/app.git")):
            with self.subTest(url=url):
                trace.unlink(missing_ok=True)
                result = self.git("ls-remote", url, check=False)
                self.assertNotEqual(result.returncode, 0)  # the local stub has no Git peer
                command = json.loads(trace.read_text())
                self.assertEqual(command.count("-i"), 1)
                self.assertEqual(command[command.index("-i") + 1], str(self.directory / f"{index}.secret"))
                self.assertIn("IdentityAgent=none", command)

    def test_store_and_erase_never_write_or_expose_credentials(self):
        self.install([{}])
        for operation in ("store", "erase"):
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                HELPER.credential(self.directory, operation)
            self.assertEqual(output.getvalue(), "")

    def test_cli_exact_repo_token_only_in_child_env_and_explicit_repo_override(self):
        self.install([{"kind": "gitlab_https", "scope": "git.example"},
                      {"kind": "gitlab_https", "scope": "https://git.example/team/app", "token": "exact-token"}])
        os.environ["GITLAB_TOKEN"] = "ambient-must-not-be-used"
        with patch.object(HELPER.subprocess, "run", return_value=subprocess.CompletedProcess([], 0)) as run:
            self.assertEqual(HELPER.exec_cli(self.directory, "glab", "git@git.example:team/app.git",
                                           ["--", "mr", "list", "--repo", "git@git.example:team/app.git"]), 0)
        self.assertEqual(run.call_args.kwargs["env"]["GITLAB_TOKEN"], "exact-token")
        self.assertNotIn("exact-token", repr(run.call_args.args[0]))
        self.assertEqual(os.environ["GITLAB_TOKEN"], "ambient-must-not-be-used")
        explicit = self.directory.parent / "repo-credentials"
        explicit.mkdir()
        (explicit / "manifest").write_text("0\tgitlab_https\thttps://git.example/team/app.git\n")
        (explicit / "0.token").write_text("explicit-token\n")
        with patch.object(HELPER.subprocess, "run", return_value=subprocess.CompletedProcess([], 0)) as run:
            HELPER.exec_cli(self.directory, "glab", "https://git.example/team/app.git", ["--", "mr", "list"])
        self.assertEqual(run.call_args.kwargs["env"]["GITLAB_TOKEN"], "explicit-token")
        with self.assertRaises(ValueError):
            HELPER.exec_cli(self.directory, "curl", "https://git.example/team/app.git", ["--", "anything"])

    def test_http_api_scope_is_preserved_for_cli_defaults_and_repo_requests(self):
        self.install([{"kind": "gitlab_https", "scope": "http://git.example/team/app"}])
        self.assertIn("api_protocol: http", (self.home / ".config/glab-cli/config.yml").read_text())
        observed = []

        def invoke(command, env, check):
            config = Path(env["GLAB_CONFIG_DIR"])
            observed.append(config)
            self.assertIn("api_protocol: http", (config / "config.yml").read_text())
            self.assertEqual((config / "config.yml").stat().st_mode & 0o777, 0o600)
            return subprocess.CompletedProcess(command, 0)

        with patch.object(HELPER.subprocess, "run", side_effect=invoke):
            HELPER.exec_cli(self.directory, "glab", "git@git.example:team/app", ["--", "mr", "list"])
        self.assertFalse(observed[0].exists())


if __name__ == "__main__":
    unittest.main()
