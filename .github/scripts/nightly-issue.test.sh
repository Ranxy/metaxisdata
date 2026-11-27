#!/usr/bin/env bash
# Scenario tests for nightly-issue.sh. Each case pins one decision: a red run
# opens or refreshes the single tracking issue, a green, cancelled, off-branch
# or debugging run leaves it alone, and a failed API call aborts instead of
# guessing "no tracker" and opening a second issue.
#
# The gh stub replays a scenario out of $STATE and logs every call, so the test
# needs neither a token nor network.
#
# Run directly: bash .github/scripts/nightly-issue.test.sh
set -euo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
script="${here}/nightly-issue.sh"
work=$(mktemp -d)
trap 'rm -rf "${work}"' EXIT

cat >"${work}/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
state="${STATE:?}"
log() { printf '%s\n' "$*" >>"${state}/calls"; }
case "$1" in
  label) log LABEL; exit 0 ;;
  issue)
    case "$2" in
      list)
        if [[ -f "${state}/fail-list" ]]; then log LIST-FAIL; exit 1; fi
        log LIST
        if [[ -s "${state}/tracker" ]]; then cat "${state}/tracker"; fi
        exit 0 ;;
      create) log CREATE; shift 2; printf '%s\n' "$@" >"${state}/created"; exit 0 ;;
      edit) log "EDIT $3"; exit 0 ;;
      comment) log "COMMENT $3"; exit 0 ;;
      close) log "CLOSE $3"; exit 0 ;;
    esac ;;
  api)
    case "$2" in
      */issues/*)
        if [[ -f "${state}/fail-body" ]]; then log BODY-FAIL; exit 1; fi
        log BODY; cat "${state}/body"; exit 0 ;;
      */actions/runs/*/jobs) log JOBS; cat "${state}/jobs"; exit 0 ;;
    esac ;;
esac
log "UNEXPECTED $*"
exit 1
STUB
chmod +x "${work}/gh"

failed=0

reset() {
  rm -rf "${work}/state" "${work}/summary"
  mkdir -p "${work}/state"
  : >"${work}/state/tracker"
  : >"${work}/state/body"
  : >"${work}/state/jobs"
  : >"${work}/state/calls"
  : >"${work}/summary"
}

# run_case <name> <expected-exit> <expected-calls> [VAR=value ...]
run_case() {
  local name=$1 want_exit=$2 want_calls=$3
  shift 3
  local got_exit=0
  env -i PATH="${work}:${PATH}" STATE="${work}/state" \
    GH_TOKEN=test GITHUB_REPOSITORY=owner/repo RESULT=success RUN_ID=42 \
    SHA=0123456789abcdef0123456789abcdef01234567 \
    EVENT_NAME=schedule INPUT_REF= REF_NAME=main DEFAULT_BRANCH=main \
    GITHUB_STEP_SUMMARY="${work}/summary" \
    "$@" bash "${script}" || got_exit=$?
  local got_calls
  got_calls=$(tr '\n' ' ' <"${work}/state/calls" | sed 's/ $//')
  if [[ "${got_exit}" -eq "${want_exit}" && "${got_calls}" == "${want_calls}" ]]; then
    printf 'ok   %s\n' "${name}"
  else
    printf 'FAIL %s\n     exit %s (want %s)\n     calls %q (want %q)\n' \
      "${name}" "${got_exit}" "${want_exit}" "${got_calls}" "${want_calls}"
    failed=1
  fi
}

marker="<!-- nightly-integration-tracker -->"

# A red run with no tracker opens exactly one issue.
reset
echo "- [baseline](https://example.invalid/1)" >"${work}/state/jobs"
run_case "red opens a tracking issue" 0 "LABEL LIST JOBS CREATE" RESULT=failure

# A failure with no job-level result still opens an issue, with a fallback line.
reset
run_case "red with no job results" 0 "LABEL LIST JOBS CREATE" RESULT=failure
if grep -qF "before any job reported a result" "${work}/state/created"; then
  printf 'ok   issue body explains a missing job result\n'
else
  printf 'FAIL issue body lacks the missing-job-result fallback\n'
  failed=1
fi

# A red run with the tracker refreshes it instead of opening a second one.
reset
echo 7 >"${work}/state/tracker"
printf '%s\n' "${marker}" >"${work}/state/body"
echo "- [postgres 17](https://example.invalid/2)" >"${work}/state/jobs"
run_case "red refreshes the existing issue" 0 "LABEL LIST BODY JOBS EDIT 7" RESULT=failure

# A green run touches nothing: an open issue means the fix has not merged, and
# closing it here would hide that. The fix's pull request closes it.
reset
echo 7 >"${work}/state/tracker"
printf '%s\n' "${marker}" >"${work}/state/body"
run_case "green leaves the issue open" 0 ""

# A green run with nothing open is silent too.
reset
run_case "green with nothing open" 0 ""

# The body tells the reader the nightly will not close it.
reset
echo "- [postgres 17](https://example.invalid/2)" >"${work}/state/jobs"
env -i PATH="${work}:${PATH}" STATE="${work}/state" \
  GH_TOKEN=test GITHUB_REPOSITORY=owner/repo RESULT=failure RUN_ID=42 \
  SHA=0123456789abcdef0123456789abcdef01234567 \
  EVENT_NAME=schedule INPUT_REF= REF_NAME=main DEFAULT_BRANCH=main \
  GITHUB_STEP_SUMMARY="${work}/summary" \
  bash "${script}"
if grep -qF "Close it from the pull request" "${work}/state/created"; then
  printf 'ok   issue body says who closes it\n'
else
  printf 'FAIL issue body does not say who closes it\n'
  failed=1
fi

# Someone else's open issue in the label is not reused as our tracker, and is
# not edited.
reset
echo 7 >"${work}/state/tracker"
echo "a hand-written issue" >"${work}/state/body"
echo "- [baseline](https://example.invalid/1)" >"${work}/state/jobs"
run_case "an issue without the marker is not reused" 0 "LABEL LIST BODY JOBS CREATE" RESULT=failure

# Cancelled and skipped runs are not results.
reset
run_case "cancelled" 0 "" RESULT=cancelled
reset
run_case "skipped" 0 "" RESULT=skipped

# An off-branch dispatch must not touch main's issue.
reset
run_case "dispatch from a feature branch" 0 "" EVENT_NAME=workflow_dispatch REF_NAME=feature

# Nor may a main dispatch that tests a feature branch.
reset
run_case "main dispatch against a feature ref" 0 "" EVENT_NAME=workflow_dispatch INPUT_REF=feature

# A missing branch name is a wiring mistake, not a reason to guess.
reset
run_case "missing REF_NAME fails closed" 1 "" REF_NAME=
reset
run_case "missing DEFAULT_BRANCH fails closed" 1 "" DEFAULT_BRANCH=

# A transient API failure aborts before any write: no duplicate, no false close.
reset
: >"${work}/state/fail-list"
run_case "a failed issue list aborts" 1 "LABEL LIST-FAIL" RESULT=failure

reset
echo 7 >"${work}/state/tracker"
printf '%s\n' "${marker}" >"${work}/state/body"
: >"${work}/state/fail-body"
run_case "a failed body read aborts" 1 "LABEL LIST BODY-FAIL" RESULT=failure

# The created issue carries the run, the commit, the failed legs and the marker.
reset
echo "- [postgres 17](https://example.invalid/2)" >"${work}/state/jobs"
env -i PATH="${work}:${PATH}" STATE="${work}/state" \
  GH_TOKEN=test GITHUB_REPOSITORY=owner/repo RESULT=failure RUN_ID=42 \
  SHA=0123456789abcdef0123456789abcdef01234567 \
  EVENT_NAME=schedule INPUT_REF= REF_NAME=main DEFAULT_BRANCH=main \
  GITHUB_STEP_SUMMARY="${work}/summary" \
  bash "${script}"
for want in "actions/runs/42" "commit/0123456789abcdef0123456789abcdef01234567" \
  "postgres 17" "${marker}"; do
  if grep -qF "${want}" "${work}/state/created"; then
    printf 'ok   issue body carries %s\n' "${want}"
  else
    printf 'FAIL issue body is missing %s\n' "${want}"
    failed=1
  fi
done

exit "${failed}"
