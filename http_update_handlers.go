package main

import (
	"context"
	"errors"
	"net/http"
)

func parsePackageAction(r *http.Request, command string) (string, string, *CommandResult) {
	var manager string
	var id string
	if requestIsJSON(r) {
		var payload struct {
			Manager   string `json:"manager"`
			PackageID string `json:"package_id"`
		}
		if err := decodeJSONRequest(r, &payload); err != nil {
			result := validationCommandResult(command, err)
			return "", "", &result
		}
		manager = payload.Manager
		id = payload.PackageID
	} else {
		_ = r.ParseForm()
		manager = r.Form.Get("manager")
		id = r.Form.Get("package_id")
	}
	if err := validateManagerAndID(manager, id); err != nil {
		result := validationCommandResult(command, err)
		return "", "", &result
	}
	return manager, id, nil
}

func parsePackageUpdateAction(r *http.Request) (string, string, bool, bool, *CommandResult) {
	var manager string
	var id string
	var allowUnknown bool
	var allowPinned bool
	if requestIsJSON(r) {
		var payload struct {
			Manager             string `json:"manager"`
			PackageID           string `json:"package_id"`
			AllowUnknownVersion bool   `json:"allow_unknown_version"`
			AllowPinned         bool   `json:"allow_pinned"`
		}
		if err := decodeJSONRequest(r, &payload); err != nil {
			result := validationCommandResult("update", err)
			return "", "", false, false, &result
		}
		manager = payload.Manager
		id = payload.PackageID
		allowUnknown = payload.AllowUnknownVersion
		allowPinned = payload.AllowPinned
	} else {
		_ = r.ParseForm()
		manager = r.Form.Get("manager")
		id = r.Form.Get("package_id")
		allowUnknown, _ = formBool(r, "allow_unknown_version")
		allowPinned, _ = formBool(r, "allow_pinned")
	}
	if err := validateManagerAndID(manager, id); err != nil {
		result := validationCommandResult("update", err)
		return "", "", false, false, &result
	}
	return manager, id, allowUnknown, allowPinned, nil
}

func parseManagerRequest(r *http.Request) (string, *CommandResult) {
	if requestIsJSON(r) {
		var payload struct {
			Manager string `json:"manager"`
		}
		if err := decodeJSONRequest(r, &payload); err != nil {
			result := validationCommandResult("manager install", err)
			return "", &result
		}
		return payload.Manager, nil
	}
	_ = r.ParseForm()
	return r.Form.Get("manager"), nil
}

func parseUpdateAllPackageKeys(r *http.Request) ([]string, *UpdateResult) {
	if requestIsJSON(r) {
		var payload struct {
			PackageKey  oneOrManyStrings `json:"package_key"`
			PackageKeys oneOrManyStrings `json:"package_keys"`
		}
		if err := decodeJSONRequest(r, &payload); err != nil {
			result := UpdateResult{Result: validationCommandResult("update-all", err)}
			return nil, &result
		}
		return combineStringLists(payload.PackageKey, payload.PackageKeys), nil
	}
	_ = r.ParseForm()
	return r.Form["package_key"], nil
}

func (app *App) handleInstallAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	manager, id, invalid := parsePackageAction(r, "install")
	if invalid != nil {
		writeJSON(w, http.StatusBadRequest, commandResponse(*invalid))
		return
	}
	result := installPackage(manager, id)
	app.refreshInventory(true)
	writeJSON(w, http.StatusOK, refreshedCommandResponse(result))
}

func (app *App) handleManagerInstallAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	manager, invalid := parseManagerRequest(r)
	if invalid != nil {
		writeJSON(w, http.StatusBadRequest, commandResponse(*invalid))
		return
	}
	if !isManagedPackageManager(manager) {
		result := validationCommandResult("manager install", managerValidationError())
		writeJSON(w, http.StatusBadRequest, commandResponse(result))
		return
	}
	result := installManager(manager)
	app.refreshStatus(true)
	writeJSON(w, http.StatusOK, commandResponse(result))
}

func (app *App) handleUpdateAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	manager, id, allowUnknownVersion, allowPinned, invalid := parsePackageUpdateAction(r)
	if invalid != nil {
		writeJSON(w, http.StatusBadRequest, commandResponse(*invalid))
		return
	}
	pkg := app.packageForUpdate(manager, id)
	pkg.AllowUnknownVersionUpdate = allowUnknownVersion
	pkg.AllowPinnedUpdate = allowPinned
	result := app.updatePackageWithInventoryRetry(context.Background(), pkg)
	app.refreshInventory(true)
	response := refreshedCommandResponse(result)
	response.Notice = updateFailureNotice(result)
	writeJSON(w, http.StatusOK, response)
}

func (app *App) handleUpdateAllAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	packageKeys, invalid := parseUpdateAllPackageKeys(r)
	if invalid != nil {
		results := []UpdateResult{*invalid}
		writeJSON(w, http.StatusBadRequest, UpdateJobStatus{Results: results, RefreshStarted: false, Notice: updateResultsFailureNotice(results)})
		return
	}
	for _, key := range packageKeys {
		if err := validatePackageKey(key); err != nil {
			result := UpdateResult{Key: key, Result: validationCommandResult("update-all", err)}
			results := []UpdateResult{result}
			writeJSON(w, http.StatusBadRequest, UpdateJobStatus{Results: results, RefreshStarted: false, Notice: updateResultsFailureNotice(results)})
			return
		}
	}
	status, err := app.startUpdateJob(packageKeys)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, errUpdateJobRunning) {
			code = http.StatusConflict
		}
		status.Error = err.Error()
		writeJSON(w, code, status)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (app *App) handleUpdateAllStatusAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, app.updateJobStatus())
}

func (app *App) handleUpdateAllCancelAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	writeJSON(w, http.StatusOK, app.cancelUpdateJob())
}
