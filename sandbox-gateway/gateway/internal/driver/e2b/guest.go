package e2b

import _ "embed"

// GuestRelaySource is installed by the E2B driver, outside the image/runtime
// bundle. Run it as python3 relay.py ROOT; ROOT/relay.json contains secret,
// marker and an explicit ports allowlist. ROOT must be private to root.
//
//go:embed relay.py
var GuestRelaySource string
