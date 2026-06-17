package main

const pageScriptDataLoading = `
  async function loadStatus(force){
    try{
      var data = await (await fetch(api("/api/status", force ? {refresh:"1"} : {}))).json();
      renderStatus(data);
      if(data.loading){ setTimeout(function(){ loadStatus(false); }, 800); }
    }catch(e){ showNotice("Could not load status: " + e.message); }
  }
  async function loadPackages(force){
    try{
      var data = await (await fetch(api("/api/packages", force ? {refresh:"1"} : {}))).json();
      renderPackages(data);
      if(data.loading){ setTimeout(function(){ loadPackages(false); }, 900); }
      return data;
    }catch(e){ showNotice("Could not load packages: " + e.message); }
  }
  async function refreshPackagesAfterUpdate(refreshAlreadyStarted){
    var data = await loadPackages(!refreshAlreadyStarted);
    while(data && data.loading){
      await new Promise(function(resolve){ setTimeout(resolve, 900); });
      data = await loadPackages(false);
    }
    return data;
  }
  async function runUpdateRequest(path, params, keys, message){
    setGlobalProgress(true, message || "Updating packages...");
    setUpdateBusy(true, keys);
    showNotice(message || "Updating packages...", true);
    try{
      var response = await postForm(path, params);
      var payload = await response.json();
      if(!response.ok && !payload.result && !payload.results){
        throw new Error(payload.error || "Update failed");
      }
      showNotice(summarizeUpdatePayload(payload));
      if(payload.refresh_started){
        setGlobalProgress(true, "Refreshing package status...");
        showNotice("Refreshing package status...", true);
        await refreshPackagesAfterUpdate(true);
      }
    }catch(e){
      showNotice("Update failed: " + e.message);
    }finally{
      setUpdateBusy(false, []);
      setGlobalProgress(false);
    }
  }
`
