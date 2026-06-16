package main

const pageScriptPackageRender = `
  function packageNameCell(pkg){
    var secondary = pkg.action_backend === "appx-inventory" ? "Store app" : pkg.id;
    if(pkg.unknown_version){
      secondary += " - unknown installed version";
    }
    return '<strong>' + html(pkg.name) + '</strong><br><span class="muted">' + html(secondary) + '</span>';
  }
	function managerCell(pkg){
		var backend = pkg.action_backend ? '<br><span class="muted">' + html(backendLabel(pkg.action_backend)) + '</span>' : '';
		return '<span class="badge manager-badge">' + html(managerLabel(pkg.manager)) + '</span>' + backend;
	}
  function autoButton(pkg){
    if(pkg.update_supported === false){
      return '<span class="muted">N/A</span>';
    }
    if(pkg.unknown_version){
      return '<span class="muted">Explicit only</span>';
    }
    return '<button class="auto-package toggle-button" type="button" data-key="' + attr(pkg.key) + '" data-enabled="' + (pkg.auto_update ? 'true' : 'false') + '"' + (updateBusy ? ' disabled' : '') + '><span>' + (pkg.auto_update ? 'On' : 'Off') + '</span></button>';
  }
	function updateForm(pkg){
		if(pkg.update_supported === false){
			return '<span class="muted">Inventory only</span>';
		}
    var unknownConfirm = pkg.unknown_version ? '<label class="check-control unknown-confirm"><input type="checkbox" name="allow_unknown_version" value="true"' + (updateBusy ? ' disabled' : '') + '> Allow unknown version update</label>' : '';
    var pinnedConfirm = pkg.manager === "winget" ? '<label class="check-control pinned-confirm"><input type="checkbox" name="allow_pinned" value="true"' + (updateBusy ? ' disabled' : '') + '> Allow pinned update</label>' : '';
		return '<form class="update-form" data-key="' + attr(pkg.key) + '" data-unknown-version="' + (pkg.unknown_version ? 'true' : 'false') + '" method="post" action="/api/update"><input type="hidden" name="token" value="' + attr(token) + '"><input type="hidden" name="manager" value="' + attr(pkg.manager) + '"><input type="hidden" name="package_id" value="' + attr(pkg.id) + '">' + unknownConfirm + pinnedConfirm + '<button type="submit"' + (updateBusy ? ' disabled' : '') + '>' + icon("update") + '<span>' + (pkg.unknown_version ? 'Update Anyway' : 'Update') + '</span></button><div class="row-progress hidden"><div class="progress-bar"><span></span></div></div></form>';
	}
	function installedAction(pkg){
		if(pkg.action_backend === "store-cli-resolved" && pkg.update_available){
			return updateForm(pkg);
		}
		return '<span class="muted">-</span>';
	}
  function packageMatchesInstalledSearch(pkg){
    var query = installedSearchQuery.trim().toLowerCase();
    if(!query){ return true; }
    return [pkg.name, pkg.id, pkg.manager, pkg.version, pkg.available_version].some(function(value){
      return String(value || "").toLowerCase().indexOf(query) !== -1;
    });
  }
  function renderUpdatesTable(updates, loading){
    var target = $("updates-body");
    if(!target){ return; }
    if(updates.length === 0){
      target.innerHTML = '<tr><td colspan="7">' + (loading ? 'Checking for updates...' : 'No updates available.') + '</td></tr>';
      return;
    }
    target.innerHTML = updates.map(function(pkg){
      var selectable = packageBulkUpdateable(pkg);
      return '<tr data-key="' + attr(pkg.key) + '"><td><input form="update-selected-form" type="checkbox" name="package_key" value="' + attr(pkg.key) + '"' + ((updateBusy || !selectable) ? ' disabled' : '') + '></td><td>' + packageNameCell(pkg) + '</td><td>' + managerCell(pkg) + '</td><td>' + html(pkg.version) + '</td><td>' + html(pkg.available_version) + '</td><td>' + autoButton(pkg) + '</td><td>' + updateForm(pkg) + '</td></tr>';
    }).join("");
  }
  function renderInstalledTable(loading){
    var target = $("packages-body");
    var status = $("installed-page-status");
    var prev = $("installed-prev");
    var next = $("installed-next");
    if(!target){ return; }
    var visiblePackages = packages.filter(packageMatchesInstalledSearch);
    var total = visiblePackages.length;
    var totalPages = Math.max(1, Math.ceil(total / installedPageSize));
    if(installedPage > totalPages){ installedPage = totalPages; }
    if(installedPage < 1){ installedPage = 1; }
	if(total === 0){
		target.innerHTML = '<tr><td colspan="7">' + (loading ? 'Loading packages...' : (installedSearchQuery ? 'No packages match your filter.' : 'No managed packages found.')) + '</td></tr>';
      if(status){ status.textContent = loading ? 'Loading...' : (installedSearchQuery ? 'No matches' : 'No packages'); }
      if(prev){ prev.disabled = true; }
      if(next){ next.disabled = true; }
      return;
    }
    var start = (installedPage - 1) * installedPageSize;
    var visible = visiblePackages.slice(start, start + installedPageSize);
	target.innerHTML = visible.map(function(pkg){
		var rowStatus = pkg.update_supported === false ? '<span class="badge">Inventory only</span>' : (pkg.unknown_version && pkg.update_available ? '<span class="badge warn">Explicit update</span>' : (pkg.update_available ? '<span class="badge warn">Update</span>' : '<span class="badge ok">Current</span>'));
		return '<tr data-key="' + attr(pkg.key) + '"><td>' + packageNameCell(pkg) + '</td><td>' + managerCell(pkg) + '</td><td>' + html(pkg.version) + '</td><td>' + html(pkg.available_version) + '</td><td>' + rowStatus + '</td><td>' + autoButton(pkg) + '</td><td>' + installedAction(pkg) + '</td></tr>';
	}).join("");
    if(status){
      status.textContent = "Showing " + (start + 1) + "-" + Math.min(start + installedPageSize, total) + " of " + total + (installedSearchQuery ? " matches" : "");
    }
    if(prev){ prev.disabled = installedPage <= 1; }
    if(next){ next.disabled = installedPage >= totalPages; }
  }
  function renderPackages(data){
    renderManagers(data);
    packages = data.packages || [];
    latestPackagesLoading = !!data.loading;
    var updates = packages.filter(function(pkg){ return !!pkg.update_available; });
    var updateablePackages = packages.filter(function(pkg){ return pkg.update_supported !== false && !pkg.unknown_version; });
    $("auto-all").disabled = updateBusy || updateablePackages.length === 0;
    $("auto-none").disabled = updateBusy || updateablePackages.length === 0;
    renderUpdatesTable(updates, !!data.loading);
    renderInstalledTable(!!data.loading);
    var supportedUpdates = updates.filter(packageBulkUpdateable);
    $("update-all-button").disabled = updateBusy || supportedUpdates.length === 0;
    $("update-selected-button").disabled = updateBusy || supportedUpdates.length === 0;
    renderDashboardSummary();
  }
`
