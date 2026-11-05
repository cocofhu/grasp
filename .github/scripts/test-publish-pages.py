#!/usr/bin/env python3
"""Exercise the real publisher against local Git repos and injected SSH failures."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


SCRIPTS = Path(__file__).resolve().parent
REAL_GIT = shutil.which("git")


class PublishPagesTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory(prefix="pages publish ")
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.repo = self.root / "source"
        self.remote = self.root / "pages.git"
        self.state = self.root / "transport.json"
        self.bin = self.root / "bin"
        self.bin.mkdir()
        scripts = self.repo / ".github/scripts"
        scripts.mkdir(parents=True)
        for name in ("publish-pages.sh", "retry-network.sh"):
            shutil.copy2(SCRIPTS / name, scripts / name)
        public = self.repo / "docs/public"
        public.mkdir(parents=True)
        (public / "index.html").write_text("new site\n")
        self.env = dict(os.environ, GIT_CONFIG_GLOBAL=os.devnull,
                        GIT_CONFIG_SYSTEM=os.devnull, GIT_AUTHOR_NAME="Test",
                        GIT_AUTHOR_EMAIL="test@example.com", GIT_COMMITTER_NAME="Test",
                        GIT_COMMITTER_EMAIL="test@example.com")
        self.git("init", "--bare", "--initial-branch=main", str(self.remote))
        seed = self.root / "seed"
        self.git("init", "--initial-branch=main", str(seed))
        (seed / "index.html").write_text("old site\n")
        (seed / "old.txt").write_text("remove me\n")
        (seed / "CNAME").write_text("pages.example.com\n")
        self.git("-C", str(seed), "add", ".")
        self.git("-C", str(seed), "commit", "-m", "seed")
        self.git("-C", str(seed), "push", str(self.remote), "main")
        self.initial_head = self.git("rev-parse", "main", cwd=self.remote).stdout

        # Rewrite only the exact fixture URL. All Git mutations and tree checks
        # remain real, and no command contacts GitHub or uses a deploy key.
        self.executable("git", """#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
path = Path(os.environ['TEST_TRANSPORT_STATE'])
state = json.loads(path.read_text())
op = sys.argv[1]
state['attempts'][op] = state['attempts'].get(op, 0) + 1
remaining = state['failures'].get(op, 0)
if remaining:
    state['failures'][op] -= 1
path.write_text(json.dumps(state))
if remaining:
    print('fixture: SSH transport failed', file=sys.stderr)
    sys.exit(128)
args = [os.environ['TEST_REMOTE'] if arg == 'git@github.com:example/pages.git' else arg
        for arg in sys.argv[1:]]
os.execv(os.environ['TEST_REAL_GIT'], ['git', *args])
""")
        self.executable("ssh-keyscan", "#!/bin/sh\nprintf '%s\\n' 'github.com ssh-ed25519 fixture'\n")
        self.executable("sleep", "#!/bin/sh\nexit 0\n")

    def executable(self, name, content):
        path = self.bin / name
        path.write_text(content)
        path.chmod(0o755)

    def git(self, *args, cwd=None, check=True):
        return subprocess.run([REAL_GIT, *args], cwd=cwd, env=self.env,
                              text=True, capture_output=True, check=check)

    def publish(self, failures=None, branch="main"):
        self.state.write_text(json.dumps({"failures": failures or {}, "attempts": {}}))
        env = dict(self.env, PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                   TEST_REAL_GIT=REAL_GIT, TEST_REMOTE=str(self.remote),
                   TEST_TRANSPORT_STATE=str(self.state), PAGES_DEPLOY_KEY="fixture only",
                   PAGES_REPO="example/pages", PAGES_BRANCH=branch, GITHUB_SHA="fixture")
        result = subprocess.run(["bash", str(self.repo / ".github/scripts/publish-pages.sh")],
                                env=env, text=True, capture_output=True, timeout=20)
        return result, json.loads(self.state.read_text())["attempts"]

    def assertPublished(self, branch="main"):
        self.assertEqual(self.git("show", f"{branch}:index.html", cwd=self.remote).stdout, "new site\n")
        self.git("cat-file", "-e", f"{branch}:.nojekyll", cwd=self.remote)
        self.assertNotEqual(self.git("cat-file", "-e", f"{branch}:old.txt",
                                    cwd=self.remote, check=False).returncode, 0)

    def test_transient_failures_recover_and_preserve_the_domain(self):
        result, attempts = self.publish({"ls-remote": 1, "clone": 1, "push": 1})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertPublished()
        self.assertEqual(self.git("show", "main:CNAME", cwd=self.remote).stdout, "pages.example.com\n")
        for op in ("ls-remote", "clone", "push"):
            self.assertEqual(attempts[op], 2)

    def test_permanent_auth_failure_never_creates_an_orphan_tree(self):
        result, attempts = self.publish({"ls-remote": 10})
        self.assertEqual(result.returncode, 128, result.stderr)
        self.assertEqual(attempts, {"ls-remote": 3})
        self.assertNotIn("creating orphan", result.stdout)

    def test_permanent_clone_failure_is_not_replaced_with_git_init(self):
        result, attempts = self.publish({"clone": 10})
        self.assertEqual(result.returncode, 128, result.stderr)
        self.assertEqual(attempts, {"ls-remote": 1, "clone": 3})

    def test_permanent_push_failure_is_reported_and_leaves_remote_intact(self):
        result, attempts = self.publish({"push": 10})
        self.assertEqual(result.returncode, 128, result.stderr)
        self.assertEqual(attempts["push"], 3)
        self.assertNotIn("published ", result.stdout)
        self.assertEqual(self.git("rev-parse", "main", cwd=self.remote).stdout, self.initial_head)

    def test_missing_branch_creates_an_orphan(self):
        result, attempts = self.publish(branch="site")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(attempts["ls-remote"], 1)
        self.assertPublished("site")
        self.assertEqual(self.git("rev-list", "--count", "site", cwd=self.remote).stdout.strip(), "1")

    def test_empty_repository_can_be_published(self):
        self.git("update-ref", "-d", "refs/heads/main", cwd=self.remote)
        result, _ = self.publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertPublished()

    def test_unchanged_site_does_not_commit_or_push(self):
        result, _ = self.publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        head = self.git("rev-parse", "main", cwd=self.remote).stdout
        result, attempts = self.publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("pages unchanged:", result.stdout)
        self.assertNotIn("push", attempts)
        self.assertEqual(self.git("rev-parse", "main", cwd=self.remote).stdout, head)


if __name__ == "__main__":
    unittest.main()
