#!/usr/bin/env bash
# Confirm a built desktop sandbox image carries the desktop shell, the app
# catalog with every advertised executable, and Blender with the official MCP
# add-on bound to 127.0.0.1:6092. Offline; nothing is pushed.
set -euo pipefail

image="${1:?image ref}"

docker run --rm --entrypoint bash "$image" -lc '
set -euo pipefail
test -f /etc/grasp-desktop
test -L /usr/local/bin/desktop-session.sh
test -L /usr/local/bin/grasp-desktop
for cmd in Xvfb x11vnc websockify socat chrome-devtools-mcp \
           openbox plank fcitx5 dbus-launch feh import xdotool wmctrl xdpyinfo \
           blender blender-mcp godot drawio Slicer gimp inkscape kdenlive melt \
           freecad openscad kicad qgis paraview sol kigo mousepad evince lxterminal pcmanfm; do
  command -v "$cmd" >/dev/null || { echo "missing $cmd" >&2; exit 1; }
done
locale -a | grep -qi "zh_CN.utf8"
python3 - <<"PY"
import json, os, shutil
apps = json.load(open("/opt/grasp-desktop/apps.json"))
assert len(apps) >= 15, len(apps)
for a in apps:
    exe = a["exec"][0]
    assert exe.startswith("/opt/grasp-runtime") or exe.startswith("/usr/local/bin/desktop-session.sh") or shutil.which(exe), a["id"]
    if "icon" in a:
        assert os.path.isfile(a["icon"]), a["icon"]
print("apps.json: %d apps" % len(apps))
PY
blender --version | head -1 | grep -q "^Blender 5\.1\."
blender -b --python-expr "
import bpy, sys
m = \"bl_ext.user_default.mcp\"
p = bpy.context.preferences
ok = m in p.addons and p.addons[m].preferences.port == 6092 and p.addons[m].preferences.host == \"127.0.0.1\" and p.system.use_online_access
sys.exit(0 if ok else 3)
" >/dev/null
/opt/blender-mcp/bin/python -c "import blmcp"
echo "desktop image OK"
'
