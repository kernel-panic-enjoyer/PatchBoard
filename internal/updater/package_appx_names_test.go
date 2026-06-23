package updater

import "testing"

func TestFriendlyAppxNameCleansPackageIdentity(t *testing.T) {
	cases := map[string]string{
		"19568ShareX.ShareX":                                 "ShareX",
		"19568ShareX.ShareX_egrzcvs15399j":                   "ShareX",
		"28017CharlesMilette.TranslucentTB":                  "Translucent TB",
		"28017CharlesMilette.TranslucentTB_v826wp6bftszj":    "Translucent TB",
		"38002AlexanderFrangos.TwinkleTray":                  "Twinkle Tray",
		"9662DuongDieuPhap.ImageGlass":                       "Image Glass",
		"Microsoft.WindowsNotepad":                           "Windows Notepad",
		"Microsoft.WindowsAppRuntime.Singleton":              "Windows App Runtime Singleton",
		"Microsoft.WindowsAppRuntime.CBS.1.8":                "Windows App Runtime CBS 1.8",
		"MicrosoftCorporationII.WinAppRuntime.Singleton":     "Win App Runtime Singleton",
		"Contoso.FooBar.Baz2":                                "Foo Bar Baz2",
		"1527c705-839a-4832-9118-54d4bd6a0c89_cw5n1h2txyewy": "Store app",
	}
	for input, want := range cases {
		if got := friendlyAppxName(input, ""); got != want {
			t.Fatalf("friendlyAppxName(%q) = %q, want %q", input, got, want)
		}
	}
	if got := friendlyAppxName("19568ShareX.ShareX", "ShareX"); got != "ShareX" {
		t.Fatalf("manifest display name should win, got %q", got)
	}
	if got := friendlyAppxName("19568ShareX.ShareX", "ms-resource:AppName"); got != "ShareX" {
		t.Fatalf("resource display name should fall back to package cleanup, got %q", got)
	}
	if got := friendlyAppxName("Microsoft.Todos", "ms-resource:AppName", "Microsoft To Do"); got != "Microsoft To Do" {
		t.Fatalf("start menu display name should win over resource fallback, got %q", got)
	}
}

func TestMergeAppxInventoryPackagesDoesNotResolveByDisplayName(t *testing.T) {
	state := defaultState()
	managers := map[string]ManagerStatus{
		managerStore: {Available: true},
	}
	commandResults := map[string]CommandResult{}
	appx := []Package{
		{
			ID:            "OpenAI.Codex_abc123",
			Name:          "Codex",
			Version:       "1.0.0.0",
			Manager:       managerStore,
			Source:        sourceNativeAppX,
			Match:         "OpenAI.Codex_1.0.0.0_x64__abc123",
			ActionBackend: backendAppXInventory,
		},
	}

	got := mergeAppxInventoryPackages(&state, managers, commandResults, nil, appx, map[string]string{})

	if len(got) != 1 || got[0].ID != appx[0].ID || got[0].UpdateSupported || got[0].UpdateAvailable {
		t.Fatalf("native Store row should stay inventory-only until transactional assessment applies: %#v", got)
	}
	if len(commandResults) != 0 {
		t.Fatalf("display-name resolver command output must not be recorded: %#v", commandResults)
	}
}
