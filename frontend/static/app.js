const $ = id => document.getElementById(id);
const STORAGE_KEY = 'gp_user';
const COLS_KEY = 'gp_columns';
const TYPE_NAME = {0:'AUTO',1:'SOURCE',2:'MC-JAVA',3:'MC-LEGACY',4:'MC-BEDROCK'};

const DEFAULT_IMG = "data:image/svg+xml;utf8," + encodeURIComponent(
  `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64">` +
  `<rect width="64" height="64" fill="#000"/><rect width="32" height="32" fill="#f0f"/>` +
  `<rect x="32" y="32" width="32" height="32" fill="#f0f"/></svg>`);

let servers = [];
const expanded = new Set();
const flashing = new Set();
const pendingPolls = new Set();
const cooldowns = {};
let editingId = null;
let checkingAddr = false;
let staleRefreshCooldown = 0;

// ---------- helpers ----------
const esc = s => String(s ?? '').replace(/[&<>"']/g, c =>
  ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

const fmtTime = ts => ts ? new Date(ts).toLocaleTimeString('ru-RU', { hour12: false }) : '—';
const fmtDateTime = ts => ts ? new Date(ts).toLocaleString('ru-RU', { hour12: false }) : '—';
const trunc = s => s == null || s === '' ? '—'
  : `<span class="cell-trunc" title="${esc(s)}">${esc(s)}</span>`;

const asArray = v => Array.isArray(v) ? v : (typeof v === 'string' ? [v] : []);

function fmtAgo(sec) {
  if (sec < 0) sec = 0;
  if (sec < 60) return `${sec}s назад`;
  const m = Math.floor(sec / 60), s = sec % 60;
  if (m < 60) return s > 0 ? `${m}m ${s}s назад` : `${m}m назад`;
  const h = Math.floor(m / 60), mm = m % 60;
  if (h < 24) return mm > 0 ? `${h}h ${mm}m назад` : `${h}h назад`;
  const d = Math.floor(h / 24), hh = h % 24;
  return hh > 0 ? `${d}d ${hh}h назад` : `${d}d назад`;
}

function fmtCountdown(sec) {
  if (sec <= 0) return '0s';
  if (sec < 60) return `${sec}s`;
  const m = Math.floor(sec / 60), s = sec % 60;
  if (m < 60) return s > 0 ? `${m}m ${s}s` : `${m}m`;
  const h = Math.floor(m / 60), mm = m % 60;
  if (h < 24) return mm > 0 ? `${h}h ${mm}m` : `${h}h`;
  const d = Math.floor(h / 24), hh = h % 24;
  return hh > 0 ? `${d}d ${hh}h` : `${d}d`;
}

setInterval(() => {
  const now = Date.now();
  document.querySelectorAll('.live-timer').forEach(el => {
    const ts = parseInt(el.dataset.ts, 10);
    if (!ts) return;
    if (el.dataset.mode === 'since') {
      el.textContent = fmtAgo(Math.max(0, Math.floor((now - ts) / 1000)));
    } else if (el.dataset.mode === 'until') {
      el.textContent = fmtCountdown(Math.max(0, Math.ceil((ts - now) / 1000)));
    }
  });
}, 1000);

const statusCell = st => !st
  ? `<span class="st-wait">○ ЖДЁТ</span>`
  : st.ok ? `<span class="st-ok">● ОНЛАЙН</span>` : `<span class="st-fail">● ОФФЛАЙН</span>`;

const playersCell = st => {
  const p = st?.payload;
  return p && p.online_players != null ? `${p.online_players}/${p.max_players ?? 0}` : '—';
};

function lastPollCell(s) {
  const ts = s.state?.checked_at;
  if (!ts) return '—';
  const ageSec = Math.floor((Date.now() - ts) / 1000);
  const eff = s.effective_interval_sec || 60;
  const stale = ageSec > eff * 1.5;
  const color = stale ? 'var(--warn)' : 'var(--fg)';
  const badge = s.state?.from_cache
    ? ` <span class="cache-mark" title="данные из кеша — реального опроса не было">💾</span>`
    : '';
  return `<span class="live-timer" data-mode="since" data-ts="${ts}" style="color:${color}" title="${fmtDateTime(ts)}">${fmtAgo(ageSec)}</span>${badge}`;
}

function nextPollCell(s) {
  const nextAt = s.next_poll_at;
  if (!nextAt) return '—';
  const remainingMs = nextAt - Date.now();
  if (remainingMs <= 0) {
    return `<span style="color:var(--warn);font-variant-numeric:tabular-nums">0s</span>`;
  }
  return `<span class="live-timer" data-mode="until" data-ts="${nextAt}"
                style="color:var(--dim);font-variant-numeric:tabular-nums"
                title="${fmtDateTime(nextAt)}">${fmtCountdown(Math.ceil(remainingMs / 1000))}</span>`;
}

const FAVICON_RE = /^data:image\/(png|jpeg|gif|webp|x-icon);base64,[A-Za-z0-9+/]+=*$/;
function imageUrl(s) {
  const ic = s.state?.payload?.icon;
  if (ic && FAVICON_RE.test(ic)) return ic;
  if (s.icon_url && /^\/static\//.test(s.icon_url)) return s.icon_url;
  return DEFAULT_IMG;
}
window.onImgError = img => { img.onerror = null; img.src = DEFAULT_IMG; };

function highlightJSON(obj) {
  let json;
  try { json = typeof obj === 'string' ? obj : JSON.stringify(obj, null, 2); }
  catch { json = String(obj); }
  json = json.replace(/[&<>]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;'}[c]));
  return json.replace(
    /("(?:\\u[a-fA-F0-9]{4}|\\[^u]|[^\\"])*"\s*:?|\btrue\b|\bfalse\b|\bnull\b|-?\d+(?:\.\d+)?(?:[eE][+\-]?\d+)?)/g,
    m => {
      let cls = 'j-num';
      if (m[0] === '"') cls = /:\s*$/.test(m) ? 'j-key' : 'j-str';
      else if (m === 'true' || m === 'false') cls = 'j-bool';
      else if (m === 'null') cls = 'j-null';
      return `<span class="${cls}">${m}</span>`;
    });
}

// ---------- columns ----------
const COLUMNS = [
  { key:'idx', label:'', width:'60px', always:true, pin:'start',
    render:(s,i)=>`<span class="chev">${expanded.has(s.id)?'▼':'▶'}</span><span class="rownum">${i+1}</span>` },

  { key:'image', label:'', width:'56px', always:true,
    render:s=>`<img class="srv-img" src="${imageUrl(s)}" onerror="onImgError(this)" alt="">` },

  { key:'address', label:'АДРЕС', width:'auto', always:true,
    render:s=>`<a href="#" onclick="toggleExpand(${s.id},event);return false">${esc(s.address)}</a>`
      + (s.label?` <span class="pill">${esc(s.label)}</span>`:'')
      + (s.enabled===false?` <span class="pill" style="color:var(--warn);border-color:var(--warn)">OFF</span>`:'') },

  { key:'label',     label:'МЕТКА',       width:'120px', render:s=>s.label?esc(s.label):'—' },
  { key:'type',      label:'ТИП',         width:'110px', render:s=>`<span class="pill">${TYPE_NAME[s.type]||'?'}</span>` },
  { key:'interval',  label:'ИНТЕРВАЛ',    width:'90px',  render:s=>s.effective_interval_sec+'s' },
  { key:'status',    label:'СТАТУС',      width:'110px', render:s=>statusCell(s.state) },
  { key:'players',   label:'ИГРОКИ',      width:'110px', render:s=>playersCell(s.state) },
  { key:'next_poll', label:'СЛЕД. ОПРОС', width:'130px', render:s=>nextPollCell(s) },
  { key:'checked',   label:'ПОСЛЕДНИЙ',   width:'150px', render:s=>lastPollCell(s) },

  { key:'name',     label:'ИМЯ',      width:'160px', render:s=>trunc(s.state?.payload?.name) },
  { key:'motd',     label:'MOTD',     width:'200px', render:s=>{
      const arr = asArray(s.state?.payload?.motd?.clean);
      return trunc(arr.join(' / '));
    } },
  { key:'version',  label:'ВЕРСИЯ',   width:'150px', render:s=>{
      const arr = asArray(s.state?.payload?.versions);
      return trunc(arr.slice(0,3).join(', '));
    } },
  { key:'protocol', label:'PROTO',    width:'80px',  render:s=>s.state?.payload?.protocol ?? '—' },
  { key:'software', label:'SOFTWARE', width:'130px', render:s=>trunc(s.state?.payload?.software) },
  { key:'gamemode', label:'GAMEMODE', width:'120px', render:s=>trunc(s.state?.payload?.gamemode) },
  { key:'map',      label:'MAP',      width:'140px', render:s=>trunc(s.state?.payload?.map) },
  { key:'ip',       label:'IP:PORT',  width:'170px',
    render:s=>{const p=s.state?.payload; if(!p) return '—'; return esc(p.ip||'')+(p.port?':'+p.port:'');} },

  { key:'actions',  label:'ДЕЙСТВИЯ', width:'230px', always:true, pin:'end',
    render:s=>{
      const pending = pendingPolls.has(s.id);
      const cooling = cooldowns[s.id] && Date.now() < cooldowns[s.id];
      const btnText = pending ? '…' : (cooling ? '⏳' : 'ОПРОС');
      const disabled = pending || cooling;
      return `<div class="actions">
        <button class="ghost sm" data-poll-id="${s.id}" onclick="pollId(${s.id},event)" ${disabled?'disabled':''}>${btnText}</button>
        <button class="ghost sm" onclick="editServer(${s.id},event)">ИЗМ</button>
        <button class="danger sm" onclick="delServer(${s.id},event)">✕</button>
      </div>`;
    } },
];

const DEFAULT_COLS = ['idx','image','address','type','interval','status','players','next_poll','checked','actions'];
let visibleCols = loadCols();

function loadCols(){
  try {
    const raw = localStorage.getItem(COLS_KEY);
    let arr = null;
    if (raw) {
      const parsed = JSON.parse(raw);
      if (Array.isArray(parsed) && parsed.length) arr = parsed;
    }
    if (!arr) arr = [...DEFAULT_COLS];

    const OLD1 = ['idx','image','address','type','interval','status','players','checked','actions'];
    const OLD2 = ['idx','image','address','type','interval','status','players','next_poll','actions'];
    const eq = (a,b) => a.length === b.length && a.every((k,i) => k === b[i]);
    if (eq(arr, OLD1) || eq(arr, OLD2)) arr = [...DEFAULT_COLS];

    const allKeys = new Set(COLUMNS.map(c => c.key));
    arr = arr.filter(k => allKeys.has(k));
    COLUMNS.forEach(c => {
      if (c.always && !arr.includes(c.key)) arr.push(c.key);
    });
    return arr;
  } catch { return [...DEFAULT_COLS]; }
}
function saveCols(){ localStorage.setItem(COLS_KEY, JSON.stringify(visibleCols)); }
const isVisible = k => visibleCols.includes(k);

function activeColumns() {
  const byKey = Object.fromEntries(COLUMNS.map(c => [c.key, c]));
  const pinStart = COLUMNS.filter(c => c.pin === 'start');
  const pinEnd   = COLUMNS.filter(c => c.pin === 'end');
  const pinned   = new Set([...pinStart.map(c=>c.key), ...pinEnd.map(c=>c.key)]);
  const middle = visibleCols
    .filter(k => !pinned.has(k))
    .map(k => byKey[k])
    .filter(Boolean);
  return [...pinStart, ...middle, ...pinEnd];
}

function filteredServers() {
  const q = ($('search')?.value || '').toLowerCase().trim();
  if (!q) return servers;
  return servers.filter(s =>
    (s.address || '').toLowerCase().includes(q) ||
    (s.label || '').toLowerCase().includes(q));
}

function buildRowHTML(s, i, cols) {
  return cols.map(c => `<td>${c.render(s, i) ?? ''}</td>`).join('');
}

function attachRowHandlers(tr, s) {
  tr.addEventListener('click', ev => {
    if (ev.target.closest('button,a,input,select')) return;
    toggleExpand(s.id);
  });
}

// ---------- drag-n-drop колонок ----------
let draggingKey = null;

function attachHeaderDnD() {
  document.querySelectorAll('#thead th[data-col-key]').forEach(th => {
    const key = th.dataset.colKey;
    const col = COLUMNS.find(c => c.key === key);
    if (!col || col.pin) return;

    th.draggable = true;
    th.addEventListener('dragstart', e => {
      draggingKey = key;
      th.classList.add('dragging');
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', key);
    });
    th.addEventListener('dragend', () => {
      th.classList.remove('dragging');
      draggingKey = null;
      document.querySelectorAll('#thead th').forEach(x => x.classList.remove('drop-target'));
    });
    th.addEventListener('dragover', e => {
      if (!draggingKey || draggingKey === key) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
      th.classList.add('drop-target');
    });
    th.addEventListener('dragleave', () => th.classList.remove('drop-target'));
    th.addEventListener('drop', e => {
      e.preventDefault();
      th.classList.remove('drop-target');
      const srcKey = e.dataTransfer.getData('text/plain');
      if (!srcKey || srcKey === key) return;
      reorderColumn(srcKey, key);
    });
  });
}

function reorderColumn(srcKey, targetKey) {
  const arr = [...visibleCols];
  const srcIdx = arr.indexOf(srcKey);
  const tgtIdx = arr.indexOf(targetKey);
  if (srcIdx < 0 || tgtIdx < 0) return;
  arr.splice(srcIdx, 1);
  arr.splice(tgtIdx, 0, srcKey);
  visibleCols = arr;
  saveCols();
  renderTable();
}

// ---------- rendering ----------
function renderTable(){
  try {
    const cols = activeColumns();
    $('thead').innerHTML = '<tr>' + cols.map(c =>
      `<th data-col-key="${c.key}" style="width:${c.width}">${esc(c.label)}</th>`).join('') + '</tr>';
    attachHeaderDnD();

    const tbody = $('rows');
    tbody.innerHTML = '';
    const visible = filteredServers();
    if (!visible.length) {
      const msg = servers.length ? 'нет совпадений' : 'нет серверов — добавь первый';
      tbody.innerHTML = `<tr><td colspan="${cols.length}" style="color:var(--dim)">${msg}</td></tr>`;
      return;
    }
    visible.forEach((s, i) => {
      const tr = document.createElement('tr');
      tr.dataset.rowId = String(s.id);
      tr.className = 'expandable'
        + (expanded.has(s.id) ? ' open' : '')
        + (flashing.has(s.id) ? ' flash' : '');
      try {
        tr.innerHTML = buildRowHTML(s, i, cols);
      } catch (e) {
        console.error('render row failed', s, e);
        tr.innerHTML = `<td colspan="${cols.length}" style="color:var(--err)">ошибка рендера (${esc(s.address || 'unknown')}): ${esc(e.message)}</td>`;
      }
      attachRowHandlers(tr, s);
      tbody.appendChild(tr);
      if (expanded.has(s.id)) {
        const dr = document.createElement('tr');
        dr.className = 'detail-row';
        try {
          dr.innerHTML = `<td colspan="${cols.length}">${renderDetail(s)}</td>`;
        } catch (e) {
          dr.innerHTML = `<td colspan="${cols.length}" style="color:var(--err)">ошибка рендера деталей: ${esc(e.message)}</td>`;
        }
        tbody.appendChild(dr);
      }
    });
  } catch (e) {
    console.error('renderTable failed', e);
  }
}

function updateRowById(id){
  const s = servers.find(x => x.id === id);
  if (!s) return;
  const visible = filteredServers();
  const i = visible.findIndex(x => x.id === id);
  const tr = document.querySelector(`tr.expandable[data-row-id="${id}"]`);
  if (i < 0) { if (tr) tr.remove(); return; }
  if (!tr) { renderTable(); return; }

  const cols = activeColumns();
  const tds = tr.children;

  cols.forEach((c, idx) => {
    const td = tds[idx];
    if (!td) return;
    if (c.key === 'actions') {
      const pollBtn = td.querySelector('button[data-poll-id]');
      if (pollBtn) {
        const pending = pendingPolls.has(id);
        const cooling = cooldowns[id] && Date.now() < cooldowns[id];
        pollBtn.disabled = pending || cooling;
        pollBtn.textContent = pending ? '…' : (cooling ? '⏳' : 'ОПРОС');
        return;
      }
      td.innerHTML = c.render(s, i) ?? '';
      return;
    }
    if (c.key === 'next_poll' || c.key === 'checked') {
      const el = td.querySelector('.live-timer');
      const newTs = c.key === 'next_poll'
        ? (s.next_poll_at || null)
        : (s.state?.checked_at || null);
      if (el && newTs !== null && parseInt(el.dataset.ts, 10) === newTs) {
        if (c.key === 'checked') {
          const hasBadge = !!td.querySelector('.cache-mark');
          const needBadge = !!s.state?.from_cache;
          if (hasBadge !== needBadge) {
            td.innerHTML = c.render(s, i) ?? '';
          }
        }
        return;
      }
      td.innerHTML = c.render(s, i) ?? '';
      return;
    }
    const newHTML = c.render(s, i) ?? '';
    if (td.innerHTML !== newHTML) td.innerHTML = newHTML;
  });

  const isOpen = expanded.has(id);
  const shouldFlash = flashing.has(id);
  const want = 'expandable' + (isOpen ? ' open' : '') + (shouldFlash ? ' flash' : '');
  if (tr.className !== want) tr.className = want;

  let dr = tr.nextElementSibling;
  if (isOpen) {
    const detailHTML = `<td colspan="${cols.length}">${renderDetail(s)}</td>`;
    if (dr && dr.classList.contains('detail-row')) {
      if (dr.innerHTML !== detailHTML) dr.innerHTML = detailHTML;
    } else {
      dr = document.createElement('tr');
      dr.className = 'detail-row';
      dr.innerHTML = detailHTML;
      tr.after(dr);
    }
  } else if (dr && dr.classList.contains('detail-row')) {
    dr.remove();
  }
}

function renderDetail(s){
  const st = s.state;
  if (!st) return `<span style="color:var(--dim)">ещё не опрашивался</span>`;
  if (st.payload) return renderServerCard(st.payload, { withRaw: true });
  if (st.error) return `<div class="sec-title">ОШИБКА</div><pre class="json">${esc(st.error)}</pre>`;
  return `<span style="color:var(--dim)">—</span>`;
}

function toggleExpand(id, ev){
  if (ev) ev.stopPropagation();
  if (expanded.has(id)) expanded.delete(id); else expanded.add(id);
  updateRowById(id);
}

function renderServerCard(p, { withRaw = false } = {}){
  if (!p) return '<i style="color:var(--dim)">пусто</i>';
  const KV  = 'display:grid;grid-template-columns:180px 1fr;gap:4px 12px;font-size:12px';
  const LBL = 'color:var(--dim);font-weight:400';
  const pl = asArray(p.players);
  const versArr = asArray(p.versions);
  const motdArr = asArray(p.motd?.clean);
  const rawArr = asArray(p.motd?.raw);
  const motdLines = motdArr.join(' / ') || rawArr.join(' / ') || '—';
  const versions  = versArr.slice(0,8).join(', ') + (versArr.length>8?'…':'');

  let h = `<div class="kv" style="${KV}">
    <b style="${LBL}">GameKind</b><span>${esc(p.game_kind||'—')}</span>
    <b style="${LBL}">Online</b><span style="color:${p.online?'var(--acc)':'var(--err)'}">${p.online?'YES':'NO'}</span>
    <b style="${LBL}">Name</b><span>${esc(p.name||'—')}</span>
    <b style="${LBL}">IP : Port</b><span>${esc(p.ip||'')}${p.port?':'+p.port:''}</span>
    ${p.hostname?`<b style="${LBL}">Hostname</b><span>${esc(p.hostname)}</span>`:''}
    <b style="${LBL}">Players</b><span>${p.online_players??'—'} / ${p.max_players??'—'}</span>
    <b style="${LBL}">MOTD</b><span>${esc(motdLines)}</span>
    ${versions?`<b style="${LBL}">Versions</b><span>${esc(versions)}</span>`:''}
    ${p.protocol?`<b style="${LBL}">Protocol</b><span>${p.protocol}</span>`:''}
    ${p.software?`<b style="${LBL}">Software</b><span>${esc(p.software)}</span>`:''}
    ${p.gamemode?`<b style="${LBL}">Gamemode</b><span>${esc(p.gamemode)}</span>`:''}
    ${p.map?`<b style="${LBL}">Map</b><span>${esc(p.map)}</span>`:''}
    ${p.game_id>=0?`<b style="${LBL}">Game ID</b><span>${p.game_id}</span>`:''}
    ${p.serverid?`<b style="${LBL}">ServerID</b><span>${esc(p.serverid)}</span>`:''}
  </div>`;
  if (pl.length) {
    h += `<div class="sec-title">ИГРОКИ (${pl.length})</div>
      <div style="max-height:180px;overflow:auto;font-size:11px">
        ${pl.slice(0,80).map(x => `<div>${esc(x.name)} ${x.time>=0?`<span style="color:var(--dim)">${x.time}s</span>`:''}</div>`).join('')}
        ${pl.length>80?`<div style="color:var(--dim)">… и ещё ${pl.length-80}</div>`:''}
      </div>`;
  }
  if (withRaw) {
    h += `<details class="raw"><summary>RAW JSON</summary><pre class="json">${highlightJSON(p)}</pre></details>`;
  }
  return h;
}

// ---------- column picker ----------
function renderColPicker(){
  const menu = $('colPickerMenu');
  const cols = COLUMNS.filter(c => !c.always);
  menu.innerHTML =
    cols.map(c => `<label><input type="checkbox" data-col="${c.key}" ${isVisible(c.key)?'checked':''}>${esc(c.label)}</label>`).join('')
    + `<div class="sep">ДЕЙСТВИЯ</div><button class="ghost" style="width:100%" onclick="resetCols()">СБРОСИТЬ</button>`;
  menu.querySelectorAll('input[type=checkbox]').forEach(cb => {
    cb.addEventListener('change', () => {
      const k = cb.dataset.col;
      if (cb.checked) {
        if (!visibleCols.includes(k)) visibleCols.push(k);
      } else {
        visibleCols = visibleCols.filter(x => x !== k);
      }
      saveCols(); renderTable();
    });
  });
}
function toggleColPicker(ev){
  ev.stopPropagation();
  renderColPicker();
  $('colPickerMenu').classList.toggle('on');
}
function resetCols(){
  visibleCols = [...DEFAULT_COLS];
  saveCols(); renderColPicker(); renderTable();
}
document.addEventListener('click', ev => {
  if (!ev.target.closest('.colpicker')) $('colPickerMenu').classList.remove('on');
});

// ---------- API ----------
async function api(path, opts = {}) {
  const r = await fetch(path, { credentials: 'include', headers: { 'Content-Type': 'application/json' }, ...opts });
  const text = await r.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch {}
  return { ok: r.ok, status: r.status, data };
}

async function loadMe(){
  const r = await api('/api/me');
  if (r.ok && r.data) {
    $('who').textContent = r.data.username;
    localStorage.setItem(STORAGE_KEY, r.data.username);
  } else {
    localStorage.removeItem(STORAGE_KEY);
    location.replace('/auth');
  }
}

async function loadLimits(){
  const r = await api('/api/limits');
  const el = $('limitsPanel');
  if (!el) return;
  if (!r.ok || !r.data) {
    el.innerHTML = `<div class="limits-h" style="color:var(--acc);font-weight:700;margin-bottom:8px;">ЛИМИТЫ</div><div style="color:var(--err)">недоступно</div>`;
    return;
  }
  const l = r.data;
  const row = (label, value) => 
    `<div class="l-row" style="display:flex;justify-content:space-between;gap:8px;margin-bottom:4px;">
       <span>${label}</span><b style="color:var(--fg);font-weight:700;font-variant-numeric:tabular-nums;">${value}</b>
     </div>`;

  el.innerHTML = `
    <div class="limits-h" style="color:var(--acc);font-weight:700;letter-spacing:2px;margin-bottom:8px;border-bottom:1px solid var(--line);padding-bottom:6px;font-size:11px;">ЛИМИТЫ</div>
    ${row('Серверов на юзера', l.max_servers_per_user)}
    ${row('Мин. интервал опроса', l.min_real_interval + 's')}
    ${row('Интервал MC', l.minecraft_interval + 's')}
    ${row('Интервал прочих', l.other_interval + 's')}
    ${row('Мин. длина пароля', l.password_min)}
    ${row('Параллельных запросов', l.max_query_concurrent)}
    ${row('SSE-соединений', l.sse_max)}
  `;
}

// Загрузка серверов с retry + автообновлением устаревших.
async function loadServers(){
  for (let attempt = 0; attempt < 5; attempt++) {
    try {
      const r = await api('/api/servers');
      if (r.ok) {
        if (Array.isArray(r.data)) {
          servers = r.data;
          renderTable();
          triggerStaleRefresh();
          return;
        }
        console.warn('loadServers: unexpected data', r.data);
      } else {
        console.warn('loadServers', r.status, 'attempt', attempt + 1);
      }
    } catch (e) {
      console.warn('loadServers error', e, 'attempt', attempt + 1);
    }
    await new Promise(res => setTimeout(res, 500 * (attempt + 1)));
  }
  console.error('loadServers failed after retries');
  // Если таблица пуста и мы не смогли загрузить данные — выводим ошибку, а не "нет серверов"
  if (!servers.length) {
    $('rows').innerHTML = `<tr><td colspan="10" style="color:var(--err);text-align:center;padding:20px;">Не удалось загрузить сервера. Попробуйте перезагрузить страницу.</td></tr>`;
  }
}

// Если в списке есть устаревшие или совсем без state — попросить бэк их освежить.
async function triggerStaleRefresh(){
  if (Date.now() < staleRefreshCooldown) return;
  const stale = servers.filter(s => {
    if (!s.enabled) return false;
    if (!s.state || !s.state.checked_at) return true;
    const eff = s.effective_interval_sec || 60;
    return (Date.now() - s.state.checked_at) >= eff * 1000;
  });
  if (stale.length === 0) return;
  staleRefreshCooldown = Date.now() + 5000;
  try {
    await api('/api/servers/refresh-stale', { method: 'POST' });
  } catch (e) {
    console.warn('refresh-stale failed', e);
  }
}

// ---------- SSE ----------
let es = null;
let sseRetryTimer = null;

function setLive(on){
  const el = $('live');
  if (!el) return;
  el.textContent = on ? '● LIVE' : '○ OFFLINE';
  el.style.color = on ? 'var(--acc)' : 'var(--dim)';
}

function connectEvents(){
  if (es) return;
  es = new EventSource('/api/events');
  es.addEventListener('states', e => {
    setLive(true);
    try {
      const dtos = JSON.parse(e.data);
      if (!Array.isArray(dtos)) return;
      for (const dto of dtos) {
        const idx = servers.findIndex(x => x.id === dto.id);
        
        if (pendingPolls.has(dto.id)) pendingPolls.delete(dto.id);

        if (idx >= 0 && servers[idx].state && dto.state &&
            servers[idx].state.checked_at === dto.state.checked_at) {
          servers[idx] = dto;
          continue;
        }
        if (idx >= 0) servers[idx] = dto; else servers.push(dto);
        
        if (pendingPolls.has(dto.id)) continue;
        flashing.add(dto.id);
        setTimeout(() => { flashing.delete(dto.id); updateRowById(dto.id); }, 1600);
        updateRowById(dto.id);
      }
    } catch (err) { console.error('sse parse', err); }
  });
  es.onopen = () => setLive(true);
  es.onerror = () => {
    setLive(false);
    if (es) { es.close(); es = null; }
    clearTimeout(sseRetryTimer);
    sseRetryTimer = setTimeout(connectEvents, 3000);
  };
}

// ---------- IP check ----------
async function checkAddr(){
  if (checkingAddr) return;
  const addr = $('ipAddr').value.trim();
  if (!addr) return;

  checkingAddr = true;
  const box = $('ipResult');
  const body = $('ipResultBody');
  const btn = $('btnToggleResult');

  box.className = 'result';
  box.style.display = 'flex';        // чтобы flex-разметка шапки и тела работала
  body.style.display = 'block';      // на случай, если было свернуто ранее
  if (btn) btn.textContent = 'СВЕРНУТЬ';

  body.textContent = '► опрос…';

  try {
    const r = await api('/api/query', {
      method: 'POST',
      body: JSON.stringify({ address: addr, type: parseInt($('ipType').value, 10) }),
    });
    if (!r.ok) {
      box.className = 'result err';
      body.innerHTML = `<span style="color:var(--err)">${esc(r.data?.error || 'ошибка запроса')}</span>`;
      return;
    }
    if (r.data?.error) {
      box.className = 'result err';
      body.innerHTML = `<span style="color:var(--err)">${esc(r.data.error)}</span>`;
      return;
    }

    const ts = r.data?.checked_at;
    const headerLine = r.data?.from_cache
      ? `<div style="margin-bottom:8px">
           <span class="cache-mark" style="font-size:13px">💾</span>
           <span style="color:var(--dim);font-size:11px;margin-left:6px">
             из кеша — последний реальный опрос: <b>${fmtTime(ts)}</b>
             <span style="color:#5a6473">(${fmtAgo(Math.floor((Date.now() - ts) / 1000))})</span>
           </span>
         </div>`
      : `<div style="margin-bottom:8px;color:var(--dim);font-size:11px">
           опрос выполнен: <b>${fmtTime(ts)}</b>
         </div>`;
    
    body.innerHTML = headerLine + renderServerCard(r.data.payload, { withRaw: true });
  } finally {
    checkingAddr = false;
  }
}

// Функция сворачивания результата
function toggleResult(){
  const body = $('ipResultBody');
  const btn = $('btnToggleResult');
  if (!body || !btn) return;

  if (body.style.display === 'none') {
    body.style.display = 'block';
    btn.textContent = 'СВЕРНУТЬ';
  } else {
    body.style.display = 'none';
    btn.textContent = 'РАЗВЕРНУТЬ';
  }
}

// ---------- CRUD ----------
function openAdd(){
  editingId = null;
  $('addTitle').textContent = 'НОВЫЙ СЕРВЕР';
  $('fAddr').value = ''; $('fType').value = '0';
  $('fInterval').value = 0; $('fLabel').value = '';
  $('mAdd').classList.add('on');
  $('fAddr').focus();
}
function editServer(id, ev){
  if (ev) ev.stopPropagation();
  const s = servers.find(x => x.id === id); if (!s) return;
  editingId = id;
  $('addTitle').textContent = 'РЕДАКТИРОВАНИЕ #' + id;
  $('fAddr').value = s.address;
  $('fType').value = String(s.type);
  $('fInterval').value = s.interval_sec || 0;
  $('fLabel').value = s.label || '';
  $('mAdd').classList.add('on');
}
function closeModal(id){ $(id).classList.remove('on'); }

async function saveServer(){
  const body = {
    address: $('fAddr').value.trim(),
    type: parseInt($('fType').value, 10),
    interval_sec: parseInt($('fInterval').value, 10) || 0,
    label: $('fLabel').value.trim(),
  };
  if (!body.address) return alert('Введите адрес');
  const url = editingId ? `/api/servers/${editingId}` : '/api/servers';
  const method = editingId ? 'PATCH' : 'POST';
  const r = await api(url, { method, body: JSON.stringify(body) });
  if (!r.ok) return alert(r.data?.error || 'Ошибка сохранения');
  closeModal('mAdd');
  if (!editingId && r.data) {
    servers.push(r.data);
    renderTable();
    flashing.add(r.data.id);
    updateRowById(r.data.id);
    setTimeout(() => { flashing.delete(r.data.id); updateRowById(r.data.id); }, 1600);
  } else {
    await loadServers();
  }
}

async function delServer(id, ev){
  if (ev) ev.stopPropagation();
  if (!confirm('Удалить сервер #' + id + '?')) return;
  const r = await api(`/api/servers/${id}`, { method: 'DELETE' });
  if (!r.ok) return alert(r.data?.error || 'Ошибка удаления');
  expanded.delete(id);
  pendingPolls.delete(id);
  delete cooldowns[id];
  await loadServers();
}

async function pollId(id, ev){
  if (ev) ev.stopPropagation();
  if (pendingPolls.has(id)) return;
  if (cooldowns[id] && Date.now() < cooldowns[id]) return;
  const i = servers.findIndex(x => x.id === id);
  if (i < 0) return;

  pendingPolls.add(id);
  updateRowById(id);
  try {
    const r = await api(`/api/servers/${id}/poll`, { method: 'POST' });
    if (r.ok && r.data) {
      servers[i].state = r.data;
      if (r.data.from_cache) {
        cooldowns[id] = Date.now() + 3000;
        flashing.add(id);
        setTimeout(() => { flashing.delete(id); updateRowById(id); }, 800);
      }
    } else if (r.status === 429) {
      cooldowns[id] = Date.now() + 5000;
    }
  } finally {
    pendingPolls.delete(id);
    updateRowById(id);
  }
}

async function pollAll(){
  if (!servers.length) return;
  if (!confirm('Отправить реальный запрос ко всем серверам прямо сейчас?')) return;
  
  servers.forEach(s => {
    if (s.enabled) pendingPolls.add(s.id);
  });
  renderTable();

  const r = await api('/api/servers/poll-all', { method: 'POST' });
  if (!r.ok) {
    alert(r.data?.error || 'Ошибка');
    servers.forEach(s => pendingPolls.delete(s.id));
    renderTable();
    return;
  }
}

async function logout(){
  try { await fetch('/logout', { method: 'POST', credentials: 'include' }); } catch {}
  localStorage.removeItem(STORAGE_KEY);
  location.replace('/auth');
}

// ---------- init ----------
loadMe();
loadLimits();
loadServers();
connectEvents();
setInterval(loadServers, 60_000);