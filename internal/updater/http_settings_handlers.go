package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type applicationPreferenceSettings struct {
	AppUpdateAutoInstallEnabled *bool `json:"app_update_auto_install_enabled"`
	AppUpdateCheckingEnabled    *bool `json:"app_update_checking_enabled"`
	RemoveNewDesktopShortcuts   *bool `json:"remove_new_desktop_shortcuts"`
}

func setThemePreference(requestedTheme string) (State, error) {
	return setThemePreferenceContext(context.Background(), requestedTheme)
}

func setThemePreferenceContext(ctx context.Context, requestedTheme string) (State, error) {
	stateStore, err := defaultStateStore()
	if err != nil {
		return State{}, err
	}
	return setThemePreferenceWithStore(ctx, stateStore, requestedTheme)
}

func setThemePreferenceWithStore(ctx context.Context, stateStore StateStore, requestedTheme string) (State, error) {
	themePreference := "dark"
	if requestedTheme == "light" {
		themePreference = "light"
	}
	return stateStore.Update(ctx, func(state *State) error {
		state.Theme = themePreference
		return nil
	})
}

func setAppUpdatePromptDismissedVersion(dismissedVersion string) (State, error) {
	return setAppUpdatePromptDismissedVersionContext(context.Background(), dismissedVersion)
}

func setAppUpdatePromptDismissedVersionContext(ctx context.Context, dismissedVersion string) (State, error) {
	stateStore, err := defaultStateStore()
	if err != nil {
		return State{}, err
	}
	return setAppUpdatePromptDismissedVersionWithStore(ctx, stateStore, dismissedVersion)
}

func setAppUpdatePromptDismissedVersionWithStore(ctx context.Context, stateStore StateStore, dismissedVersion string) (State, error) {
	dismissedVersion = strings.TrimSpace(dismissedVersion)
	return stateStore.Update(ctx, func(state *State) error {
		state.AppUpdatePromptDismissedVersion = dismissedVersion
		return nil
	})
}

func setApplicationPreferences(preferences applicationPreferenceSettings) (State, error) {
	return setApplicationPreferencesContext(context.Background(), preferences)
}

func setApplicationPreferencesContext(ctx context.Context, preferences applicationPreferenceSettings) (State, error) {
	stateStore, err := defaultStateStore()
	if err != nil {
		return State{}, err
	}
	return setApplicationPreferencesWithStore(ctx, stateStore, preferences)
}

func setApplicationPreferencesWithStore(ctx context.Context, stateStore StateStore, preferences applicationPreferenceSettings) (State, error) {
	return stateStore.Update(ctx, func(state *State) error {
		if preferences.AppUpdateAutoInstallEnabled != nil {
			state.AppUpdateAutoInstallEnabled = *preferences.AppUpdateAutoInstallEnabled
		}
		if preferences.AppUpdateCheckingEnabled != nil {
			state.AppUpdateChecksDisabled = !*preferences.AppUpdateCheckingEnabled
		}
		if preferences.RemoveNewDesktopShortcuts != nil {
			state.RemoveNewDesktopShortcuts = *preferences.RemoveNewDesktopShortcuts
		}
		return nil
	})
}

func parseStartupRequest(r *http.Request) (bool, *CommandResult) {
	if requestIsJSON(r) {
		var startupSettings struct {
			Enabled *bool `json:"enabled"`
		}
		if err := decodeSmallJSONRequest(r, &startupSettings); err != nil {
			result := validationCommandResult("startup settings", err)
			return false, &result
		}
		if startupSettings.Enabled == nil {
			result := validationCommandResult("startup settings", fmt.Errorf("missing enabled setting"))
			return false, &result
		}
		return *startupSettings.Enabled, nil
	}
	if err := parseFormRequest(r); err != nil {
		result := validationCommandResult("startup settings", err)
		return false, &result
	}
	startupEnabled, err := requiredFormBool(r, "enabled")
	if err != nil {
		result := validationCommandResult("startup settings", err)
		return false, &result
	}
	return startupEnabled, nil
}

func requiredFormBool(request *http.Request, fieldName string) (bool, error) {
	if !request.Form.Has(fieldName) {
		return false, fmt.Errorf("missing %s setting", fieldName)
	}
	return parseBoolSetting(request.Form.Get(fieldName), fieldName)
}

func parseAutoUpdateRequest(r *http.Request) (*bool, []string, *bool, *CommandResult) {
	if requestIsJSON(r) {
		var autoUpdateSettings struct {
			Global         *bool            `json:"global"`
			PackageKey     oneOrManyStrings `json:"package_key"`
			PackageKeys    oneOrManyStrings `json:"package_keys"`
			PackageEnabled *bool            `json:"package_enabled"`
		}
		if err := decodeSmallJSONRequest(r, &autoUpdateSettings); err != nil {
			result := validationCommandResult("auto-update settings", err)
			return nil, nil, nil, &result
		}
		packageKeys := combineStringLists(autoUpdateSettings.PackageKey, autoUpdateSettings.PackageKeys)
		return autoUpdateSettings.Global, packageKeys, autoUpdateSettings.PackageEnabled, nil
	}
	if err := parseFormRequest(r); err != nil {
		result := validationCommandResult("auto-update settings", err)
		return nil, nil, nil, &result
	}
	var globalAutoUpdateEnabled *bool
	if value, ok := formBool(r, "global"); ok {
		globalAutoUpdateEnabled = &value
	}
	var packageAutoUpdateEnabled *bool
	if value, ok := formBool(r, "package_enabled"); ok {
		packageAutoUpdateEnabled = &value
	}
	return globalAutoUpdateEnabled, r.Form["package_key"], packageAutoUpdateEnabled, nil
}

func parseThemeRequest(r *http.Request) (string, error) {
	if requestIsJSON(r) {
		var themeSettings struct {
			Theme string `json:"theme"`
		}
		if err := decodeSmallJSONRequest(r, &themeSettings); err != nil {
			return "", err
		}
		return themeSettings.Theme, nil
	}
	if err := parseFormRequest(r); err != nil {
		return "", err
	}
	return r.Form.Get("theme"), nil
}

func parseAppUpdatePromptRequest(r *http.Request) (string, error) {
	var dismissedVersion string
	if requestIsJSON(r) {
		var appUpdatePromptSettings struct {
			Version string `json:"version"`
		}
		if err := decodeSmallJSONRequest(r, &appUpdatePromptSettings); err != nil {
			return "", err
		}
		dismissedVersion = appUpdatePromptSettings.Version
	} else {
		if err := parseFormRequest(r); err != nil {
			return "", err
		}
		dismissedVersion = r.Form.Get("version")
	}
	return strings.TrimSpace(dismissedVersion), nil
}

func parseApplicationPreferencesRequest(r *http.Request) (applicationPreferenceSettings, error) {
	var preferences applicationPreferenceSettings
	if requestIsJSON(r) {
		var rawPreferences map[string]json.RawMessage
		if err := decodeRawJSONMapRequestBounded(r, &rawPreferences, maxSmallJSONBodyBytes); err != nil {
			return preferences, err
		}
		for fieldName, rawValue := range rawPreferences {
			value, err := parseRawJSONBool(rawValue, fieldName)
			if err != nil {
				return preferences, err
			}
			switch fieldName {
			case "app_update_auto_install_enabled":
				preferences.AppUpdateAutoInstallEnabled = &value
			case "app_update_checking_enabled":
				preferences.AppUpdateCheckingEnabled = &value
			case "remove_new_desktop_shortcuts":
				preferences.RemoveNewDesktopShortcuts = &value
			default:
				return preferences, fmt.Errorf("unknown field %s", fieldName)
			}
		}
	} else {
		if err := parseFormRequest(r); err != nil {
			return preferences, err
		}
		if value, ok, err := optionalFormBool(r, "app_update_auto_install_enabled"); err != nil {
			return preferences, err
		} else if ok {
			preferences.AppUpdateAutoInstallEnabled = &value
		}
		if value, ok, err := optionalFormBool(r, "app_update_checking_enabled"); err != nil {
			return preferences, err
		} else if ok {
			preferences.AppUpdateCheckingEnabled = &value
		}
		if value, ok, err := optionalFormBool(r, "remove_new_desktop_shortcuts"); err != nil {
			return preferences, err
		} else if ok {
			preferences.RemoveNewDesktopShortcuts = &value
		}
	}
	if preferences.AppUpdateAutoInstallEnabled == nil && preferences.AppUpdateCheckingEnabled == nil && preferences.RemoveNewDesktopShortcuts == nil {
		return preferences, fmt.Errorf("missing preference setting")
	}
	return preferences, nil
}

func parseRawJSONBool(rawValue json.RawMessage, fieldName string) (bool, error) {
	if strings.EqualFold(strings.TrimSpace(string(rawValue)), "null") {
		return false, fmt.Errorf("invalid %s setting", fieldName)
	}
	var value bool
	if err := json.Unmarshal(rawValue, &value); err != nil {
		return false, fmt.Errorf("invalid %s setting", fieldName)
	}
	return value, nil
}

func optionalFormBool(request *http.Request, fieldName string) (bool, bool, error) {
	if !request.Form.Has(fieldName) {
		return false, false, nil
	}
	value, err := parseBoolSetting(request.Form.Get(fieldName), fieldName)
	if err != nil {
		return false, true, err
	}
	return value, true, nil
}

func parseBoolSetting(rawValue, fieldName string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(rawValue)) {
	case "true", "1", "on", "yes":
		return true, nil
	case "false", "0", "off", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid %s setting", fieldName)
	}
}

func (app *App) handleStartupSettingsAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	startupEnabled, validationFailure := parseStartupRequest(r)
	if validationFailure != nil {
		writeJSON(w, http.StatusBadRequest, commandResponse(*validationFailure))
		return
	}
	result := setStartupContext(r.Context(), startupEnabled)
	app.refreshStatus(true)
	writeJSON(w, http.StatusOK, commandResponse(result))
}

func (app *App) handleAutoUpdateSettingsAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	globalAutoUpdateEnabled, packageKeys, packageAutoUpdateEnabled, validationFailure := parseAutoUpdateRequest(r)
	if validationFailure != nil {
		writeJSON(w, http.StatusBadRequest, commandResponse(*validationFailure))
		return
	}
	state, result := setAutoUpdateContext(r.Context(), globalAutoUpdateEnabled, packageKeys, packageAutoUpdateEnabled)
	app.refreshStatus(true)
	writeJSON(w, http.StatusOK, settingsCommandResponse(state, result))
}

func (app *App) handleThemeSettingsAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	theme, err := parseThemeRequest(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	state, err := setThemePreferenceContext(r.Context(), theme)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse(state))
}

func (app *App) handleApplicationPreferencesSettingsAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	preferences, err := parseApplicationPreferencesRequest(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	state, err := setApplicationPreferencesContext(r.Context(), preferences)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse(state))
}

func (app *App) handleAppUpdatePromptSettingsAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	dismissedVersion, err := parseAppUpdatePromptRequest(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	state, err := setAppUpdatePromptDismissedVersionContext(r.Context(), dismissedVersion)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse(state))
}
