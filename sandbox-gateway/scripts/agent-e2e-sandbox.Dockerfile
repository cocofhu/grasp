# Agent switching E2E sandbox: the product sandbox image, with the agents that
# need proprietary platform authorization replaced by the labelled wire doubles
# in agent-e2e-matrix.json: a CLI binary (platform-wire-fixture) or an SDK
# package (platform-sdk-fixture). Every native-cli entry must already be
# installed. Build context: sandbox-gateway/scripts.
ARG SANDBOX_IMAGE=universal-sandbox:agent-e2e
FROM ${SANDBOX_IMAGE}
COPY agent-e2e-matrix.json agent-platform-wire-fixture.mjs agent-cursor-sdk-fixture.cjs /opt/grasp-agent-e2e/
RUN set -eu; \
    node_bin="$(command -v node)"; \
    node -e 'for (const t of require("/opt/grasp-agent-e2e/agent-e2e-matrix.json").tools) console.log(t.mode, t.bin || t.sdk, t.id)' \
    | while read -r mode target id; do \
        if [ "$mode" = platform-sdk-fixture ]; then \
          pkg="$target/node_modules/@cursor/sdk"; \
          [ -f "$pkg/package.json" ] || { echo "sandbox image is missing the SDK at $pkg ($id)" >&2; exit 1; }; \
          rm -rf "$pkg"; mkdir -p "$pkg"; \
          cp /opt/grasp-agent-e2e/agent-cursor-sdk-fixture.cjs "$pkg/index.js"; \
          printf '{"name":"@cursor/sdk","version":"0.0.0-grasp-e2e-fixture","main":"index.js"}\n' > "$pkg/package.json"; \
          echo "$id: $mode $pkg"; \
          continue; \
        fi; \
        path="$(command -v "$target")" || { echo "sandbox image is missing $target ($id)" >&2; exit 1; }; \
        if [ "$mode" = platform-wire-fixture ]; then \
          rm -f "$path"; \
          printf '#!/bin/sh\nexec %s /opt/grasp-agent-e2e/agent-platform-wire-fixture.mjs %s "$@"\n' "$node_bin" "$id" > "$path"; \
          chmod 755 "$path"; \
        fi; \
        echo "$id: $mode $path"; \
      done
