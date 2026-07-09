package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type packageMutationOptions struct {
	RemoveNewDesktopShortcuts bool
}

type packageMutationOptionsContextKey struct{}

var desktopShortcutDirectories = defaultDesktopShortcutDirectories

func withPackageMutationOptions(ctx context.Context, options packageMutationOptions) context.Context {
	return context.WithValue(ctx, packageMutationOptionsContextKey{}, options)
}

func packageMutationOptionsFromContext(ctx context.Context) packageMutationOptions {
	if ctx == nil {
		return packageMutationOptions{}
	}
	options, _ := ctx.Value(packageMutationOptionsContextKey{}).(packageMutationOptions)
	return options
}

func packageMutationOptionsFromState(state State) packageMutationOptions {
	return packageMutationOptions{RemoveNewDesktopShortcuts: state.RemoveNewDesktopShortcuts}
}

func runPackageMutationWithDesktopShortcutCleanup(ctx context.Context, actionLabel string, action func() CommandResult) CommandResult {
	if action == nil {
		return validationCommandResult(actionLabel, fmt.Errorf("package action is nil"))
	}
	if !packageMutationOptionsFromContext(ctx).RemoveNewDesktopShortcuts {
		return action()
	}
	before, beforeDiagnostics := snapshotDesktopShortcuts()
	result := action()
	cleanupSummary := cleanupNewDesktopShortcuts(before)
	diagnostics := append(beforeDiagnostics, cleanupSummary.Diagnostics...)
	if len(diagnostics) > 0 {
		appendDesktopShortcutCleanupDiagnostics(ctx, diagnostics)
		result.Stderr = appendCommandOutput(result.Stderr, strings.Join(diagnostics, "\n"))
	}
	if len(cleanupSummary.Removed) > 0 {
		message := fmt.Sprintf("Removed %d newly-created desktop shortcut(s).", len(cleanupSummary.Removed))
		appendLogLineContext(ctx, "app", message, []string{logCategoryApplication})
		result.Stdout = appendCommandOutput(result.Stdout, message)
	}
	return result
}

type desktopShortcutCleanupSummary struct {
	Removed     []string
	Diagnostics []string
}

func snapshotDesktopShortcuts() (map[string]bool, []string) {
	shortcuts := map[string]bool{}
	var diagnostics []string
	for _, directory := range desktopShortcutDirectories() {
		entries, err := os.ReadDir(directory)
		if err != nil {
			if !os.IsNotExist(err) {
				diagnostics = append(diagnostics, fmt.Sprintf("Could not inspect desktop shortcuts in %s: %s", directory, err))
			}
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".lnk") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				diagnostics = append(diagnostics, fmt.Sprintf("Could not inspect desktop shortcut %s: %s", filepath.Join(directory, entry.Name()), err))
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}
			shortcuts[canonicalShortcutPath(filepath.Join(directory, entry.Name()))] = true
		}
	}
	return shortcuts, diagnostics
}

func cleanupNewDesktopShortcuts(before map[string]bool) desktopShortcutCleanupSummary {
	after, diagnostics := snapshotDesktopShortcuts()
	summary := desktopShortcutCleanupSummary{Diagnostics: diagnostics}
	var newShortcutPaths []string
	for shortcutPath := range after {
		if !before[shortcutPath] {
			newShortcutPaths = append(newShortcutPaths, shortcutPath)
		}
	}
	sort.Strings(newShortcutPaths)
	for _, shortcutPath := range newShortcutPaths {
		if err := os.Remove(shortcutPath); err != nil {
			summary.Diagnostics = append(summary.Diagnostics, fmt.Sprintf("Could not remove newly-created desktop shortcut %s: %s", shortcutPath, err))
			continue
		}
		summary.Removed = append(summary.Removed, shortcutPath)
		appLog("Removed newly-created desktop shortcut %s.", shortcutPath)
	}
	return summary
}

func appendDesktopShortcutCleanupDiagnostics(ctx context.Context, diagnostics []string) {
	for _, diagnostic := range diagnostics {
		if strings.TrimSpace(diagnostic) == "" {
			continue
		}
		appendLogLineContext(ctx, "app", diagnostic, []string{logCategoryApplication})
	}
}

func appendCommandOutput(existing, extra string) string {
	existing = strings.TrimSpace(existing)
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return existing
	}
	if existing == "" {
		return extra
	}
	return existing + "\n" + extra
}

func canonicalShortcutPath(path string) string {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		absolutePath = path
	}
	return strings.ToLower(filepath.Clean(absolutePath))
}

func defaultDesktopShortcutDirectories() []string {
	var directories []string
	if homeDirectory, err := os.UserHomeDir(); err == nil && strings.TrimSpace(homeDirectory) != "" {
		directories = append(directories, filepath.Join(homeDirectory, "Desktop"))
	}
	if publicDirectory := strings.TrimSpace(os.Getenv("PUBLIC")); publicDirectory != "" {
		directories = append(directories, filepath.Join(publicDirectory, "Desktop"))
	}
	return uniqueDesktopShortcutDirectories(directories)
}

func uniqueDesktopShortcutDirectories(directories []string) []string {
	seen := map[string]bool{}
	var unique []string
	for _, directory := range directories {
		trimmedDirectory := strings.TrimSpace(directory)
		if trimmedDirectory == "" {
			continue
		}
		key := canonicalShortcutPath(trimmedDirectory)
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, trimmedDirectory)
	}
	return unique
}
