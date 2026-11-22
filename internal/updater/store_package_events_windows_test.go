//go:build windows

package updater

import (
	"context"
	"testing"
	"time"
	"unsafe"
)

func TestPackageCatalogEventSourceOpensForCurrentUser(t *testing.T) {
	if isAdmin() {
		t.Skip("PackageCatalog event source intentionally refuses elevated parent context")
	}
	userSID, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	events, cleanup, err := (packageCatalogEventSource{}).Subscribe(ctx, StoreInstalledIdentity{
		UserSID:           userSID,
		PackageFamilyName: "Microsoft.WindowsStore_8wekyb3d8bbwe",
	})
	if err != nil {
		t.Fatalf("PackageCatalog current-user subscription failed: %v", err)
	}
	if cleanup == nil {
		t.Fatal("PackageCatalog subscription did not return cleanup")
	}
	cleanup()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("PackageCatalog subscription channel remained open after cleanup")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PackageCatalog subscription cleanup did not close event channel")
	}
}

func TestPackageCatalogEventSinkDropsEventsAfterClose(t *testing.T) {
	events := make(chan StorePackageChangeEvent, 1)
	sink := newPackageCatalogEventSink(events)
	if !sink.send(StorePackageChangeEvent{PackageFullName: "first"}) {
		t.Fatal("first event was not delivered")
	}
	if sink.send(StorePackageChangeEvent{PackageFullName: "full"}) {
		t.Fatal("send to a full channel must drop, not block")
	}
	sink.close()
	sink.close()
	// A late COM callback after the subscription closed must not panic.
	if sink.send(StorePackageChangeEvent{PackageFullName: "late"}) {
		t.Fatal("event delivered after close")
	}
	if event, ok := <-events; !ok || event.PackageFullName != "first" {
		t.Fatalf("unexpected buffered event %#v ok=%t", event, ok)
	}
	if _, ok := <-events; ok {
		t.Fatal("events channel was not closed")
	}
}

func TestPackageCatalogEventHandlerStaysLiveUntilLastRelease(t *testing.T) {
	sink := newPackageCatalogEventSink(make(chan StorePackageChangeEvent, 1))
	handler := newPackageCatalogEventHandler(StoreInstalledIdentity{}, packageCatalogEventUpdating, sink)
	packageCatalogEventHandlerAddRef(unsafe.Pointer(handler)) // COM keeps a reference
	handler.release()                                         // the subscription drops its own
	if _, live := livePackageCatalogEventHandlers.Load(handler); !live {
		t.Fatal("handler was released while COM still holds a reference")
	}
	packageCatalogEventHandlerRelease(unsafe.Pointer(handler))
	if _, live := livePackageCatalogEventHandlers.Load(handler); live {
		t.Fatal("handler stayed registered after the last reference was released")
	}
}

func TestPackageCatalogEventHandlerInvokeIgnoresNilArgs(t *testing.T) {
	sink := newPackageCatalogEventSink(make(chan StorePackageChangeEvent, 1))
	handler := newPackageCatalogEventHandler(StoreInstalledIdentity{}, packageCatalogEventUpdating, sink)
	defer handler.release()
	if got := packageCatalogEventHandlerInvoke(unsafe.Pointer(handler), nil, nil); got != 0 {
		t.Fatalf("Invoke returned %#x", got)
	}
}
