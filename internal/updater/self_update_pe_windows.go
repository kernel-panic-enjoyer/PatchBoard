//go:build windows

package updater

import (
	"debug/pe"
	"errors"
	"fmt"
	"runtime"
	"strings"
)

func validateDownloadedSelfUpdateExecutable(path string, metadata selfUpdateReleaseMetadata) error {
	if metadata.GOOS != runtime.GOOS || metadata.GOARCH != runtime.GOARCH {
		return fmt.Errorf("self-update metadata platform %s/%s does not match %s/%s", metadata.GOOS, metadata.GOARCH, runtime.GOOS, runtime.GOARCH)
	}
	if !strings.HasPrefix(strings.TrimSpace(metadata.GoVersion), "go") {
		return errors.New("self-update metadata Go version is invalid")
	}
	if !metadata.Stripped || metadata.Unstripped {
		return errors.New("self-update release metadata does not describe a stripped release build")
	}
	file, err := pe.Open(path)
	if err != nil {
		return fmt.Errorf("self-update executable is not a valid PE file: %w", err)
	}
	defer file.Close()
	expectedMachine, err := expectedSelfUpdatePEMachine(runtime.GOARCH)
	if err != nil {
		return err
	}
	if file.FileHeader.Machine != expectedMachine {
		return fmt.Errorf("self-update PE machine %#x does not match expected %#x", file.FileHeader.Machine, expectedMachine)
	}
	return nil
}

func expectedSelfUpdatePEMachine(goarch string) (uint16, error) {
	switch goarch {
	case "amd64":
		return pe.IMAGE_FILE_MACHINE_AMD64, nil
	case "arm64":
		return pe.IMAGE_FILE_MACHINE_ARM64, nil
	case "386":
		return pe.IMAGE_FILE_MACHINE_I386, nil
	default:
		return 0, fmt.Errorf("unsupported self-update architecture %q", goarch)
	}
}
