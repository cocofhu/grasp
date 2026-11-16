# Run at image build time inside Blender:
#   blender -b --online-mode --python blender-mcp-setup.py
# Enables the official MCP add-on, binds it to 127.0.0.1:6092 with auto start,
# allows online access (the add-on refuses to listen without it) and saves the
# user preferences so every GUI Blender session starts the bridge.
import bpy

MODULE = "bl_ext.user_default.mcp"

bpy.context.preferences.system.use_online_access = True
if MODULE not in bpy.context.preferences.addons:
    bpy.ops.preferences.addon_enable(module=MODULE)
prefs = bpy.context.preferences.addons[MODULE].preferences
prefs.host = "127.0.0.1"
prefs.port = 6092
prefs.use_autostart = True
bpy.ops.wm.save_userpref()
print("blender-mcp-setup: %s on %s:%d" % (MODULE, prefs.host, prefs.port))
