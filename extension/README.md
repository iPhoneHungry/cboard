# cboard clipper (browser extension)

A small MV3 extension (Chrome + Firefox) that captures the page you're on, lets you annotate it
(box / arrow / text / pen), optionally records your clicks as repro steps, and files it as a
**cboard ticket** — with the annotated screenshot attached — straight to your local board over MCP.

It talks to `create_ticket` and `save_artifact`, so the screenshot **previews inline on the card**
in Test & Review.

## Install (unpacked)

cboard must be running (`cboard` → `http://localhost:8787`).

**Chrome / Edge**
1. `chrome://extensions` → enable **Developer mode**.
2. **Load unpacked** → select this `extension/` folder.

**Firefox**
1. `about:debugging#/runtime/this-firefox`.
2. **Load Temporary Add-on…** → pick `extension/manifest.json`.

## Use

Click the toolbar icon (set the **board endpoint** first if it isn't `http://localhost:8787`).
Two ways to grab a page:

- **📸 Capture now** — screenshots the visible tab and opens the editor right away.
- **⏺ Record steps, then capture** — drops a small bar on the page; reproduce the issue (clicks
  are logged as repro steps), then press **✓ Done** on that bar to grab the shot and open the
  editor. Hit ✕ (or **Stop** in the popup) to cancel.

In the editor: draw on the shot (box / arrow / text / pen), fill in a title / description / steps
(recorded steps are pre-filled), and **Create ticket**. The card lands in **planning** with the
screenshot attached; open the board to triage it.

## How it connects

```
popup ──capture──▶ background ──captureVisibleTab──▶ editor.html
                       │                                  │ annotate + form
                       ◀───────── file(ticket) ───────────┘
                       │
                       ├─ POST /mcp  create_ticket(title, body)
                       └─ POST /mcp  save_artifact(id, screenshot.png, base64)
```

The background service worker is the only piece with host access to the board, so all network
calls happen there. Endpoint is stored per-browser; the captured image and recorded steps live in
`storage.session` between capture and filing.

## Notes & limits

- **Local only by default.** `host_permissions` covers `localhost:8787` / `127.0.0.1:8787`. To
  point at another host, add it to `host_permissions` in `manifest.json` (a remote board would
  also need to be reachable and is unauthenticated — trusted networks only).
- **Visible viewport** is captured (one frame). Full-page scroll-stitch is a future addition.
- **Step recording** logs your clicks while it's on; only the steps from the **page you capture**
  are filed (interactions on the board or other tabs are dropped). Turning it on also attaches to
  the tab that's open at that moment, so you don't have to reload first. Toggle it off in the popup.
- Annotations are baked into the PNG at full resolution before upload.
