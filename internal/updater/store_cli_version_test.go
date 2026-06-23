package updater

import (
	"strings"
	"testing"
)

// parseStoreCLIVersion extracts a "vMAJOR.MINOR…" token from Store CLI banner
// output. It and its regression below are intentionally kept in the default
// (untagged) test suite: although it is consumed by the env-gated live tests in
// store_live_integration_test.go (build tag "storelive"), the parsing behaviour
// itself is a non-destructive unit regression that must keep running in the
// normal `go test ./...` suite. Because this is an untagged _test.go file, the
// function is compiled for both the default and the storelive test builds.
func parseStoreCLIVersion(output string) string {
	for _, raw := range strings.Split(output, "\n") {
		for _, field := range strings.Fields(strings.TrimSpace(raw)) {
			field = strings.Trim(field, ",;()[]")
			if len(field) < 2 || field[0] != 'v' {
				continue
			}
			if field[1] >= '0' && field[1] <= '9' {
				return field
			}
		}
	}
	return ""
}

func TestParseStoreCLIVersion(t *testing.T) {
	output := "██████╗ ████████╗\n\nv22605.1401.12.0 - Preview\n"
	if got := parseStoreCLIVersion(output); got != "v22605.1401.12.0" {
		t.Fatalf("version = %q", got)
	}
}
