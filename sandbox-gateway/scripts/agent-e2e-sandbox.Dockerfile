# Agent switching E2E sandbox: the product sandbox image, with the CLIs that
# need proprietary platform authorization replaced by the labelled wire doubles
# in agent-e2e-matrix.json. Every native-cli entry must already be installed.
# Build context: sandbox-gateway/scripts.
ARG SANDBOX_IMAGE=universal-sandbox:agent-e2e
FROM ${SANDBOX_IMAGE}
COPY agent-e2e-matrix.json agent-platform-wire-fixture.mjs /opt/grasp-agent-e2e/
RUN set -eu; \
    node_bin="$(command -v node)"; \
    node -e 'for (const t of require("/opt/grasp-agent-e2e/agent-e2e-matrix.json").tools) console.log(t.mode, t.bin, t.id)' \
    | while read -r mode bin id; do \
        path="$(command -v "$bin")" || { echo "sandbox image is missing $bin ($id)" >&2; exit 1; }; \
        if [ "$mode" = platform-wire-fixture ]; then \
          rm -f "$path"; \
          printf '#!/bin/sh\nexec %s /opt/grasp-agent-e2e/agent-platform-wire-fixture.mjs %s "$@"\n' "$node_bin" "$id" > "$path"; \
          chmod 755 "$path"; \
        fi; \
        echo "$id: $mode $path"; \
      done
