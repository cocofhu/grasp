"""Offline production-builder tests. The E2B SDK is deliberately not required."""

import ast
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("grasp_prepare_template", HERE / "prepare-template.py")
tool = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(tool)
IMAGE = "ghcr.io/example/grasp@sha256:" + "a" * 64


class FakeTemplate:
    last = None
    build_args = None

    def __init__(self, **kwargs):
        self.context = kwargs
        self.actions = []
        FakeTemplate.last = self

    def record(self, operation, *args, **kwargs):
        self.actions.append((operation, args, kwargs))
        return self

    def from_image(self, *args, **kwargs):
        return self.record("from_image", *args, **kwargs)

    def set_user(self, *args, **kwargs):
        return self.record("set_user", *args, **kwargs)

    def set_envs(self, *args, **kwargs):
        return self.record("set_envs", *args, **kwargs)

    def copy(self, *args, **kwargs):
        return self.record("copy", *args, **kwargs)

    def run_cmd(self, *args, **kwargs):
        return self.record("run_cmd", *args, **kwargs)

    @classmethod
    def build(cls, *args, **kwargs):
        cls.build_args = (args, kwargs)
        return SimpleNamespace(template_id="template-123", build_id="build-456")


class FakeSandbox:
    kwargs = None
    guest = None
    probe = None
    failure = None
    cleanup_failure = None
    memory_mb = 3072
    cpu_count = 2

    @classmethod
    def create(cls, template, **kwargs):
        cls.kwargs = {"template": template, **kwargs}
        cls.guest = cls()
        cls.guest.sandbox_id = "owned-probe-123"
        cls.guest.commands = cls.guest
        cls.guest.deleted = False
        return cls.guest

    def run(self, command, **kwargs):
        self.command = command
        if self.failure:
            raise self.failure
        return SimpleNamespace(exit_code=0, stdout=json.dumps(self.probe))

    def get_info(self):
        return SimpleNamespace(template_id="template-123", cpu_count=self.cpu_count, memory_mb=self.memory_mb)

    def kill(self):
        self.deleted = True
        if self.cleanup_failure:
            raise self.cleanup_failure


class PrepareTemplateTest(unittest.TestCase):
    def args(self, **changes):
        result = tool.parser().parse_args([
            "--image", IMAGE, "--alias", "grasp-prod-v1", "--name", "standard",
            "--cpu-count", "2", "--memory-mb", "3072",
        ])
        for name, value in changes.items():
            setattr(result, name, value)
        return result

    def setUp(self):
        FakeSandbox.failure = FakeSandbox.cleanup_failure = None
        FakeSandbox.memory_mb = 3072
        FakeSandbox.cpu_count = 2
        FakeSandbox.guest = None
        plan = tool.validate(self.args())
        FakeSandbox.probe = {
            "status": "passed", "imageLevel": plan["imageLevel"],
            "bootstrapSHA256": plan["bootstrapSHA256"], "nativeCLIs": list(tool.NATIVE_CLIS),
            "workspaceAvailableBytes": 25 * (1 << 30),
        }

    def test_validate_requires_pinned_oci_and_explicit_profile(self):
        self.assertEqual(tool.validate(self.args())["cpuCount"], 2)
        for image in ("ubuntu:latest", IMAGE[:-1], IMAGE.replace("sha256", "sha512"), "https://" + IMAGE):
            with self.subTest(image=image), self.assertRaises(tool.PreparationError):
                tool.validate(self.args(image=image))
        for changes in ({"alias": "bad;echo x"}, {"name": "bad\nimage"}, {"cpu_count": 0},
                        {"memory_mb": -1}, {"minimum_workspace_gib": 0}, {"request_timeout": 0}):
            with self.subTest(changes=changes), self.assertRaises(tool.PreparationError):
                tool.validate(self.args(**changes))

    def test_missing_sources_and_invalid_level_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with self.assertRaises(tool.PreparationError):
                tool.validate(self.args(repo_root=root))
            (root / tool.BOOTSTRAP).parent.mkdir(parents=True)
            (root / tool.BOOTSTRAP).write_text("#!/bin/bash\n")
            (root / tool.IMAGE_LEVEL).write_text("invalid\n")
            with self.assertRaises(tool.PreparationError):
                tool.validate(self.args(repo_root=root))

    def test_builder_uses_pinned_image_without_file_uploads(self):
        args = self.args()
        builder = tool.configure_template(FakeTemplate, args, tool.validate(args))
        self.assertFalse(any(action[0] == "copy" for action in builder.actions))
        self.assertEqual(builder.actions[0][1], (IMAGE,))
        command = builder.actions[-1][1][0]
        self.assertIn("/etc/profile.d/grasp-e2b-env.sh", command)
        self.assertIn("PLAYWRIGHT_BROWSERS_PATH", command)
        self.assertNotIn("min_free_disk", command)
        for cli in tool.NATIVE_CLIS:
            self.assertIn(cli, command)
        for disk_required in (False, True):
            script = tool.validation_command(tool.validate(args), disk_required)
            code = script.split("\n", 1)[1].rsplit("GRASP_TEMPLATE_VALIDATE", 1)[0]
            ast.parse(code)  # A real Python script is sent, not a shell placeholder.

    def test_image_validation_rejects_mismatched_bootstrap_and_level_without_overwriting(self):
        args = self.args()
        plan = tool.validate(args)
        source_bootstrap = (args.repo_root / tool.BOOTSTRAP).read_bytes()
        source_level = str(plan["imageLevel"]) + "\n"
        script = tool.validation_command(plan, require_disk=False)
        code = script.split("\n", 1)[1].rsplit("GRASP_TEMPLATE_VALIDATE", 1)[0]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            bootstrap = root / "grasp-bootstrap.sh"
            level = root / "etc/grasp-image-level"
            executable = root / "usr/bin/native-cli"
            level.parent.mkdir(parents=True)
            executable.parent.mkdir(parents=True)
            executable.write_text("#!/bin/sh\nexit 0\n")
            executable.chmod(0o755)
            cases = (
                (source_bootstrap, source_level, None),
                (b"#!/bin/sh\nexit 1\n", source_level, "bootstrap does not match the source checkout"),
                (source_bootstrap, str(plan["imageLevel"] + 1) + "\n", "IMAGE_LEVEL does not match the source checkout"),
            )
            for image_bootstrap, image_level, error in cases:
                with self.subTest(error=error):
                    bootstrap.write_bytes(image_bootstrap)
                    bootstrap.chmod(0o755)
                    level.write_text(image_level)
                    stdout = io.StringIO()
                    with patch("pathlib.Path", side_effect=lambda path: root / str(path).lstrip("/")), \
                            patch("shutil.which", return_value="/usr/bin/native-cli"), \
                            patch("glob.glob", return_value=[str(executable)]), contextlib.redirect_stdout(stdout):
                        if error:
                            with self.assertRaises(SystemExit) as caught:
                                exec(compile(code, "<template-validation>", "exec"), {})
                            self.assertEqual(str(caught.exception), error)
                        else:
                            exec(compile(code, "<template-validation>", "exec"), {})
                            self.assertEqual(json.loads(stdout.getvalue())["status"], "passed")
                    self.assertEqual(bootstrap.read_bytes(), image_bootstrap)
                    self.assertEqual(level.read_text(), image_level)

    def test_build_probe_profile_and_cleanup(self):
        report = tool.prepare(self.args(), FakeTemplate, FakeSandbox)
        self.assertEqual(report["status"], "passed")
        self.assertEqual(report["templateID"], "template-123")
        self.assertEqual(report["buildID"], "build-456")
        self.assertEqual(report["gatewayTemplates"], {
            "standard": {"templateID": "template-123",
            "cpuCores": 2, "memoryMB": 3072, "diskGi": 20},
        })
        self.assertNotIn("min_free_disk_mb", FakeTemplate.build_args[1])
        self.assertFalse(FakeSandbox.kwargs["network"]["allow_public_traffic"])
        self.assertFalse(FakeSandbox.kwargs["allow_internet_access"])
        self.assertTrue(FakeSandbox.kwargs["secure"])
        self.assertEqual(FakeSandbox.kwargs["lifecycle"], {"on_timeout": "kill", "auto_resume": False})
        self.assertEqual(report["probeCleanup"], "deleted")
        self.assertTrue(FakeSandbox.guest.deleted)

    def test_insufficient_probe_capacity_fails_and_cleans_up(self):
        FakeSandbox.probe["workspaceAvailableBytes"] = 19 * (1 << 30)
        report = tool.prepare(self.args(), FakeTemplate, FakeSandbox)
        self.assertEqual(report["status"], "failed")
        self.assertNotIn("gatewayTemplates", report)
        self.assertEqual(report["probeCleanup"], "deleted")

    def test_explicit_free_disk_target_reaches_provider_and_is_reported(self):
        FakeSandbox.memory_mb = 8192
        FakeSandbox.cpu_count = 8
        for target in (0, 52224):
            with self.subTest(target=target):
                args = self.args(cpu_count=8, memory_mb=8192, minimum_workspace_gib=50, min_free_disk_mb=target)
                FakeSandbox.probe["workspaceAvailableBytes"] = 51 * (1 << 30)
                report = tool.prepare(args, FakeTemplate, FakeSandbox)
                self.assertEqual(report["status"], "passed")
                self.assertEqual(FakeTemplate.build_args[1]["min_free_disk_mb"], target)
                self.assertEqual(FakeTemplate.build_args[1]["cpu_count"], 8)
                self.assertEqual(FakeTemplate.build_args[1]["memory_mb"], 8192)
                self.assertEqual(report["requestedMinFreeDiskMB"], target)
                self.assertEqual(report["gatewayTemplates"]["standard"]["diskGi"], 50)
                self.assertTrue(FakeSandbox.guest.deleted)
        for target in (-1, 2147483648):
            with self.subTest(target=target), self.assertRaises(tool.PreparationError):
                tool.validate(self.args(min_free_disk_mb=target))

    def test_requested_disk_growth_does_not_replace_actual_capacity_probe(self):
        args = self.args(minimum_workspace_gib=50, min_free_disk_mb=52224)
        FakeSandbox.probe["workspaceAvailableBytes"] = 49 * (1 << 30)
        report = tool.prepare(args, FakeTemplate, FakeSandbox)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(FakeTemplate.build_args[1]["min_free_disk_mb"], 52224)
        self.assertNotIn("gatewayTemplates", report)
        self.assertEqual(report["probeCleanup"], "deleted")

    def test_provider_resource_mismatch_fails_and_cleans_up(self):
        FakeSandbox.memory_mb = 1024
        report = tool.prepare(self.args(), FakeTemplate, FakeSandbox)
        self.assertEqual(report["status"], "failed")
        self.assertNotIn("gatewayTemplates", report)
        self.assertEqual(report["probeCleanup"], "deleted")

    def test_probe_errors_never_print_provider_bodies_or_credentials(self):
        FakeSandbox.failure = RuntimeError("E2B_API_KEY=DO_NOT_PRINT provider private body")
        report = tool.prepare(self.args(), FakeTemplate, FakeSandbox)
        self.assertEqual(report["errorClass"], "RuntimeError")
        self.assertNotIn("DO_NOT_PRINT", json.dumps(report))
        self.assertNotIn("private body", json.dumps(report))
        self.assertTrue(FakeSandbox.guest.deleted)

    def test_cleanup_failure_reports_owned_guest_and_withholds_profile(self):
        FakeSandbox.cleanup_failure = RuntimeError("DO_NOT_PRINT")
        report = tool.prepare(self.args(), FakeTemplate, FakeSandbox)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(report["stage"], "cleanup")
        self.assertEqual(report["probeSandboxID"], "owned-probe-123")
        self.assertNotIn("gatewayTemplates", report)
        self.assertNotIn("DO_NOT_PRINT", json.dumps(report))

    def test_dry_run_needs_no_sdk_or_provider_and_writes_private_safe_json(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / "result.json"
            argv = ["--image", IMAGE, "--alias", "grasp-prod-v1", "--name", "standard",
                    "--cpu-count", "2", "--memory-mb", "3072", "--dry-run", "--output", str(output)]
            stdout = io.StringIO()
            with patch.object(tool, "load_sdk", side_effect=AssertionError("SDK must not load")), \
                    patch.dict(os.environ, {"E2B_API_KEY": "DO_NOT_PRINT"}), contextlib.redirect_stdout(stdout):
                self.assertEqual(tool.main(argv), 0)
            result = json.loads(stdout.getvalue())
            self.assertEqual(result["status"], "validated")
            self.assertFalse(result["providerContacted"])
            self.assertNotIn("DO_NOT_PRINT", output.read_text())
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)

    def test_sdk_version_must_match_pin_without_importing_sdk(self):
        with patch.object(tool.importlib.metadata, "version", return_value="0.0.0"):
            with self.assertRaises(tool.PreparationError):
                tool.load_sdk()


if __name__ == "__main__":
    unittest.main()
