package updater

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderedHTMLContainsAsyncUpdateHooks(t *testing.T) {
	var output bytes.Buffer
	data := PageData{
		Theme: "dark",
	}
	if err := pageTemplate.Execute(&output, data); err != nil {
		t.Fatal(err)
	}
	rendered := output.String()
	surface := rendered + "\n" + uiJS + "\n" + uiCSS
	for _, expected := range []string{
		`class="dashboard-hero"`,
		`rel="icon" href="/favicon.ico?v=`,
		`rel="shortcut icon" href="/favicon.ico?v=`,
		`rel="stylesheet" href="/assets/ui.css?v=`,
		`meta name="patchboard-csrf-token"`,
		`src="/assets/ui.js?v=`,
		`defer`,
		`id="dashboard-summary"`,
		`id="store-status-modal"`,
		`role="dialog"`,
		`aria-modal="true"`,
		`id="settings-button"`,
		`id="settings-modal"`,
		`id="settings-modal-title"`,
		`data-settings-open`,
		`data-settings-close`,
		`openSettingsModal`,
		`closeSettingsModal`,
		`id="store-status-close"`,
		`data-store-status-open`,
		`data-store-status-close`,
		`openStoreStatusModal`,
		`closeStoreStatusModal`,
		`id="store-scan-health-summary"`,
		`id="store-scan-health-body"`,
		`id="store-rescan-button"`,
		`id="store-diagnostics-export-button"`,
		`id="package-diagnostics-modal"`,
		`id="package-diagnostics-body"`,
		`data-package-diagnostics-open`,
		`data-package-diagnostics-close`,
		`openPackageDiagnosticsModal`,
		`closePackageDiagnosticsModal`,
		`id="app-update-modal"`,
		`Application update available`,
		`id="app-update-dismiss-version"`,
		`Don&#39;t show again for this version`,
		`data-app-update-close`,
		`openAppUpdateModal`,
		`closeAppUpdateModal`,
		`/api/settings/app-update-prompt`,
		`{keepalive:true}`,
		`packageDiagnosticsButton`,
		`diagnostics-button`,
		`/api/store/diagnostics/export`,
		`id="summary-updates"`,
		`id="summary-packages"`,
		`id="summary-managers"`,
		`id="summary-automation"`,
		`id="toast-region"`,
		`id="shutdown-form"`,
		`id="shutdown-button"`,
		`id="updates-section"`,
		`id="install-progress"`,
		`id="install-progress-status"`,
		`id="update-progress"`,
		`id="update-progress-status"`,
		`role="status"`,
		`aria-live="polite"`,
		`aria-busy="false"`,
		`role="progressbar"`,
		`aria-valuetext="In progress"`,
		`function progressBar(label)`,
		`id="updates-prev"`,
		`id="updates-page-status"`,
		`id="updates-next"`,
		`Check for Updates`,
		`class="table-pager"`,
		`class="table-footer split"`,
		`class="update-all-form"`,
		`id="search-form"`,
		`<span class="sr-only">Search and install packages</span>`,
		`id="search-prev"`,
		`id="search-page-status"`,
		`id="search-next"`,
		`id="search-provenance"`,
		`Backend / Notes`,
		`Exact ID`,
		`sourceLabel`,
		`executionBackendLabel`,
		`installRouteText`,
		`renderSearchProvenance`,
		`searchCommandResult`,
		`data.command_results`,
		`searchFailureReason`,
		`searchShownPhrase`,
		`searchOriginLabel`,
		`searchManagerLabel`,
		` unavailable;`,
		` search failed;`,
		`searchMatchCell`,
		`searchActionCell`,
		`installedSearchPackageKeys`,
		`installSearchPackageKey`,
		`installedInventoryHasSearchPackage`,
		`markSearchPackageInstalledFromForm`,
		`searchPackageAlreadyInstalled`,
		`Installed from this search session.`,
		`pkg.match_reason`,
		`package-id`,
		`install-route`,
		`action="/api/install"`,
		`aria-label="Install `,
		`action="/api/managers/install"`,
		`enqueueUpdateRequest`,
		`rowUpdateState`,
		`activeUpdateJobRunning`,
		`Queued`,
		`postCommandPayload`,
		`postForm("/api/status/refresh"`,
		`X-PatchBoard`,
		`X-PatchBoard-CSRF`,
		`function csrfToken()`,
		`form.id === "shutdown-form"`,
		`postForm("/shutdown"`,
		`payload.result && !payload.result.ok`,
		`Application scan completed with errors`,
		`Could not update startup setting`,
		`Could not update auto-update settings`,
		`id="automation-settings-open"`,
		`id="automation-summary-status"`,
		`Open Settings`,
		`id="app-update-checking-toggle"`,
		`id="app-update-auto-install-toggle"`,
		`id="desktop-shortcut-cleanup-toggle"`,
		`/api/settings/preferences`,
		`app_update_auto_install_enabled`,
		`app_update_checking_enabled`,
		`remove_new_desktop_shortcuts`,
		`Automatic application self update`,
		`Remove new Desktop shortcuts`,
		`autoEffectiveEnabled`,
		`Repair Daily Auto-Update`,
		`Remove Daily Auto-Update Task`,
		`Daily Auto-Update unavailable`,
		`auto_task_unsupported_reason`,
		`disabled (task registered)`,
		`Start with Windows: " + (data.startup_enabled ? "enabled" : "disabled")`,
		`Daily update task: " + autoStatus`,
		`id="app-update-check"`,
		`id="app-update-apply"`,
		`id="app-update-status"`,
		`id="settings-application-install-title"`,
		`Application installation`,
		`id="application-install-status"`,
		`id="application-install-button"`,
		`Install to Program Files`,
		`Repair installation`,
		`id="application-restart-installed"`,
		`Restart from installed copy`,
		`renderApplicationInstallStatus`,
		`/api/application/install`,
		`/api/application/restart-installed`,
		`class="app-footer"`,
		`id="app-license-note"`,
		`GPL-3.0-only`,
		`https://github.com/kernel-panic-enjoyer/PatchBoard`,
		`Application update`,
		`renderApplicationInfo`,
		`startAppSelfUpdate`,
		`renderAppUpdateStatus`,
		`incompatible_reason`,
		`is not compatible with this build`,
		`maybeShowAppUpdatePrompt`,
		`maybeStartAutomaticSelfUpdate`,
		`pendingAutomaticAppUpdate`,
		`autoSelfUpdateStartedVersions`,
		`appUpdateCheckingEnabled`,
		`appUpdateAutoInstallEnabled`,
		`appUpdatePromptDismissedVersion`,
		`/api/app-update/check`,
		`/api/app-update/apply`,
		`allow_unknown_version`,
		`allow_pinned`,
		`id="update-allow-unknown"`,
		`id="update-allow-pinned"`,
		`Global update options`,
		`<th>Select</th>`,
		`aria-label="Select `,
		`aria-label="Auto-update for `,
		`aria-pressed="`,
		`data-package-name`,
		`appendGlobalUpdateOptions`,
		`allowUnknownVersionUpdates`,
		`allowPinnedUpdates`,
		`packageCanBeIncludedInBulkUpdate`,
		`storeAssessmentActive`,
		`storeUpdateState`,
		`if(pkg && pkg.manager === "store"){`,
		`return "unknown";`,
		`storeScanHealth`,
		`renderStoreScanHealth`,
		`Provider diagnostics`,
		`Exact target unavailable`,
		`pkg.cannot_update_reason || "Unavailable"`,
		`accepted_not_verified`,
		`Verifying update`,
		`updatesEmptyState`,
		`No actionable updates available. Review Store scan health for diagnostics.`,
		`state-badge`,
		`updatePageSize = 10`,
		`showToast`,
		`Math.max(duration || 10000, 10000)`,
		`pauseToastTimers`,
		`resumeToastTimers`,
		`document.hidden`,
		`visibilitychange`,
		`toast-close`,
		`icon("close")`,
		`toast-region`,
		`toast-progress`,
		`--toast-progress`,
		`requestAnimationFrame`,
		`new EventSource`,
		`api("/api/events"`,
		`AbortController`,
		`statusRequestSeq`,
		`packageRequestSeq`,
		`scheduleStatusLoad`,
		`schedulePackageLoad`,
		`bottom:18px`,
		`startUpdateJob`,
		`checkActiveUpdateJob`,
		`api("/api/jobs/status"`,
		`api("/api/jobs")`,
		`postForm("/api/inventory/refresh"`,
		`postForm(cancelID ? "/api/jobs/cancel"`,
		`id="cancel-updates-button"`,
		`id="update-preflight-panel"`,
		`id="confirm-update-job"`,
		`id="cancel-update-preflight"`,
		`id="update-preflight-summary"`,
		`id="update-preflight-body"`,
		`id="update-preflight-excluded"`,
		`buildUpdatePreflight`,
		`renderUpdatePreflight`,
		`confirmPendingUpdateJob`,
		`hideUpdatePreflight`,
		`bulkElevationPreflightNote`,
		`Chocolatey packages will use one UAC prompt when elevation is needed; WinGet updates run in the current user context.`,
		`packageRiskNotes`,
		`pinned-package override`,
		`id="update-results-panel"`,
		`id="view-update-job-log"`,
		`View Job Log`,
		`id="retry-failed-updates"`,
		`id="update-results-summary"`,
		`id="update-results-body"`,
		`renderUpdateResultPanel`,
		`renderLatestUpdateResult`,
		`retryFailedUpdateResults`,
		`Succeeded`,
		`Failed`,
		`Skipped`,
		`result.code === 204`,
		`Cancelled`,
		`status.package_keys`,
		`applyUpdateJobPackageKeys`,
		`response.status === 409 && status.running`,
		`active && !status.cancel_requested`,
		`installFromForm`,
		`waitForJob`,
		`jobComplete`,
		`jobSucceeded`,
		`reconcileJobs`,
		`activeServerJobs`,
		`upsertServerJob`,
		`renderSearchTable`,
		`searchPageSize = 10`,
		`installManagerFromForm`,
		`refreshStatusAfterManagerInstall`,
		`setInstallProgress`,
		`install-progress-panel`,
		`refreshPackagesAfterUpdate`,
		`id="session-log-panel"`,
		`class="log-tab active"`,
		`role="tablist"`,
		`aria-orientation="horizontal"`,
		`role="tab"`,
		`aria-controls="session-log"`,
		`tabindex="0"`,
		`tabindex="-1"`,
		`role="tabpanel"`,
		`aria-labelledby="log-tab-all"`,
		`data-log-category="all"`,
		`data-log-category="application"`,
		`data-log-category="searches"`,
		`data-log-category="updates"`,
		`data-log-category="winget"`,
		`data-log-category="store"`,
		`data-log-category="chocolatey"`,
		`id="log-search"`,
		`id="log-connection-status"`,
		`class="hero-topline"`,
		`connection-badge`,
		`Reconnecting to backend`,
		`setLogConnectionState`,
		`currentBackendConnectionState`,
		`backendHasConnected`,
		`backendActionControlSelector`,
		`applyBackendAvailabilityState`,
		`dataset.backendDisabled`,
		`backendFormRequiresConnection`,
		`blockBackendActionWhenDisconnected`,
		`blockBackendFormWhenDisconnected`,
		`#search-form button[type='submit']`,
		`#export-log-view`,
		`document.addEventListener("click", function(event){`,
		`document.addEventListener("submit", function(event){`,
		`heartbeatIntervalMs = 10000`,
		`connectionStaleAfterMs = heartbeatIntervalMs + 5000`,
		`lastBackendContactAt`,
		`connectionWatchdogTimer`,
		`reconnectingAnimationIntervalMs = 600`,
		`reconnectingAnimationTimer`,
		`startReconnectingAnimation`,
		`stopReconnectingAnimation`,
		`if(reconnectingAnimationTimer && reconnectingAnimationBaseMessage === nextMessage){`,
		`reconnectingAnimationDots = (reconnectingAnimationDots + 1) % 4`,
		`backendHasConnected ? "Reconnecting to backend" : "Connecting to backend"`,
		`markBackendContact`,
		`scheduleConnectionWatchdog`,
		`checkBackendConnectionFreshness`,
		`eventStream.addEventListener("heartbeat"`,
		`width:27ch`,
		`maxBrowserLogEntries`,
		`maxBrowserLogBytes`,
		`maxBrowserCategoryLogEntries`,
		`maxBrowserCategoryLogBytes`,
		`logEntriesByCategory`,
		`logBytesByCategory`,
		`Older log entries were discarded before this point.`,
		`gap_detected`,
		`api("/api/jobs/log"`,
		`trimBrowserLogs`,
		`trimBrowserCategoryLogs`,
		`prepareLogEntry`,
		`_formatted`,
		`logBytes`,
		`id="copy-log-view"`,
		`id="export-log-view"`,
		`id="clear-log-view"`,
		`id="log-autoscroll"`,
		`activeLogCategory`,
		`logSearchQuery`,
		`filteredLogLines`,
		`setActiveLogCategory`,
		`focusAdjacentLogTab`,
		`ArrowRight`,
		`ArrowLeft`,
		`Home`,
		`End`,
		`clearCurrentLogView`,
		`exportLogs`,
		`copyLogView`,
		`navigator.clipboard.writeText`,
		`document.execCommand("copy")`,
		`api("/api/logs"`,
		`api("/api/logs/export"`,
		`id="updates-body"`,
		`id="installed-search"`,
		`for="installed-search">Filter installed packages`,
		`for="log-search">Search active log`,
		`id="installed-page-status"`,
		`id="updates-manager-filter"`,
		`id="installed-manager-filter"`,
		`color-scheme:light`,
		`color-scheme:dark`,
		`accent-color:var(--blue)`,
		`class="table-filter"`,
		`appearance:none`,
		`background-image:linear-gradient`,
		`id="updates-store-loading"`,
		`id="installed-store-loading"`,
		`store-loading-note`,
		`Microsoft Store is still checking for updates`,
		`updatesManagerFilter`,
		`installedManagerFilter`,
		`packageMatchesManagerFilter`,
		`visibleUpdates`,
		`renderStoreLoadingNotes`,
		`updatesTableShowsLoadingRow`,
		`syncManagerFilterOptions`,
		`data.store_loading`,
		`data.loading || data.store_loading`,
		`storeOnlyUpdatesLoading`,
		`Checking Store...`,
		`packages.filter(packageShouldAppearInUpdateQueue)`,
		`packageMatchesInstalledSearch`,
		`packageAvailableCell`,
		`packageAvailableCell(pkg, {statusBadge:false, compact:true})`,
		`updateActionCell`,
		`pkg.can_update_now`,
		`pkg.preference_eligible`,
		`pkg.cannot_update_reason`,
		`pkg.exact_target_kind`,
		`return !!pkg.can_update_now || state === "conflict";`,
		`row-actions`,
		`.row-actions{display:flex`,
		`--row-update-action-width:132px`,
		`.update-form button{width:100%}`,
		`.row-actions .update-form{flex:0 0 var(--row-update-action-width)}`,
		`.action-cell{padding-right:24px}`,
		`class="action-cell"`,
		`.row-progress{width:100%}`,
		`--package-row-height:78px`,
		`package-name-cell`,
		`cell-clip`,
		`-webkit-line-clamp:2`,
		`icon-only`,
		`managersRendered`,
		`renderUpdatesTable`,
		`renderInstalledTable`,
		`installedAction`,
		`updating-current`,
		`managerAvailabilityText`,
		`managerDisplayDetails`,
		`renderDashboardSummary`,
		`managerLabel`,
		`backendLabel`,
		`managerCell(pkg, {compact:true})`,
		`appendUpdateJobCounter`,
		`message.slice(-3) === "..."`,
		`appendUpdateJobCounter(status.notice || ("Starting update: " + name), counter)`,
		`function icon(name)`,
		`function spinner()`,
		`function loadingText(message)`,
		`function setLoadingContent(target, message, loading)`,
		`function loadingTableRow(colspan, message)`,
		`class="spinner"`,
		`class="loading-message"`,
		`class="loading-text"`,
		`conic-gradient`,
		`will-change:transform`,
		`--spinner-angle`,
		`function startSpinnerLoop()`,
		`function updateSpinnerPhase()`,
		`function observeSpinnerPresence()`,
		`MutationObserver`,
		`requestAnimationFrame(tick)`,
		`prefers-reduced-motion:reduce`,
		`class="button-icon"`,
		`class="summary-card`,
		`compactNoticeText`,
		`truncateNoticeText`,
		`firstMeaningfulOutputLine`,
		`See Session Log for full output.`,
		`max-height:96px`,
		`manager.inventory_available`,
		`pkg.action_backend`,
		`Inventory only`,
		`Store apps detected via`,
	} {
		if !strings.Contains(surface, expected) {
			t.Fatalf("rendered page or embedded assets did not contain %q", expected)
		}
	}
	for _, unexpected := range []string{
		`Inventory: `,
		`Actions: `,
		`unknown-confirm`,
		`pinned-confirm`,
		`Update Anyway`,
		`Available Usage: store`,
		`Usage: store <command>`,
		`store-health-compact`,
		`? "Current" : "-"`,
		`action="/install"`,
		`action="/manager/install"`,
		`action="/update"`,
		`action="/update-all"`,
		`class="status-grid"`,
		`{{if .CommandResult}}`,
		`{{if .ActionResults}}`,
		`{{if .Scan}}`,
		`showNotice("Refreshing package status...", true)`,
		`showNotice(rowUpdateProgressMessage(), true)`,
		`rowUpdateQueue`,
		`rowUpdateActive`,
		`processRowUpdateQueue`,
		`rowUpdateQueueActive`,
		`queuedRowUpdateKeys`,
		`showNotice(message, active || !!(status && status.refresh_started))`,
		`showNotice(message || "Starting updates...", true)`,
		`data-token`,
		`name="token"`,
		`searchParams.set("token"`,
		`PFN:`,
		`&times;`,
		`offered ? offered : "Update available"`,
		`packageNameCell(pkg, {diagnostics:true})`,
		`managerCell(pkg) + '</td><td>' + html(pkg.version)`,
		`Manager / Backend`,
		`Backend:`,
		`.row-actions{display:grid`,
		`.row-progress{margin-top:8px;min-width:100px}`,
		`.row-actions .update-form{display:grid;gap:8px;justify-items:start;flex:0 0 auto}`,
		`Log reconnecting`,
		`Session log disconnected. Reconnecting`,
		`Log disconnected; retrying`,
		`Store update status is unknown. Review scan health.`,
		`!!pkg.exact_action_target_available && !!pkg.installed_package_family_name && !!pkg.store_product_id`,
		`Store updates require an exact verified action target`,
		`Store updates require a fresh available assessment`,
	} {
		if strings.Contains(surface, unexpected) {
			t.Fatalf("rendered page or embedded assets should not contain %q", unexpected)
		}
	}
	for _, unexpectedInline := range []string{`<style>`, `<script>!function`, `<script>`} {
		if strings.Contains(rendered, unexpectedInline) {
			t.Fatalf("rendered page should not contain inline asset block %q", unexpectedInline)
		}
	}
	scanIndex := strings.Index(rendered, `id="scan-button"`)
	connectionIndex := strings.Index(rendered, `id="log-connection-status"`)
	settingsIndex := strings.Index(rendered, `id="settings-button"`)
	shutdownIndex := strings.Index(rendered, `id="shutdown-form"`)
	if connectionIndex < 0 || settingsIndex < 0 || shutdownIndex < 0 || !(connectionIndex < settingsIndex && settingsIndex < shutdownIndex) {
		t.Fatalf("expected connection status, settings, then stop in header actions, connection=%d settings=%d shutdown=%d", connectionIndex, settingsIndex, shutdownIndex)
	}
	mainIndex := strings.Index(rendered, `<main>`)
	installedIndex := strings.Index(rendered, `id="installed-section"`)
	if scanIndex < 0 || mainIndex < 0 || installedIndex < 0 {
		t.Fatalf("expected scan control in installed packages section, scan=%d main=%d installed=%d", scanIndex, mainIndex, installedIndex)
	}
	if strings.Contains(rendered[:mainIndex], `id="scan-button"`) || scanIndex < installedIndex {
		t.Fatalf("expected scan control outside header and inside installed packages section, scan=%d main=%d installed=%d", scanIndex, mainIndex, installedIndex)
	}
	progressIndex := strings.Index(rendered, `id="update-progress"`)
	updatesIndex := strings.Index(rendered, `Updates Available`)
	if progressIndex < 0 || updatesIndex < 0 || progressIndex > updatesIndex {
		t.Fatalf("expected update progress banner before updates table, progress=%d updates=%d", progressIndex, updatesIndex)
	}
	updateResultsIndex := strings.Index(rendered, `id="update-results-panel"`)
	updatePreflightIndex := strings.Index(rendered, `id="update-preflight-panel"`)
	updatesSectionIndex := strings.Index(rendered, `id="updates-section"`)
	if updateResultsIndex < 0 || updatePreflightIndex < 0 || updatesSectionIndex < 0 || !(updateResultsIndex < updatePreflightIndex && updatePreflightIndex < updatesSectionIndex) {
		t.Fatalf("expected update results before preflight and preflight before updates queue, results=%d preflight=%d updates=%d", updateResultsIndex, updatePreflightIndex, updatesSectionIndex)
	}
	installProgressIndex := strings.Index(rendered, `id="install-progress"`)
	searchResultsIndex := strings.Index(rendered, `id="search-results-panel"`)
	if installProgressIndex < 0 || searchResultsIndex < 0 || installProgressIndex > searchResultsIndex {
		t.Fatalf("expected install progress banner before search results, progress=%d search=%d", installProgressIndex, searchResultsIndex)
	}
}

func TestSuccessfulUpdateResultsSuppressPrimaryUpdateRows(t *testing.T) {
	for _, expected := range []string{
		`var completedUpdateKeys = {};`,
		`function recordSucceededUpdateResults(status){`,
		`if(job.job_id && completedJobIDs[job.job_id]){ return; }`,
		`completedUpdateKeys[row.key] = true;`,
		`function packageHiddenAfterSuccessfulUpdate(pkg){`,
		`if(packageHiddenAfterSuccessfulUpdate(pkg)){ return false; }`,
		`if(jobsInitialized){ recordSucceededUpdateResults(serverJobs); }`,
	} {
		if !strings.Contains(uiJS, expected) {
			t.Fatalf("expected successful update rows to suppress primary update entries, missing %q", expected)
		}
	}
}

func TestCompletedUpdateJobsClearGlobalProgress(t *testing.T) {
	start := strings.Index(uiJS, "function reconcileJobs(jobs){")
	if start < 0 {
		t.Fatal("reconcileJobs function not found")
	}
	end := strings.Index(uiJS[start:], "\n  function reconcileAuxiliaryJobProgress")
	if end < 0 {
		t.Fatal("reconcileJobs function end not found")
	}
	body := uiJS[start : start+end]
	for _, expected := range []string{
		`setUpdateBusy(false, [], "");`,
		`setGlobalProgress(false, "", false);`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("completed update jobs should clear global progress, missing %q; body:\n%s", expected, body)
		}
	}
	if strings.Contains(body, `}else if(!updateBusy){`) {
		t.Fatalf("completed update job cleanup must not depend on updateBusy already being false; body:\n%s", body)
	}
}

func TestStoreUnknownRowsStayOutOfPrimaryUpdateQueue(t *testing.T) {
	for _, expected := range []string{
		`if(pkg && pkg.manager === "store" && !storeAssessmentActive(pkg)){ return false; }`,
		`return !!pkg.can_update_now || state === "conflict";`,
		`return showStatusBadge ? stateBadge(pkg) : '<span class="muted">-</span>';`,
	} {
		if !strings.Contains(uiJS, expected) {
			t.Fatalf("expected Store unknown handling to contain %q", expected)
		}
	}
	for _, unexpected := range []string{
		`if(pkg && pkg.manager === "store" && !storeAssessmentActive(pkg)){ return true; }`,
		`state === "unknown" || state === "conflict"`,
		`text = "Unknown";`,
		`return withOptionalBadge("Unknown", true);`,
	} {
		if strings.Contains(uiJS, unexpected) {
			t.Fatalf("Store unknown packages should not render as update rows or duplicate Unknown text, found %q", unexpected)
		}
	}
}

func TestUpdateSelectedUsesPersistentSelectionState(t *testing.T) {
	for _, expected := range []string{
		`var selectedUpdateKeys = new Set();`,
		`function selectedUpdatePackageKeys()`,
		`selectedUpdateKeys.add(key);`,
		`selectedUpdateKeys.delete(key);`,
		`var keys = selectedUpdatePackageKeys();`,
		`keys.forEach(function(key){ params.append("package_key", key); });`,
		`button.disabled = updateBusy || activeUpdateJobRunning() || selectedUpdatePackageKeys().length === 0;`,
		`checked ? ' checked' : ''`,
	} {
		if !strings.Contains(uiJS, expected) {
			t.Fatalf("expected persistent update selection support to contain %q", expected)
		}
	}
	if strings.Contains(uiJS, `var params = appendGlobalUpdateOptions(new URLSearchParams(new FormData(form)));
      var keys = params.getAll("package_key");`) {
		t.Fatal("update selected submit must not depend only on currently rendered checkbox DOM")
	}
}

func TestUIConsistencyLabelsAndHealthFallbacks(t *testing.T) {
	for _, expected := range []string{
		`"native-appx": "AppX inventory"`,
		`counts.pending === 0`,
		`var updateCandidates = packages.filter(packageShouldAppearInUpdateQueue);`,
		`var installedReason = searchPackageInstalledInCurrentSession(pkg) ? "Installed from this search session." : "Already installed.";`,
		`function packageByKey(key)`,
	} {
		if !strings.Contains(uiJS, expected) {
			t.Fatalf("expected UI consistency fix to contain %q", expected)
		}
	}
	if strings.Count(uiJS, `function packageByKey(key)`) != 1 {
		t.Fatalf("expected exactly one packageByKey implementation")
	}
}

func TestDirectInstallCompletionToastIsCallerOwned(t *testing.T) {
	for _, expected := range []string{
		`var callerHandledCompletionJobIDs = {};`,
		`function markJobCompletionHandledByCaller(jobID)`,
		`function jobCompletionHandledByCaller(jobID)`,
		`if(jobCompletionHandledByCaller(job.job_id)){`,
		`completedJobIDs[job.job_id] = true;`,
	} {
		if !strings.Contains(uiJS, expected) {
			t.Fatalf("expected direct install toast ownership support to contain %q", expected)
		}
	}
	if count := strings.Count(uiJS, `markJobCompletionHandledByCaller(payload.job_id);`); count != 2 {
		t.Fatalf("expected package and manager install flows to claim their job toast, count=%d", count)
	}
}

func TestSearchResultsSourceUsesInstalledPackageBadge(t *testing.T) {
	for _, expected := range []string{
		`function searchSourceCell(pkg)`,
		`return managerCell(pkg || {}, {compact:true});`,
		`<td>' + searchSourceCell(pkg) + '</td>`,
	} {
		if !strings.Contains(uiJS, expected) {
			t.Fatalf("expected search results source badge support to contain %q", expected)
		}
	}
	if strings.Contains(uiJS, `<td>' + html(sourceLabel(pkg.source || pkg.manager)) + '</td>`) {
		t.Fatal("search results source column should reuse manager badges instead of free-text source labels")
	}
}

func TestStaleConnectionWatchdogFallsBackWithoutReload(t *testing.T) {
	for _, expected := range []string{
		`function closeEventStream()`,
		`function backendContactIsFresh()`,
		`function scheduleLogPolling(delayOverride)`,
		`closeEventStream();`,
		`scheduleLogPolling(0);`,
	} {
		if !strings.Contains(uiJS, expected) {
			t.Fatalf("expected stale connection recovery support to contain %q", expected)
		}
	}
}

func TestSessionLogTabSwitchScrollsToLatestEntries(t *testing.T) {
	start := strings.Index(uiJS, `function setActiveLogCategory(category){`)
	if start < 0 {
		t.Fatal("setActiveLogCategory function not found")
	}
	end := strings.Index(uiJS[start:], `function focusAdjacentLogTab`)
	if end < 0 {
		t.Fatal("focusAdjacentLogTab function not found after setActiveLogCategory")
	}
	body := uiJS[start : start+end]
	if !strings.Contains(body, `renderLogLines(true);`) {
		t.Fatalf("switching log tabs should render and scroll to latest entries; body:\n%s", body)
	}
	if strings.Contains(body, `renderLogLines(false);`) {
		t.Fatalf("switching log tabs should not preserve a stale scroll position; body:\n%s", body)
	}
}
