//go:build windows

package updater

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func assertUserPrivatePermissions(t *testing.T, path string) {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor == nil {
		t.Fatalf("%s has no security descriptor", path)
	}
	sddl := descriptor.String()
	userSID, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"D:P", "FA;;;SY", "FA;;;BA", "FA;;;" + userSID} {
		if !strings.Contains(sddl, want) {
			t.Fatalf("%s should have private DACL entry %q, got %s", path, want, sddl)
		}
	}
	for _, forbidden := range []string{";;;WD", ";;;BU", ";;;AU"} {
		if strings.Contains(sddl, forbidden) {
			t.Fatalf("%s grants broad access through %q: %s", path, forbidden, sddl)
		}
	}
}
