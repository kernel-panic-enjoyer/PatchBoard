package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Diagnostic log files survive the process. The in-memory session log is lost
// when PatchBoard exits unexpectedly, and Go runtime crashes do not produce
// Windows Error Reporting events, so without these files an abrupt exit leaves
// no evidence of what happened.
const (
	diagnosticLogDirectoryName = "logs"
	diagnosticLogFileSuffix    = ".log"
	diagnosticLogTimeLayout    = "20060102T150405Z"
	sessionLogFilePrefix       = "session-"
	crashLogFilePrefix         = "crash-"
	sessionLogFileMaxBytes     = 4 * 1024 * 1024
	sessionLogFilesRetained    = 8
	crashLogFilesRetained      = 10
)

var activeSessionLogFileSink atomic.Pointer[sessionLogFileSink]

// recoveredPanicReportSequence keeps report names unique when several panics
// are recovered within the same second.
var recoveredPanicReportSequence atomic.Int64

func diagnosticLogDir() (string, error) {
	base, err := stateDir()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(base, diagnosticLogDirectoryName)
	if err := ensureUserPrivateDir(directory); err != nil {
		return "", err
	}
	return directory, nil
}

// startDiagnosticLogFiles mirrors the session log to a rotating file and routes
// fatal Go runtime output (unrecovered panics, fatal errors) to a crash file.
// The returned function stops both and removes the crash file when it stayed
// empty, so only processes that actually crashed leave one behind.
func startDiagnosticLogFiles() func() {
	directory, err := diagnosticLogDir()
	if err != nil {
		appLog("Diagnostic log files are disabled: %s.", err)
		return func() {}
	}
	startedAt := time.Now()
	stopCrashOutput := startCrashOutputFile(directory, startedAt, os.Getpid())
	sink, err := openSessionLogFileSink(directory, startedAt, os.Getpid(), sessionLogFileMaxBytes, sessionLogFilesRetained)
	if err != nil {
		appLog("Session log file is disabled: %s.", err)
		return stopCrashOutput
	}
	// Entries logged before the sink opened (CLI parsing, state migration) are
	// copied once so the file starts at the beginning of the session.
	for _, entry := range sessionLogs.Snapshot() {
		sink.write(entry)
	}
	activeSessionLogFileSink.Store(sink)
	appLog("Diagnostic logs are written to %s.", directory)
	return func() {
		activeSessionLogFileSink.CompareAndSwap(sink, nil)
		sink.close()
		stopCrashOutput()
	}
}

// startWorkerCrashOutputFile routes fatal runtime output of a helper process
// (such as the elevated worker) to its own crash file.
func startWorkerCrashOutputFile() func() {
	directory, err := diagnosticLogDir()
	if err != nil {
		return func() {}
	}
	return startCrashOutputFile(directory, time.Now(), os.Getpid())
}

// writeRecoveredPanicReport keeps the stack of a recovered panic in a crash
// file. It is for processes without a session log file, where appLog output
// would otherwise be lost.
func writeRecoveredPanicReport(source string, panicValue any, stack []byte) {
	directory, err := diagnosticLogDir()
	if err != nil {
		return
	}
	now := time.Now()
	path := filepath.Join(directory, diagnosticLogFileName(crashLogFilePrefix, now, os.Getpid(), int(recoveredPanicReportSequence.Add(1))))
	file, err := createDiagnosticLogFile(path)
	if err != nil {
		return
	}
	defer file.Close()
	report := fmt.Sprintf("Recovered panic in %s at %s: %v\n\n%s", source, now.UTC().Format(time.RFC3339), panicValue, stack)
	_, _ = file.WriteString(sanitizeLogText(report))
}

func writeSessionLogFileEntry(entry LogEntry) {
	if sink := activeSessionLogFileSink.Load(); sink != nil {
		sink.write(entry)
	}
}

func diagnosticLogFileName(prefix string, startedAt time.Time, pid int, sequence int) string {
	name := prefix + startedAt.UTC().Format(diagnosticLogTimeLayout) + "-" + fmt.Sprint(pid)
	if sequence > 0 {
		name += fmt.Sprintf("-%03d", sequence)
	}
	return name + diagnosticLogFileSuffix
}

func createDiagnosticLogFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, userPrivateFileMode)
	if err != nil {
		return nil, err
	}
	if err := protectUserPrivateFile(path); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return file, nil
}

// pruneDiagnosticLogFiles keeps at most keep files with the given prefix.
// Names embed a UTC start timestamp, so lexical order is chronological. When
// removeEmpty is set, zero-byte files are deleted first; that is how crash
// files left by processes that were killed (rather than crashing) are cleared.
// Files still open by a running instance cannot be deleted on Windows and are
// skipped silently.
func pruneDiagnosticLogFiles(directory, prefix string, keep int, removeEmpty bool) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, diagnosticLogFileSuffix) {
			continue
		}
		if removeEmpty {
			if info, err := entry.Info(); err == nil && info.Size() == 0 {
				_ = os.Remove(filepath.Join(directory, name))
				continue
			}
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for len(names) > keep && len(names) > 0 {
		_ = os.Remove(filepath.Join(directory, names[0]))
		names = names[1:]
	}
}

func startCrashOutputFile(directory string, startedAt time.Time, pid int) func() {
	pruneDiagnosticLogFiles(directory, crashLogFilePrefix, crashLogFilesRetained-1, true)
	path := filepath.Join(directory, diagnosticLogFileName(crashLogFilePrefix, startedAt, pid, 0))
	file, err := createDiagnosticLogFile(path)
	if err != nil {
		appLog("Crash log file is disabled: %s.", err)
		return func() {}
	}
	if err := debug.SetCrashOutput(file, debug.CrashOptions{}); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		appLog("Crash log file is disabled: %s.", err)
		return func() {}
	}
	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
			info, statErr := file.Stat()
			_ = file.Close()
			if statErr == nil && info.Size() == 0 {
				_ = os.Remove(path)
			}
		})
	}
}

// sessionLogFileSink appends formatted session log entries to a size-capped
// file. Writes go straight to the OS without user-space buffering, so lines
// written before an abrupt exit are not lost with the process.
type sessionLogFileSink struct {
	mu        sync.Mutex
	directory string
	startedAt time.Time
	pid       int
	maxBytes  int64
	keep      int
	sequence  int
	file      *os.File
	written   int64
}

func openSessionLogFileSink(directory string, startedAt time.Time, pid int, maxBytes int64, keep int) (*sessionLogFileSink, error) {
	if keep < 1 {
		keep = 1
	}
	sink := &sessionLogFileSink{directory: directory, startedAt: startedAt, pid: pid, maxBytes: maxBytes, keep: keep}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if err := sink.rotateLocked(); err != nil {
		return nil, err
	}
	return sink, nil
}

func (sink *sessionLogFileSink) rotateLocked() error {
	if sink.file != nil {
		_ = sink.file.Close()
		sink.file = nil
	}
	pruneDiagnosticLogFiles(sink.directory, sessionLogFilePrefix, sink.keep-1, false)
	sink.sequence++
	path := filepath.Join(sink.directory, diagnosticLogFileName(sessionLogFilePrefix, sink.startedAt, sink.pid, sink.sequence))
	file, err := createDiagnosticLogFile(path)
	if err != nil {
		return err
	}
	sink.file = file
	sink.written = 0
	return nil
}

func (sink *sessionLogFileSink) write(entry LogEntry) {
	line := formatSessionLogFileLine(entry)
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.file == nil {
		return
	}
	if sink.written > 0 && sink.written+int64(len(line)) > sink.maxBytes {
		if err := sink.rotateLocked(); err != nil {
			return
		}
	}
	written, err := sink.file.WriteString(line)
	sink.written += int64(written)
	if err != nil {
		// A failing disk must not break logging or the app; stop mirroring.
		_ = sink.file.Close()
		sink.file = nil
	}
}

func (sink *sessionLogFileSink) close() {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.file != nil {
		_ = sink.file.Close()
		sink.file = nil
	}
}

// formatSessionLogFileLine uses the same redaction as Export Logs, with a full
// UTC timestamp and the entry ID so lines can be matched to exported archives.
func formatSessionLogFileLine(entry LogEntry) string {
	message := strings.ReplaceAll(sanitizeLogText(entry.Message), "\n", "\n\t")
	return fmt.Sprintf("%s #%d %s%s %s\n", entry.Timestamp, entry.ID, strings.ToUpper(entry.Stream), formatLogEntryMetadata(entry), message)
}
