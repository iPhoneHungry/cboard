// Content script: while recording is on, report lightweight repro steps to the worker. It
// caches the on/off state (queried on load, updated by broadcast) so it stays silent otherwise.
// Guard against double-binding: this can be both declared (manifest) and injected on demand.
if (window.__cboardRecorder) { /* already attached */ } else {
window.__cboardRecorder = true;

let on = false;
chrome.runtime.sendMessage({ type: "isRecording" }).then((r) => { on = !!(r && r.on); }).catch(() => {});
chrome.runtime.onMessage.addListener((msg) => { if (msg && msg.type === "recording") on = !!msg.on; });

function label(el) {
  if (!el || !el.tagName) return "element";
  const tag = el.tagName.toLowerCase();
  const txt = (el.innerText || el.value || el.getAttribute("aria-label") || el.name || el.id || "")
    .trim().replace(/\s+/g, " ").slice(0, 40);
  return txt ? `${tag} "${txt}"` : `<${tag}>`;
}

function report(text) {
  if (on) chrome.runtime.sendMessage({ type: "step", text }).catch(() => {});
}

document.addEventListener("click", (e) => report("Click " + label(e.target)), true);
document.addEventListener("change", (e) => {
  const t = e.target;
  if (t && /^(INPUT|SELECT|TEXTAREA)$/.test(t.tagName)) report("Set " + label(t));
}, true);

}
