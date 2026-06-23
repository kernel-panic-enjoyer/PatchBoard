#!/usr/bin/env bash
# Thin POSIX wrapper around the test suites for non-PowerShell shells.
#
# Runs each suite independently and reports each result:
#   1. Root module unit/integration tests:  go test ./...
#   2. Browser-level UI tests (separate tests/browser module):
#        go test -tags uitestsupport ./...   (only when Chrome/Edge is available)
# Pass --live to also run the gated destructive Microsoft Store tests
# (build tag "storelive"); they self-skip unless UPDATER_RUN_STORE_LIVE_* is set.
# Pass --skip-browser to skip the browser suite.
#
# Requires Go >= 1.26 on PATH.
set -u
root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
run_live=0
skip_browser=0
for arg in "$@"; do
	case "$arg" in
		--live) run_live=1 ;;
		--skip-browser) skip_browser=1 ;;
		*) echo "unknown argument: $arg" >&2; exit 2 ;;
	esac
done

root_rc=0
browser_rc="skipped"
live_rc="not run"

echo "== Root module tests: go test ./... =="
( cd "$root" && go test ./... -count=1 ); root_rc=$?

browser_available() {
	[ -n "${CHROME_PATH:-}" ] && [ -f "${CHROME_PATH}" ] && return 0
	for name in msedge.exe chrome.exe chromium.exe msedge google-chrome chromium chromium-browser; do
		command -v "$name" >/dev/null 2>&1 && return 0
	done
	for p in \
		"${ProgramFiles:-}/Microsoft/Edge/Application/msedge.exe" \
		"${LocalAppData:-}/Microsoft/Edge/Application/msedge.exe" \
		"${ProgramFiles:-}/Google/Chrome/Application/chrome.exe"; do
		[ -f "$p" ] && return 0
	done
	return 1
}

if [ "$skip_browser" -eq 1 ]; then
	echo "== Browser module tests: skipped (--skip-browser) =="
elif browser_available; then
	echo "== Browser module tests: go test -tags uitestsupport ./... (tests/browser) =="
	( cd "$root/tests/browser" && go test -tags uitestsupport ./... -count=1 ); browser_rc=$?
else
	echo "== Browser module tests: skipped (no Chromium/Edge found) =="
fi

if [ "$run_live" -eq 1 ]; then
	echo "== Live Store tests: go test -tags storelive ./internal/updater/ -run TestLive =="
	( cd "$root" && go test -tags storelive ./internal/updater/ -run TestLive -count=1 -v ); live_rc=$?
fi

echo ""
echo "== Summary =="
printf '  root     %s\n' "$([ "$root_rc" -eq 0 ] && echo PASS || echo "FAIL (exit $root_rc)")"
printf '  browser  %s\n' "$browser_rc"
[ "$run_live" -eq 1 ] && printf '  live     %s\n' "$live_rc"

if [ "$root_rc" -ne 0 ] || { [ "$browser_rc" != "skipped" ] && [ "$browser_rc" != "0" ] && [ "$browser_rc" != "not run" ]; }; then
	exit 1
fi
exit 0
