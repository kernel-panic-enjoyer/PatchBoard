package updater

import "sync/atomic"

type processExecutionMode int32

const (
	processModeInteractive processExecutionMode = iota
	processModeScheduledAutoUpdate
	processModeElevatedWorker
)

var activeProcessExecutionMode atomic.Int32

func configureProcessExecutionMode(mode cliMode) {
	switch mode {
	case cliModeAutoUpdate:
		setProcessExecutionMode(processModeScheduledAutoUpdate)
	case cliModeElevatedWorker:
		setProcessExecutionMode(processModeElevatedWorker)
	default:
		setProcessExecutionMode(processModeInteractive)
	}
}

func setProcessExecutionMode(mode processExecutionMode) {
	activeProcessExecutionMode.Store(int32(mode))
}

func currentProcessExecutionMode() processExecutionMode {
	return processExecutionMode(activeProcessExecutionMode.Load())
}

func hardenedProcessExecutionMode() bool {
	switch currentProcessExecutionMode() {
	case processModeScheduledAutoUpdate, processModeElevatedWorker:
		return true
	default:
		return false
	}
}

func userEnvironmentOverridesAllowed() bool {
	return !hardenedProcessExecutionMode()
}
