import importlib.util
import json
import os
import tempfile
import unittest

_spec = importlib.util.spec_from_file_location(
    "build_apps", os.path.join(os.path.dirname(os.path.abspath(__file__)), "build-apps.py"))
build_apps = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(build_apps)

HERE = os.path.dirname(os.path.abspath(__file__))


class BuildAppsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = self.tmp.name
        self.icons = os.path.join(self.dir, "theme")
        os.makedirs(self.icons)
        with open(os.path.join(self.icons, "gimp.png"), "wb") as f:
            f.write(b"png")

    def tearDown(self):
        self.tmp.cleanup()

    def test_build_copies_icons_and_keeps_fields(self):
        entries = [
            {"id": "gimp", "name": "GIMP", "nameZh": "图像", "exec": ["gimp"], "cli": "gimp -i",
             "category": "graphics", "icons": ["missing", "gimp"]},
            {"id": "browser", "name": "Browser", "exec": ["/runtime/only"], "runtime": True,
             "category": "system", "icons": []},
        ]
        out = os.path.join(self.dir, "out")
        apps, missing = build_apps.build(entries, out, which=lambda c: "/usr/bin/" + c, dirs=[self.icons])
        self.assertEqual(missing, [])
        self.assertEqual([a["id"] for a in apps], ["gimp", "browser"])
        self.assertEqual(apps[0]["icon"], os.path.join(out, "icons", "gimp.png"))
        self.assertTrue(os.path.isfile(apps[0]["icon"]))
        self.assertEqual(apps[0]["cli"], "gimp -i")
        self.assertNotIn("icons", apps[0])
        self.assertNotIn("icon", apps[1])
        self.assertNotIn("runtime", apps[1])

    def test_missing_executable_is_reported(self):
        entries = [{"id": "x", "name": "X", "exec": ["nope"], "category": "c"}]
        apps, missing = build_apps.build(entries, self.dir, which=lambda c: None, dirs=[])
        self.assertEqual((apps, missing), ([], ["x"]))

    def test_absolute_icon_candidate(self):
        icon = os.path.join(self.icons, "gimp.png")
        self.assertEqual(build_apps.find_icon(["/does/not/exist.svg", icon], dirs=[]), icon)
        self.assertIsNone(build_apps.find_icon(["none"], dirs=[self.icons]))

    def test_xpm_is_last_resort(self):
        pixmaps = os.path.join(self.dir, "pixmaps")
        os.makedirs(pixmaps)
        for d, name in ((pixmaps, "pv.xpm"), (self.icons, "pv.png")):
            with open(os.path.join(d, name), "wb") as f:
                f.write(b"x")
        self.assertEqual(build_apps.find_icon(["pv"], dirs=[pixmaps, self.icons]), os.path.join(self.icons, "pv.png"))
        self.assertEqual(build_apps.find_icon(["pv"], dirs=[pixmaps]), os.path.join(pixmaps, "pv.xpm"))

    def test_main_usage_and_failure(self):
        self.assertEqual(build_apps.main(["build-apps.py"]), 2)
        src = os.path.join(self.dir, "in.json")
        with open(src, "w", encoding="utf-8") as f:
            json.dump([{"id": "x", "name": "X", "exec": ["definitely-not-installed-xyz"], "category": "c"}], f)
        self.assertEqual(build_apps.main(["build-apps.py", src, self.dir]), 1)

    def test_main_writes_catalog(self):
        src = os.path.join(self.dir, "in.json")
        with open(src, "w", encoding="utf-8") as f:
            json.dump([{"id": "sh", "name": "Shell", "nameZh": "终端", "exec": ["sh"], "category": "system"}], f)
        self.assertEqual(build_apps.main(["build-apps.py", src, self.dir]), 0)
        with open(os.path.join(self.dir, "apps.json"), encoding="utf-8") as f:
            self.assertEqual(json.load(f)[0]["nameZh"], "终端")

    def test_shipped_catalog_is_well_formed(self):
        with open(os.path.join(HERE, "apps.in.json"), encoding="utf-8") as f:
            entries = json.load(f)
        ids = [e["id"] for e in entries]
        self.assertEqual(len(ids), len(set(ids)))
        for e in entries:
            self.assertTrue(e["exec"] and e["exec"][0], e["id"])
            self.assertTrue(e["name"] and e["nameZh"] and e["category"], e["id"])


if __name__ == "__main__":
    unittest.main()
