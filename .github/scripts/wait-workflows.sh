#!/usr/bin/env bash
# Wait for the path-filtered sibling workflows of this commit and fail when any
# of them did not succeed. Used by the `ci-required` job in ci.yml so a single
# check can be required in branch protection.
#
# A workflow that never started for this commit (path filter did not match)
# counts as OK. Needs: gh, jq, GH_TOKEN with actions:read.
#
# Env:
#   REPO        owner/name (GITHUB_REPOSITORY)
#   HEAD_SHA    commit the runs belong to
#   EVENT       push | pull_request (only runs of this event are considered)
#   WORKFLOWS   newline- or comma-separated workflow names to wait for
#   POLL_SECS   poll interval (default 30)
set -euo pipefail

: "${REPO:?}" "${HEAD_SHA:?}" "${EVENT:?}" "${WORKFLOWS:?}"
POLL_SECS="${POLL_SECS:-30}"

names_json="$(printf '%s\n' "${WORKFLOWS//,/$'\n'}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | grep -v '^$' | jq -R . | jq -s .)"
echo "waiting for workflows: $(jq -c . <<<"$names_json") on ${HEAD_SHA} (${EVENT})"

while true; do
  runs="$(gh api --paginate "repos/${REPO}/actions/runs?head_sha=${HEAD_SHA}&event=${EVENT}&per_page=100" \
    --jq '.workflow_runs[] | {name, status, conclusion, run_number, run_attempt, html_url}' | jq -s .)"
  # Latest run (and attempt) per workflow name, limited to the watched set.
  latest="$(jq --argjson names "$names_json" '
    [ .[] | select(.name as $n | $names | index($n)) ]
    | group_by(.name)
    | map(max_by([.run_number, .run_attempt]))' <<<"$runs")"

  pending="$(jq -r '.[] | select(.status != "completed") | .name' <<<"$latest")"
  jq -r '.[] | "  \(.name): \(.status)/\(.conclusion // "-")  \(.html_url)"' <<<"$latest"
  if [[ -z "$pending" ]]; then
    break
  fi
  echo "still running: $(paste -sd, <<<"$pending"); next poll in ${POLL_SECS}s"
  sleep "$POLL_SECS"
done

bad="$(jq -r '.[] | select(.conclusion != "success" and .conclusion != "skipped") | "\(.name)=\(.conclusion)"' <<<"$latest")"
if [[ -n "$bad" ]]; then
  echo "::error::required workflows did not succeed: $(paste -sd' ' <<<"$bad")"
  exit 1
fi
echo "all triggered workflows succeeded (not triggered = not affected by this change)"
