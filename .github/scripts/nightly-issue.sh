#!/usr/bin/env bash
# Reconcile one nightly run with one tracking issue: a red run opens the issue
# or refreshes its body, the next green run closes it. The marker below is the
# dedup key, so an edited, rerun or manually dispatched nightly never opens a
# second issue for the same suite.
#
# Inputs come from the environment (see the "report" job in nightly.yml):
#   GH_TOKEN, GITHUB_REPOSITORY, RESULT, RUN_ID, SHA, EVENT_NAME, INPUT_REF.
set -euo pipefail

label="ci/nightly"
marker="<!-- nightly-integration-tracker -->"
title="[nightly] integration suite is red on main"
run_url="https://github.com/${GITHUB_REPOSITORY}/actions/runs/${RUN_ID}"

summary() {
  printf '%s\n' "$*" >> "${GITHUB_STEP_SUMMARY:-/dev/null}"
}

# A dispatch against an arbitrary ref is a debugging run; it must not touch the
# issue that stands for main.
if [[ "${EVENT_NAME}" != "schedule" && -n "${INPUT_REF:-}" ]]; then
  summary "Dispatched against \`${INPUT_REF}\`; tracking issue left alone."
  exit 0
fi

if [[ "${RESULT}" == "cancelled" || "${RESULT}" == "skipped" ]]; then
  summary "Run ${RESULT}; tracking issue left alone."
  exit 0
fi

# The label must exist before it can be attached; --force creates or updates it.
gh label create "${label}" \
  --description "Automated nightly CI report" \
  --color b60205 \
  --force >/dev/null

# A failed list or body read must abort: guessing "no tracker" would open a
# duplicate issue on every transient API error.
tracker=""
for number in $(gh issue list --label "${label}" --state open --limit 100 --json number --jq '.[].number'); do
  body=$(gh api "repos/${GITHUB_REPOSITORY}/issues/${number}" --jq .body)
  if [[ "${body}" == *"${marker}"* ]]; then
    tracker="${number}"
    break
  fi
done

if [[ "${RESULT}" == "success" ]]; then
  if [[ -n "${tracker}" ]]; then
    gh issue comment "${tracker}" --body "Green again on \`${SHA}\`: ${run_url}"
    gh issue close "${tracker}"
    summary "Green; closed #${tracker}."
  else
    summary "Green; no tracking issue is open."
  fi
  exit 0
fi

# The job-level view is enough to say which leg is red; the failing test names
# are one click away in the leg's log.
failed=$(gh api "repos/${GITHUB_REPOSITORY}/actions/runs/${RUN_ID}/jobs" --paginate \
  --jq '.jobs[] | select(.conclusion == "failure" or .conclusion == "timed_out" or .conclusion == "startup_failure") | select(.name != "Report nightly status") | "- [\(.name)](\(.html_url))"' || true)
if [[ -z "${failed}" ]]; then
  failed="- the run failed before any job reported a result"
fi

body=$(cat <<EOF
${marker}
The nightly integration matrix is red on \`main\`.

- Run: ${run_url}
- Commit: [\`${SHA:0:9}\`](https://github.com/${GITHUB_REPOSITORY}/commit/${SHA})
- Failed legs:
${failed}

This issue is refreshed by every nightly run and closed by the next green one.
Closing it by hand while main is still red only means the next run reopens it.
EOF
)

if [[ -n "${tracker}" ]]; then
  gh issue edit "${tracker}" --body "${body}"
  summary "Still red; refreshed #${tracker}."
else
  gh issue create --title "${title}" --label "${label}" --body "${body}"
  summary "Red; opened the tracking issue."
fi
