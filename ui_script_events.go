package main

const pageScriptEvents = `
  document.addEventListener("click", function(event){
    var autoButton = event.target.closest(".auto-package");
    if(autoButton){
      setPackageAuto(autoButton.dataset.key, autoButton.dataset.enabled !== "true", autoButton);
    }
  });
  document.addEventListener("submit", function(event){
    var form = event.target;
    if(form.id === "search-form"){
      event.preventDefault();
      var query = String($("search-input").value || "").trim();
      if(!query){
        showNotice("Enter a package name to search.");
        return;
      }
      var url = new URL(window.location.href);
      url.searchParams.set("q", query);
      window.history.replaceState(null, "", url.toString());
      loadSearch(query);
      return;
    }
    if(form.matches(".install-form")){
      event.preventDefault();
      installFromForm(form);
      return;
    }
    if(form.matches(".manager-install-form")){
      event.preventDefault();
      installManagerFromForm(form);
      return;
    }
    if(form.matches(".update-form")){
      event.preventDefault();
      var key = form.dataset.key;
      if(form.dataset.unknownVersion === "true"){
        var allowed = form.querySelector('input[name="allow_unknown_version"]');
        if(!allowed || !allowed.checked){
          showNotice("This package has an unknown installed version. Check the confirmation box before updating it.");
          return;
        }
      }
      runUpdateRequest("/api/update", new URLSearchParams(new FormData(form)), [key], "Updating package...");
      return;
    }
    if(form.id === "update-selected-form"){
      event.preventDefault();
      var params = new URLSearchParams(new FormData(form));
      var keys = params.getAll("package_key");
      if(keys.length === 0){
        showNotice("Select at least one package to update.");
        return;
      }
      startUpdateJob(params, keys, "Updating selected packages...");
      return;
    }
    if(form.matches(".update-all-form")){
      event.preventDefault();
      var allKeys = updateableUpdateKeys();
      startUpdateJob(new URLSearchParams(new FormData(form)), allKeys, "Updating all packages...");
    }
  });
  $("theme-toggle").addEventListener("click", function(){
    var next = currentTheme() === "dark" ? "light" : "dark";
    setTheme(next);
    postForm("/api/settings/theme", {theme:next}).catch(function(){});
  });
  $("scan-button").addEventListener("click", async function(){
    var button = this;
    button.disabled = true;
    showNotice("Scanning applications...");
    try{
      var response = await postForm("/api/scan", {});
      var data = await response.json();
      if(!response.ok){ throw new Error(data.error || "Scan failed"); }
      renderScan(data);
      if(data.errors && data.errors.length){
        showNotice("Application scan completed with errors. Review Scan Results for details.");
      }else{
        showNotice("Application scan completed.");
      }
    }catch(e){ showNotice("Scan failed: " + e.message); }
    button.disabled = false;
  });
  $("refresh-packages").addEventListener("click", function(){ loadPackages(true); });
  $("installed-search").addEventListener("input", function(){
    installedSearchQuery = this.value || "";
    installedPage = 1;
    renderInstalledTable(false);
  });
  $("installed-prev").addEventListener("click", function(){
    installedPage--;
    renderInstalledTable(false);
  });
  $("installed-next").addEventListener("click", function(){
    installedPage++;
    renderInstalledTable(false);
  });
  $("startup-toggle").addEventListener("click", async function(){
    var button = this;
    var enabled = this.dataset.enabled !== "true";
    button.disabled = true;
    try{
      await postCommandPayload("/api/settings/startup", {enabled:enabled ? "true" : "false"}, "Could not update startup setting");
      showNotice("Startup setting updated.");
    }catch(e){
      showNotice("Could not update startup setting: " + e.message);
    }finally{
      button.disabled = false;
      loadStatus(true);
    }
  });
  $("auto-global-toggle").addEventListener("click", async function(){
    var button = this;
    var enabled = this.dataset.enabled !== "true";
    button.disabled = true;
    try{
      await postCommandPayload("/api/settings/auto-update", {global:enabled ? "true" : "false"}, "Could not update auto-update setting");
      showNotice("Auto-update setting updated.");
    }catch(e){
      showNotice("Could not update auto-update setting: " + e.message);
    }finally{
      button.disabled = false;
      loadStatus(true);
    }
  });
  $("auto-all").addEventListener("click", function(){ setAllAuto(true); });
  $("auto-none").addEventListener("click", function(){ setAllAuto(false); });
  $("clear-log-view").addEventListener("click", function(){
    logLines = [];
    renderLogLines(false);
  });
  $("copy-log-view").addEventListener("click", function(){ copyLogView(); });
  $("cancel-updates-button").addEventListener("click", function(){ cancelUpdateJob(); });
`
