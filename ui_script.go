package main

const pageJS = pageScriptShell +
	pageScriptLogConsole +
	pageScriptRequests +
	pageScriptThemeAndLabels +
	pageScriptStatusRender +
	pageScriptPackageRender +
	pageScriptAuxiliaryRender +
	pageScriptDataLoading +
	pageScriptUpdateJobs +
	pageScriptActions +
	pageScriptEvents +
	pageScriptBoot

const pageScriptShell = `
(function(){
  var token = document.body.dataset.token || "";
  var packages = [];
  var updateBusy = false;
  var installedPage = 1;
  var installedPageSize = 10;
  var installedSearchQuery = "";
  var lastLogID = 0;
  var logLines = [];
  var maxLogLines = 2000;
  var managersRendered = false;
  var updateJobPollTimer = null;
  var activeUpdateKeys = [];
  var activeUpdateJobID = "";
  var latestStatus = null;
  var latestPackagesLoading = true;
  function $(id){ return document.getElementById(id); }
  function api(path, params){
    var url = new URL(path, window.location.origin);
    url.searchParams.set("token", token);
    Object.keys(params || {}).forEach(function(key){ url.searchParams.set(key, params[key]); });
    return url.toString();
  }
  function html(value){
    return String(value == null ? "" : value).replace(/[&<>"']/g, function(ch){
      return {"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[ch];
    });
  }
  function attr(value){ return html(value); }
  function icon(name){
    var paths = {
      moon:'<path d="M12 3a6 6 0 1 0 6 6c0 5-4 9-9 9a6 6 0 0 0 3-15Z"/>',
      sun:'<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.9 4.9 1.4 1.4"/><path d="m17.7 17.7 1.4 1.4"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m4.9 19.1 1.4-1.4"/><path d="m17.7 6.3 1.4-1.4"/>',
      refresh:'<path d="M21 12a9 9 0 0 1-15.5 6.2"/><path d="M3 12a9 9 0 0 1 15.5-6.2"/><path d="M3 18v-6h6"/><path d="M21 6v6h-6"/>',
      update:'<path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M5 21h14"/>',
      install:'<path d="M12 5v14"/><path d="M5 12h14"/>',
      check:'<path d="m5 12 4 4L19 6"/>',
      alert:'<path d="M12 9v4"/><path d="M12 17h.01"/><path d="M10.3 4.3 2.5 18a2 2 0 0 0 1.7 3h15.6a2 2 0 0 0 1.7-3L13.7 4.3a2 2 0 0 0-3.4 0Z"/>',
      box:'<path d="M4 7h16"/><path d="M6 7v12h12V7"/><path d="M9 11h6"/>'
    };
    return '<span class="button-icon" aria-hidden="true"><svg viewBox="0 0 24 24">' + (paths[name] || paths.box) + '</svg></span>';
  }
  function showNotice(message){
    var notice = $("notice");
    if(!notice){ return; }
    notice.textContent = message || "";
    notice.classList.toggle("hidden", !message);
  }
`
