package main

import (
	"regexp"
	"strings"
)

var packageTableColumnSpacing = regexp.MustCompile(`\s{2,}`)

func splitPackageTableColumns(line string) []string {
	return packageTableColumnSpacing.Split(strings.TrimSpace(line), -1)
}
