package updater

import "testing"

func setProcessExecutionModeForTest(t *testing.T, mode processExecutionMode) {
	t.Helper()
	previousMode := currentProcessExecutionMode()
	setProcessExecutionMode(mode)
	t.Cleanup(func() {
		setProcessExecutionMode(previousMode)
	})
}

func TestConfigureProcessExecutionModeFromCLI(t *testing.T) {
	cases := []struct {
		name string
		mode cliMode
		want processExecutionMode
	}{
		{"server", cliModeServer, processModeInteractive},
		{"auto update", cliModeAutoUpdate, processModeScheduledAutoUpdate},
		{"elevated worker", cliModeElevatedWorker, processModeElevatedWorker},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setProcessExecutionModeForTest(t, processModeInteractive)
			configureProcessExecutionMode(tc.mode)
			if got := currentProcessExecutionMode(); got != tc.want {
				t.Fatalf("execution mode = %d, want %d", got, tc.want)
			}
		})
	}
}
