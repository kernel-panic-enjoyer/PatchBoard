package updater

import (
	"strings"
	"testing"
)

func TestUpdateAllFailureNoticeCompactsNoisyChocolateyOutput(t *testing.T) {
	noisyOutput := `Chocolatey v2.7.2
Upgrading the following packages:
all
By upgrading, you accept licenses for the packages.
anaconda3 v2025.12.0 is the latest version available based on your source(s).
arduino v2.3.6 is the latest version available based on your source(s).
You have chocolatey v2.7.2 installed. Version 2.7.3 is available based on your source(s).
Downloading package from source 'https://community.chocolatey.org/api/v2/'
[Approved] chocolatey package files upgrade completed. Performing other installation steps.
WARNING: It's very likely you will need to close and reopen shells before you can use choco.
`
	notice := updateResultsFailureNotice([]UpdateResult{{
		Key: packageKey(managerChoco, "*"),
		Result: CommandResult{
			Code:    1603,
			Command: `C:\ProgramData\chocolatey\bin\choco.exe upgrade all -y --no-progress --no-color`,
			Stdout:  noisyOutput,
		},
	}})

	for _, expected := range []string{
		"1 update command(s) finished with errors.",
		"choco upgrade all failed with code 1603",
		"WARNING:",
		"See Session Log for full output.",
	} {
		if !strings.Contains(notice, expected) {
			t.Fatalf("notice missing %q: %s", expected, notice)
		}
	}
	for _, unexpected := range []string{
		"anaconda3 v2025.12.0",
		"arduino v2.3.6",
		"[Approved] chocolatey package files",
	} {
		if strings.Contains(notice, unexpected) {
			t.Fatalf("notice included noisy output %q: %s", unexpected, notice)
		}
	}
	if len(notice) > 300 {
		t.Fatalf("notice too long: %d %q", len(notice), notice)
	}
}

func TestUpdateFailureNoticePrefersActionableWingetOutput(t *testing.T) {
	notice := updateFailureNotice(CommandResult{
		Code:    2316632151,
		Command: `C:\Users\User\AppData\Local\Microsoft\WindowsApps\winget.exe upgrade --id yt-dlp.FFmpeg --exact --source winget`,
		Stdout: `Gefunden FFmpeg for yt-dlp [yt-dlp.FFmpeg] Version N-124716-g054dffd133-20260531
Diese Anwendung wird von ihrem Besitzer an Sie lizenziert.
Microsoft ist nicht verantwortlich und erteilt keine Lizenzen für Pakete von Drittanbietern.
Das Portable-Paket kann nicht entfernt werden, da es geändert wurde. Um dies außer Kraft zu setzen, verwenden Sie "--force"`,
	})

	if !strings.Contains(notice, "Portable-Paket kann nicht entfernt werden") || !strings.Contains(notice, "--force") {
		t.Fatalf("notice should surface the actionable portable-package refusal, got %q", notice)
	}
	if strings.Contains(notice, "Gefunden FFmpeg") {
		t.Fatalf("notice should not stop at generic winget progress output: %q", notice)
	}
}

func TestUpdateResultsFailureNoticeIgnoresSkippedResults(t *testing.T) {
	notice := updateResultsFailureNotice([]UpdateResult{
		{Key: "winget:Current.App", Result: CommandResult{Code: commandSkippedCode, Command: "update Current App", Stdout: "Skipped: No longer actionable after refresh."}},
	})
	if notice != "" {
		t.Fatalf("skipped update should not produce a failure notice: %q", notice)
	}
}
