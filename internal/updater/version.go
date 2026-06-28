package updater

import (
	"strconv"
	"strings"
)

var appVersion = "0.0.0-dev"

func currentAppVersion() string {
	version := strings.TrimSpace(appVersion)
	if version == "" {
		return "0.0.0-dev"
	}
	return version
}

func normalizeAppVersion(version string) (string, bool) {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	if version == "" {
		return "", false
	}
	if index := strings.IndexAny(version, "-+"); index >= 0 {
		version = version[:index]
	}
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "", false
	}
	normalized := make([]string, 3)
	for index, part := range parts {
		if part == "" {
			return "", false
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return "", false
		}
		normalized[index] = strconv.Itoa(value)
	}
	return strings.Join(normalized, "."), true
}

func compareAppVersions(left, right string) int {
	leftParts, leftOK := parseAppVersionParts(left)
	rightParts, rightOK := parseAppVersionParts(right)
	if !leftOK && !rightOK {
		return 0
	}
	if !leftOK {
		return -1
	}
	if !rightOK {
		return 1
	}
	for index := range leftParts {
		if leftParts[index] > rightParts[index] {
			return 1
		}
		if leftParts[index] < rightParts[index] {
			return -1
		}
	}
	return 0
}

func parseAppVersionParts(version string) ([3]int, bool) {
	normalized, ok := normalizeAppVersion(version)
	if !ok {
		return [3]int{}, false
	}
	var parts [3]int
	for index, part := range strings.Split(normalized, ".") {
		value, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}, false
		}
		parts[index] = value
	}
	return parts, true
}
