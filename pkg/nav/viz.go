package nav

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const FileGraphHTML = "GRAPH.html"

// GraphNode / GraphLink are JSON for the interactive neuron viz.
type GraphNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // module | func
	Group string `json:"group,omitempty"`
	Files int    `json:"files,omitempty"`
	Funcs int    `json:"funcs,omitempty"`
	Hub   bool   `json:"hub,omitempty"`
}

type GraphLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// GraphVizPayload is embedded into GRAPH.html / /_am/map/viz.
type GraphVizPayload struct {
	Title   string               `json:"title"`
	Root    string               `json:"root,omitempty"`
	Focus   string               `json:"focus,omitempty"` // empty = module mesh
	Modules []GraphNode          `json:"modules"`
	Mesh    []GraphLink          `json:"mesh"`
	Subnets map[string]SubnetViz `json:"subnets"`
}

type SubnetViz struct {
	Nodes []GraphNode `json:"nodes"`
	Links []GraphLink `json:"links"`
}

// BuildGraphVizPayload prepares interactive viz data.
func BuildGraphVizPayload(scan ScanResult, g GraphSnapshot, focus string) GraphVizPayload {
	p := GraphVizPayload{
		Title:   scan.Label,
		Root:    scan.Root,
		Focus:   strings.TrimSpace(focus),
		Subnets: map[string]SubnetViz{},
	}
	for _, path := range g.ModOrder {
		var files, funcs int
		for _, m := range scan.Modules {
			if m.RelPath == path {
				files, funcs = m.FileCount, m.FuncCount
				break
			}
		}
		p.Modules = append(p.Modules, GraphNode{
			ID: path, Label: shortMod(path), Kind: "module",
			Files: files, Funcs: funcs,
		})
	}
	for _, e := range g.Mesh {
		p.Mesh = append(p.Mesh, GraphLink{Source: e.From, Target: e.To})
	}
	for path, edges := range g.Subnets {
		nodeSet := map[string]bool{}
		hubSet := map[string]bool{}
		for _, h := range g.Hubs[path] {
			hubSet[h] = true
		}
		var links []GraphLink
		for _, e := range edges {
			nodeSet[e.From] = true
			nodeSet[e.To] = true
			links = append(links, GraphLink{Source: e.From, Target: e.To})
		}
		for _, h := range g.Hubs[path] {
			nodeSet[h] = true
		}
		var nodes []GraphNode
		for n := range nodeSet {
			nodes = append(nodes, GraphNode{
				ID: n, Label: n, Kind: "func", Group: path, Hub: hubSet[n],
			})
		}
		// stable-ish order
		sortGraphNodes(nodes)
		p.Subnets[path] = SubnetViz{Nodes: nodes, Links: links}
		p.Subnets[shortMod(path)] = p.Subnets[path]
	}
	return p
}

func sortGraphNodes(nodes []GraphNode) {
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Label < nodes[j].Label })
}

// RenderGraphHTML returns a self-contained interactive neuron map page.
func RenderGraphHTML(scan ScanResult, g GraphSnapshot, focus string) ([]byte, error) {
	payload := BuildGraphVizPayload(scan, g, focus)
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	title := html.EscapeString(scan.Label)
	page := strings.ReplaceAll(graphHTMLTemplate, "__TITLE__", title)
	page = strings.ReplaceAll(page, "__PAYLOAD__", string(raw))
	return []byte(page), nil
}

// WriteGraphHTML writes ~/.am/workspaces/<name>/GRAPH.html (and docs/ for amux).
func WriteGraphHTML(startDir, focus string) (string, Bundle, error) {
	scan, g, err := LoadGraphForDir(startDir)
	if err != nil {
		return "", Bundle{}, err
	}
	b := Resolve(scan.Root)
	if err := os.MkdirAll(b.WorkspaceDir, 0o755); err != nil {
		return "", b, err
	}
	htmlBytes, err := RenderGraphHTML(scan, g, focus)
	if err != nil {
		return "", b, err
	}
	path := filepath.Join(b.WorkspaceDir, FileGraphHTML)
	if err := os.WriteFile(path, htmlBytes, 0o644); err != nil {
		return "", b, err
	}
	if b.IsAmuxRepository() {
		docs := filepath.Join(b.ProjectRoot, "docs")
		_ = os.MkdirAll(docs, 0o755)
		_ = os.WriteFile(filepath.Join(docs, FileGraphHTML), htmlBytes, 0o644)
	}
	return path, b, nil
}

// OpenInBrowser opens a local file or URL.
func OpenInBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

func renderMermaidMesh(g GraphSnapshot) string {
	var b strings.Builder
	b.WriteString("```mermaid\nflowchart LR\n")
	seenNode := map[string]bool{}
	idOf := func(path string) string {
		s := shortMod(path)
		s = strings.ReplaceAll(s, "/", "_")
		s = strings.ReplaceAll(s, "-", "_")
		s = strings.ReplaceAll(s, ".", "_")
		if s == "" {
			s = "mod"
		}
		if s[0] >= '0' && s[0] <= '9' {
			s = "m_" + s
		}
		return s
	}
	ensure := func(path string) {
		id := idOf(path)
		if seenNode[id] {
			return
		}
		seenNode[id] = true
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", id, shortMod(path))
	}
	if len(g.Mesh) == 0 {
		b.WriteString("  empty[no edges]\n")
	} else {
		for _, e := range g.Mesh {
			ensure(e.From)
			ensure(e.To)
			fmt.Fprintf(&b, "  %s --> %s\n", idOf(e.From), idOf(e.To))
		}
	}
	b.WriteString("```\n")
	return b.String()
}

// graphHTMLTemplate — vanilla force-directed graph, click module → subnet.
const graphHTMLTemplate = `<!DOCTYPE html>
<html lang="vi">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>amux GRAPH — __TITLE__</title>
<style>
  :root { --bg:#0f1419; --panel:#1a2332; --ink:#e7ecf3; --muted:#8b9bb4; --accent:#5b9cff; --hub:#ffb020; --link:#3d5a80; }
  * { box-sizing: border-box; }
  body { margin:0; font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, sans-serif; background:var(--bg); color:var(--ink); }
  header { display:flex; flex-wrap:wrap; gap:12px; align-items:center; padding:12px 16px; background:var(--panel); border-bottom:1px solid #243044; }
  header h1 { font-size:16px; margin:0; font-weight:600; }
  header .meta { color:var(--muted); font-size:12px; }
  #toolbar { display:flex; gap:8px; flex-wrap:wrap; margin-left:auto; }
  button, select { background:#243044; color:var(--ink); border:1px solid #345; border-radius:8px; padding:6px 10px; cursor:pointer; font-size:13px; }
  button:hover { border-color:var(--accent); }
  main { display:grid; grid-template-columns: 1fr 280px; height: calc(100vh - 54px); }
  #canvas-wrap { position:relative; overflow:hidden; }
  canvas { display:block; width:100%; height:100%; cursor:grab; }
  canvas.dragging { cursor:grabbing; }
  aside { background:var(--panel); border-left:1px solid #243044; padding:14px; overflow:auto; font-size:13px; }
  aside h2 { font-size:13px; margin:0 0 8px; color:var(--accent); text-transform:uppercase; letter-spacing:.04em; }
  aside .hint { color:var(--muted); font-size:12px; line-height:1.45; margin-bottom:12px; }
  #detail code { background:#0f1419; padding:1px 5px; border-radius:4px; }
  #edges { list-style:none; padding:0; margin:0; }
  #edges li { padding:4px 0; border-bottom:1px solid #243044; color:var(--muted); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size:11px; }
  .legend span { display:inline-block; width:10px; height:10px; border-radius:50%; margin-right:6px; vertical-align:middle; }
  @media (max-width: 800px) { main { grid-template-columns: 1fr; } aside { display:none; } }
</style>
</head>
<body>
<header>
  <h1>GRAPH · __TITLE__</h1>
  <span class="meta" id="subtitle">module mesh</span>
  <div id="toolbar">
    <select id="modSelect"><option value="">Module mesh (all)</option></select>
    <button type="button" id="btnMesh">Mesh</button>
    <button type="button" id="btnReload">Reset layout</button>
  </div>
</header>
<main>
  <div id="canvas-wrap"><canvas id="c"></canvas></div>
  <aside>
    <h2>Liên kết</h2>
    <p class="hint">Kéo node · scroll zoom · click module để xem subnet function (nơ-ron con). Mũi tên = hướng gọi / import.</p>
    <div class="legend" style="margin-bottom:10px">
      <div><span style="background:var(--accent)"></span> module / func</div>
      <div><span style="background:var(--hub)"></span> hub</div>
    </div>
    <div id="detail"></div>
    <h2 style="margin-top:16px">Edges</h2>
    <ul id="edges"></ul>
  </aside>
</main>
<script>
const DATA = __PAYLOAD__;
const canvas = document.getElementById('c');
const ctx = canvas.getContext('2d');
const subtitle = document.getElementById('subtitle');
const detail = document.getElementById('detail');
const edgesEl = document.getElementById('edges');
const modSelect = document.getElementById('modSelect');

let nodes = [], links = [];
let sim = [];
let transform = { x: 0, y: 0, k: 1 };
let drag = null;
let focus = DATA.focus || '';

DATA.modules.forEach(m => {
  const o = document.createElement('option');
  o.value = m.id; o.textContent = m.label + ' (' + m.funcs + 'f)';
  modSelect.appendChild(o);
});

function resize() {
  const wrap = document.getElementById('canvas-wrap');
  canvas.width = wrap.clientWidth * devicePixelRatio;
  canvas.height = wrap.clientHeight * devicePixelRatio;
  canvas.style.width = wrap.clientWidth + 'px';
  canvas.style.height = wrap.clientHeight + 'px';
  ctx.setTransform(devicePixelRatio, 0, 0, devicePixelRatio, 0, 0);
}
window.addEventListener('resize', () => { resize(); });

function loadView(mod) {
  focus = mod || '';
  modSelect.value = focus;
  if (!focus) {
    subtitle.textContent = 'module mesh — A → B = import/depends';
    nodes = DATA.modules.map(m => ({...m}));
    links = DATA.mesh.map(l => ({...l}));
  } else {
    const key = focus;
    const sub = DATA.subnets[key] || DATA.subnets[key.replace(/^pkg\\//,'')] || {nodes:[], links:[]};
    subtitle.textContent = 'subnet · ' + (nodesLabel(key)) + ' — func → func';
    nodes = (sub.nodes || []).map(n => ({...n}));
    links = (sub.links || []).map(l => ({...l}));
  }
  const W = canvas.clientWidth || 800, H = canvas.clientHeight || 600;
  const map = {};
  nodes.forEach((n,i) => {
    const a = (i / Math.max(nodes.length,1)) * Math.PI * 2;
    n.x = W/2 + Math.cos(a) * Math.min(W,H) * 0.28;
    n.y = H/2 + Math.sin(a) * Math.min(W,H) * 0.28;
    n.vx = 0; n.vy = 0;
    map[n.id] = n;
  });
  links.forEach(l => { l.source = map[l.source] || l.source; l.target = map[l.target] || l.target; });
  links = links.filter(l => typeof l.source === 'object' && typeof l.target === 'object');
  renderEdgeList();
  detail.innerHTML = focus
    ? '<p>Đang xem subnet <code>' + esc(nodesLabel(focus)) + '</code>. Click trống hoặc bấm Mesh để về tổng.</p>'
    : '<p>Click một <b>module</b> để mở mạng function bên trong.</p>';
}

function nodesLabel(id) {
  const m = DATA.modules.find(x => x.id === id);
  return m ? m.label : id;
}
function esc(s){ return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

function renderEdgeList() {
  edgesEl.innerHTML = '';
  const max = 80;
  links.slice(0, max).forEach(l => {
    const li = document.createElement('li');
    const a = l.source.label || l.source.id;
    const b = l.target.label || l.target.id;
    li.textContent = a + ' → ' + b;
    edgesEl.appendChild(li);
  });
  if (links.length > max) {
    const li = document.createElement('li');
    li.textContent = '… +' + (links.length - max) + ' more';
    edgesEl.appendChild(li);
  }
}

function tick() {
  const W = canvas.clientWidth, H = canvas.clientHeight;
  // repulsion
  for (let i=0;i<nodes.length;i++){
    for (let j=i+1;j<nodes.length;j++){
      let dx = nodes[i].x - nodes[j].x, dy = nodes[i].y - nodes[j].y;
      let d2 = dx*dx + dy*dy || 1;
      let f = 1200 / d2;
      let d = Math.sqrt(d2);
      dx/=d; dy/=d;
      nodes[i].vx += dx*f; nodes[i].vy += dy*f;
      nodes[j].vx -= dx*f; nodes[j].vy -= dy*f;
    }
  }
  // springs
  links.forEach(l => {
    let dx = l.target.x - l.source.x, dy = l.target.y - l.source.y;
    let d = Math.sqrt(dx*dx+dy*dy) || 1;
    let f = (d - 110) * 0.02;
    dx/=d; dy/=d;
    l.source.vx += dx*f; l.source.vy += dy*f;
    l.target.vx -= dx*f; l.target.vy -= dy*f;
  });
  // center gravity
  nodes.forEach(n => {
    n.vx += (W/2 - n.x) * 0.005;
    n.vy += (H/2 - n.y) * 0.005;
    if (drag && drag.id === n.id) { n.vx=0; n.vy=0; return; }
    n.vx *= 0.85; n.vy *= 0.85;
    n.x += n.vx; n.y += n.vy;
  });
}

function drawArrow(x1,y1,x2,y2) {
  const ang = Math.atan2(y2-y1, x2-x1);
  const len = Math.hypot(x2-x1,y2-y1) || 1;
  const shrink = Math.min(22, len*0.35);
  const sx = x1 + Math.cos(ang)*shrink;
  const sy = y1 + Math.sin(ang)*shrink;
  const ex = x2 - Math.cos(ang)*shrink;
  const ey = y2 - Math.sin(ang)*shrink;
  ctx.beginPath();
  ctx.moveTo(sx,sy); ctx.lineTo(ex,ey);
  ctx.strokeStyle = 'rgba(91,156,255,0.35)';
  ctx.lineWidth = 1.2;
  ctx.stroke();
  const ah = 7;
  ctx.beginPath();
  ctx.moveTo(ex,ey);
  ctx.lineTo(ex - ah*Math.cos(ang-0.4), ey - ah*Math.sin(ang-0.4));
  ctx.lineTo(ex - ah*Math.cos(ang+0.4), ey - ah*Math.sin(ang+0.4));
  ctx.closePath();
  ctx.fillStyle = 'rgba(91,156,255,0.55)';
  ctx.fill();
}

function paint() {
  const W = canvas.clientWidth, H = canvas.clientHeight;
  ctx.clearRect(0,0,W,H);
  // subtle grid
  ctx.save();
  ctx.translate(transform.x, transform.y);
  ctx.scale(transform.k, transform.k);
  links.forEach(l => drawArrow(l.source.x, l.source.y, l.target.x, l.target.y));
  nodes.forEach(n => {
    const r = n.kind === 'module' ? 8 + Math.min(14, (n.funcs||0)/20) : (n.hub ? 9 : 6);
    ctx.beginPath();
    ctx.arc(n.x, n.y, r, 0, Math.PI*2);
    ctx.fillStyle = n.hub ? '#ffb020' : '#5b9cff';
    ctx.fill();
    ctx.lineWidth = 1.5;
    ctx.strokeStyle = '#0f1419';
    ctx.stroke();
    ctx.fillStyle = '#e7ecf3';
    ctx.font = '12px ui-sans-serif, system-ui';
    ctx.textAlign = 'center';
    ctx.fillText(n.label, n.x, n.y + r + 14);
  });
  ctx.restore();
}

function loop(){ tick(); paint(); requestAnimationFrame(loop); }

function screenToWorld(sx, sy) {
  return { x: (sx - transform.x)/transform.k, y: (sy - transform.y)/transform.k };
}
function hit(sx, sy) {
  const p = screenToWorld(sx, sy);
  for (let i=nodes.length-1;i>=0;i--){
    const n = nodes[i];
    const r = 16;
    if ((p.x-n.x)**2 + (p.y-n.y)**2 <= r*r) return n;
  }
  return null;
}

canvas.addEventListener('mousedown', e => {
  const rect = canvas.getBoundingClientRect();
  const n = hit(e.clientX-rect.left, e.clientY-rect.top);
  if (n) { drag = n; canvas.classList.add('dragging'); }
  else { drag = { pan:true, x:e.clientX, y:e.clientY, tx:transform.x, ty:transform.y }; canvas.classList.add('dragging'); }
});
window.addEventListener('mousemove', e => {
  if (!drag) return;
  if (drag.pan) {
    transform.x = drag.tx + (e.clientX - drag.x);
    transform.y = drag.ty + (e.clientY - drag.y);
    return;
  }
  const rect = canvas.getBoundingClientRect();
  const p = screenToWorld(e.clientX-rect.left, e.clientY-rect.top);
  drag.x = p.x; drag.y = p.y;
});
window.addEventListener('mouseup', e => {
  if (!drag) return;
  const was = drag;
  drag = null; canvas.classList.remove('dragging');
  if (was.pan) return;
  if (!focus && was.kind === 'module') {
    loadView(was.id);
  } else if (was.kind === 'func') {
    detail.innerHTML = '<p>Function <code>' + esc(was.label) + '</code>' + (was.hub?' (hub)':'') +
      ' · module <code>' + esc(nodesLabel(focus)) + '</code></p><p class="hint">Chi tiết: <code>am map get --func ' + esc(was.label) + '</code></p>';
  }
});
canvas.addEventListener('click', e => {
  if (focus) {
    const rect = canvas.getBoundingClientRect();
    if (!hit(e.clientX-rect.left, e.clientY-rect.top) && !e.detail) { /* noop */ }
  }
});
canvas.addEventListener('wheel', e => {
  e.preventDefault();
  const rect = canvas.getBoundingClientRect();
  const mx = e.clientX - rect.left, my = e.clientY - rect.top;
  const before = screenToWorld(mx, my);
  transform.k *= (e.deltaY < 0 ? 1.1 : 0.9);
  transform.k = Math.max(0.3, Math.min(3, transform.k));
  const after = screenToWorld(mx, my);
  transform.x += (after.x - before.x) * transform.k;
  transform.y += (after.y - before.y) * transform.k;
}, {passive:false});

document.getElementById('btnMesh').onclick = () => loadView('');
document.getElementById('btnReload').onclick = () => loadView(focus);
modSelect.onchange = () => loadView(modSelect.value);

resize();
loadView(focus);
loop();
</script>
</body>
</html>
`
