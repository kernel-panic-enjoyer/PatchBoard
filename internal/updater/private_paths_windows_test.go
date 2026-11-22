//go:build windows

package updater

import (
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

const privatePathFullAccess windows.ACCESS_MASK = 0x1F01FF

func daclContainsFullAccessSID(dacl *windows.ACL, want *windows.SID) (bool, error) {
	if dacl == nil {
		return false, nil
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return false, err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Mask&privatePathFullAccess != privatePathFullAccess {
			continue
		}
		aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if aceSID.Equals(want) {
			return true, nil
		}
	}
	return false, nil
}

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
	for _, want := range []string{"D:P", "FA;;;SY", "FA;;;BA"} {
		if !strings.Contains(sddl, want) {
			t.Fatalf("%s should have private DACL entry %q, got %s", path, want, sddl)
		}
	}
	userSIDValue, err := windows.StringToSid(userSID)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	hasUserAccess, err := daclContainsFullAccessSID(dacl, userSIDValue)
	if err != nil {
		t.Fatal(err)
	}
	if !hasUserAccess {
		t.Fatalf("%s should have a full-access DACL entry for user SID %q, got %s", path, userSID, sddl)
	}
	for _, forbidden := range []string{";;;WD", ";;;BU", ";;;AU"} {
		if strings.Contains(sddl, forbidden) {
			t.Fatalf("%s grants broad access through %q: %s", path, forbidden, sddl)
		}
	}
}
