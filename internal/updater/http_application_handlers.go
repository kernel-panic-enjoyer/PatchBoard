package updater

import (
	"net/http"
)

var (
	applicationInstallRunner          = installApplicationContext
	applicationRestartInstalledRunner = restartInstalledApplicationContext
)

func (app *App) handleApplicationInstallAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	result := applicationInstallRunner(r.Context())
	app.refreshStatus(true)
	writeJSON(w, http.StatusOK, commandResponse(result))
}

func (app *App) handleApplicationRestartInstalledAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	result := applicationRestartInstalledRunner(r.Context())
	if !result.OK {
		writeJSON(w, http.StatusOK, commandResponse(result))
		return
	}
	writeJSON(w, http.StatusOK, commandResponse(result))
	app.requestShutdownSoon("Restart from installed copy")
}
