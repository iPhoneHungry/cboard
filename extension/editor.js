// Editor: draw the screenshot at natural resolution, let the user annotate, then composite
// and file. The canvas is kept at the image's native size and CSS-scaled to fit, so pointer
// coordinates map by a single scale factor and the export is full-resolution.

const $ = (id) => document.getElementById(id);
const canvas = $("canvas");
const ctx = canvas.getContext("2d");

let img = null;          // the base screenshot
let shapes = [];         // annotation objects, drawn in order
let tool = "box";
let color = "#eb2f96";
let drawing = null;      // in-progress shape
let capture = null;

function setTool(t) {
  tool = t;
  document.querySelectorAll(".tool[data-tool]").forEach((b) =>
    b.classList.toggle("active", b.dataset.tool === t));
}

// Pointer position in image (natural) coordinates.
function pos(e) {
  const r = canvas.getBoundingClientRect();
  const sx = canvas.width / r.width, sy = canvas.height / r.height;
  return { x: (e.clientX - r.left) * sx, y: (e.clientY - r.top) * sy };
}

function redraw() {
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  if (img) ctx.drawImage(img, 0, 0);
  const lw = Math.max(2, Math.round(canvas.width / 400)); // scale stroke to image size
  for (const s of shapes.concat(drawing ? [drawing] : [])) drawShape(s, lw);
}

function drawShape(s, lw) {
  ctx.strokeStyle = s.color; ctx.fillStyle = s.color; ctx.lineWidth = lw;
  ctx.lineJoin = "round"; ctx.lineCap = "round";
  if (s.type === "box") {
    ctx.strokeRect(s.x, s.y, s.w, s.h);
  } else if (s.type === "arrow") {
    arrow(s.x, s.y, s.x2, s.y2, lw);
  } else if (s.type === "pen") {
    ctx.beginPath();
    s.pts.forEach((p, i) => (i ? ctx.lineTo(p.x, p.y) : ctx.moveTo(p.x, p.y)));
    ctx.stroke();
  } else if (s.type === "text") {
    const size = Math.max(16, Math.round(canvas.width / 45));
    ctx.font = `700 ${size}px system-ui, sans-serif`;
    ctx.textBaseline = "top";
    const pad = size * 0.25, w = ctx.measureText(s.text).width;
    ctx.globalAlpha = 0.85; ctx.fillStyle = "#fff";
    ctx.fillRect(s.x - pad, s.y - pad, w + pad * 2, size + pad * 2);
    ctx.globalAlpha = 1; ctx.fillStyle = s.color;
    ctx.fillText(s.text, s.x, s.y);
  }
}

function arrow(x1, y1, x2, y2, lw) {
  const head = Math.max(10, lw * 4), a = Math.atan2(y2 - y1, x2 - x1);
  ctx.beginPath(); ctx.moveTo(x1, y1); ctx.lineTo(x2, y2); ctx.stroke();
  ctx.beginPath(); ctx.moveTo(x2, y2);
  ctx.lineTo(x2 - head * Math.cos(a - Math.PI / 6), y2 - head * Math.sin(a - Math.PI / 6));
  ctx.lineTo(x2 - head * Math.cos(a + Math.PI / 6), y2 - head * Math.sin(a + Math.PI / 6));
  ctx.closePath(); ctx.fill();
}

canvas.addEventListener("pointerdown", (e) => {
  const p = pos(e);
  if (tool === "text") {
    const text = prompt("Label text:");
    if (text) { shapes.push({ type: "text", x: p.x, y: p.y, text, color }); redraw(); }
    return;
  }
  if (tool === "pen") drawing = { type: "pen", pts: [p], color };
  else if (tool === "arrow") drawing = { type: "arrow", x: p.x, y: p.y, x2: p.x, y2: p.y, color };
  else drawing = { type: "box", x: p.x, y: p.y, w: 0, h: 0, color };
  canvas.setPointerCapture(e.pointerId);
});

canvas.addEventListener("pointermove", (e) => {
  if (!drawing) return;
  const p = pos(e);
  if (drawing.type === "pen") drawing.pts.push(p);
  else if (drawing.type === "arrow") { drawing.x2 = p.x; drawing.y2 = p.y; }
  else { drawing.w = p.x - drawing.x; drawing.h = p.y - drawing.y; }
  redraw();
});

canvas.addEventListener("pointerup", () => {
  if (drawing) { shapes.push(drawing); drawing = null; redraw(); }
});

document.querySelectorAll(".tool[data-tool]").forEach((b) =>
  b.addEventListener("click", () => setTool(b.dataset.tool)));
$("color").addEventListener("input", (e) => (color = e.target.value));
$("undo").addEventListener("click", () => { shapes.pop(); redraw(); });
$("clear").addEventListener("click", () => { shapes = []; redraw(); });

function buildBody() {
  const desc = $("desc").value.trim();
  const steps = $("steps").value.trim();
  const lines = [];
  if (desc) lines.push(desc, "");
  if (steps) {
    lines.push("## Steps to reproduce");
    steps.split("\n").map((s) => s.trim()).filter(Boolean).forEach((s, i) =>
      lines.push(s.match(/^\d+[.)]/) ? s : `${i + 1}. ${s}`));
    lines.push("");
  }
  lines.push("## Environment");
  if (capture) {
    lines.push(`- **Page:** ${capture.url}`);
    lines.push(`- **Captured:** ${capture.at}`);
    lines.push(`- **User agent:** ${capture.ua}`);
  }
  return lines.join("\n");
}

$("file").addEventListener("click", async () => {
  const btn = $("file"), status = $("status");
  btn.disabled = true; status.textContent = "Filing…"; status.className = "";
  try {
    const imageB64 = canvas.toDataURL("image/png").split(",")[1];
    const title = $("title").value.trim() || (capture && capture.title) || "Page report";
    const res = await chrome.runtime.sendMessage({ type: "file", payload: { title, body: buildBody(), imageB64 } });
    if (!res || !res.ok) throw new Error((res && res.error) || "failed");
    status.className = "ok";
    status.innerHTML = `✓ Created <strong>${res.id}</strong> in ${res.lane}. ` +
      `<a href="${res.endpoint}/" target="_blank">Open board ↗</a>`;
  } catch (e) {
    status.className = "err";
    status.textContent = "✕ " + (e && e.message ? e.message : e) +
      " — is cboard running and the endpoint correct?";
  } finally {
    btn.disabled = false;
  }
});

(async () => {
  setTool("box");
  const res = await chrome.runtime.sendMessage({ type: "getCapture" });
  if (!res || !res.capture) { $("pagemeta").textContent = "No capture found — close this tab and try again."; return; }
  capture = res.capture;
  $("title").value = capture.title || "";
  $("pagemeta").textContent = capture.url;
  if (res.steps && res.steps.length) {
    $("steps").value = res.steps.map((s, i) => `${i + 1}. ${s}`).join("\n");
  }
  img = new Image();
  img.onload = () => { canvas.width = img.naturalWidth; canvas.height = img.naturalHeight; redraw(); };
  img.src = capture.image;
})();
