package updater

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func diagnosticLogFilesWithPrefix(t *testing.T, directory, prefix string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) && strings.HasSuffix(entry.Name(), diagnosticLogFileSuffix) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func TestSessionLogFileSinkRotatesWithinRetention(t *testing.T) {
	directory := t.TempDir()
	startedAt := time.Date(2026, 11, 21, 10, 36, 12, 0, time.UTC)
	for i := 0; i < 3; i++ {
		older := filepath.Join(directory, diagnosticLogFileName(sessionLogFilePrefix, startedAt.Add(-time.Duration(i+1)*time.Hour), 100+i, 1))
		if err := os.WriteFile(older, []byte("old\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Each 60-byte message line is ~100 bytes, so a 250-byte cap holds two lines
	// per file and twelve lines force several rotations.
	sink, err := openSessionLogFileSink(directory, startedAt, 4242, 250, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 12; i++ {
		sink.write(LogEntry{ID: int64(i), Timestamp: "2026-11-21T10:36:13Z", Stream: "stdout", Message: strings.Repeat("x", 60)})
	}
	sink.close()

	names := diagnosticLogFilesWithPrefix(t, directory, sessionLogFilePrefix)
	if len(names) != 3 {
		t.Fatalf("expected retention to keep 3 session files, got %v", names)
	}
	for _, name := range names {
		if !strings.Contains(name, "-4242-") {
			t.Fatalf("session file %s from an older process survived retention; files=%v", name, names)
		}
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > 250 {
			t.Fatalf("session file %s exceeded the size cap with %d bytes", name, len(data))
		}
	}
	last, err := os.ReadFile(filepath.Join(directory, names[len(names)-1]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(last), " #12 ") {
		t.Fatalf("newest session file does not hold the newest entry: %q", last)
	}
}

func TestSessionLogFileSinkRedactsLikeLogExport(t *testing.T) {
	directory := t.TempDir()
	sink, err := openSessionLogFileSink(directory, time.Now(), os.Getpid(), sessionLogFileMaxBytes, 2)
	if err != nil {
		t.Fatal(err)
	}
	sink.write(LogEntry{
		ID:        1,
		Timestamp: "2026-11-21T10:36:12Z",
		Stream:    "app",
		Message:   `Opened C:\Users\Alice\AppData\Local\Temp\x.msi as S-1-5-21-1111-2222-3333-1001 with token=abcdef123456`,
	})
	sink.close()
	names := diagnosticLogFilesWithPrefix(t, directory, sessionLogFilePrefix)
	if len(names) != 1 {
		t.Fatalf("expected one session file, got %v", names)
	}
	data, err := os.ReadFile(filepath.Join(directory, names[0]))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, secret := range []string{"Alice", "abcdef123456", "S-1-5-21-1111"} {
		if strings.Contains(text, secret) {
			t.Fatalf("session log file leaked %q: %q", secret, text)
		}
	}
	if !strings.Contains(text, `%USERPROFILE%\AppData`) || !strings.Contains(text, "token=[redacted]") {
		t.Fatalf("session log file is missing redaction markers: %q", text)
	}
}

func TestSessionLogsMirrorToActiveFileSink(t *testing.T) {
	directory := t.TempDir()
	sink, err := openSessionLogFileSink(directory, time.Now(), os.Getpid(), sessionLogFileMaxBytes, 2)
	if err != nil {
		t.Fatal(err)
	}
	activeSessionLogFileSink.Store(sink)
	t.Cleanup(func() {
		activeSessionLogFileSink.CompareAndSwap(sink, nil)
		sink.close()
	})
	appLog("Mirror check %s.", "elevated batch")
	(&LogBuffer{}).Append("app", "private buffer entry must not be mirrored")
	sink.close()

	names := diagnosticLogFilesWithPrefix(t, directory, sessionLogFilePrefix)
	data, err := os.ReadFile(filepath.Join(directory, names[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "APP Mirror check elevated batch.") {
		t.Fatalf("session log entry was not mirrored to disk: %q", data)
	}
	if strings.Contains(string(data), "private buffer entry") {
		t.Fatalf("a non-session LogBuffer was mirrored to disk: %q", data)
	}
}

func TestSessionLogFileLineIncludesTimestampIDAndMetadata(t *testing.T) {
	line := formatSessionLogFileLine(LogEntry{
		ID:         9,
		Timestamp:  "2026-11-21T10:36:19Z",
		Stream:     "app",
		Message:    "first\nsecond",
		JobID:      "job-1",
		PackageKey: "winget:Cryptomator.Cryptomator",
	})
	want := "2026-11-21T10:36:19Z #9 APP [job=job-1 package=winget:Cryptomator.Cryptomator] first\n\tsecond\n"
	if line != want {
		t.Fatalf("line = %q, want %q", line, want)
	}
}

func TestPruneDiagnosticLogFilesRemovesEmptyCrashFilesAndKeepsNewest(t *testing.T) {
	directory := t.TempDir()
	base := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	var nonEmpty []string
	for i := 0; i < 4; i++ {
		name := diagnosticLogFileName(crashLogFilePrefix, base.Add(time.Duration(i)*time.Hour), 10+i, 0)
		if err := os.WriteFile(filepath.Join(directory, name), []byte("panic: boom\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		nonEmpty = append(nonEmpty, name)
	}
	empty := diagnosticLogFileName(crashLogFilePrefix, base.Add(10*time.Hour), 99, 0)
	if err := os.WriteFile(filepath.Join(directory, empty), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(directory, "notes.txt")
	if err := os.WriteFile(unrelated, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	pruneDiagnosticLogFiles(directory, crashLogFilePrefix, 2, true)

	got := diagnosticLogFilesWithPrefix(t, directory, crashLogFilePrefix)
	want := nonEmpty[2:]
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("crash files after prune = %v, want %v", got, want)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("prune removed an unrelated file: %v", err)
	}
}

func TestCrashOutputFileIsRemovedAfterCleanExit(t *testing.T) {
	directory := t.TempDir()
	stop := startCrashOutputFile(directory, time.Now(), os.Getpid())
	if names := diagnosticLogFilesWithPrefix(t, directory, crashLogFilePrefix); len(names) != 1 {
		t.Fatalf("expected one open crash file, got %v", names)
	}
	stop()
	stop()
	if names := diagnosticLogFilesWithPrefix(t, directory, crashLogFilePrefix); len(names) != 0 {
		t.Fatalf("empty crash file was not removed after clean exit: %v", names)
	}
}
