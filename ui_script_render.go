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
  function packageBulkUpdateable(pkg){
    return !!pkg.update_available && pkg.update_supported !== false && !pkg.unknown_version;
  }
`
