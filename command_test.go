package main

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManagerCommandOverride(t *testing.T) {
	t.Setenv("UPDATER_WINGET_PATH", filepath.Join("C:", "Tools", "winget.exe"))
	got := managerCommand("winget", "--version")
	if len(got) != 2 || got[0] != filepath.Join("C:", "Tools", "winget.exe") || got[1] != "--version" {
		t.Fatalf("unexpected manager command: %#v", got)
	}

	t.Setenv("UPDATER_STORE_PATH", filepath.Join("C:", "Tools", "store.exe"))
	got = managerCommand("store", "--help")
	if len(got) != 2 || got[0] != filepath.Join("C:", "Tools", "store.exe") || got[1] != "--help" {
		t.Fatalf("unexpected store manager command: %#v", got)
	}
}

func TestIsWingetCommand(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{filepath.Join("C:", "Users", "User", "AppData", "Local", "Microsoft", "WindowsApps", "winget.exe"), "--version"}, true},
		{[]string{"winget", "--version"}, true},
		{[]string{"cmd.exe", "/d", "/c", "winget", "--version"}, true},
		{[]string{"choco", "--version"}, false},
		{[]string{"cmd.exe", "/c", "winget", "--version"}, false},
	}
	for _, tc := range cases {
		if got := isWingetCommand(tc.args); got != tc.want {
			t.Fatalf("isWingetCommand(%#v) = %t, want %t", tc.args, got, tc.want)
		}
	}
}

func TestIsStoreCommand(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{filepath.Join("C:", "Users", "User", "AppData", "Local", "Microsoft", "WindowsApps", "store.exe"), "--help"}, true},
		{[]string{"store", "--help"}, true},
		{[]string{"cmd.exe", "/d", "/c", "store", "--help"}, true},
		{[]string{"winget", "--version"}, false},
		{[]string{"cmd.exe", "/c", "store", "--help"}, false},
	}
	for _, tc := range cases {
		if got := isStoreCommand(tc.args); got != tc.want {
			t.Fatalf("isStoreCommand(%#v) = %t, want %t", tc.args, got, tc.want)
		}
	}
}

func TestPackageManagerMutationCommandDetection(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"store", "search", "Codex"}, false},
		{[]string{"store", "install", "OpenAI.Codex"}, true},
		{[]string{"cmd.exe", "/d", "/c", "store", "updates"}, true},
		{[]string{"winget", "list"}, false},
		{[]string{"winget", "upgrade", "--all"}, true},
		{[]string{"cmd.exe", "/d", "/c", "winget", "search", "git"}, false},
		{[]string{"choco", "outdated"}, false},
		{[]string{"choco", "upgrade", "all"}, true},
	}
	for _, tc := range cases {
		if got := isPackageManagerMutationCommand(tc.args); got != tc.want {
			t.Fatalf("isPackageManagerMutationCommand(%#v) = %t, want %t", tc.args, got, tc.want)
		}
	}
}

func TestLogBufferAppendSinceAndRetention(t *testing.T) {
	buffer := newLogBuffer(3)
	first := buffer.Append("app", "one")
	second := buffer.Append("stdout", "two")
	third := buffer.Append("stderr", "three")
	fourth := buffer.Append("exit", "four")

	if first.ID != 1 || second.ID != 2 || third.ID != 3 || fourth.ID != 4 {
		t.Fatalf("unexpected log ids: %d %d %d %d", first.ID, second.ID, third.ID, fourth.ID)
	}
	if buffer.LatestID() != 4 {
		t.Fatalf("expected latest id 4, got %d", buffer.LatestID())
	}

	retained := buffer.Since(0)
	if len(retained) != 3 || retained[0].Message != "two" || retained[2].Message != "four" {
		t.Fatalf("unexpected retained entries: %#v", retained)
	}

	newer := buffer.Since(2)
	if len(newer) != 2 || newer[0].ID != 3 || newer[1].ID != 4 {
		t.Fatalf("unexpected since entries: %#v", newer)
	}
}

func TestAppendLogChunkDropsCarriageReturnSpinnerFrames(t *testing.T) {
	oldLogs := sessionLogs
	sessionLogs = newLogBuffer(10)
	defer func() { sessionLogs = oldLogs }()

	pending := appendLogChunk("stdout", "", "Downloading\r|\r/\r-\r")
	pending = appendLogChunk("stdout", pending, `\`+"\rDone\n")
	if pending != "" {
		t.Fatalf("expected no pending log text, got %q", pending)
	}

	entries := sessionLogs.Since(0)
	if len(entries) != 1 || entries[0].Message != "Done" {
		t.Fatalf("expected only final line, got %#v", entries)
	}
}

func TestStreamCommandOutputKeepsRawOutputWhileDroppingSpinnerLog(t *testing.T) {
	oldLogs := sessionLogs
	sessionLogs = newLogBuffer(10)
	defer func() { sessionLogs = oldLogs }()

	raw := "Downloading\r|\r/\r-\rDone\n"
	var output bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	streamCommandOutput(strings.NewReader(raw), "stdout", &output, &wg)
	wg.Wait()

	if output.String() != raw {
		t.Fatalf("raw output changed: got %q want %q", output.String(), raw)
	}
	entries := sessionLogs.Since(0)
	if len(entries) != 1 || entries[0].Message != "Done" {
		t.Fatalf("expected only final log line, got %#v", entries)
	}
}

func TestAppendLogChunkPreservesNormalLines(t *testing.T) {
	oldLogs := sessionLogs
	sessionLogs = newLogBuffer(10)
	defer func() { sessionLogs = oldLogs }()

	pending := appendLogChunk("stdout", "", "first\r")
	pending = appendLogChunk("stdout", pending, "\nsecond\nthird")
	pending = appendLogChunk("stdout", pending, "\n")
	if pending != "" {
		t.Fatalf("expected no pending log text, got %q", pending)
	}

	entries := sessionLogs.Since(0)
	if len(entries) != 3 || entries[0].Message != "first" || entries[1].Message != "second" || entries[2].Message != "third" {
		t.Fatalf("unexpected normal log lines: %#v", entries)
	}
}

func TestRunCommandContextCancellation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows command cancellation test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	result := runCommandContext(ctx, 10*time.Second, "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "Start-Sleep -Seconds 5")

	if result.OK || result.Code != commandCancelledCode || !strings.Contains(result.Stderr, "Cancelled.") {
		t.Fatalf("expected cancelled command result, got %#v", result)
	}
}

func TestRunCommandContextCancellationWhileWaitingForMutationLock(t *testing.T) {
	packageManagerMutationMu.Lock()
	defer packageManagerMutationMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	started := time.Now()
	result := runCommandContext(ctx, 10*time.Second, "choco.exe", "upgrade", "example-package")
	elapsed := time.Since(started)

	if result.OK || result.Code != commandCancelledCode || !strings.Contains(result.Stderr, "Cancelled.") {
		t.Fatalf("expected cancelled command result, got %#v", result)
	}
	if elapsed > time.Second {
		t.Fatalf("cancel while waiting for package-manager lock took too long: %s", elapsed)
	}
	if !strings.Contains(result.Command, "choco.exe upgrade example-package") {
		t.Fatalf("unexpected command text: %q", result.Command)
	}
}

func TestRunCommandContextTimeoutWhileWaitingForMutationLock(t *testing.T) {
	packageManagerMutationMu.Lock()
	defer packageManagerMutationMu.Unlock()

	started := time.Now()
	result := runCommandContext(context.Background(), 50*time.Millisecond, "choco.exe", "upgrade", "example-package")
	elapsed := time.Since(started)

	if result.OK || result.Code != 124 || !strings.Contains(result.Stderr, "Timed out.") {
		t.Fatalf("expected timeout command result, got %#v", result)
	}
	if elapsed > time.Second {
		t.Fatalf("timeout while waiting for package-manager lock took too long: %s", elapsed)
	}
	if !strings.Contains(result.Command, "choco.exe upgrade example-package") {
		t.Fatalf("unexpected command text: %q", result.Command)
	}
}
