// Content script: while recording is on it (1) shows a small on-page control bar with a Done
// button, and (2) reports lightweight repro steps to the worker. Guard against double-binding —
// this can be both declared (manifest) and injected on demand.
if (window.__cboardRecorder) { /* already attached */ } else {
window.__cboardRecorder = true;

let on = false;

chrome.runtime.sendMessage({ type: "isRecording" }).then((r) => setOn(!!(r && r.on))).catch(() => {});
chrome.runtime.onMessage.addListener((msg) => { if (msg && msg.type === "recording") setOn(!!msg.on); });

function setOn(v) { on = v; if (on) showBar(); else removeBar(); }

// ── on-page control bar ──
let bar = null;
function showBar() {
  if (bar || !document.body) return;
  bar = document.createElement("div");
  bar.id = "__cboard_rec_bar";
  bar.style.cssText =
    "position:fixed;top:12px;left:50%;transform:translateX(-50%);z-index:2147483647;" +
    "background:#1f2130;color:#fff;font:600 13px system-ui,sans-serif;padding:7px 9px;" +
    "border-radius:10px;box-shadow:0 6px 24px rgba(0,0,0,.35);display:flex;gap:8px;align-items:center";
  const dot = document.createElement("span");
  dot.textContent = "● Recording";
  dot.style.cssText = "color:#eb2f96";
  const done = document.createElement("button");
  done.textContent = "✓ Done — capture";
  done.style.cssText = "background:#667eea;color:#fff;border:0;border-radius:7px;padding:6px 10px;font:inherit;cursor:pointer";
  const cancel = document.createElement("button");
  cancel.textContent = "✕";
  cancel.title = "Cancel recording";
  cancel.style.cssText = "background:#3a3d50;color:#cfd2e6;border:0;border-radius:7px;padding:6px 9px;font:inherit;cursor:pointer";
  done.addEventListener("click", (e) => {
    e.stopPropagation();
    removeBar(); // take it out of the DOM so it isn't in the screenshot
    chrome.runtime.sendMessage({ type: "finishRecording" }).catch(() => {});
  });
  cancel.addEventListener("click", (e) => {
    e.stopPropagation();
    chrome.runtime.sendMessage({ type: "setRecording", on: false }).catch(() => {});
  });
  bar.append(dot, done, cancel);
  document.body.appendChild(bar);
}
function removeBar() { if (bar) { bar.remove(); bar = null; } }

// ── step capture ──
function label(el) {
  if (!el || !el.tagName) return "element";
  const tag = el.tagName.toLowerCase();
  const txt = (el.innerText || el.value || el.getAttribute("aria-label") || el.name || el.id || "")
    .trim().replace(/\s+/g, " ").slice(0, 40);
  return txt ? `${tag} "${txt}"` : `<${tag}>`;
}
function report(text) { if (on) chrome.runtime.sendMessage({ type: "step", text }).catch(() => {}); }
function inBar(el) { return bar && el && bar.contains(el); }

document.addEventListener("click", (e) => { if (!inBar(e.target)) report("Click " + label(e.target)); }, true);
document.addEventListener("change", (e) => {
  const t = e.target;
  if (t && !inBar(t) && /^(INPUT|SELECT|TEXTAREA)$/.test(t.tagName)) report("Set " + label(t));
}, true);

}
