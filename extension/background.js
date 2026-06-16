// Service worker: the only context with host access to the local board. It captures the
// visible tab, holds the recorded repro steps, and files the ticket over MCP. The popup and
// editor are thin UIs that message this worker.

const DEFAULT_ENDPOINT = "http://localhost:8787";

async function getEndpoint() {
  const { endpoint } = await chrome.storage.sync.get("endpoint");
  return (endpoint || DEFAULT_ENDPOINT).replace(/\/+$/, "");
}

// mcp issues one JSON-RPC tools/call against the board's /mcp endpoint. cboard is stateless,
// so no initialize handshake is needed. Tool errors come back as isError results, not throws.
async function mcp(endpoint, name, args) {
  const res = await fetch(endpoint + "/mcp", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      jsonrpc: "2.0", id: Date.now(), method: "tools/call",
      params: { name, arguments: args },
    }),
  });
  if (!res.ok) throw new Error("HTTP " + res.status + " from " + endpoint);
  const j = await res.json();
  if (j.error) throw new Error(j.error.message || "rpc error");
  const r = j.result || {};
  const text = r.content && r.content[0] && r.content[0].text;
  if (r.isError) throw new Error(text || "tool error");
  try { return JSON.parse(text || "{}"); } catch { return {}; }
}

// Create the ticket, then attach the annotated screenshot as an artifact (which previews
// inline on the card in Test & Review).
async function fileTicket({ title, body, imageB64 }) {
  const endpoint = await getEndpoint();
  const created = await mcp(endpoint, "create_ticket", { title: title || "Untitled report", body: body || "" });
  const id = created.id;
  if (!id) throw new Error("create_ticket returned no id");
  if (imageB64) {
    await mcp(endpoint, "save_artifact", { id, name: "screenshot.png", content: imageB64, encoding: "base64" });
  }
  return { id, lane: created.lane || "planning", endpoint };
}

// Tell every content script whether recording is on, so they only message us while active.
async function broadcastRecording(on) {
  const tabs = await chrome.tabs.query({});
  for (const t of tabs) {
    if (t.id) chrome.tabs.sendMessage(t.id, { type: "recording", on }).catch(() => {});
  }
}

async function captureToEditor(tab) {
  const dataUrl = await chrome.tabs.captureVisibleTab(tab.windowId, { format: "png" });
  const capture = {
    image: dataUrl,
    url: tab.url || "",
    title: tab.title || "",
    ua: navigator.userAgent,
    at: new Date().toISOString(),
  };
  await chrome.storage.session.set({ capture });
  await chrome.tabs.create({ url: chrome.runtime.getURL("editor.html") });
}

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  (async () => {
    try {
      switch (msg.type) {
        case "capture": {
          const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
          await captureToEditor(tab);
          sendResponse({ ok: true });
          break;
        }
        case "setRecording": {
          await chrome.storage.session.set({ recording: !!msg.on });
          if (msg.on) await chrome.storage.session.set({ steps: [] });
          broadcastRecording(!!msg.on);
          sendResponse({ ok: true });
          break;
        }
        case "isRecording": {
          const { recording } = await chrome.storage.session.get("recording");
          sendResponse({ ok: true, on: !!recording });
          break;
        }
        case "step": {
          const { recording, steps = [] } = await chrome.storage.session.get(["recording", "steps"]);
          if (recording && msg.text) {
            steps.push(msg.text);
            await chrome.storage.session.set({ steps });
          }
          sendResponse({ ok: true });
          break;
        }
        case "getCapture": {
          const { capture, steps = [] } = await chrome.storage.session.get(["capture", "steps"]);
          sendResponse({ ok: true, capture, steps });
          break;
        }
        case "file": {
          const result = await fileTicket(msg.payload);
          sendResponse({ ok: true, ...result });
          break;
        }
        default:
          sendResponse({ ok: false, error: "unknown message: " + msg.type });
      }
    } catch (e) {
      sendResponse({ ok: false, error: String(e && e.message ? e.message : e) });
    }
  })();
  return true; // async sendResponse
});
