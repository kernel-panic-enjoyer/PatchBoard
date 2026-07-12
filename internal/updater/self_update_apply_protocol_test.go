package updater

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSelfUpdateApplyProtocolRejectsUnknownFields(t *testing.T) {
	payload := []byte(`{"protocol_version":1,"request_id":"request","capability":"capability","request":{},"unexpected":true}`)
	var framed bytes.Buffer
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(len(payload)))
	framed.Write(length[:])
	framed.Write(payload)
	var message selfUpdateApplyMessage
	if err := readSelfUpdateApplyMessage(&framed, &message); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field rejection, got %v", err)
	}
}

func TestSelfUpdateApplyProtocolRejectsOversizedFrame(t *testing.T) {
	var framed bytes.Buffer
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], maxSelfUpdateApplyMessageBytes+1)
	framed.Write(length[:])
	var message selfUpdateApplyMessage
	if err := readSelfUpdateApplyMessage(&framed, &message); err == nil || !strings.Contains(err.Error(), "message size") {
		t.Fatalf("expected oversized-frame rejection, got %v", err)
	}
}

func TestValidateSelfUpdateApplyMessageRejectsExpiredAndMismatchedCapability(t *testing.T) {
	now := time.Now()
	tempRoot := t.TempDir()
	t.Setenv("UPDATER_TEMP_DIR", tempRoot)
	stagingDir := filepath.Join(tempRoot, "self-update")
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(stagingDir, "PatchBoard-update.exe")
	if err := os.WriteFile(sourcePath, []byte("update"), 0o600); err != nil {
		t.Fatal(err)
	}
	validRequest := selfUpdateApplyRequest{
		SourcePath:      sourcePath,
		TargetPath:      filepath.Join(tempRoot, "portable", releaseAssetExecutable),
		ExpectedSHA256:  strings.Repeat("a", 64),
		ParentPID:       42,
		ParentUserSID:   "S-1-5-21-test-1001",
		ParentSessionID: 7,
		DeadlineUnixMS:  now.Add(time.Minute).UnixMilli(),
	}
	message := selfUpdateApplyMessage{
		ProtocolVersion: selfUpdateApplyProtocolVersion,
		RequestID:       "request",
		Capability:      "capability",
		Request:         validRequest,
	}
	if err := validateSelfUpdateApplyMessage(message, "different", now); err == nil || !strings.Contains(err.Error(), "capability") {
		t.Fatalf("expected capability rejection, got %v", err)
	}
	message.Request.DeadlineUnixMS = now.Add(-time.Second).UnixMilli()
	if err := validateSelfUpdateApplyMessage(message, "capability", now); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected deadline rejection, got %v", err)
	}
}

func TestSelfUpdateApplyProtocolRoundTrip(t *testing.T) {
	want := selfUpdateApplyResponse{
		ProtocolVersion: selfUpdateApplyProtocolVersion,
		RequestID:       "request",
		State:           selfUpdateApplyReadyState,
	}
	var framed bytes.Buffer
	if err := writeSelfUpdateApplyMessage(&framed, want); err != nil {
		t.Fatal(err)
	}
	var got selfUpdateApplyResponse
	if err := readSelfUpdateApplyMessage(&framed, &got); err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	if !bytes.Equal(wantJSON, gotJSON) {
		t.Fatalf("round trip mismatch: got %s want %s", gotJSON, wantJSON)
	}
}

func TestSelfUpdateApplyManifestIsPrivateBoundedAndOneUse(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("UPDATER_TEMP_DIR", tempRoot)
	expiresAt := time.Now().Add(time.Minute)
	manifestPath, err := createSelfUpdateApplyManifest(`\\.\pipe\PatchBoard-test`, "capability", expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := consumeSelfUpdateApplyManifest(manifestPath, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.PipeName != `\\.\pipe\PatchBoard-test` || manifest.Capability != "capability" {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("consumed manifest still exists: %v", err)
	}
	if _, err := consumeSelfUpdateApplyManifest(manifestPath, time.Now()); err == nil {
		t.Fatal("consumed manifest was reusable")
	}
}

func TestSelfUpdateApplyManifestRejectsUnknownFieldsAndIsConsumed(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("UPDATER_TEMP_DIR", tempRoot)
	manifestDirectory := filepath.Join(tempRoot, "self-update")
	if err := ensureUserPrivateDir(manifestDirectory); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(manifestDirectory, "apply-invalid.json")
	payload := fmt.Sprintf(`{"protocol_version":1,"pipe_name":"pipe","capability":"capability","expires_unix_ms":%d,"unknown":true}`, time.Now().Add(time.Minute).UnixMilli())
	if err := writeUserPrivateFile(manifestPath, []byte(payload)); err != nil {
		t.Fatal(err)
	}
	if _, err := consumeSelfUpdateApplyManifest(manifestPath, time.Now()); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field rejection, got %v", err)
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("invalid manifest was not consumed: %v", err)
	}
}
