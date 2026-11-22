package updater

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestUpdateBatchDeadlineLetsEveryPackageUseItsFullTimeout(t *testing.T) {
	for _, count := range []int{0, 1, 5, 40} {
		deadline := updateBatchDeadline(count)
		packages := max(count, 1)
		if deadline < time.Duration(packages)*packageActionTimeout+updateBatchOverhead {
			t.Fatalf("deadline for %d package(s) is %s, shorter than their action timeouts", count, deadline)
		}
	}
}

func TestUpdateJobStoppedByDeadlineIsTimedOutNotCancelled(t *testing.T) {
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	var status OperationJobStatus
	markUpdateJobStopped(&status, context.Background(), expired, updateBatchDeadline(2))
	if status.State != jobStateFailed || status.CancelRequested || !strings.Contains(status.Notice, "timed out") {
		t.Fatalf("deadline stop = %#v", status)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	status = OperationJobStatus{}
	markUpdateJobStopped(&status, cancelled, cancelled, updateBatchDeadline(2))
	if status.State != jobStateCancelled || !status.CancelRequested || status.Notice != "Update cancelled." {
		t.Fatalf("user cancel = %#v", status)
	}

	// A declined UAC prompt returns a cancelled result while both contexts
	// are still live: that is a cancel, not a timeout.
	status = OperationJobStatus{}
	markUpdateJobStopped(&status, context.Background(), context.Background(), updateBatchDeadline(2))
	if status.State != jobStateCancelled {
		t.Fatalf("declined UAC = %#v", status)
	}
}

func TestScheduledPackageDeadlineMatchesPackageActionTimeout(t *testing.T) {
	if scheduledPackageActionDeadline != packageActionTimeout {
		t.Fatalf("scheduled package deadline %s != package action timeout %s", scheduledPackageActionDeadline, packageActionTimeout)
	}
}
