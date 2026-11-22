package updater

import "context"

func chocoPackageCommand(chocoAction, packageID string) []string {
	args := []string{chocoAction, packageID, "-y", "--no-progress", "--no-color"}
	if chocoAction == "upgrade" {
		// Without this, "choco upgrade" installs a package that is not
		// installed, so trying a variant name such as foo.install could add
		// a package the user never had.
		args = append(args, "--fail-on-not-installed")
	}
	return managerCommand(managerChoco, args...)
}

func runChocoUpgradePackageWithFallbackContext(ctx context.Context, pkg Package) CommandResult {
	return runPackageUpdateCandidates(ctx, chocoUpdateTargetCandidates(pkg), "choco target", func(candidatePackageID string) CommandResult {
		return runPackageActionCommand(ctx, managerChoco, packageActionTimeout, chocoPackageCommand("upgrade", candidatePackageID)...)
	})
}
