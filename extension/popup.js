// Popup: configure the endpoint, toggle step recording, and kick off a capture.

const $ = (id) => document.getElementById(id);

(async () => {
  const { endpoint } = await chrome.storage.sync.get("endpoint");
  $("endpoint").value = endpoint || "http://localhost:8787";
  const { recording } = await chrome.storage.session.get("recording");
  $("recording").checked = !!recording;
})();

$("endpoint").addEventListener("change", () => {
  chrome.storage.sync.set({ endpoint: $("endpoint").value.trim() });
});

$("recording").addEventListener("change", () => {
  chrome.runtime.sendMessage({ type: "setRecording", on: $("recording").checked });
});

$("capture").addEventListener("click", async () => {
  await chrome.storage.sync.set({ endpoint: $("endpoint").value.trim() });
  await chrome.runtime.sendMessage({ type: "capture" });
  window.close(); // the editor opens in its own tab
});
