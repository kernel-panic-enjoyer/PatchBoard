//go:build windows

package updater

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// RULE-103: an App that never recorded its listen address must not trust
// every Host header.
func TestTrustedHostFailsClosedWithoutListenAddress(t *testing.T) {
	app := &App{}
	for host, want := range map[string]bool{
		"evil.test":        false,
		"evil.test:4183":   false,
		"127.0.0.1:9999":   false,
		"127.0.0.1:4183":   true,
		"localhost:4183":   true,
		"[::1]:4183":       true,
		"":                 false,
		"192.168.1.5:4183": false,
	} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Host = host
		if got := app.trustedHost(request); got != want {
			t.Errorf("trustedHost(%q) = %t, want %t", host, got, want)
		}
	}
}

// RULE-110: a hash field must be exactly one digest, not merely contain one.
func TestSHA256FieldsMustBeExactlyOneDigest(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	for value, want := range map[string]bool{
		digest:                  true,
		strings.ToUpper(digest): true,
		digest + " extra":       false,
		"prefix " + digest:      false,
		digest + "\n" + digest:  false,
		digest[:63]:             false,
		strings.Repeat("g", 64): false,
	} {
		if got := isSHA256Hex(value); got != want {
			t.Errorf("isSHA256Hex(%q) = %t, want %t", value, got, want)
		}
	}
}

// RULE-272: staged downloads in the self-update folder must not count as
// pending health requests.
func TestPendingHealthRequestCountIgnoresOtherFiles(t *testing.T) {
	directory := t.TempDir()
	for index := 0; index < maxPendingHealthRequests+4; index++ {
		name := filepath.Join(directory, "PatchBoard-v0.0."+string(rune('a'+index))+".exe")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "health-1"+selfUpdateHealthRequestFileSuffix), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "health-dir"+selfUpdateHealthRequestFileSuffix), 0o700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := countPendingSelfUpdateHealthRequests(entries); got != 1 {
		t.Fatalf("counted %d pending health requests, want 1", got)
	}
}

// RULE-383: a caller-supplied bootstrap token must be long and URL-safe.
func TestCallerBootstrapTokenStrength(t *testing.T) {
	for token, wantOK := range map[string]bool{
		"abc":                                  false,
		"smoke-token":                          false,
		strings.Repeat("a", 31):                false,
		strings.Repeat("a", 32):                true,
		"0123456789abcdef0123456789abcdef-._~": true,
		strings.Repeat("a", 32) + "&x=1":       false,
		strings.Repeat("a", 32) + " ":          false,
	} {
		if err := validateCallerBootstrapToken(token); (err == nil) != wantOK {
			t.Errorf("validateCallerBootstrapToken(%q) err=%v, want ok=%t", token, err, wantOK)
		}
	}
	generated, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCallerBootstrapToken(generated); err != nil {
		t.Fatalf("generated token fails the caller policy: %v", err)
	}
}

// Security 5: the admin worker opens the pipe at identification level so the
// pipe's server cannot impersonate it.
func TestElevatedWorkerPipeClientCannotBeImpersonated(t *testing.T) {
	const mask = windows.SECURITY_SQOS_PRESENT | windows.SECURITY_IDENTIFICATION | windows.SECURITY_IMPERSONATION | windows.SECURITY_DELEGATION
	if elevatedWorkerPipeClientFlags&mask != windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION {
		t.Fatalf("pipe client flags %#x do not request identification-level SQOS", elevatedWorkerPipeClientFlags)
	}
}
