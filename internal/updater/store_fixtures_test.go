package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storeCLIFixtureVersion is the Store CLI build the bundled output samples were
// captured from. Fixtures live under testdata/storecli/<version>/<locale>/.
const storeCLIFixtureVersion = "22605.1401.12.0"

// loadStoreCLIFixture returns the Store CLI output sample at
// testdata/storecli/<version>/<locale>/<name>. The content is normalized to LF
// line endings and the trailing newline is stripped, so the returned string is
// byte-identical to a captured Store CLI invocation (which has no trailing
// newline), independent of how git materializes the file on disk.
func loadStoreCLIFixture(t *testing.T, version, locale, name string) string {
	t.Helper()
	path := filepath.Join("testdata", "storecli", version, locale, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("load Store CLI fixture %s: %v", path, err)
	}
	return strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
}
