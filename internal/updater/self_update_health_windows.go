//go:build windows

package updater

import (
	"bytes"
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
	selfUpdateStartupHealthTimeout    = 30 * time.Second
	maxSelfUpdateHealthRequestBytes   = 4 * 1024
	maxPendingHealthRequests          = 8
	selfUpdateHealthRequestFileSuffix = ".request.json"
	selfUpdateHealthAckFileSuffix     = ".ack.json"
)

type selfUpdateStartupHealthRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	RequestID       string `json:"request_id"`
	TargetPath      string `json:"target_path"`
	ExpectedSHA256  string `json:"expected_sha256"`
	UserSID         string `json:"user_sid"`
	SessionID       uint32 `json:"session_id"`
	DeadlineUnixMS  int64  `json:"deadline_unix_ms"`
}

type selfUpdateStartupHealthAck struct {
	ProtocolVersion  int    `json:"protocol_version"`
	RequestID        string `json:"request_id"`
	ExecutableSHA256 string `json:"executable_sha256"`
	AcknowledgedAt   string `json:"acknowledged_at"`
}

func createSelfUpdateStartupHealthRequest(applyRequest selfUpdateApplyRequest) (selfUpdateStartupHealthRequest, string, string, error) {
	var healthRequest selfUpdateStartupHealthRequest
	directory, err := selfUpdateDownloadDir()
	if err != nil {
		return healthRequest, "", "", err
	}
	requestID, err := randomToken()
	if err != nil {
		return healthRequest, "", "", err
	}
	targetPath, err := filepath.Abs(applyRequest.TargetPath)
	if err != nil {
		return healthRequest, "", "", err
	}
	healthRequest = selfUpdateStartupHealthRequest{
		ProtocolVersion: selfUpdateApplyProtocolVersion,
		RequestID:       requestID,
		TargetPath:      targetPath,
		ExpectedSHA256:  strings.ToLower(strings.TrimSpace(applyRequest.ExpectedSHA256)),
		UserSID:         applyRequest.ParentUserSID,
		SessionID:       applyRequest.ParentSessionID,
		DeadlineUnixMS:  time.Now().Add(selfUpdateStartupHealthTimeout).UnixMilli(),
	}
	requestPath := filepath.Join(directory, "health-"+requestID+selfUpdateHealthRequestFileSuffix)
	ackPath := filepath.Join(directory, "health-"+requestID+selfUpdateHealthAckFileSuffix)
	payload, err := json.Marshal(healthRequest)
	if err != nil {
		return healthRequest, "", "", err
	}
	if len(payload) > maxSelfUpdateHealthRequestBytes {
		return healthRequest, "", "", errors.New("self-update startup health request is too large")
	}
	if err := writeUserPrivateFile(requestPath, payload); err != nil {
		return healthRequest, "", "", err
	}
	return healthRequest, requestPath, ackPath, nil
}

func acknowledgePendingSelfUpdateHealth() error {
	directory, err := selfUpdateDownloadDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(entries) > maxPendingHealthRequests {
		return errors.New("too many pending self-update startup health requests")
	}
	currentExecutable, err := os.Executable()
	if err != nil {
		return err
	}
	currentExecutable, err = filepath.Abs(currentExecutable)
	if err != nil {
		return err
	}
	currentSHA256, err := fileSHA256(currentExecutable)
	if err != nil {
		return err
	}
	currentUserSID, err := currentUserSID()
	if err != nil {
		return err
	}
	currentSessionID, err := currentSessionID()
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "health-") || !strings.HasSuffix(entry.Name(), selfUpdateHealthRequestFileSuffix) {
			continue
		}
		requestPath := filepath.Join(directory, entry.Name())
		healthRequest, err := readSelfUpdateStartupHealthRequest(requestPath)
		if err != nil {
			_ = os.Remove(requestPath)
			continue
		}
		if !time.UnixMilli(healthRequest.DeadlineUnixMS).After(time.Now()) {
			_ = os.Remove(requestPath)
			continue
		}
		if !sameSelfUpdatePath(healthRequest.TargetPath, currentExecutable) ||
			!strings.EqualFold(healthRequest.ExpectedSHA256, currentSHA256) ||
			!strings.EqualFold(healthRequest.UserSID, currentUserSID) ||
			healthRequest.SessionID != currentSessionID {
			continue
		}
		ack := selfUpdateStartupHealthAck{
			ProtocolVersion:  selfUpdateApplyProtocolVersion,
			RequestID:        healthRequest.RequestID,
			ExecutableSHA256: currentSHA256,
			AcknowledgedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		}
		ackPayload, err := json.Marshal(ack)
		if err != nil {
			return err
		}
		ackPath := strings.TrimSuffix(requestPath, selfUpdateHealthRequestFileSuffix) + selfUpdateHealthAckFileSuffix
		if err := writeUserPrivateFile(ackPath, ackPayload); err != nil {
			return err
		}
		if err := os.Remove(requestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func readSelfUpdateStartupHealthRequest(path string) (selfUpdateStartupHealthRequest, error) {
	var healthRequest selfUpdateStartupHealthRequest
	if err := decodeSelfUpdateHealthJSON(path, &healthRequest); err != nil {
		return healthRequest, err
	}
	if healthRequest.ProtocolVersion != selfUpdateApplyProtocolVersion || strings.TrimSpace(healthRequest.RequestID) == "" || strings.TrimSpace(healthRequest.TargetPath) == "" || !sha256LinePattern.MatchString(healthRequest.ExpectedSHA256) || strings.TrimSpace(healthRequest.UserSID) == "" || healthRequest.DeadlineUnixMS <= 0 {
		return healthRequest, errors.New("self-update startup health request is invalid")
	}
	return healthRequest, nil
}

func readSelfUpdateStartupHealthAck(path string) (selfUpdateStartupHealthAck, error) {
	var ack selfUpdateStartupHealthAck
	if err := decodeSelfUpdateHealthJSON(path, &ack); err != nil {
		return ack, err
	}
	if ack.ProtocolVersion != selfUpdateApplyProtocolVersion || strings.TrimSpace(ack.RequestID) == "" || !sha256LinePattern.MatchString(ack.ExecutableSHA256) || strings.TrimSpace(ack.AcknowledgedAt) == "" {
		return ack, errors.New("self-update startup health acknowledgement is invalid")
	}
	return ack, nil
}

func decodeSelfUpdateHealthJSON(path string, destination any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	payload, readErr := readBounded(file, maxSelfUpdateHealthRequestBytes, "self-update startup health payload")
	closeErr := file.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("self-update startup health payload contains trailing JSON data")
		}
		return err
	}
	return nil
}

func validateSelfUpdateStartupHealthAck(healthRequest selfUpdateStartupHealthRequest, ack selfUpdateStartupHealthAck) error {
	if ack.RequestID != healthRequest.RequestID {
		return errors.New("self-update startup health acknowledgement request ID mismatch")
	}
	if !strings.EqualFold(ack.ExecutableSHA256, healthRequest.ExpectedSHA256) {
		return fmt.Errorf("self-update startup health acknowledgement hash %s does not match expected hash", ack.ExecutableSHA256)
	}
	return nil
}
