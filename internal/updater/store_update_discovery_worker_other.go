//go:build !windows

package updater

import (
	"context"
	"fmt"
	"os"
)

const storeUpdateDiscoveryCommand = "WinRT AppInstallManager update discovery"

type storeUpdateDiscoveryWorkerProvider struct{}

type winrtStoreUpdateDiscoveryProvider struct{}

func (provider storeUpdateDiscoveryWorkerProvider) Discover(ctx context.Context, scan StoreScanGeneration, families []StorePackagedAppFamily) (storeUpdateDiscoveryWorkerResponse, CommandResult) {
	err := fmt.Errorf("WinRT Store update discovery is unsupported on this platform")
	return incompleteStoreUpdateDiscoveryResponse(scan, err), CommandResult{Command: storeUpdateDiscoveryCommand, Code: 1, Stderr: err.Error()}
}

func runStoreUpdateDiscoveryWorkerFromArgs() int {
	fmt.Fprintln(os.Stderr, "store update discovery worker is unsupported on this platform")
	return 2
}

func incompleteStoreUpdateDiscoveryResponse(scan StoreScanGeneration, err error) storeUpdateDiscoveryWorkerResponse {
	return storeUpdateDiscoveryWorkerResponse{
		ProtocolVersion: storeUpdateDiscoveryWorkerProtocolVersion,
		ScanID:          scan.ScanID,
		UserSID:         scan.UserSID,
		Partial:         true,
		Errors:          []string{err.Error()},
	}
}
