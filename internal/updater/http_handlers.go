package updater

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func formBool(request *http.Request, fieldName string) (bool, bool) {
	if !request.Form.Has(fieldName) {
		return false, false
	}
	normalizedValue := strings.ToLower(strings.TrimSpace(request.Form.Get(fieldName)))
	return normalizedValue == "true" || normalizedValue == "1" || normalizedValue == "on" || normalizedValue == "yes", true
}

func validatePackageKey(packageKey string) error {
	manager, id, err := splitPackageKey(packageKey)
	if err != nil {
		return err
	}
	return validateManagerAndID(manager, id)
}

type apiRoute struct {
	Method       string
	MaxBodyBytes int64
	Handler      func(*App, http.ResponseWriter, *http.Request)
}

var apiRoutes = map[string]apiRoute{
	"/api/store/diagnostics/export": {
		Method:       http.MethodGet,
		MaxBodyBytes: 0,
		Handler:      handleStoreDiagnosticsExportAPI,
	},
	"/api/logs/export": {
		Method:       http.MethodGet,
		MaxBodyBytes: 0,
		Handler:      handleLogsExportAPI,
	},
	"/api/status": {
		Method:       http.MethodGet,
		MaxBodyBytes: 0,
		Handler: func(app *App, w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, app.statusSnapshotContext(r.Context()))
		},
	},
	"/api/status/refresh": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler: func(app *App, w http.ResponseWriter, r *http.Request) {
			app.refreshStatus(true)
			writeJSON(w, http.StatusAccepted, app.statusSnapshotContext(r.Context()))
		},
	},
	"/api/app-update/check": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler: func(app *App, w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, app.appUpdateStatusContext(r.Context(), true))
		},
	},
	"/api/app-update/apply": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler: func(app *App, w http.ResponseWriter, r *http.Request) {
			jobAcceptedResponse(w, app.startSelfUpdateJob())
		},
	},
	"/api/application/install": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleApplicationInstallAPI,
	},
	"/api/application/restart-installed": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleApplicationRestartInstalledAPI,
	},
	"/api/packages": {
		Method:       http.MethodGet,
		MaxBodyBytes: 0,
		Handler: func(app *App, w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, app.inventorySnapshotContext(r.Context()))
		},
	},
	"/api/inventory/refresh": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleInventoryRefreshAPI,
	},
	"/api/jobs/status": {
		Method:       http.MethodGet,
		MaxBodyBytes: 0,
		Handler:      (*App).handleJobStatusAPI,
	},
	"/api/jobs/log": {
		Method:       http.MethodGet,
		MaxBodyBytes: 0,
		Handler:      (*App).handleJobLogAPI,
	},
	"/api/jobs": {
		Method:       http.MethodGet,
		MaxBodyBytes: 0,
		Handler:      (*App).handleJobsAPI,
	},
	"/api/jobs/cancel": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleJobCancelAPI,
	},
	"/api/events": {
		Method:       http.MethodGet,
		MaxBodyBytes: 0,
		Handler:      (*App).handleEventsAPI,
	},
	"/api/logs": {
		Method:       http.MethodGet,
		MaxBodyBytes: 0,
		Handler:      handleLogsAPI,
	},
	"/api/search": {
		Method:       http.MethodGet,
		MaxBodyBytes: 0,
		Handler:      handleSearchAPI,
	},
	"/api/install": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleInstallAPI,
	},
	"/api/managers/install": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleManagerInstallAPI,
	},
	"/api/scan": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleScanAPI,
	},
	"/api/update": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleUpdateAPI,
	},
	"/api/update-all/status": {
		Method:       http.MethodGet,
		MaxBodyBytes: 0,
		Handler:      (*App).handleUpdateAllStatusAPI,
	},
	"/api/update-all/cancel": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleUpdateAllCancelAPI,
	},
	"/api/update-all": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxPackageListBodyBytes,
		Handler:      (*App).handleUpdateAllAPI,
	},
	"/api/settings/startup": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleStartupSettingsAPI,
	},
	"/api/settings/auto-update": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleAutoUpdateSettingsAPI,
	},
	"/api/settings/theme": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleThemeSettingsAPI,
	},
	"/api/settings/app-update-prompt": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleAppUpdatePromptSettingsAPI,
	},
	"/api/settings/preferences": {
		Method:       http.MethodPost,
		MaxBodyBytes: maxSmallJSONBodyBytes,
		Handler:      (*App).handleApplicationPreferencesSettingsAPI,
	},
}

func (app *App) serveAPI(w http.ResponseWriter, r *http.Request) {
	route, ok := apiRoutes[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !requireMethod(w, r, route.Method) {
		return
	}
	route.Handler(app, w, r)
}

func handleStoreDiagnosticsExportAPI(app *App, w http.ResponseWriter, r *http.Request) {
	diagnosticsJSON, err := buildStoreDiagnosticsExport(r.Context(), loadState())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAttachmentResponse(w, "application/json", storeDiagnosticsExportFilename(time.Now()), diagnosticsJSON)
}

func handleLogsExportAPI(app *App, w http.ResponseWriter, _ *http.Request) {
	logArchive, err := buildLogArchiveFromSnapshot(sessionLogs.ExportSnapshot())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAttachmentResponse(w, "application/zip", logExportFilename(time.Now()), logArchive)
}

func handleLogsAPI(_ *App, w http.ResponseWriter, r *http.Request) {
	since, ok := parseLogSince(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, logsAPIResponseFromQuery(sessionLogs.Query(since)))
}

func handleSearchAPI(_ *App, w http.ResponseWriter, r *http.Request) {
	searchResults, err := searchPackages(r.URL.Query().Get("q"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, searchResults)
}

func writeAttachmentResponse(w http.ResponseWriter, contentType, filename string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func parseLogSince(w http.ResponseWriter, r *http.Request) (int64, bool) {
	var sinceID int64
	if sinceParam := r.URL.Query().Get("since"); sinceParam != "" {
		parsedSinceID, err := strconv.ParseInt(sinceParam, 10, 64)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "since must be an integer")
			return 0, false
		}
		sinceID = parsedSinceID
	}
	return sinceID, true
}

func logsAPIResponseFromQuery(result LogQueryResult) logsAPIResponse {
	return logsAPIResponse(result)
}

func logExportFilename(now time.Time) string {
	return now.Format("2006-01-02_15-04-05") + "_patchboard-logs.zip"
}

func storeDiagnosticsExportFilename(now time.Time) string {
	return now.Format("2006-01-02_15-04-05") + "_store-diagnostics.json"
}

func (app *App) serveHTTP(w http.ResponseWriter, r *http.Request) {
	app.registerSessionSecretsForLogRedaction()
	setSecurityHeaders(w)
	if !app.trustedHost(r) {
		writeAPIError(w, http.StatusMisdirectedRequest, "untrusted host")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		app.serveFrontendAsset(w, r)
		return
	}
	if r.URL.Path == "/favicon.ico" {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
		w.Header().Set("ETag", `"`+appIconVersion()+`"`)
		_, _ = w.Write(appIconICO)
		return
	}
	if app.handleBootstrap(w, r) {
		return
	}
	if !app.sessionOK(r) {
		http.Error(w, "Unauthorized. Start the app and use the tokenized bootstrap URL.", http.StatusUnauthorized)
		return
	}
	if !app.requestBoundaryOK(w, r) {
		return
	}
	if r.URL.Path == "/shutdown" {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		_, _ = io.WriteString(w, "Stopping")
		go func() {
			time.Sleep(200 * time.Millisecond)
			app.requestShutdown("WebUI Stop")
		}()
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		if !limitAPIRequestBody(w, r) {
			return
		}
		app.serveAPI(w, r)
		return
	}

	switch r.URL.Path {
	case "/":
		app.render(w, r, PageData{})
	default:
		http.NotFound(w, r)
	}
}

func limitAPIRequestBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil || r.Body == http.NoBody {
		return true
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if r.ContentLength != 0 {
			writeAPIError(w, http.StatusBadRequest, "request body is not allowed")
			return false
		}
		return true
	}
	r.Body = http.MaxBytesReader(w, r.Body, apiRequestBodyLimit(r.URL.Path))
	return true
}

func apiRequestBodyLimit(path string) int64 {
	if route, ok := apiRoutes[path]; ok && route.MaxBodyBytes > 0 {
		return route.MaxBodyBytes
	}
	return maxJSONBodyBytes
}

func (app *App) render(w http.ResponseWriter, r *http.Request, pageData PageData) {
	savedState := loadState()
	pageData.Admin = isAdmin()
	pageData.StateDir, _ = stateDir()
	pageData.Theme = savedState.Theme
	pageData.IconVersion = appIconVersion()
	pageData.AssetVersion = frontendAssetVersion()
	pageData.CSRFToken = csrfTokenForSession(app.webSession.sessionToken)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTemplate.Execute(w, pageData); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (app *App) serveFrontendAsset(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	assetVersion := frontendAssetVersion()
	w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
	w.Header().Set("ETag", `"`+assetVersion+`"`)
	switch r.URL.Path {
	case "/assets/ui.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = io.WriteString(w, uiCSS)
	case "/assets/ui.js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = io.WriteString(w, uiJS)
	default:
		http.NotFound(w, r)
	}
}
