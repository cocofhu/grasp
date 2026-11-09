#!/usr/bin/env python3
"""Local storage identity and Compose contracts; no Docker daemon is required."""

import concurrent.futures
import json
import os
from pathlib import Path
import stat
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/local-objectstore.sh"
PREPARE = r'''
source "$1"
prepare_local_objectstore "$2"
python3 - <<'PY'
import hashlib, json, os
names = ['GRASP_S3_ENDPOINT', 'GRASP_S3_BUCKET', 'GRASP_S3_PREFIX',
         'GRASP_S3_REGION', 'GRASP_S3_PATH_STYLE', 'COMPOSE_PROFILES',
         'GRASP_LOCAL_OBJECTSTORE_UID', 'GRASP_LOCAL_OBJECTSTORE_GID']
result = {key: os.environ.get(key) for key in names}
access = os.environ.get('GRASP_S3_ACCESS_KEY_ID', '')
secret = os.environ.get('GRASP_S3_SECRET_ACCESS_KEY', '')
result['credential_fingerprint'] = hashlib.sha256((access + '\0' + secret).encode()).hexdigest()
result['credential_lengths'] = [len(access), len(secret)]
print(json.dumps(result))
PY
'''


class LocalObjectStorageTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="grasp-local-storage-test-")
        self.addCleanup(self.temporary.cleanup)
        self.path = Path(self.temporary.name) / "objects"
        self.env = {
            key: value for key, value in os.environ.items()
            if not key.startswith(("GRASP_S3_", "GRASP_LOCAL_OBJECTSTORE_"))
            and key not in ("GRASP_STORAGE_DRIVER", "GRASP_LOCAL_OBJECTSTORE", "COMPOSE_PROFILES")
        }
        self.env["GRASP_STORAGE_DRIVER"] = "s3"
        self.env["GRASP_LOCAL_OBJECTSTORE"] = "1"

    def prepare(self, **overrides):
        return subprocess.run(
            ["bash", "-euo", "pipefail", "-c", PREPARE, "test", str(SCRIPT), str(self.path)],
            env=self.env | overrides, capture_output=True, text=True, check=False,
        )

    def successful(self, **overrides):
        result = self.prepare(**overrides)
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def test_identity_survives_restart_and_permissions_are_private(self):
        first = self.successful(COMPOSE_PROFILES="observability")
        identity = (self.path / "client.env").read_bytes()
        second = self.successful(COMPOSE_PROFILES="observability")
        self.assertEqual(first, second)
        self.assertTrue(identity == (self.path / "client.env").read_bytes())
        self.assertEqual(first["credential_lengths"], [32, 64])
        self.assertEqual(first["COMPOSE_PROFILES"], "observability,local-storage")
        self.assertEqual(first["GRASP_S3_ENDPOINT"], "http://127.0.0.1:8333")
        storage_uid = int(first["GRASP_LOCAL_OBJECTSTORE_UID"])
        storage_gid = int(first["GRASP_LOCAL_OBJECTSTORE_GID"])
        self.assertNotEqual(storage_uid, 0)
        self.assertEqual(storage_uid, 10001 if os.getuid() == 0 else os.getuid())
        self.assertEqual((self.path / "data").stat().st_uid, storage_uid)
        self.assertEqual((self.path / "data").stat().st_gid, storage_gid)
        self.assertEqual((self.path / "iam.json").stat().st_uid, storage_uid)
        self.assertEqual((self.path / "client.env").stat().st_uid, os.getuid())
        for name in ("client.env", "iam.json"):
            self.assertEqual(stat.S_IMODE((self.path / name).stat().st_mode), 0o600)
        # A host may inherit setgid on directories; it grants no access.
        self.assertEqual(stat.S_IMODE(self.path.stat().st_mode) & 0o777, 0o700)
        iam = json.loads((self.path / "iam.json").read_text())
        self.assertEqual(iam["identities"][0]["actions"], ["Read:grasp", "Write:grasp", "List:grasp"])

    def test_concurrent_first_starts_keep_one_identity(self):
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            results = list(pool.map(lambda _: self.prepare(), range(16)))
        self.assertTrue(all(item.returncode == 0 for item in results), "concurrent preparation failed")
        fingerprints = {json.loads(item.stdout)["credential_fingerprint"] for item in results}
        self.assertEqual(len(fingerprints), 1)
        self.assertEqual(len(list(self.path.glob(".client.*"))), 0)
        self.assertEqual(len(list(self.path.glob(".iam.*"))), 0)

    def test_partial_cloud_configuration_never_provisions_local_storage(self):
        for key in ("GRASP_S3_ACCESS_KEY_ID", "GRASP_S3_SECRET_ACCESS_KEY", "GRASP_S3_SESSION_TOKEN"):
            result = self.prepare(**{key: "synthetic-partial-cloud-credential"})
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(self.path.exists())
            self.assertNotIn("synthetic-partial-cloud-credential", result.stderr + result.stdout)

    def test_missing_endpoint_requires_explicit_local_selection_before_creating_storage(self):
        for value in ("", "0", "true", "invalid"):
            result = self.prepare(GRASP_LOCAL_OBJECTSTORE=value)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("GRASP_LOCAL_OBJECTSTORE=1", result.stderr)
            self.assertFalse(self.path.exists())
        del self.env["GRASP_LOCAL_OBJECTSTORE"]
        result = self.prepare()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.path.exists())

    def test_cloud_endpoint_and_local_driver_leave_storage_untouched(self):
        self.successful(GRASP_S3_ENDPOINT="https://example.invalid", GRASP_S3_ACCESS_KEY_ID="external")
        self.assertFalse(self.path.exists())
        self.successful(GRASP_STORAGE_DRIVER="local")
        self.assertFalse(self.path.exists())

    def test_invalid_port_and_bucket_fail_before_creating_identity(self):
        for value in ("0", "65536", "-1", "8333/tcp"):
            self.assertNotEqual(self.prepare(GRASP_LOCAL_OBJECTSTORE_PORT=value).returncode, 0)
        for value in ("ab", "wrong/bucket", "UPPER", "../escape", "a" * 64):
            self.assertNotEqual(self.prepare(GRASP_S3_BUCKET=value).returncode, 0)
        self.assertFalse(self.path.exists())

    def test_changed_bucket_and_damaged_identity_are_not_regenerated(self):
        self.successful()
        identity = (self.path / "client.env").read_bytes()
        self.assertNotEqual(self.prepare(GRASP_S3_BUCKET="another-bucket").returncode, 0)
        self.assertTrue(identity == (self.path / "client.env").read_bytes())
        (self.path / "client.env").write_text("LOCAL_BUCKET=grasp\nLOCAL_ACCESS_KEY=broken\n")
        self.assertNotEqual(self.prepare().returncode, 0)
        self.assertTrue((self.path / "client.env").read_text().endswith("broken\n"))

    def test_symlink_credentials_are_rejected_without_touching_target(self):
        self.path.mkdir()
        target = self.path.parent / "untouched"
        target.write_text("original")
        (self.path / "client.env").symlink_to(target)
        self.assertNotEqual(self.prepare().returncode, 0)
        self.assertEqual(target.read_text(), "original")

    def test_missing_identity_for_existing_data_requires_restore(self):
        self.successful()
        (self.path / "data" / "retained-object").write_bytes(b"previous data")
        previous_iam = (self.path / "iam.json").read_bytes()
        (self.path / "client.env").unlink()
        self.assertNotEqual(self.prepare().returncode, 0)
        self.assertFalse((self.path / "client.env").exists())
        self.assertTrue(previous_iam == (self.path / "iam.json").read_bytes())

    def test_root_uid_and_invalid_runtime_identity_are_rejected(self):
        for value in ("0", "-1", "nobody", "2147483648", "1000:1000"):
            self.assertNotEqual(self.prepare(GRASP_LOCAL_OBJECTSTORE_UID=value).returncode, 0)

    @unittest.skipUnless(os.getuid() == 0, "root is required to migrate an old root-owned data mount")
    def test_root_owned_data_moves_to_nonroot_without_changing_identity_or_symlink_target(self):
        first = self.successful()
        data = self.path / "data"
        retained = data / "retained-object"
        retained.write_bytes(b"retained payload")
        target = self.path.parent / "outside"
        target.write_bytes(b"outside payload")
        (data / "outside-link").symlink_to(target)
        os.chown(data, 0, 0)
        os.chown(retained, 0, 0)
        second = self.successful()
        self.assertEqual(first, second)
        self.assertEqual(retained.read_bytes(), b"retained payload")
        self.assertEqual(retained.stat().st_uid, 10001)
        self.assertEqual(target.stat().st_uid, 0)
        self.assertEqual(target.read_bytes(), b"outside payload")

    @unittest.skipUnless(subprocess.run(["sh", "-c", "command -v docker >/dev/null"], check=False).returncode == 0,
                         "Docker CLI is needed to validate Compose configuration")
    def test_release_and_source_compose_use_the_same_loopback_only_store(self):
        definitions = []
        for name in ("compose.release.yaml", "docker-compose.yml"):
            result = subprocess.run(
                ["docker", "compose", "-f", str(ROOT / name), "--profile", "local-storage", "config", "--format", "json"],
                env=self.env | {"HOST_REPO_DIR": str(ROOT)}, capture_output=True, text=True, check=False,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            services = json.loads(result.stdout)["services"]
            definition = services["objectstore"]
            self.assertEqual(definition["user"], "10001:10001")
            definitions.append(definition)
            self.assertEqual(definition["ports"][0]["host_ip"], "127.0.0.1")
            self.assertEqual(len(definition["ports"]), 1)
            self.assertIn("-s3.autoCreateBucket=false", definition["command"])
            app = services["grasp" if name == "compose.release.yaml" else "server"]
            self.assertEqual(app["environment"]["GRASP_STORAGE_DRIVER"], "s3")
            self.assertIn("GRASP_S3_SECRET_ACCESS_KEY", app["environment"])
        for key in ("image", "entrypoint", "user", "command", "ports", "healthcheck"):
            self.assertEqual(definitions[0][key], definitions[1][key])
        self.assertIn("@sha256:", definitions[0]["image"])


if __name__ == "__main__":
    unittest.main()
