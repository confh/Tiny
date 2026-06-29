const ids = [
  "status", "targetName", "targetMeta", "alloc", "sys", "goroutines", "procs",
  "tasks", "taskIdle", "targetStatus", "gcRuns", "target", "observer",
  "runtime", "program", "barAlloc", "barHeap", "barNextGC", "chart",
  "gomaxprocsInput", "gcPercentInput", "functionsTable", "globalsTable",
  "commandsTable", "exposedTable", "commandResult", "messagesTable",
  "eventsTable", "messageInput", "functionFilter", "globalFilter", "eventFilter",
  "connectionEndpoint", "connectionPassword", "connectButton", "connectionStatus"
];

const el = Object.fromEntries(ids.map((id) => [id, document.getElementById(id)]));
const defaultEndpoint = window.__OBSERVER_DEFAULT_ENDPOINT__ || "http://127.0.0.1:4040";
const defaultPassword = window.__OBSERVER_DEFAULT_PASSWORD__ || "tiny";
let endpoint = normalizeEndpoint(readSetting("observer.endpoint", defaultEndpoint));
let password = readSetting("observer.password", defaultPassword);
const history = [];
let controlsTouched = false;
let paused = false;
let editingGlobal = null;
let globalsLockedUntil = 0;
const commandPayloads = new Map();
let activeCommandKey = null;

function sendObserverWindowAction(action) {
  if (typeof window.observerWindowAction === "function") {
    try {
      const result = window.observerWindowAction(action);
      if (result && typeof result.catch === "function") result.catch(() => { });
    } catch (_) {
    }
  }
}

function cssVar(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function normalizeEndpoint(value) {
  let next = String(value || defaultEndpoint).trim();
  if (!next) next = defaultEndpoint;
  next = next.replace(/\/snapshot\/?$/, "").replace(/\/$/, "");
  return next;
}

function readSetting(key, fallback) {
  try {
    return localStorage.getItem(key) || fallback;
  } catch (_) {
    return fallback;
  }
}

function writeSetting(key, value) {
  try {
    localStorage.setItem(key, value);
  } catch (_) {
  }
}

function syncConnectionInputs() {
  el.connectionEndpoint.value = endpoint;
  el.connectionPassword.value = password;
}

function bytes(value) {
  const n = Number(value || 0);
  const units = ["B", "KB", "MB", "GB"];
  let size = n;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit++;
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function duration(ms) {
  const total = Math.max(0, Math.floor(Number(ms || 0) / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

function rows(target, items) {
  target.replaceChildren(...items.flatMap(([key, value]) => {
    const dt = document.createElement("dt");
    const dd = document.createElement("dd");
    dt.textContent = key;
    dd.textContent = value;
    return [dt, dd];
  }));
}

function div(className, text) {
  const node = document.createElement("div");
  node.className = className || "";
  if (text !== undefined) node.textContent = text;
  return node;
}

function tableHeader(labels) {
  const row = div("table-row");
  labels.forEach((label) => {
    const node = document.createElement("strong");
    node.textContent = label;
    row.append(node);
  });
  return row;
}

function addSample(data) {
  const memory = data.memory || {};
  const tasks = data.tasks || {};
  history.push({ memory: Number(memory.alloc || 0), goroutines: Number(data.goroutines || 0), tasks: Number(tasks.active || 0) });
  if (history.length > 90) history.shift();
}

function drawSeries(ctx, values, max, color, width, height, pad) {
  if (values.length < 2) return;
  ctx.beginPath();
  ctx.strokeStyle = color;
  ctx.lineWidth = 2;
  values.forEach((value, index) => {
    const x = pad + (index / (values.length - 1)) * (width - pad * 2);
    const y = height - pad - (value / max) * (height - pad * 2);
    if (index === 0) ctx.moveTo(x, y);
    else ctx.lineTo(x, y);
  });
  ctx.stroke();
}

function drawChart() {
  const ctx = el.chart.getContext("2d");
  const rect = el.chart.getBoundingClientRect();
  const ratio = window.devicePixelRatio || 1;
  const width = Math.max(1, Math.floor(rect.width * ratio));
  const height = Math.max(1, Math.floor(rect.height * ratio));
  if (el.chart.width !== width || el.chart.height !== height) {
    el.chart.width = width;
    el.chart.height = height;
  }
  const pad = 24;
  ctx.clearRect(0, 0, width, height);
  ctx.fillStyle = cssVar("--surface-3") || "#121519";
  ctx.fillRect(0, 0, width, height);
  ctx.strokeStyle = cssVar("--line-soft") || "#20252a";
  for (let i = 0; i < 5; i++) {
    const y = pad + i * ((height - pad * 2) / 4);
    ctx.beginPath();
    ctx.moveTo(pad, y);
    ctx.lineTo(width - pad, y);
    ctx.stroke();
  }
  const mem = history.map((x) => x.memory);
  const go = history.map((x) => x.goroutines);
  const task = history.map((x) => x.tasks);
  drawSeries(ctx, mem, Math.max(1, ...mem), cssVar("--accent") || "#7fd1ae", width, height, pad);
  drawSeries(ctx, go, Math.max(1, ...go), cssVar("--accent-2") || "#82b7ff", width, height, pad);
  drawSeries(ctx, task, Math.max(1, ...task), cssVar("--danger") || "#ff7d73", width, height, pad);
}

function pct(value, max) {
  if (!max) return "0%";
  return `${Math.max(2, Math.min(100, (Number(value || 0) / Number(max)) * 100))}%`;
}

function render(data) {
  const memory = data.memory || {};
  const taskPool = data.taskPool || {};
  const tasks = data.tasks || {};
  const observer = data.observer || {};
  const controls = data.controls || {};

  addSample(data);
  drawChart();

  el.targetName.textContent = `PID ${data.pid ?? ""}`;
  el.targetMeta.textContent = `${endpoint} - ${data.goos || ""}/${data.goarch || ""}`;
  el.alloc.textContent = bytes(memory.alloc);
  el.sys.textContent = `${bytes(memory.sys)} sys`;
  el.goroutines.textContent = data.goroutines ?? 0;
  el.procs.textContent = `${data.gomaxprocs ?? 0} procs`;
  el.tasks.textContent = `${tasks.active ?? 0}`;
  el.taskIdle.textContent = `${tasks.completed ?? 0} done, ${tasks.failed ?? 0} failed`;
  el.targetStatus.textContent = data.status || "unknown";
  el.gcRuns.textContent = `${memory.numGC ?? 0} GC runs`;

  if (!controlsTouched) {
    el.gomaxprocsInput.value = controls.gomaxprocs ?? data.gomaxprocs ?? 1;
    el.gcPercentInput.value = controls.gcPercent ?? 100;
  }

  rows(el.target, [["Endpoint", `${endpoint}/snapshot`], ["PID", data.pid ?? ""], ["Executable", data.executable ?? ""], ["CWD", data.cwd ?? ""], ["Status", data.status ?? ""], ["Uptime", duration(data.uptimeMs)]]);
  rows(el.observer, [["Auth required", observer.authRequired ? "yes" : "no"], ["Requests", observer.requestCount ?? 0], ["Last access", observer.lastAccess || "none"], ["Started", observer.serverStartedAt || ""], ["Shutdown", data.shutdown?.registered ? "registered" : "not registered"]]);
  rows(el.runtime, [["Goroutines", data.goroutines ?? 0], ["GOMAXPROCS", data.gomaxprocs ?? 0], ["GC percent", controls.gcPercent ?? 100], ["Function calls", tasks.calls ?? 0], ["Stack depth", data.stackDepth ?? 0], ["Frame depth", data.frameDepth ?? 0], ["Tasks started", tasks.started ?? 0], ["Tasks active", tasks.active ?? 0], ["Tasks completed", tasks.completed ?? 0], ["Tasks failed", tasks.failed ?? 0], ["Task pool", `${taskPool.active ?? 0}/${taskPool.limit ?? 0}`]]);
  rows(el.program, [["Functions", data.functionCount ?? 0], ["Classes", data.classCount ?? 0], ["Interfaces", data.interfaceCount ?? 0], ["Globals", data.globalCount ?? 0], ["Heap objects", memory.heapObjects ?? 0], ["Total allocated", bytes(memory.totalAlloc)]]);

  el.barAlloc.style.width = pct(memory.alloc, Math.max(memory.heapSys || 1, memory.nextGC || 1));
  el.barHeap.style.width = pct(memory.heapAlloc, Math.max(memory.heapSys || 1, memory.nextGC || 1));
  el.barNextGC.style.width = pct(memory.nextGC, Math.max(memory.heapSys || 1, memory.nextGC || 1));

  renderFunctionTable(data);
  renderGlobalsTable(data);
  renderCommands(data);
  renderMessages(data);
  renderEvents(data);
}

function renderFunctionTable(data) {
  const filter = (el.functionFilter.value || "").toLowerCase();
  const byName = new Map((data.functionCalls || []).map((row) => [row.name, Number(row.calls || 0)]));
  const names = Array.from(new Set([...(data.functionNames || []), ...byName.keys()])).filter((name) => name.toLowerCase().includes(filter));
  names.sort((a, b) => (byName.get(b) || 0) - (byName.get(a) || 0) || a.localeCompare(b));
  const total = Math.max(1, Number((data.tasks || {}).calls || 0));
  el.functionsTable.replaceChildren(tableHeader(["Function", "Calls", "Share", ""]), ...names.slice(0, 120).map((name) => {
    const calls = byName.get(name) || 0;
    const row = div("table-row");
    row.append(div("", name), div("", String(calls)), div("", `${Math.round((calls / total) * 100)}%`), div("", ""));
    return row;
  }));
}

function renderGlobalsTable(data) {
  const active = document.activeElement;
  if (active?.dataset?.globalName) editingGlobal = active.dataset.globalName;
  if (editingGlobal || Date.now() < globalsLockedUntil) return;
  const filter = (el.globalFilter.value || "").toLowerCase();
  const globals = [...(data.globals || [])].filter((item) => String(item.name).toLowerCase().includes(filter)).sort((a, b) => String(a.name).localeCompare(String(b.name)));
  el.globalsTable.replaceChildren(tableHeader(["Global", "Type", "Value", ""]), ...globals.slice(0, 150).map((item) => {
    const row = div("table-row");
    row.append(div("", `${item.name}${item.constant ? " const" : ""}`), div("", item.type || ""));
    if (item.editable) {
      const input = document.createElement("input");
      input.value = item.value ?? "";
      input.dataset.globalName = item.name;
      ["pointerdown", "focus", "input"].forEach((event) => input.addEventListener(event, () => {
        editingGlobal = item.name;
        globalsLockedUntil = Date.now() + 15000;
      }));
      input.addEventListener("blur", () => setTimeout(() => {
        editingGlobal = null;
        globalsLockedUntil = Date.now() + 3000;
      }, 250));
      const button = document.createElement("button");
      button.textContent = "Set";
      button.addEventListener("click", () => setGlobal(item.name, item.type, input.value));
      row.append(input, button);
    } else {
      row.append(div("", item.value ?? ""), div("", ""));
    }
    return row;
  }));
}

function renderCommands(data) {
  const active = document.activeElement;
  if (active?.dataset?.commandKey) {
    commandPayloads.set(active.dataset.commandKey, active.value);
    activeCommandKey = active.dataset.commandKey;
  }
  if (activeCommandKey) return;

  const commandsHeader = tableHeader(["Command", "Payload", "Run"]);
  const exposedHeader = tableHeader(["Function", "Payload", "Call"]);
  commandsHeader.classList.add("command-row");
  exposedHeader.classList.add("command-row");
  el.commandsTable.replaceChildren(commandsHeader, ...(data.commands || []).map((cmd) => commandRow(cmd.name, "command")));
  el.exposedTable.replaceChildren(exposedHeader, ...(data.exposed || []).map((fn) => commandRow(fn.name, "call")));
}

function commandRow(name, kind) {
  const key = `${kind}:${name}`;
  const row = div("table-row");
  row.classList.add("command-row");
  const input = document.createElement("input");
  input.placeholder = "JSON or text";
  input.value = commandPayloads.get(key) || "";
  input.dataset.commandKey = key;
  ["pointerdown", "focus", "input"].forEach((event) => input.addEventListener(event, () => {
    activeCommandKey = key;
    commandPayloads.set(key, input.value);
  }));
  input.addEventListener("blur", () => setTimeout(() => {
    commandPayloads.set(key, input.value);
    activeCommandKey = null;
    refresh(true);
  }, 150));
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
      invokeRegistered(kind, name, input.value);
    }
  });
  const button = document.createElement("button");
  button.textContent = kind === "command" ? "Run" : "Call";
  button.addEventListener("click", () => invokeRegistered(kind, name, input.value));
  const nameCell = div("name-cell", name);
  row.append(nameCell, input, button);
  return row;
}

function renderMessages(data) {
  const rows = [...(data.messages || [])].reverse().slice(0, 100);
  el.messagesTable.replaceChildren(tableHeader(["Time", "From", "Text", ""]), ...rows.map((msg) => {
    const row = div("table-row");
    row.append(div("", msg.time || ""), div("", msg.from || ""), div("", msg.text || ""), div("", ""));
    return row;
  }));
}

function renderEvents(data) {
  const filter = (el.eventFilter.value || "").toLowerCase();
  const rows = [...(data.events || [])].reverse().filter((evt) => `${evt.kind} ${evt.message}`.toLowerCase().includes(filter)).slice(0, 150);
  el.eventsTable.replaceChildren(tableHeader(["Time", "Kind", "Message", ""]), ...rows.map((evt) => {
    const row = div("table-row");
    row.append(div("", evt.time || ""), div("", evt.kind || ""), div("", evt.message || ""), div("", ""));
    return row;
  }));
}

async function observerCall(action) {
  const envelope = JSON.parse(await window.observerRequest(JSON.stringify({ action, endpoint, password })));
  if (envelope.status < 200 || envelope.status >= 300) throw new Error(`${envelope.status}: ${envelope.body}`);
  const body = String(envelope.body || "").trim();
  if (!body.startsWith("{") && !body.startsWith("[")) throw new Error(`unexpected response: ${body.slice(0, 80)}`);
  return JSON.parse(body);
}

async function refresh(force = false) {
  if (paused && !force) return;
  try {
    render(await observerCall("snapshot"));
    el.status.textContent = paused ? "Paused" : `Updated ${new Date().toLocaleTimeString()}`;
    el.connectionStatus.textContent = "Connected";
  } catch (error) {
    el.status.textContent = `Cannot reach target: ${error}`;
    el.connectionStatus.textContent = "Connection failed";
  }
}

async function forceGC() { try { await observerCall("gc"); await refresh(true); } catch (e) { el.status.textContent = `GC failed: ${e}`; } }
async function applyGomaxprocs() { try { await observerCall(`gomaxprocs:${el.gomaxprocsInput.value}`); controlsTouched = false; await refresh(true); } catch (e) { el.status.textContent = `GOMAXPROCS failed: ${e}`; } }
async function applyGcPercent() { try { await observerCall(`gcPercent:${el.gcPercentInput.value}`); controlsTouched = false; await refresh(true); } catch (e) { el.status.textContent = `GC percent failed: ${e}`; } }
async function setGlobal(name, type, value) { try { await observerCall(`global:${encodeURIComponent(name)}\n${encodeURIComponent(type)}\n${encodeURIComponent(value)}`); editingGlobal = null; globalsLockedUntil = 0; await refresh(true); } catch (e) { el.status.textContent = `Set global failed: ${e}`; } }
async function invokeRegistered(kind, name, payload) { try { const result = await observerCall(`${kind}:${name}\n${payload}`); el.commandResult.textContent = JSON.stringify(result.result, null, 2); await refresh(true); } catch (e) { el.commandResult.textContent = String(e); } }
async function sendMessage() { try { await observerCall(`message:${el.messageInput.value}`); el.messageInput.value = ""; await refresh(true); } catch (e) { el.status.textContent = `Message failed: ${e}`; } }
async function shutdownTarget() { try { await observerCall("shutdown"); await refresh(true); } catch (e) { el.status.textContent = `Shutdown failed: ${e}`; } }
function connectTarget() {
  endpoint = normalizeEndpoint(el.connectionEndpoint.value);
  password = el.connectionPassword.value || "";
  writeSetting("observer.endpoint", endpoint);
  writeSetting("observer.password", password);
  syncConnectionInputs();
  history.length = 0;
  el.connectionStatus.textContent = "Connecting";
  refresh(true);
}

function selectTab(name) {
  document.querySelectorAll(".tab").forEach((tab) => tab.classList.toggle("active", tab.dataset.tab === name));
  document.querySelectorAll(".page").forEach((page) => page.classList.toggle("active", page.dataset.page === name));
}

el.gomaxprocsInput.addEventListener("input", () => { controlsTouched = true; });
el.gcPercentInput.addEventListener("input", () => { controlsTouched = true; });
el.functionFilter.addEventListener("input", () => refresh(true));
el.globalFilter.addEventListener("input", () => refresh(true));
el.eventFilter.addEventListener("input", () => refresh(true));
document.getElementById("pause").addEventListener("click", () => { paused = !paused; document.getElementById("pause").textContent = paused ? "Resume" : "Pause"; refresh(true); });
document.getElementById("refresh").addEventListener("click", () => refresh(true));
document.getElementById("forceGc").addEventListener("click", forceGC);
el.connectButton.addEventListener("click", connectTarget);
el.connectionEndpoint.addEventListener("keydown", (event) => { if (event.key === "Enter") connectTarget(); });
el.connectionPassword.addEventListener("keydown", (event) => { if (event.key === "Enter") connectTarget(); });
document.getElementById("setGomaxprocs").addEventListener("click", applyGomaxprocs);
document.getElementById("setGcPercent").addEventListener("click", applyGcPercent);
document.getElementById("sendMessage").addEventListener("click", sendMessage);
document.getElementById("shutdownTarget").addEventListener("click", shutdownTarget);
document.getElementById("globalsTable").addEventListener("pointerdown", () => { globalsLockedUntil = Date.now() + 15000; });
document.querySelectorAll(".tab").forEach((tab) => tab.addEventListener("click", () => selectTab(tab.dataset.tab)));

document.querySelector("[data-window-drag]")?.addEventListener("mousedown", (event) => {
  if (event.detail > 1) return;

  if (!event.target.closest("button, input, textarea")) {
    sendObserverWindowAction("drag");
  }
});

document.querySelector("[data-window-drag]")?.addEventListener("dblclick", (event) => {
  if (!event.target.closest("button, input, textarea")) {
    event.preventDefault();
    event.stopPropagation();
    sendObserverWindowAction("maximize");
  }
});

document.querySelectorAll("[data-window-action]").forEach((button) => {
  button.addEventListener("click", () => sendObserverWindowAction(button.dataset.windowAction));
});
document.querySelectorAll("[data-window-resize]").forEach((handle) => {
  handle.addEventListener("mousedown", (event) => {
    event.preventDefault();
    event.stopPropagation();
    sendObserverWindowAction(`resize:${handle.dataset.windowResize}`);
  });
});
syncConnectionInputs();
refresh(true);
setInterval(() => refresh(false), 1000);

window.addEventListener("load", () => {
  setTimeout(() => {
    sendObserverWindowAction("ready");
  }, 150);
});
