package updater

const pageScriptBoot = `
  setTheme(currentTheme());
  loadStatus(false);
  startEventStream();
  loadPackages(false).then(function(){ checkActiveUpdateJob(); });
  loadLogs();
  var query = new URLSearchParams(window.location.search).get("q");
  if(query){
    $("search-input").value = query;
    loadSearch(query);
  }
})();
`
