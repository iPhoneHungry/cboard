// Popup: configure the endpoint and start either an instant capture or a record-then-Done flow.

const $ = (id) => document.getElementById(id);

function saveEndpoint() {
  return chrome.storage.sync.set({ endpoint: $("endpoint").value.trim() });
}

(async () => {
  const { endpoint } = await chrome.storage.sync.get("endpoint");
  $("endpoint").value = endpoint || "http://localhost:8787";
  const { on } = (await chrome.runtime.sendMessage({ type: "isRecording" })) || {};
  if (on) {
    // Already recording — surface Stop and de-emphasize the start buttons.
    $("recstate").style.display = "block";
    $("record").style.display = "none";
  }
})();

$("endpoint").addEventListener("change", saveEndpoint);

$("capture").addEventListener("click", async () => {
  await saveEndpoint();
  await chrome.runtime.sendMessage({ type: "capture" });
  window.close(); // editor opens in its own tab
});

$("record").addEventListener("click", async () => {
  await saveEndpoint();
  await chrome.runtime.sendMessage({ type: "setRecording", on: true });
  window.close(); // the on-page bar takes over; press Done there to capture
});

$("stop").addEventListener("click", async () => {
  await chrome.runtime.sendMessage({ type: "setRecording", on: false });
  window.close();
});
