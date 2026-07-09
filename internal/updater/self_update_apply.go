package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type selfUpdateApplyRequest struct {
	SourcePath     string
	TargetPath     string
	ExpectedSHA256 string
	ParentPID      int
	Restart        bool
	Elevated       bool
}

func validateSelfUpdateApplyRequest(request selfUpdateApplyRequest) error {
	if strings.TrimSpace(request.SourcePath) == "" {
		return errors.New("self-update source path is required")
	}
	if strings.TrimSpace(request.TargetPath) == "" {
		return errors.New("self-update target path is required")
	}
	sourcePath, err := filepath.Abs(request.SourcePath)
	if err != nil {
		return fmt.Errorf("self-update source path is invalid: %w", err)
	}
	targetPath, err := filepath.Abs(request.TargetPath)
	if err != nil {
		return fmt.Errorf("self-update target path is invalid: %w", err)
	}
	if sameSelfUpdatePath(sourcePath, targetPath) {
		return errors.New("self-update source and target paths must differ")
	}
	if err := validateSelfUpdateSourcePath(sourcePath); err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Base(targetPath), releaseAssetExecutable) {
		return fmt.Errorf("self-update target must be %s", releaseAssetExecutable)
	}
	if _, err := os.Stat(sourcePath); err != nil {
		return fmt.Errorf("self-update source is not readable: %w", err)
	}
	if request.ExpectedSHA256 == "" || !sha256LinePattern.MatchString(request.ExpectedSHA256) {
		return errors.New("self-update expected SHA-256 is invalid")
	}
	return nil
}

func validateSelfUpdateSourcePath(sourcePath string) error {
	downloadDir, err := selfUpdateDownloadDir()
	if err != nil {
		return err
	}
	downloadDir, err = filepath.Abs(downloadDir)
	if err != nil {
		return err
	}
	if !pathWithinDirectory(sourcePath, downloadDir) {
		return errors.New("self-update source must be inside PatchBoard's self-update staging directory")
	}
	return nil
}

func pathWithinDirectory(path, directory string) bool {
	path = filepath.Clean(path)
	directory = filepath.Clean(directory)
	if sameSelfUpdatePath(path, directory) {
		return true
	}
	relative, err := filepath.Rel(directory, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func sameSelfUpdatePath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func replaceExecutableForSelfUpdate(request selfUpdateApplyRequest) error {
	if err := validateSelfUpdateApplyRequest(request); err != nil {
		return err
	}
	actual, err := fileSHA256(request.SourcePath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, request.ExpectedSHA256) {
		return fmt.Errorf("self-update checksum mismatch: got %s want %s", actual, request.ExpectedSHA256)
	}
	targetDir := filepath.Dir(request.TargetPath)
	temp, err := os.CreateTemp(targetDir, ".PatchBoard-replace-*.exe")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if err := copyFileContents(temp, request.SourcePath); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, 0o755); err != nil {
		return err
	}
	if err := replaceFileKeepingBackup(tempPath, request.TargetPath, request.TargetPath+".bak"); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func copyFileContents(writer io.Writer, sourcePath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = io.Copy(writer, source)
	return err
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
