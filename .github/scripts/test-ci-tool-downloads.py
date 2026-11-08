#!/usr/bin/env python3
"""Offline download, cache, cleanup and exit-status checks for pinned CI tools."""

import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


SCRIPTS = Path(__file__).resolve().parent
TOOLS = {
    "actionlint.sh": ("actionlint", "actionlint-1.7.12", "w:gz"),
    "shellcheck-error.sh": (
        "shellcheck-v0.11.0/shellcheck",
        "shellcheck-0.11.0",
        "w:xz",
    ),
}
MOCK_CURL = """#!/usr/bin/env python3
import os
from pathlib import Path
import shutil
import sys
args = sys.argv[1:]
assert args[args.index('--connect-timeout') + 1] == '15'
assert args[args.index('--max-time') + 1] == '60'
assert any(arg.startswith('https://github.com/') for arg in args)
state = Path(os.environ['GRASP_CI_TOOL_TEST_ATTEMPTS'])
attempt = int(state.read_text()) + 1 if state.exists() else 1
state.write_text(str(attempt))
output = Path(args[args.index('-o') + 1])
if attempt <= int(os.environ['GRASP_CI_TOOL_TEST_FAILURES']):
    output.write_bytes(b'incomplete HTTP 503 download')
    sys.exit(22)
if os.environ['GRASP_CI_TOOL_TEST_CORRUPT'] == '1':
    output.write_bytes(b'not an archive')
else:
    shutil.copyfile(os.environ['GRASP_CI_TOOL_TEST_ARCHIVE'], output)
"""
MOCK_TOOL = b"""#!/usr/bin/env python3
import os
from pathlib import Path
log = Path(os.environ['GRASP_CI_TOOL_TEST_RUNS'])
with log.open('a') as output:
    output.write('run\\n')
raise SystemExit(int(os.environ['GRASP_CI_TOOL_TEST_STATUS']))
"""


class CIToolDownloads(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="grasp-ci-download-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def fixture(self, tool, *, failures=0, corrupt=False, status=0):
        root = self.root / tool
        root.mkdir()
        commands = root / "bin"
        commands.mkdir()
        for name, source in (("curl", MOCK_CURL), ("sleep", "#!/bin/sh\nexit 0\n")):
            path = commands / name
            path.write_text(source)
            path.chmod(0o755)
        work = root / "work"
        work.mkdir()
        member, cached, mode = TOOLS[tool]
        archive = root / "archive"
        with tarfile.open(archive, mode) as output:
            info = tarfile.TarInfo(member)
            info.mode = 0o755
            info.size = len(MOCK_TOOL)
            output.addfile(info, io.BytesIO(MOCK_TOOL))
        env = {
            **os.environ,
            "PATH": str(commands) + os.pathsep + os.environ["PATH"],
            "TMPDIR": str(work),
            "XDG_CACHE_HOME": str(root / "cache"),
            "GRASP_CI_TOOL_TEST_ATTEMPTS": str(root / "attempts"),
            "GRASP_CI_TOOL_TEST_ARCHIVE": str(archive),
            "GRASP_CI_TOOL_TEST_RUNS": str(root / "runs"),
            "GRASP_CI_TOOL_TEST_FAILURES": str(failures),
            "GRASP_CI_TOOL_TEST_CORRUPT": str(int(corrupt)),
            "GRASP_CI_TOOL_TEST_STATUS": str(status),
        }
        return env, root / "cache" / "grasp-ci" / cached

    def run_tool(self, tool, env):
        result = subprocess.run(
            [str(SCRIPTS / tool)],
            env=env,
            text=True,
            capture_output=True,
            timeout=10,
            check=False,
        )
        self.assertEqual(list(Path(env["TMPDIR"]).iterdir()), [], result.stderr)
        return result

    def test_transient_download_recovers_and_then_uses_cache(self):
        for tool in TOOLS:
            with self.subTest(tool=tool):
                env, cached = self.fixture(tool, failures=1)
                result = self.run_tool(tool, env)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertTrue(cached.is_file())
                self.assertEqual(Path(env["GRASP_CI_TOOL_TEST_ATTEMPTS"]).read_text(), "2")
                self.assertEqual(Path(env["GRASP_CI_TOOL_TEST_RUNS"]).read_text(), "run\n")
                result = self.run_tool(tool, env)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(Path(env["GRASP_CI_TOOL_TEST_ATTEMPTS"]).read_text(), "2")

    def test_exhausted_download_fails_without_cache_or_tool_execution(self):
        for tool in TOOLS:
            with self.subTest(tool=tool):
                env, cached = self.fixture(tool, failures=99)
                result = self.run_tool(tool, env)
                self.assertEqual(result.returncode, 22, result.stderr)
                self.assertEqual(Path(env["GRASP_CI_TOOL_TEST_ATTEMPTS"]).read_text(), "3")
                self.assertFalse(cached.exists())
                self.assertFalse(Path(env["GRASP_CI_TOOL_TEST_RUNS"]).exists())

    def test_invalid_archive_is_not_cached_or_retried_as_transport(self):
        for tool in TOOLS:
            with self.subTest(tool=tool):
                env, cached = self.fixture(tool, corrupt=True)
                result = self.run_tool(tool, env)
                self.assertNotEqual(result.returncode, 0, result.stderr)
                self.assertEqual(Path(env["GRASP_CI_TOOL_TEST_ATTEMPTS"]).read_text(), "1")
                self.assertFalse(cached.exists())
                self.assertFalse(Path(env["GRASP_CI_TOOL_TEST_RUNS"]).exists())

    def test_linter_failure_is_preserved_and_never_retried(self):
        for tool in TOOLS:
            with self.subTest(tool=tool):
                env, cached = self.fixture(tool, status=42)
                result = self.run_tool(tool, env)
                self.assertEqual(result.returncode, 42, result.stderr)
                self.assertTrue(cached.is_file())
                self.assertEqual(Path(env["GRASP_CI_TOOL_TEST_ATTEMPTS"]).read_text(), "1")
                self.assertEqual(Path(env["GRASP_CI_TOOL_TEST_RUNS"]).read_text(), "run\n")


if __name__ == "__main__":
    unittest.main()
