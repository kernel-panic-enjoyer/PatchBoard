package updater

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// remainingFunctionVariableHooks are the package-level function variables
// production code still has. Tests swap them; that is global state, so the
// list may only shrink. Update-job steps live on App (updateJobDeps) and scan
// readers are passed in (applicationScanSources). The package-action seams
// run inside the elevated worker too, where there is no App; the rest are
// operating-system seams of install, task, Store and folder code.
var remainingFunctionVariableHooks = map[string]bool{
	"applicationInstallCurrentExecutable":     true,
	"applicationInstallPathsProvider":         true,
	"applicationInstallRunner":                true,
	"applicationInstalledAppsEntryExists":     true,
	"applicationRestartInstalledRunner":       true,
	"createApplicationStartMenuShortcut":      true,
	"createAutoUpdateTaskRunner":              true,
	"cryptoRandomRead":                        true,
	"currentUserLocalAppDataProvider":         true,
	"defaultStoreExactCatalogProvider":        true,
	"deleteApplicationInstalledAppsEntry":     true,
	"deleteTaskRunner":                        true,
	"desktopShortcutDirectories":              true,
	"elevatedWorkerBatchInvocationRunner":     true,
	"inventoryGetter":                         true,
	"launchInstalledApplicationExecutable":    true,
	"machineFoldersProvider":                  true,
	"openStoreTransactionalStoreForInventory": true,
	"packageActionCommandRunner":              true,
	"packageActionManagerAvailable":           true,
	"packageActionRetryWait":                  true,
	"readAppExecutionAliasForPath":            true,
	"runStoreTransactionalScanForInventory":   true,
	"scheduleApplicationUninstallSelfDelete":  true,
	"startupRunEntryRunner":                   true,
	"storePackagedAppInventoryProvider":       true,
	"storeScanCurrentUserSID":                 true,
	"storeScanNow":                            true,
	"storeScanSystemContextProvider":          true,
	"writeApplicationInstalledAppsEntry":      true,
}

func TestNoNewFunctionVariableHooks(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	var parsed []*ast.File
	funcNames := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fileSet, name, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		parsed = append(parsed, file)
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				funcNames[fn.Name.Name] = true
			}
		}
	}
	isFuncValue := func(expr ast.Expr) bool {
		switch value := expr.(type) {
		case *ast.FuncLit:
			return true
		case *ast.Ident:
			return funcNames[value.Name]
		}
		return false
	}
	var found []string
	for _, file := range parsed {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				valueSpec := spec.(*ast.ValueSpec)
				_, typedFunc := valueSpec.Type.(*ast.FuncType)
				for i, name := range valueSpec.Names {
					hook := typedFunc
					if i < len(valueSpec.Values) && isFuncValue(valueSpec.Values[i]) {
						hook = true
					}
					if hook {
						found = append(found, name.Name)
					}
				}
			}
		}
	}
	sort.Strings(found)
	for _, name := range found {
		if !remainingFunctionVariableHooks[name] {
			t.Errorf("new package-level function variable %s: pass the dependency in (an App field or a parameter) instead", name)
		}
	}
}
