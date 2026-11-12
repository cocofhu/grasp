#!/usr/bin/env python3
"""Build an immutable Grasp environment template, then probe one private guest.

This is operator tooling, not a runtime gateway dependency. --dry-run validates
inputs without importing the E2B SDK or contacting a provider.
"""

from __future__ import annotations

import argparse
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import re
import shlex
import sys
from typing import Any


SDK_VERSION = "2.49.1"
IMAGE_PATTERN = re.compile(r"[a-z0-9][a-z0-9._:/-]*@sha256:[0-9a-f]{64}\Z")
ALIAS_PATTERN = re.compile(r"[a-z0-9][a-z0-9-]{0,62}\Z")
ID_PATTERN = re.compile(r"[A-Za-z0-9_-]{1,128}\Z")
BOOTSTRAP = "sandbox-gateway/sandbox/scripts/grasp-bootstrap.sh"
IMAGE_LEVEL = "sandbox-gateway/sandbox/IMAGE_LEVEL"
BUILD_ENV = {
    "PATH": "/root/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/go/bin:/root/go/bin",
    "PLAYWRIGHT_BROWSERS_PATH": "/ms-playwright",
}
# Check real installed executables. Never install or replace a vendor CLI with
# an E2E wire fixture, and never perform vendor inference during preparation.
NATIVE_CLIS = ("cursor-agent", "claude", "codebuddy", "traecli", "opencode", "codex", "dsh", "kimi")
REQUIRED_COMMANDS = NATIVE_CLIS + (
    "bash", "python3", "node", "npm", "go", "git", "curl", "tar",
    "sshd", "code-server", "docker", "Xvfb", "x11vnc", "websockify",
    "chrome-devtools-mcp",
)


class PreparationError(Exception):
    """Only fixed, operator-safe messages belong in this exception."""


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--image", required=True, help="OCI environment image pinned with @sha256:<64 lowercase hex>")
    result.add_argument("--alias", required=True, help="Explicit build name; use a new versioned alias")
    result.add_argument("--name", required=True, help="Gateway template name, e.g. standard")
    result.add_argument("--cpu-count", required=True, type=int)
    result.add_argument("--memory-mb", required=True, type=int, help="Prepared guest memory in MiB")
    result.add_argument("--minimum-workspace-gib", type=int, default=20, help="Required initially available workspace bytes / 2**30; not a quota or disk sizing request")
    result.add_argument("--repo-root", type=Path, default=Path(__file__).resolve().parents[3])
    result.add_argument("--request-timeout", type=int, default=240, help="SDK request timeout in seconds")
    result.add_argument("--output", type=Path, help="Optional credential-free result JSON, written with mode 0600")
    result.add_argument("--dry-run", action="store_true", help="Validate and print the plan without SDK imports, builds or guests")
    return result


def validate(args: argparse.Namespace) -> dict[str, Any]:
    if not IMAGE_PATTERN.fullmatch(args.image) or "://" in args.image or "//" in args.image:
        raise PreparationError("--image must be an OCI reference pinned by a complete sha256 digest")
    if not ALIAS_PATTERN.fullmatch(args.alias):
        raise PreparationError("--alias must contain 1-63 lowercase letters, digits or hyphens and start with a letter or digit")
    if not args.name or len(args.name) > 256 or any(c.isspace() or ord(c) < 32 for c in args.name):
        raise PreparationError("--name must be a nonempty gateway template name without whitespace")
    if min(args.cpu_count, args.memory_mb, args.minimum_workspace_gib, args.request_timeout) <= 0:
        raise PreparationError("CPU, memory, minimum workspace and request timeout must be positive integers")
    root = args.repo_root.resolve()
    if not (root / BOOTSTRAP).is_file() or not (root / IMAGE_LEVEL).is_file():
        raise PreparationError("--repo-root must contain Grasp's original bootstrap and IMAGE_LEVEL files")
    level = (root / IMAGE_LEVEL).read_text(encoding="utf-8").strip()
    if not level.isdecimal() or int(level) < 1:
        raise PreparationError("the source IMAGE_LEVEL must be a positive integer")
    return {
        "image": args.image,
        "alias": args.alias,
        "name": args.name,
        "cpuCount": args.cpu_count,
        "memoryMB": args.memory_mb,
        "minimumInitialWorkspaceGiB": args.minimum_workspace_gib,
        "imageLevel": int(level),
        "bootstrapSHA256": hashlib.sha256((root / BOOTSTRAP).read_bytes()).hexdigest(),
        "requiredNativeCLIs": list(NATIVE_CLIS),
        "runtimeBundleBakedIn": False,
    }


def validation_command(plan: dict[str, Any], require_disk: bool) -> str:
    """One JSON result, no environment dump, model call, or credential read."""
    expected = {
        "commands": REQUIRED_COMMANDS,
        "native": NATIVE_CLIS,
        "level": plan["imageLevel"],
        "bootstrap": plan["bootstrapSHA256"],
        "minimum": plan["minimumInitialWorkspaceGiB"] * (1 << 30) if require_disk else 0,
    }
    code = """import glob,hashlib,json,os,pathlib,shutil
expected=json.loads(EXPECTED)
missing=[name for name in expected['commands'] if shutil.which(name) is None]
if missing: raise SystemExit('missing required environment commands: '+','.join(missing))
for name in expected['native']:
    path=pathlib.Path(shutil.which(name)).resolve()
    with path.open('rb') as handle: prefix=handle.read(65536)
    if '/opt/grasp-e2e/' in str(path) or b'agent-platform-wire-fixture' in prefix:
        raise SystemExit('test fixture is not a production native CLI')
if pathlib.Path('/opt/grasp-e2e/agent-platform-wire-fixture.mjs').exists():
    raise SystemExit('test fixture content is not allowed in the production template')
bootstrap=pathlib.Path('/grasp-bootstrap.sh')
if not os.access(bootstrap,os.X_OK) or hashlib.sha256(bootstrap.read_bytes()).hexdigest()!=expected['bootstrap']:
    raise SystemExit('bootstrap does not match the source checkout')
if int(pathlib.Path('/etc/grasp-image-level').read_text().strip())!=expected['level']:
    raise SystemExit('IMAGE_LEVEL does not match the source checkout')
if pathlib.Path('/opt/grasp-runtime/current/MANIFEST').exists():
    raise SystemExit('runtime bundle must be supplied by the Grasp server at create')
chromes=glob.glob('/ms-playwright/chromium-*/chrome-linux*/chrome')
if not any(os.access(path,os.X_OK) for path in chromes) and shutil.which('chromium') is None:
    raise SystemExit('headed Chromium executable is missing')
workspace=pathlib.Path('/root/workspace');workspace.mkdir(parents=True,exist_ok=True)
stats=os.statvfs(workspace);available=stats.f_bavail*stats.f_frsize
if available<expected['minimum']: raise SystemExit('initially available workspace capacity is below the requested profile')
print(json.dumps({'status':'passed','imageLevel':expected['level'],'bootstrapSHA256':expected['bootstrap'],
                 'nativeCLIs':expected['native'],'workspaceAvailableBytes':available}))
""".replace("EXPECTED", repr(json.dumps(expected)))
    return "python3 - <<'GRASP_TEMPLATE_VALIDATE'\n" + code + "GRASP_TEMPLATE_VALIDATE\n"


def profile_line(name: str, value: str) -> str:
    if name != "PATH":
        return "export " + name + "=" + shlex.quote(value) + "\n"
    # profile.d runs in name order: overwriting PATH here would drop entries that
    # earlier fragments (agent-path.sh, devtools-path.sh) already added.
    entries = " ".join(shlex.quote(p) for p in reversed(value.split(":")))
    return ('for p in ' + entries + '; do case ":$PATH:" in *":$p:"*) ;; *) PATH="$p:$PATH" ;; esac; done\n'
            "unset p\nexport PATH\n")


def configure_template(template_class: Any, args: argparse.Namespace, plan: dict[str, Any]) -> Any:
    # The pinned environment image already contains bootstrap and IMAGE_LEVEL.
    # Validate those originals below: overwriting them would hide an incompatible
    # image and unnecessarily require the provider's build-file upload service.
    builder = template_class().from_image(args.image).set_user("root").set_envs(BUILD_ENV)
    # SDK set_envs applies to build instructions. Persist only these public
    # environment defaults for login shells; the Go driver also exports them
    # for non-login bootstrap processes. The runtime bundle stays server-owned.
    profile = "".join(profile_line(name, value) for name, value in BUILD_ENV.items())
    command = (
        "set -eu\ninstall -d -m 755 /etc/profile.d\n"
        "printf %s " + shlex.quote(profile) + " > /etc/profile.d/grasp-e2b-env.sh\n"
        "chmod 644 /etc/profile.d/grasp-e2b-env.sh\n"
        + validation_command(plan, require_disk=False)
    )
    return builder.run_cmd(command, user="root")


def load_sdk() -> tuple[Any, Any]:
    try:
        installed = importlib.metadata.version("e2b")
    except importlib.metadata.PackageNotFoundError:
        raise PreparationError("install requirements-build.txt in a Python virtual environment") from None
    if installed != SDK_VERSION:
        raise PreparationError("the builder requires the exact e2b version in requirements-build.txt")
    from e2b import Sandbox, Template  # Lazy: input validation and unit tests need no SDK.
    return Template, Sandbox


def prepare(args: argparse.Namespace, template_class: Any, sandbox_class: Any) -> dict[str, Any]:
    plan = validate(args)
    report: dict[str, Any] = {"status": "failed", "stage": "build", **plan}
    guest = None
    try:
        template = configure_template(template_class, args, plan)
        built = template_class.build(template, args.alias, cpu_count=args.cpu_count,
                                     memory_mb=args.memory_mb, request_timeout=args.request_timeout)
        if not ID_PATTERN.fullmatch(str(built.template_id)) or not ID_PATTERN.fullmatch(str(built.build_id)):
            raise PreparationError("the provider returned invalid template/build identifiers")
        report.update(templateID=built.template_id, buildID=built.build_id, stage="probe")
        guest = sandbox_class.create(
            built.template_id, timeout=300, secure=True, allow_internet_access=False,
            lifecycle={"on_timeout": "kill", "auto_resume": False},
            network={"allow_public_traffic": False}, envs=BUILD_ENV,
            metadata={"grasp.template.prepare": args.alias}, request_timeout=args.request_timeout,
        )
        guest_id = getattr(guest, "sandbox_id", "")
        if isinstance(guest_id, str) and ID_PATTERN.fullmatch(guest_id):
            report["probeSandboxID"] = guest_id
        info = guest.get_info()
        if info.template_id != built.template_id or info.cpu_count != args.cpu_count or info.memory_mb != args.memory_mb:
            raise PreparationError("the private guest template, CPU or memory differs from the requested profile")
        command = "bash -lc " + shlex.quote(validation_command(plan, require_disk=True))
        result = guest.commands.run(command, user="root", timeout=120)
        if result.exit_code != 0:
            raise PreparationError("private dependency/capacity probe failed; inspect the owned build privately")
        probe = json.loads(result.stdout)
        if (probe.get("status") != "passed" or probe.get("imageLevel") != plan["imageLevel"]
                or probe.get("bootstrapSHA256") != plan["bootstrapSHA256"]
                or probe.get("nativeCLIs") != list(NATIVE_CLIS)
                or type(probe.get("workspaceAvailableBytes")) is not int
                or probe["workspaceAvailableBytes"] < args.minimum_workspace_gib * (1 << 30)):
            raise PreparationError("the private guest did not confirm required environment and initial disk capacity")
        report.update(status="passed", stage="complete", workspaceAvailableBytes=probe["workspaceAvailableBytes"],
                      gatewayTemplates={args.name: {"templateID": built.template_id,
                                      "cpuCores": args.cpu_count, "memoryMB": args.memory_mb,
                                      "diskGi": args.minimum_workspace_gib}})
    except Exception as exc:
        report["errorClass"] = type(exc).__name__
        if isinstance(exc, PreparationError):
            report["message"] = str(exc)
    finally:
        if guest is not None:
            try:
                guest.kill()
                report["probeCleanup"] = "deleted"
            except Exception as exc:
                report.update(status="failed", stage="cleanup", cleanupErrorClass=type(exc).__name__)
                report.pop("gatewayTemplates", None)
    return report


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    try:
        plan = validate(args)
        if args.dry_run:
            report = {"status": "validated", "providerContacted": False, **plan}
        else:
            if not os.environ.get("E2B_API_KEY", "").strip():
                raise PreparationError("set E2B_API_KEY in the operator environment")
            template_class, sandbox_class = load_sdk()
            report = prepare(args, template_class, sandbox_class)
    except Exception as exc:
        report = {"status": "failed", "stage": "validation", "errorClass": type(exc).__name__}
        if isinstance(exc, PreparationError):
            report["message"] = str(exc)
    encoded = json.dumps(report, indent=2) + "\n"
    if args.output:
        fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            os.fchmod(handle.fileno(), 0o600)
            handle.write(encoded)
    print(encoded, end="")
    return 0 if report["status"] in ("passed", "validated") else 1


if __name__ == "__main__":
    sys.exit(main())
