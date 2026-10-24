"use strict";

const $ = (s, root = document) => root.querySelector(s);
const esc = (s) =>
  String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
const icon = (path) => `<svg class="i" viewBox="0 0 24 24"><path d="${path}"/></svg>`;

const BADGES = {
  go: { color: "#00ADD8", text: "Go" },
  python: { color: "#3776AB", text: "Py" },
  javascript: { color: "#D9B70B", text: "JS" },
  java: { color: "#E76F00", text: "Jv" },
  rust: { color: "#B7410E", text: "Rs" },
  ruby: { color: "#CC342D", text: "Rb" },
  php: { color: "#777BB4", text: "Php" },
  c: { color: "#5C6BC0", text: "C" },
  cpp: { color: "#004482", text: "C++" },
  csharp: { color: "#68217A", text: "C#" },
  vb: { color: "#00539C", text: "VB" },
};

const state = {
  overview: null,
  theme: "system",
  languages: null,
  known: null,
  detail: null,
  tab: "versions",
  manual: null,
};

// ---------------------------------------------------------------- backend

const backend = () => window.go.desktop.App;

async function call(method, ...args) {
  try {
    return await backend()[method](...args);
  } catch (err) {
    toast(String(err?.message ?? err), "error");
    throw err;
  }
}

function bindingsReady() {
  return new Promise((resolve) => {
    const check = () => (window.go?.desktop?.App ? resolve() : setTimeout(check, 20));
    check();
  });
}

// ---------------------------------------------------------------- shell: header, theme, clock, status bar

function applyTheme(theme) {
  state.theme = theme;
  const dark = theme === "dark" || (theme === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
  for (const b of document.querySelectorAll("#theme button")) b.classList.toggle("on", b.dataset.theme === theme);
}

function tick() {
  const now = new Date();
  const time = now.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  const date = now.toLocaleDateString([], { day: "numeric", month: "short" });
  $("#clock").innerHTML = `<b>${esc(time)}</b> · ${esc(date)}`;
}

function renderOverview() {
  const o = state.overview;
  $("#host-name").textContent = o.remote ? o.host : "local";
  $("#host-dot").classList.toggle("remote", o.remote);
  const local = document.querySelector("[data-local]");
  local.classList.toggle("on", !o.remote);
  $("#local-check").hidden = o.remote;
  $("#host-servers").innerHTML = (o.servers || [])
    .map(
      (s) => `<button class="item${o.remote && s === o.host ? " on" : ""}" type="button" data-server="${esc(s)}">
        <span class="choice">${esc(s)}</span>${o.remote && s === o.host ? '<span class="check">✓</span>' : ""}
      </button>`,
    )
    .join("");
  const s = o.system;
  $("#statusbar").innerHTML = `
    <span><b>${esc(s.hostname)}</b></span>
    <span>${esc(s.os)} ${esc(s.osVersion)} · ${esc(s.arch)}</span>
    <span>${esc(s.cpu)} · ${s.cores} cores</span>
    <span>${s.memoryGiB.toFixed(1)} GB RAM</span>
    <span class="right">Lman ${esc(o.version)}</span>`;
}

async function loadOverview() {
  state.overview = await call("Overview");
  applyTheme(state.overview.theme);
  renderOverview();
}

// ---------------------------------------------------------------- list screen

function badge(id, big = false) {
  const b = BADGES[id] || { color: "#64748b", text: id.slice(0, 2) };
  return `<span class="badge${big ? " big" : ""}" style="background:${b.color}">${esc(b.text)}</span>`;
}

function hostLabel() {
  return state.overview?.remote ? state.overview.host : "this computer";
}

function listHead(sub) {
  return `<div class="head"><h1>Languages</h1><span class="sub">${sub}</span>
    <button class="btn ghost" type="button" data-known>Detectable languages</button></div>`;
}

function renderList() {
  const view = $("#view");
  const langs = state.languages;
  const head = listHead(langs ? `${langs.length} installed on ${esc(hostLabel())}` : "Scanning…");
  if (!langs) {
    view.innerHTML = head + `<div class="grid">${'<div class="tile skeleton"></div>'.repeat(5)}</div>`;
    return;
  }
  if (langs.length === 0) {
    view.innerHTML = head + `<div class="empty">No programming languages found.</div>`;
    return;
  }
  view.innerHTML =
    head +
    `<div class="grid">${langs
      .map(
        (l) => `
      <button class="tile" type="button" data-open="${esc(l.id)}">
        ${badge(l.id)}
        <div>
          <div class="name">${esc(l.name)}</div>
          ${l.active ? `<div class="active">${esc(l.active)}</div>` : '<div class="active none">not active</div>'}
          <div class="meta">${l.versions} ${l.versions === 1 ? "version" : "versions"}</div>
        </div>
      </button>`,
      )
      .join("")}</div>`;
}

async function loadLanguages(refresh = false) {
  state.languages = null;
  if (!state.detail && !state.known) renderList();
  try {
    state.languages = await call("Languages", refresh);
  } finally {
    if (state.detail) return;
    if (state.known) renderKnown();
    else renderList();
  }
}

async function openKnown() {
  state.detail = null;
  if (!state.known) {
    state.known = { loading: true };
    renderKnown();
  }
  try {
    state.known = await call("Known");
  } catch {
    state.known = null;
    renderList();
    return;
  }
  renderKnown();
}

function renderKnown() {
  const back = `<button class="back" type="button" data-back>${icon("M15 6l-6 6 6 6")} Languages</button>`;
  if (!Array.isArray(state.known)) {
    $("#view").innerHTML = back + `<div class="empty">Loading…</div>`;
    return;
  }
  const installed = new Set((state.languages || []).map((l) => l.id));
  const rows = state.known
    .map((l) => {
      const aliases = (l.aliases || []).join(", ");
      const body = `${badge(l.id)}
        <div class="known-name"><b>${esc(l.name)}</b><span>${esc(l.id)}${aliases ? ` · ${esc(aliases)}` : ""}</span></div>
        ${installed.has(l.id) ? '<span class="tag">installed</span>' : '<span class="method">not installed</span>'}`;
      return installed.has(l.id)
        ? `<button class="known-row" type="button" data-open="${esc(l.id)}">${body}</button>`
        : `<div class="known-row">${body}</div>`;
    })
    .join("");
  $("#view").innerHTML = `${back}
    <div class="head"><h1>Detectable languages</h1><span class="sub">${state.known.length} in the catalog</span></div>
    <div class="card known">${rows}</div>
    <p class="note">An installed language opens its versions. The others are looked for on ${esc(hostLabel())} and not found.</p>`;
}

// ---------------------------------------------------------------- detail screen

async function openLanguage(id, tab = "versions") {
  state.tab = tab;
  state.manual = null;
  state.detail = { id, loading: true };
  renderDetail();
  try {
    state.detail = await call("Language", id);
  } catch {
    showList();
    return;
  }
  renderDetail();
}

function showList() {
  if (state.detail && state.known) {
    state.detail = null;
    renderKnown();
    return;
  }
  state.detail = null;
  state.known = null;
  renderList();
}

function renderDetail() {
  const d = state.detail;
  const back = `<button class="back" type="button" data-back>${icon("M15 6l-6 6 6 6")} Languages</button>`;
  if (d.loading) {
    $("#view").innerHTML = back + `<div class="empty">Loading…</div>`;
    return;
  }
  const active = d.versions.find((v) => v.active);
  const tabs = [
    ["versions", `Versions · ${d.versions.length}`],
    ["env", "Environment"],
    ["manual", "Manual"],
  ];
  $("#view").innerHTML = `
    ${back}
    <div class="title">
      ${badge(d.id, true)}
      <div>
        <h1>${esc(d.name)}</h1>
        <div class="sub">${active ? `<code class="mono">${esc(active.label)}</code> runs in the terminal` : "No version is active in the terminal"}</div>
      </div>
      <button class="btn danger-ghost" type="button" data-remove-language>Remove language</button>
    </div>
    <div class="chips">${d.hints
      .map((h) => `<div class="chip"><code>${esc(h.tool)}</code><span>${esc(h.purpose)}</span></div>`)
      .join("")}</div>
    <div class="tabs">${tabs
      .map(([key, label]) => `<button type="button" data-tab="${key}" class="${state.tab === key ? "on" : ""}">${label}</button>`)
      .join("")}</div>
    <div id="tab">${renderTab(d)}</div>`;
}

function renderTab(d) {
  if (state.tab === "env") return renderEnv(d);
  if (state.tab === "manual") return renderManual(d);
  return renderVersions(d);
}

function renderVersions(d) {
  const rows = d.versions
    .map(
      (v) => `
    <div class="v">
      <div class="label">${esc(v.label)}${v.active ? '<span class="tag">active</span>' : ""}</div>
      <div class="method">${esc(v.method)}</div>
      <div class="date">${esc(v.installed || "—")}</div>
      <div class="path" title="${esc(v.realPath)}">${esc(v.path)}</div>
      <div class="actions">
        <button class="btn ghost" type="button" data-use="${esc(v.realPath)}" ${v.active ? "disabled" : ""}>Make active</button>
        <button class="btn danger-ghost" type="button" data-remove="${esc(v.realPath)}" data-label="${esc(v.label)}" ${
          v.active ? 'disabled title="Make another version active first"' : ""
        }>Remove</button>
      </div>
    </div>`,
    )
    .join("");
  return `<div class="card versions">
      <div class="v cols"><div>Version</div><div>Installed with</div><div>Date</div><div>Path</div><div></div></div>
      ${rows}
    </div>
    <p class="note">Active version changes apply to new terminals. Lman links it from ~/.lman/bin, placed first in PATH.</p>`;
}

function renderEnv(d) {
  const rows = d.env
    .map(
      (v) => `
    <form class="env-row" data-env="${esc(v.name)}">
      <code>${esc(v.name)}</code>
      <input name="value" value="${esc(v.value)}" placeholder="not set" spellcheck="false" autocomplete="off" />
      <div>${v.managed ? '<span class="tag lman">lman</span>' : v.default ? '<span class="tag default">go env</span>' : ""}</div>
      <button class="btn ghost" type="submit">Save</button>
    </form>`,
    )
    .join("");
  const note =
    d.id === "go"
      ? "Same list as go env. go env marks a value the toolchain reported, including defaults and settings from go env -w. Save exports a value for new terminals; an empty value stops exporting."
      : "Values are what a new terminal sees. Saved values are exported to new terminals; an empty value stops exporting.";
  return `<div class="card">${rows}</div>
    <p class="note">${note}</p>`;
}

function renderManual(d) {
  const docs = `<button class="link" type="button" data-docs="${esc(d.docsUrl)}">${esc(d.docsUrl)}</button>`;
  if (!state.manual) {
    loadManual(d.id);
    return `<div class="card manual">Documentation: ${docs}<pre>Loading…</pre></div>`;
  }
  return `<div class="card manual">Documentation: ${docs}<pre>${esc(state.manual)}</pre></div>`;
}

async function loadManual(id) {
  try {
    const m = await call("Manual", id);
    state.manual = m.text || "The language prints no manual.";
  } catch {
    state.manual = "The manual could not be read.";
  }
  if (state.detail?.id === id && state.tab === "manual") $("#tab").innerHTML = renderManual(state.detail);
}

// ---------------------------------------------------------------- actions

async function useVersion(path) {
  const id = state.detail.id;
  const res = await call("Use", id, path);
  toast(res.message, res.warning ? "warn" : "ok");
  await refreshAfterChange(id);
}

async function removeVersion(path, label) {
  const d = state.detail;
  if (!(await confirmDialog(`Remove ${d.name} ${label}?`, path))) return;
  const res = await call("RemoveVersion", d.id, path);
  toast(res.message);
  await refreshAfterChange(d.id);
}

async function removeLanguage() {
  const d = state.detail;
  const text = `All ${d.versions.length} versions will be removed. Versions installed by the system are listed with the command to run.`;
  if (!(await confirmDialog(`Remove ${d.name}?`, text))) return;
  try {
    const res = await call("RemoveLanguage", d.id);
    toast(res.message);
  } finally {
    await refreshAfterChange(d.id);
  }
}

async function refreshAfterChange(id) {
  await loadLanguages();
  if (state.languages?.some((l) => l.id === id)) {
    const tab = state.tab;
    await openLanguage(id, tab);
  } else {
    showList();
  }
}

async function saveEnv(form) {
  const name = form.dataset.env;
  const value = form.elements.value.value.trim();
  state.detail.env = await call("SetEnv", state.detail.id, name, value);
  $("#tab").innerHTML = renderEnv(state.detail);
  toast(value ? `${name} is set for new terminals.` : `${name} is no longer set by lman.`);
}

async function connect(server) {
  const current = state.overview?.remote ? state.overview.host : "";
  $("#host-popover").hidden = true;
  if (server === current) return;
  toast(server ? `Connecting to ${server}…` : "Back to local…");
  state.overview = await call("Connect", server);
  renderOverview();
  state.detail = null;
  state.known = null;
  await loadLanguages();
}

async function setTheme(theme) {
  applyTheme(theme);
  await call("SetTheme", theme);
}

// ---------------------------------------------------------------- dialogs and notices

function confirmDialog(title, text) {
  return new Promise((resolve) => {
    $("#modal-title").textContent = title;
    $("#modal-text").textContent = text;
    $("#modal").hidden = false;
    const done = (ok) => {
      $("#modal").hidden = true;
      $("#modal-ok").onclick = $("#modal-cancel").onclick = null;
      resolve(ok);
    };
    $("#modal-ok").onclick = () => done(true);
    $("#modal-cancel").onclick = () => done(false);
  });
}

function toast(message, kind = "ok") {
  const el = document.createElement("div");
  el.className = `toast ${kind}`;
  el.textContent = message;
  $("#toasts").appendChild(el);
  setTimeout(() => el.remove(), kind === "error" ? 9000 : 4500);
}

// ---------------------------------------------------------------- events

document.addEventListener("click", (e) => {
  const t = e.target.closest("button");
  if (!t) {
    if (!e.target.closest("#host-popover")) $("#host-popover").hidden = true;
    return;
  }
  const run = (p) => p.catch(() => {});
  if (t.id === "host-button") {
    $("#host-popover").hidden = !$("#host-popover").hidden;
  } else if (t.dataset.local !== undefined && t.closest("#host-popover")) {
    run(connect(""));
  } else if (t.dataset.server && t.closest("#host-popover")) {
    run(connect(t.dataset.server));
  } else if (t.dataset.known !== undefined && !t.dataset.open) {
    run(openKnown());
  } else if (t.dataset.open) {
    run(openLanguage(t.dataset.open));
  } else if (t.dataset.back !== undefined) {
    showList();
  } else if (t.dataset.tab) {
    state.tab = t.dataset.tab;
    renderDetail();
  } else if (t.dataset.use) {
    run(useVersion(t.dataset.use));
  } else if (t.dataset.remove) {
    run(removeVersion(t.dataset.remove, t.dataset.label));
  } else if (t.dataset.removeLanguage !== undefined) {
    run(removeLanguage());
  } else if (t.dataset.docs) {
    window.runtime.BrowserOpenURL(t.dataset.docs);
  } else if (t.dataset.theme && t.closest("#theme")) {
    run(setTheme(t.dataset.theme));
  } else if (t.id === "refresh") {
    t.classList.add("spin");
    const id = state.detail?.id;
    run(
      loadLanguages(true)
        .then(() => id && openLanguage(id, state.tab))
        .finally(() => t.classList.remove("spin")),
    );
  }
});

document.addEventListener("submit", (e) => {
  e.preventDefault();
  const form = e.target;
  if (form.id === "connect-form") {
    const server = $("#connect-input").value.trim();
    if (server) connect(server).catch(() => {});
  } else if (form.dataset.env) {
    saveEnv(form).catch(() => {});
  }
});

document.addEventListener("keydown", (e) => {
  if (e.key !== "Escape") return;
  if (!$("#modal").hidden) $("#modal-cancel").click();
  else if (!$("#host-popover").hidden) $("#host-popover").hidden = true;
  else if (state.detail || state.known) showList();
});

matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => applyTheme(state.theme));

// ---------------------------------------------------------------- boot

(async function boot() {
  applyTheme("system");
  tick();
  setInterval(tick, 1000);
  renderList();
  await bindingsReady();
  try {
    await loadOverview();
  } catch {
    return;
  }
  await loadLanguages().catch(() => {});
})();
