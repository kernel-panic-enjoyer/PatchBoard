package main

const pageScriptLogConsole = `
  function formatLogEntry(entry){
    var stamp = entry.timestamp || "";
    if(stamp){
      var date = new Date(stamp);
      stamp = isNaN(date.getTime()) ? stamp : date.toLocaleTimeString();
    }
    var stream = (entry.stream || "app").toUpperCase();
    return "[" + stamp + "] " + stream + " " + (entry.message || "");
  }
  function renderLogLines(shouldScroll){
    var target = $("session-log");
    if(!target){ return; }
    target.textContent = logLines.join("\n") + (logLines.length ? "\n" : "");
    if(shouldScroll){
      target.scrollTop = target.scrollHeight;
    }
  }
  function appendLogEntries(entries){
    if(!entries || entries.length === 0){ return; }
    entries.forEach(function(entry){
      lastLogID = Math.max(lastLogID, Number(entry.id || 0));
      logLines.push(formatLogEntry(entry));
    });
    if(logLines.length > maxLogLines){
      logLines = logLines.slice(logLines.length - maxLogLines);
    }
    var auto = $("log-autoscroll");
    renderLogLines(!auto || auto.checked);
  }
  async function loadLogs(){
    try{
      var data = await (await fetch(api("/api/logs", {since:String(lastLogID)}))).json();
      appendLogEntries(data.entries || []);
      if(typeof data.latest_id === "number" && data.latest_id > lastLogID && (!data.entries || data.entries.length === 0)){
        lastLogID = data.latest_id;
      }
    }catch(e){}
  }
  async function copyLogView(){
    var target = $("session-log");
    var text = target ? target.textContent || "" : "";
    try{
      if(navigator.clipboard && navigator.clipboard.writeText){
        await navigator.clipboard.writeText(text);
      }else{
        var textarea = document.createElement("textarea");
        textarea.value = text;
        textarea.setAttribute("readonly", "");
        textarea.style.position = "fixed";
        textarea.style.left = "-9999px";
        document.body.appendChild(textarea);
        textarea.select();
        document.execCommand("copy");
        document.body.removeChild(textarea);
      }
      showNotice("Session log copied.");
    }catch(e){
      showNotice("Could not copy session log: " + e.message);
    }
  }
`
