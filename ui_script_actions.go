package main

const pageScriptActions = `
  async function loadSearch(query){
    var body = $("search-results-body");
    $("search-results-panel").classList.remove("hidden");
    body.innerHTML = '<tr><td colspan="5">Searching...</td></tr>';
    try{
      var response = await fetch(api("/api/search", {q:query}));
      var data = await response.json();
      if(!response.ok){ throw new Error(data.error || "Search failed"); }
      renderSearch(data);
    }catch(e){
      body.innerHTML = '<tr><td colspan="5">' + html(e.message) + '</td></tr>';
    }
  }
  async function installFromForm(form){
    var button = form.querySelector("button");
    if(button){ button.disabled = true; }
    showNotice("Installing package...");
    try{
      var response = await postForm("/api/install", new URLSearchParams(new FormData(form)));
      var payload = await response.json();
      if(!response.ok){ throw new Error(payload.error || "Install failed"); }
      showNotice(resultNotice("Install command completed. Refreshing package status...", "Install finished with errors", payload.result));
      await refreshPackagesAfterUpdate(!!payload.refresh_started);
    }catch(e){
      showNotice("Install failed: " + e.message);
    }finally{
      if(button){ button.disabled = false; }
    }
  }
  async function installManagerFromForm(form){
    var button = form.querySelector("button");
    if(button){ button.disabled = true; }
    showNotice("Opening package manager install action...");
    try{
      var response = await postForm("/api/managers/install", new URLSearchParams(new FormData(form)));
      var payload = await response.json();
      if(!response.ok){ throw new Error(payload.error || "Package manager install failed"); }
      showNotice(resultNotice("Package manager install action completed.", "Package manager install finished with errors", payload.result));
      loadStatus(true);
    }catch(e){
      showNotice("Package manager install failed: " + e.message);
    }finally{
      if(button){ button.disabled = false; }
    }
  }
  async function setPackageAuto(key, enabled, button){
    button.disabled = true;
    var params = new URLSearchParams();
    params.append("package_key", key);
    params.set("package_enabled", enabled ? "true" : "false");
    try{
      await postCommandPayload("/api/settings/auto-update", params, "Could not update auto setting");
      button.dataset.enabled = enabled ? "true" : "false";
      button.innerHTML = '<span>' + (enabled ? 'On' : 'Off') + '</span>';
      showNotice("Auto-update setting updated.");
    }catch(e){
      showNotice("Could not update auto setting: " + e.message);
      loadStatus(true);
      loadPackages(true);
    }
    button.disabled = false;
  }
  async function setAllAuto(enabled){
    var params = new URLSearchParams();
    params.set("global", enabled ? "true" : "false");
    params.set("package_enabled", enabled ? "true" : "false");
    packages.forEach(function(pkg){ if(pkg.update_supported !== false && !pkg.unknown_version){ params.append("package_key", pkg.key); } });
    showNotice("Updating auto-update settings...");
    try{
      await postCommandPayload("/api/settings/auto-update", params, "Could not update auto-update settings");
      showNotice("Auto-update settings updated.");
    }catch(e){
      showNotice("Could not update auto-update settings: " + e.message);
    }finally{
      loadStatus(true);
      loadPackages(true);
    }
  }
`
