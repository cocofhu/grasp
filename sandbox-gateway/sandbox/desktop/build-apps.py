#!/usr/bin/env python3
"""Build /opt/grasp-desktop/apps.json from apps.in.json at image build time.

Each entry lists icon candidates (absolute paths or theme icon names); the
first one found is copied to <out>/icons/<id>.<ext>. Entries whose executable
is missing fail the build so the image never advertises an absent app; entries
marked "runtime" point into the runtime bundle, which is installed at sandbox
start, and are not checked here.
"""

import json
import os
import shutil
import sys

ICON_DIRS = [
    "/usr/share/icons/hicolor/scalable/apps",
    "/usr/share/icons/hicolor/256x256/apps",
    "/usr/share/icons/hicolor/128x128/apps",
    "/usr/share/icons/hicolor/96x96/apps",
    "/usr/share/icons/hicolor/64x64/apps",
    "/usr/share/icons/hicolor/48x48/apps",
    "/usr/share/icons/hicolor/32x32/apps",
    "/usr/share/icons/Adwaita/scalable/apps",
    "/usr/share/icons/Adwaita/256x256/apps",
    "/usr/share/icons/Adwaita/48x48/apps",
    "/usr/share/icons/Adwaita/scalable/places",
    "/usr/share/icons/Adwaita/48x48/places",
    "/usr/share/pixmaps",
]


def find_icon(candidates, dirs=ICON_DIRS):
    # Browsers cannot render XPM (the launch page), so it is the last resort.
    for c in candidates:
        if os.path.isabs(c):
            if os.path.isfile(c):
                return c
            continue
        for exts in ((".svg", ".png"), (".xpm",)):
            for d in dirs:
                for ext in exts:
                    p = os.path.join(d, c + ext)
                    if os.path.isfile(p):
                        return p
    return None


def build(entries, out_dir, which=shutil.which, dirs=ICON_DIRS):
    icons = os.path.join(out_dir, "icons")
    os.makedirs(icons, exist_ok=True)
    apps, missing = [], []
    for e in entries:
        if not e.get("runtime") and which(e["exec"][0]) is None:
            missing.append(e["id"])
            continue
        app = {k: e[k] for k in ("id", "name", "nameZh", "exec", "cli", "category") if k in e}
        src = find_icon(e.get("icons", []), dirs)
        if src:
            dst = os.path.join(icons, e["id"] + os.path.splitext(src)[1])
            shutil.copyfile(src, dst)
            app["icon"] = dst
        apps.append(app)
    return apps, missing


def main(argv):
    if len(argv) != 3:
        print("usage: build-apps.py apps.in.json OUT_DIR", file=sys.stderr)
        return 2
    with open(argv[1], encoding="utf-8") as f:
        entries = json.load(f)
    apps, missing = build(entries, argv[2])
    if missing:
        print("missing executables for: " + ", ".join(missing), file=sys.stderr)
        return 1
    with open(os.path.join(argv[2], "apps.json"), "w", encoding="utf-8") as f:
        json.dump(apps, f, ensure_ascii=False, indent=2)
        f.write("\n")
    print("apps.json: %d apps" % len(apps))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
