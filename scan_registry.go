package main

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

func normalizeText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func normalizeRegistryKey(name, publisher, location string) string {
	base := strings.ToLower(name + "|" + publisher + "|" + location)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	key := strings.Trim(re.ReplaceAllString(base, "-"), "-")
	if key == "" {
		key = strings.Trim(re.ReplaceAllString(strings.ToLower(name), "-"), "-")
	}
	return key
}

func parseRegQuery(output, hive string) []ScannedApp {
	valuePattern := regexp.MustCompile(`^\s+([^\s]+)\s+REG_\S+\s*(.*)$`)
	apps := map[string]map[string]string{}
	current := ""
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(strings.TrimSpace(line), "HKEY_") {
			current = strings.TrimSpace(line)
			if apps[current] == nil {
				apps[current] = map[string]string{}
			}
			continue
		}
		if current == "" {
			continue
		}
		match := valuePattern.FindStringSubmatch(line)
		if len(match) == 3 {
			apps[current][match[1]] = normalizeText(match[2])
		}
	}

	var scanned []ScannedApp
	for _, values := range apps {
		name := values["DisplayName"]
		if name == "" {
			continue
		}
		if values["SystemComponent"] == "0x1" {
			continue
		}
		releaseType := strings.ToLower(values["ReleaseType"])
		if releaseType == "hotfix" || releaseType == "security update" || releaseType == "update rollup" {
			continue
		}
		publisher := values["Publisher"]
		location := values["InstallLocation"]
		key := normalizeRegistryKey(name, publisher, location)
		scanned = append(scanned, ScannedApp{
			Key:             key,
			Name:            name,
			Version:         values["DisplayVersion"],
			Publisher:       publisher,
			InstallLocation: location,
			Source:          "registry",
			RegistryHive:    hive,
		})
	}
	sort.Slice(scanned, func(i, j int) bool { return strings.ToLower(scanned[i].Name) < strings.ToLower(scanned[j].Name) })
	return scanned
}

func readRegistryApps() ([]ScannedApp, error) {
	queries := []struct {
		key  string
		hive string
	}{
		{`HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall`, "HKLM"},
		{`HKLM\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`, "HKLM32"},
		{`HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall`, "HKCU"},
	}
	appMap := map[string]ScannedApp{}
	var errs []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, query := range queries {
		query := query
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := runCommand(60*time.Second, "reg.exe", "query", query.key, "/s")
			mu.Lock()
			defer mu.Unlock()
			if !result.OK && result.Stdout == "" {
				errs = append(errs, result.Stderr)
				return
			}
			for _, app := range parseRegQuery(result.Stdout, query.hive) {
				appMap[app.Key] = app
			}
		}()
	}
	wg.Wait()
	var apps []ScannedApp
	for _, app := range appMap {
		apps = append(apps, app)
	}
	sort.Slice(apps, func(i, j int) bool { return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name) })
	if len(apps) == 0 && len(errs) > 0 {
		return apps, errors.New(strings.Join(errs, "\n"))
	}
	return apps, nil
}
