package updater

import (
	"bytes"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	selfUpdateApplyProtocolVersion = 1
	maxSelfUpdateApplyMessageBytes = 64 * 1024
	maxSelfUpdateManifestBytes     = 4 * 1024
	selfUpdateApplyStartupTimeout  = 15 * time.Second
	selfUpdateApplyMaxFutureWindow = 5 * time.Minute
	selfUpdateApplyReadyState      = "ready"
)

type selfUpdateApplyMessage struct {
	ProtocolVersion int                    `json:"protocol_version"`
	RequestID       string                 `json:"request_id"`
	Capability      string                 `json:"capability"`
	Request         selfUpdateApplyRequest `json:"request"`
}

type selfUpdateApplyResponse struct {
	ProtocolVersion int    `json:"protocol_version"`
	RequestID       string `json:"request_id"`
	State           string `json:"state,omitempty"`
	Error           string `json:"error,omitempty"`
}

type selfUpdateApplyManifest struct {
	ProtocolVersion int    `json:"protocol_version"`
	PipeName        string `json:"pipe_name"`
	Capability      string `json:"capability"`
	ExpiresUnixMS   int64  `json:"expires_unix_ms"`
}

func createSelfUpdateApplyManifest(pipeName, capability string, expiresAt time.Time) (string, error) {
	manifestDirectory, err := selfUpdateDownloadDir()
	if err != nil {
		return "", err
	}
	manifestName, err := randomToken()
	if err != nil {
		return "", err
	}
	manifestPath := filepath.Join(manifestDirectory, "apply-"+manifestName+".json")
	payload, err := json.Marshal(selfUpdateApplyManifest{
		ProtocolVersion: selfUpdateApplyProtocolVersion,
		PipeName:        pipeName,
		Capability:      capability,
		ExpiresUnixMS:   expiresAt.UnixMilli(),
	})
	if err != nil {
		return "", err
	}
	if len(payload) > maxSelfUpdateManifestBytes {
		return "", errors.New("self-update apply manifest is too large")
	}
	if err := writeUserPrivateFile(manifestPath, payload); err != nil {
		return "", err
	}
	return manifestPath, nil
}

func consumeSelfUpdateApplyManifest(manifestPath string, now time.Time) (selfUpdateApplyManifest, error) {
	var manifest selfUpdateApplyManifest
	manifestPath, err := filepath.Abs(strings.TrimSpace(manifestPath))
	if err != nil {
		return manifest, err
	}
	manifestDirectory, err := selfUpdateDownloadDir()
	if err != nil {
		return manifest, err
	}
	manifestDirectory, err = filepath.Abs(manifestDirectory)
	if err != nil {
		return manifest, err
	}
	if !pathWithinDirectory(manifestPath, manifestDirectory) {
		return manifest, errors.New("self-update apply manifest is outside PatchBoard's staging directory")
	}
	defer os.Remove(manifestPath)
	manifestFile, err := os.Open(manifestPath)
	if err != nil {
		return manifest, err
	}
	payload, readErr := readBounded(manifestFile, maxSelfUpdateManifestBytes, "self-update apply manifest")
	closeErr := manifestFile.Close()
	if readErr != nil {
		return manifest, readErr
	}
	if closeErr != nil {
		return manifest, closeErr
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return manifest, errors.New("self-update apply manifest contains trailing JSON data")
		}
		return manifest, err
	}
	if manifest.ProtocolVersion != selfUpdateApplyProtocolVersion {
		return manifest, fmt.Errorf("unsupported self-update apply manifest version %d", manifest.ProtocolVersion)
	}
	if strings.TrimSpace(manifest.PipeName) == "" || strings.TrimSpace(manifest.Capability) == "" {
		return manifest, errors.New("self-update apply manifest is incomplete")
	}
	if !time.UnixMilli(manifest.ExpiresUnixMS).After(now) {
		return manifest, errors.New("self-update apply manifest has expired")
	}
	if time.UnixMilli(manifest.ExpiresUnixMS).After(now.Add(selfUpdateApplyMaxFutureWindow)) {
		return manifest, errors.New("self-update apply manifest expiry is too far in the future")
	}
	return manifest, nil
}

func validateSelfUpdateApplyMessage(message selfUpdateApplyMessage, expectedCapability string, now time.Time) error {
	if message.ProtocolVersion != selfUpdateApplyProtocolVersion {
		return fmt.Errorf("unsupported self-update apply protocol version %d", message.ProtocolVersion)
	}
	if strings.TrimSpace(message.RequestID) == "" {
		return errors.New("self-update apply request ID is required")
	}
	if !secureStringEqual(message.Capability, expectedCapability) {
		return errors.New("self-update apply capability is invalid")
	}
	if err := validateSelfUpdateApplyRequest(message.Request); err != nil {
		return err
	}
	if message.Request.ParentPID <= 0 {
		return errors.New("self-update parent PID is required")
	}
	if strings.TrimSpace(message.Request.ParentUserSID) == "" {
		return errors.New("self-update parent user SID is required")
	}
	if message.Request.DeadlineUnixMS <= 0 {
		return errors.New("self-update apply deadline is required")
	}
	deadline := time.UnixMilli(message.Request.DeadlineUnixMS)
	if !deadline.After(now) {
		return errors.New("self-update apply request has expired")
	}
	if deadline.After(now.Add(selfUpdateApplyMaxFutureWindow)) {
		return errors.New("self-update apply deadline is too far in the future")
	}
	return nil
}

func validateSelfUpdateApplyResponse(response selfUpdateApplyResponse, requestID string) error {
	if response.ProtocolVersion != selfUpdateApplyProtocolVersion {
		return fmt.Errorf("unsupported self-update apply response version %d", response.ProtocolVersion)
	}
	if response.RequestID != requestID {
		return errors.New("self-update apply response request ID mismatch")
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	if response.State != selfUpdateApplyReadyState {
		return fmt.Errorf("unexpected self-update apply response state %q", response.State)
	}
	return nil
}

func secureStringEqual(left, right string) bool {
	leftBytes := []byte(left)
	rightBytes := []byte(right)
	if len(leftBytes) != len(rightBytes) || len(leftBytes) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare(leftBytes, rightBytes) == 1
}

func writeSelfUpdateApplyMessage(destination io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > maxSelfUpdateApplyMessageBytes {
		return fmt.Errorf("self-update apply message exceeds %d bytes", maxSelfUpdateApplyMessageBytes)
	}
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(len(payload)))
	if err := writeSelfUpdateApplyBytes(destination, length[:]); err != nil {
		return err
	}
	return writeSelfUpdateApplyBytes(destination, payload)
}

func writeSelfUpdateApplyBytes(destination io.Writer, payload []byte) error {
	for len(payload) > 0 {
		written, err := destination.Write(payload)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		payload = payload[written:]
	}
	return nil
}

func readSelfUpdateApplyMessage(source io.Reader, destination any) error {
	var length [4]byte
	if _, err := io.ReadFull(source, length[:]); err != nil {
		return err
	}
	payloadSize := binary.LittleEndian.Uint32(length[:])
	if payloadSize == 0 || payloadSize > maxSelfUpdateApplyMessageBytes {
		return fmt.Errorf("self-update apply message size %d is invalid", payloadSize)
	}
	payload := make([]byte, payloadSize)
	if _, err := io.ReadFull(source, payload); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("self-update apply message contains trailing JSON data")
		}
		return err
	}
	return nil
}
