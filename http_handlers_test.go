package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAPILogsRequiresTokenAndReturnsEntries(t *testing.T) {
	oldLogs := sessionLogs
	sessionLogs = newLogBuffer(10)
	defer func() { sessionLogs = oldLogs }()

	sessionLogs.Append("app", "hello")
	app := &App{token: "test-token"}

	badRequest := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	badResponse := httptest.NewRecorder()
	app.serveHTTP(badResponse, badRequest)
	if badResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized log request, got %d", badResponse.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/logs?token=test-token", nil)
	response := httptest.NewRecorder()
	app.serveHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d: %s", response.Code, response.Body.String())
	}

	var decoded struct {
		Entries  []LogEntry `json:"entries"`
		LatestID int64      `json:"latest_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.LatestID != 1 || len(decoded.Entries) != 1 || decoded.Entries[0].Message != "hello" {
		t.Fatalf("unexpected log response: %#v", decoded)
	}
}

func TestShutdownRouteStopsServer(t *testing.T) {
	app := &App{token: "test-token"}
	server := httptest.NewServer(http.HandlerFunc(app.serveHTTP))
	app.server = server.Config
	defer server.Close()

	response, err := server.Client().Post(server.URL+"/shutdown?token=test-token", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected shutdown response ok, got %d", response.StatusCode)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		check, err := server.Client().Get(server.URL + "/?token=test-token")
		if err != nil {
			return
		}
		_ = check.Body.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server still responded after shutdown")
}

func TestAPIUpdateIgnoresCanceledRequestContext(t *testing.T) {
	oldRunner := updatePackageRunner
	oldGetter := inventoryGetter
	var observedErr error
	updatePackageRunner = func(ctx context.Context, pkg Package) CommandResult {
		observedErr = ctx.Err()
		return CommandResult{OK: true, Command: "update " + pkg.ID}
	}
	inventoryGetter = func() Inventory {
		return Inventory{}
	}
	defer func() {
		updatePackageRunner = oldRunner
		inventoryGetter = oldGetter
	}()

	app := &App{
		token: "test-token",
		inventory: Inventory{PackageLookup: PackageLookup{Packages: []Package{{
			Key:             "winget:Git.Git",
			Manager:         managerWinget,
			ID:              "Git.Git",
			Name:            "Git",
			UpdateAvailable: true,
			UpdateSupported: true,
		}}}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/update", strings.NewReader("token=test-token&manager=winget&package_id=Git.Git")).WithContext(ctx)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	app.serveHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected update request to complete despite canceled request context, got %d: %s", response.Code, response.Body.String())
	}
	if observedErr != nil {
		t.Fatalf("update command used canceled request context: %v", observedErr)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		app.mu.RLock()
		loading := app.inventoryLoading
		app.mu.RUnlock()
		if !loading {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("inventory refresh did not finish")
}

func TestAPIRejectsInvalidRequests(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		body       string
		content    string
		wantResult bool
		wantText   string
	}{
		{"update form", "/api/update?token=test-token", "manager=invalid&package_id=Git.Git", "application/x-www-form-urlencoded", true, managerValidationMessage},
		{"install form", "/api/install?token=test-token", "manager=invalid&package_id=Git.Git", "application/x-www-form-urlencoded", true, managerValidationMessage},
		{"manager install form", "/api/managers/install?token=test-token", "manager=invalid", "application/x-www-form-urlencoded", true, managerValidationMessage},
		{"update all form", "/api/update-all?token=test-token", "package_key=not-a-valid-key", "application/x-www-form-urlencoded", false, "package key must be manager:id"},
		{"update json", "/api/update?token=test-token", `{"manager":"invalid","package_id":"Git.Git"}`, "application/json", true, managerValidationMessage},
		{"install json", "/api/install?token=test-token", `{"manager":"winget","package_id":"bad&id"}`, "application/json", true, "winget package id or query contains unsupported characters"},
		{"manager install json", "/api/managers/install?token=test-token", `{"manager":"invalid"}`, "application/json", true, managerValidationMessage},
		{"update all json", "/api/update-all?token=test-token", `{"package_keys":["not-a-valid-key"]}`, "application/json", false, "package key must be manager:id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := &App{token: "test-token"}
			request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", tc.content)
			response := httptest.NewRecorder()

			app.serveHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected bad request, got %d: %s", response.Code, response.Body.String())
			}

			var decoded struct {
				Result         *CommandResult `json:"result"`
				Results        []UpdateResult `json:"results"`
				RefreshStarted bool           `json:"refresh_started"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.RefreshStarted {
				t.Fatal("invalid request should not start an inventory refresh")
			}
			if tc.wantResult {
				if decoded.Result == nil || decoded.Result.Code != 2 || !strings.Contains(decoded.Result.Stderr, tc.wantText) {
					t.Fatalf("unexpected validation result: %#v", decoded.Result)
				}
				return
			}
			if len(decoded.Results) != 1 || decoded.Results[0].Result.Code != 2 || !strings.Contains(decoded.Results[0].Result.Stderr, tc.wantText) {
				t.Fatalf("unexpected update-all validation result: %#v", decoded.Results)
			}
		})
	}
}

func TestSettingsJSONRequestParsers(t *testing.T) {
	updateRequest := httptest.NewRequest(http.MethodPost, "/api/update", strings.NewReader(`{"manager":"winget","package_id":"Vendor.Unknown","allow_unknown_version":true,"allow_pinned":true}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	manager, packageID, allowUnknown, allowPinned, invalidUpdate := parsePackageUpdateAction(updateRequest)
	if invalidUpdate != nil || manager != managerWinget || packageID != "Vendor.Unknown" || !allowUnknown || !allowPinned {
		t.Fatalf("unexpected update JSON parse: manager=%q packageID=%q allowUnknown=%t allowPinned=%t invalid=%#v", manager, packageID, allowUnknown, allowPinned, invalidUpdate)
	}

	startupRequest := httptest.NewRequest(http.MethodPost, "/api/settings/startup", strings.NewReader(`{"enabled":true}`))
	startupRequest.Header.Set("Content-Type", "application/json")
	enabled, invalidStartup := parseStartupRequest(startupRequest)
	if invalidStartup != nil || !enabled {
		t.Fatalf("expected enabled startup JSON parse, enabled=%t invalid=%#v", enabled, invalidStartup)
	}

	autoRequest := httptest.NewRequest(http.MethodPost, "/api/settings/auto-update", strings.NewReader(`{"global":true,"package_keys":["winget:Git.Git"],"package_enabled":false}`))
	autoRequest.Header.Set("Content-Type", "application/json")
	global, keys, packageEnabled, invalidAuto := parseAutoUpdateRequest(autoRequest)
	if invalidAuto != nil || global == nil || !*global || packageEnabled == nil || *packageEnabled || len(keys) != 1 || keys[0] != "winget:Git.Git" {
		t.Fatalf("unexpected auto-update JSON parse: global=%v keys=%#v packageEnabled=%v invalid=%#v", global, keys, packageEnabled, invalidAuto)
	}

	themeRequest := httptest.NewRequest(http.MethodPost, "/api/settings/theme", strings.NewReader(`{"theme":"light"}`))
	themeRequest.Header.Set("Content-Type", "application/json")
	theme, err := parseThemeRequest(themeRequest)
	if err != nil || theme != "light" {
		t.Fatalf("unexpected theme JSON parse: theme=%q err=%v", theme, err)
	}
}

func TestSettingsAPIsRejectMalformedJSONBeforeSideEffects(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		body       string
		wantResult bool
	}{
		{"startup", "/api/settings/startup?token=test-token", `{"enabled":`, true},
		{"auto update", "/api/settings/auto-update?token=test-token", `{"package_keys":{}}`, true},
		{"theme", "/api/settings/theme?token=test-token", `{"theme":`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := &App{token: "test-token"}
			request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			app.serveHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected bad request, got %d: %s", response.Code, response.Body.String())
			}
			if tc.wantResult {
				var decoded commandAPIResponse
				if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.Result == nil || decoded.Result.Code != 2 || !strings.Contains(decoded.Result.Stderr, "invalid JSON body") {
					t.Fatalf("expected validation command result, got %#v", decoded.Result)
				}
				return
			}
			var decoded apiErrorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(decoded.Error, "invalid JSON body") {
				t.Fatalf("expected invalid JSON error, got %#v", decoded)
			}
		})
	}
}
