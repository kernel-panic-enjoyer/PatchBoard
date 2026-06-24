package updater

import (
	"context"
	"os/exec"
)

func waitForStartedCommand(ctx context.Context, cmd *exec.Cmd, owner *commandProcessOwner) error {
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		terminateStartedCommand(cmd, owner)
		return <-done
	}
}

func terminateStartedCommand(cmd *exec.Cmd, owner *commandProcessOwner) {
	if owner != nil {
		owner.Terminate()
		return
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
