package main

const pageScriptThemeAndLabels = `
  function setTheme(theme){
    document.documentElement.dataset.theme = theme;
    try{ localStorage.setItem("windows-updater-theme", theme); }catch(e){}
    var button = $("theme-toggle");
    if(button){ button.innerHTML = icon(theme === "dark" ? "sun" : "moon") + '<span>' + (theme === "dark" ? "Light Mode" : "Dark Mode") + '</span>'; }
  }
  function currentTheme(){
    return document.documentElement.dataset.theme === "light" ? "light" : "dark";
  }
  function setText(id, value){
    var node = $(id);
    if(node){ node.textContent = value; }
  }
  function managerLabel(value){
    var labels = {
      choco: "Chocolatey",
      winget: "winget",
      store: "Store"
    };
    return labels[value] || value;
  }
  function backendLabel(value){
    var labels = {
      "appx-inventory": "AppX inventory",
      "store-cli": "Store CLI",
      "store-cli-resolved": "Store resolved",
      "winget-msstore-fallback": "winget Store fallback"
    };
    return labels[value] || value;
  }
  function allowUnknownVersionUpdates(){
    var control = $("update-allow-unknown");
    return !!control && !!control.checked;
  }
  function allowPinnedUpdates(){
    var control = $("update-allow-pinned");
    return !!control && !!control.checked;
  }
  function appendGlobalUpdateOptions(params){
    params.delete("allow_unknown_version");
    params.delete("allow_pinned");
    if(allowUnknownVersionUpdates()){ params.set("allow_unknown_version", "true"); }
    if(allowPinnedUpdates()){ params.set("allow_pinned", "true"); }
    return params;
  }
  function packageAutoUpdateable(pkg){
    return pkg.update_supported !== false && !pkg.unknown_version;
  }
  function packageBulkUpdateable(pkg){
    return !!pkg.update_available && pkg.update_supported !== false && (!pkg.unknown_version || allowUnknownVersionUpdates());
  }
`
