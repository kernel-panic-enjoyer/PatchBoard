package main

import (
	"strings"
	"time"
)

func parseStoreSearch(output string) []Package {
	return parseStorePackageTable(output)
}

func parseStoreUpdates(output string) map[string]string {
	updates := map[string]string{}
	for _, pkg := range parseStorePackageTable(output) {
		if pkg.ID == "" {
			continue
		}
		available := pkg.AvailableVersion
		if available == "" {
			available = pkg.Version
		}
		if available != "" {
			updates[packageKey(managerStore, strings.ToLower(pkg.ID))] = available
		}
	}
	return updates
}

func parseStorePackageTable(output string) []Package {
	lines := strings.Split(output, "\n")
	headerSeen := false
	var packages []Package
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || isStoreSearchNoiseLine(line) {
			continue
		}
		if !headerSeen {
			if isStoreTableHeader(line) {
				headerSeen = true
			}
			continue
		}
		if isStoreDividerLine(line) {
			continue
		}
		cols := splitStoreColumns(line)
		if len(cols) < 2 {
			continue
		}
		name := strings.TrimSpace(cols[0])
		if name == "" || strings.HasPrefix(name, "[") || isStoreSearchNoiseLine(name) {
			continue
		}
		id := name
		version := ""
		available := ""
		if len(cols) > 1 {
			if looksLikePackageID(cols[1]) {
				id = cols[1]
			}
			for i := 1; i < len(cols); i++ {
				if looksLikeVersion(cols[i]) {
					if version == "" {
						version = cols[i]
					} else if available == "" {
						available = cols[i]
					}
				}
			}
		}
		packages = append(packages, Package{
			ID:               id,
			Name:             name,
			Version:          version,
			AvailableVersion: available,
			Manager:          managerStore,
			Source:           sourceStoreCLI,
			UpdateSupported:  true,
			ActionBackend:    backendStoreCLI,
		})
	}
	return packages
}

func isStoreTableHeader(line string) bool {
	cols := splitStoreColumns(line)
	if len(cols) < 2 {
		return false
	}
	hasName := false
	hasKnownColumn := false
	for _, col := range cols {
		normalized := strings.ToLower(strings.TrimSpace(col))
		switch normalized {
		case "name", "app", "application":
			hasName = true
		case "id", "product id", "package id", "publisher", "version", "current", "available", "status", "price":
			hasKnownColumn = true
		}
	}
	return hasName && hasKnownColumn
}

func splitStoreColumns(line string) []string {
	line = normalizeStoreTableDelimiters(line)
	if strings.Contains(line, "|") {
		parts := strings.Split(line, "|")
		cols := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				cols = append(cols, part)
			}
		}
		return cols
	}
	return splitPackageTableColumns(line)
}

func normalizeStoreTableDelimiters(line string) string {
	return strings.NewReplacer(
		"\u2502", "|",
		"\u2503", "|",
		"\u2500", "-",
		"\u250c", "-",
		"\u2510", "-",
		"\u2514", "-",
		"\u2518", "-",
		"\u251c", "-",
		"\u2524", "-",
		"\u252c", "-",
		"\u2534", "-",
		"\u253c", "-",
	).Replace(line)
}

func isStoreSearchNoiseLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	normalized := normalizePackageIdentity(trimmed)
	if strings.Contains(normalized, "searchresultsfor") ||
		strings.HasPrefix(normalized, "resultsfor") ||
		strings.Contains(lower, "no results") {
		return true
	}
	return isStoreDividerLine(trimmed)
}

func isStoreDividerLine(line string) bool {
	line = strings.TrimSpace(normalizeStoreTableDelimiters(line))
	if line == "" {
		return true
	}
	nonDivider := 0
	for _, r := range line {
		if r == '-' || r == '_' || r == '=' || r == ' ' || r == '\t' {
			continue
		}
		nonDivider++
	}
	return nonDivider == 0
}

func looksLikeVersion(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || !strings.Contains(value, ".") {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && r != '.' && r != '-' {
			return false
		}
	}
	return true
}

func looksLikePackageID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.Contains(value, ".") || strings.Contains(value, "_") {
		return true
	}
	return packageIDPattern.MatchString(value) && strings.ToUpper(value) == value && len(value) >= 8
}

func storeSearch(query string) ([]Package, CommandResult) {
	result := runCommand(90*time.Second, managerCommand(managerStore, "search", query)...)
	return parseStoreSearch(result.Stdout + "\n" + result.Stderr), result
}

func storeUpdates() (map[string]string, CommandResult) {
	result := runCommand(120*time.Second, managerCommand(managerStore, "updates")...)
	return parseStoreUpdates(result.Stdout + "\n" + result.Stderr), result
}
