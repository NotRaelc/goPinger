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
let editingId = null;

const esc = s => String(s ?? '').replace(/[&<>"']/g, c =>
  ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const fmtTime = ts => ts ? new Date(ts).toLocaleTimeString() : '—';
const trunc = s => s == null || s === '' ? '—'
  : `<span class="cell-trunc" title="${esc(s)}">${esc(s)}</span>`;

function relTime(ts) {
  if (!ts) return '—';
  const sec = Math.max(0, Math.floor((Date.now() - ts) / 1000));
  if (sec < 5) return 'только что';
  if (sec < 60) return `${sec} сек назад`;
  if (sec < 3600) return `${Math.floor(sec / 60)} мин назад`;
  if (sec < 86400) return `${Math.floor(sec / 3600)} ч назад`;
  return new Date(ts).toLocaleDateString();
}

setInterval(() => {
  document.querySelectorAll('[data-rel-time]').forEach(el => {
    el.textContent = relTime(parseInt(el.dataset.relTime, 10));
  });
}, 1000);

const statusCell = st => !st
  ? `<span class="st-wait">○ ЖДЁТ</span>`
  : st.ok ? `<span class="st-ok">● ОНЛАЙН</span>` : `<span class="st-fail">● ОФФЛАЙН</span>`;

const playersCell = st => {
  const p = st?.payload;
  return p && p.online_players != null ? `${p.online_players}/${p.max_players ?? 0}` : '—';
};

function imageUrl(s) {
  const ic = s.state?.payload?.icon;
  if (ic) return ic;
  if (s.icon_url) return s.icon_url;
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
  { key:'idx', label:'', width:'60px', always:true,
    render:(s,i)=>`<span class="chev">${expanded.has(s.id)?'▼':'▶'}</span><span class="rownum">${i+1}</span>` },

  { key:'image', label:'', width:'56px', always:true,
    render:s=>`<img class="srv-img" src="${imageUrl(s)}" onerror="onImgError(this)" alt="">` },

  { key:'address', label:'АДРЕС', width:'auto', always:true,
    render:s=>`<a href="#" onclick="toggleExpand(${s.id},event);return false">${esc(s.address)}</a>`
      + (s.label?` <span class="pill">${esc(s.label)}</span>`:'')
      + (s.enabled===false?` <span class="pill" style="color:var(--warn);border-color:var(--warn)">OFF</span>`:'') },

  { key:'label',    label:'МЕТКА',    width:'120px', render:s=>s.label?esc(s.label):'—' },
  { key:'type',     label:'ТИП',      width:'110px', render:s=>`<span class="pill">${TYPE_NAME[s.type]||'?'}</span>` },
  { key:'interval', label:'ИНТЕРВАЛ', width:'90px',  render:s=>s.effective_interval_sec+'s' },
  { key:'status',   label:'СТАТУС',   width:'110px', render:s=>statusCell(s.state) },
  { key:'players',  label:'ИГРОКИ',   width:'110px', render:s=>playersCell(s.state) },
  { key:'name',     label:'ИМЯ',      width:'160px', render:s=>trunc(s.state?.payload?.name) },
  { key:'motd',     label:'MOTD',     width:'200px', render:s=>trunc((s.state?.payload?.motd?.clean||[]).join(' / ')) },
  { key:'version',  label:'ВЕРСИЯ',   width:'150px', render:s=>trunc((s.state?.payload?.versions||[]).slice(0,3).join(', ')) },
  { key:'protocol', label:'PROTO',    width:'80px',  render:s=>s.state?.payload?.protocol ?? '—' },
  { key:'software', label:'SOFTWARE', width:'130px', render:s=>trunc(s.state?.payload?.software) },
  { key:'gamemode', label:'GAMEMODE', width:'120px', render:s=>trunc(s.state?.payload?.gamemode) },
  { key:'map',      label:'MAP',      width:'140px', render:s=>trunc(s.state?.payload?.map) },
  { key:'ip',       label:'IP:PORT',  width:'170px',
    render:s=>{const p=s.state?.payload; if(!p) return '—'; return esc(p.ip||'')+(p.port?':'+p.port:'');} },

  { key:'checked',  label:'ОПРОС',    width:'150px',
    render:s=>{
      const ts = s.state?.checked_at;
      if (!ts) return '—';
      const ageSec = Math.floor((Date.now() - ts) / 1000);
      const eff = s.effective_interval_sec || 60;
      const stale = ageSec > eff * 1.5;
      const color = stale ? 'var(--warn)' : 'var(--dim)';
      const badge = s.state?.from_cache
        ? ` <span class="pill" style="background:var(--warn);color:#1a1305;border-color:var(--warn);font-weight:700">CACHE</span>`
        : '';
      return `<span data-rel-time="${ts}" title="${fmtTime(ts)}" style="color:${color}">${relTime(ts)}</span>${badge}`;
    } },

  // Кнопка всегда рендерится как "ОПРОС" без disabled —
  // pending-состояние накатывается через updateRowById (property sync,
  // без пересоздания элемента → нет фликера).
  { key:'actions',  label:'ДЕЙСТВИЯ', width:'230px', always:true,
    render:s=>`<div class="actions">
      <button class="ghost sm" data-poll-id="${s.id}" onclick="pollId(${s.id},event)">ОПРОС</button>
      <button class="ghost sm" onclick="editServer(${s.id},event)">ИЗМ</button>
      <button class="danger sm" onclick="delServer(${s.id},event)">✕</button>
    </div>` },
];

const DEFAULT_COLS = ['idx','image','address','type','interval','status','players','checked','actions'];
const visibleCols = new Set(loadCols());

function loadCols(){
  try {
    const raw = localStorage.getItem(COLS_KEY);
    if (!raw) return DEFAULT_COLS;
    const arr = JSON.parse(raw);
    return Array.isArray(arr) && arr.length ? arr : DEFAULT_COLS;
  } catch { return DEFAULT_COLS; }
}
function saveCols(){ localStorage.setItem(COLS_KEY, JSON.stringify([...visibleCols])); }
const activeColumns = () => COLUMNS.filter(c => c.always || visibleCols.has(c.key));

function filteredServers() {
  const q = ($('search')?.value || '').toLowerCase().trim();
  if (!q) return servers;
  return servers.filter(s =>
    s.address.toLowerCase().includes(q) ||
    (s.label || '').toLowerCase().includes(q));
}

// ---------- rendering ----------
function buildRowHTML(s, i, cols) {
  return cols.map(c => `<td>${c.render(s, i) ?? ''}</td>`).join('');
}

function attachRowHandlers(tr, s) {
  tr.addEventListener('click', ev => {
    if (ev.target.closest('button,a,input,select')) return;
    toggleExpand(s.id);
  });
}

function renderTable(){
  const cols = activeColumns();
  $('thead').innerHTML = '<tr>' + cols.map(c =>
    `<th style="width:${c.width}">${esc(c.label)}</th>`).join('') + '</tr>';

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
    tr.innerHTML = buildRowHTML(s, i, cols);
    attachRowHandlers(tr, s);
    tbody.appendChild(tr);

    if (expanded.has(s.id)) {
      const dr = document.createElement('tr');
      dr.className = 'detail-row';
      dr.innerHTML = `<td colspan="${cols.length}">${renderDetail(s)}</td>`;
      tbody.appendChild(dr);
    }
  });

  // Синхронизируем pending-состояние кнопок (для случая renderTable во время опроса)
  servers.forEach(s => {
    if (!pendingPolls.has(s.id)) return;
    const btn = document.querySelector(`button[data-poll-id="${s.id}"]`);
    if (btn) { btn.disabled = true; btn.textContent = '…'; }
  });
}

// Точечное обновление строки. Кнопку ОПРОС не пересоздаём — правим property.
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
  const isPending = pendingPolls.has(id);

  cols.forEach((c, idx) => {
    const td = tds[idx];
    if (!td) return;

    // Actions cell: сохраняем идентичность кнопки, обновляем только state.
    if (c.key === 'actions') {
      const pollBtn = td.querySelector('button[data-poll-id]');
      if (pollBtn) {
        pollBtn.disabled = isPending;
        pollBtn.textContent = isPending ? '…' : 'ОПРОС';
        return;
      }
      td.innerHTML = c.render(s, i) ?? '';
      return;
    }

    const newHTML = c.render(s, i) ?? '';
    if (td.innerHTML !== newHTML) td.innerHTML = newHTML;
  });

  // flash/open классы
  const isOpen = expanded.has(id);
  const shouldFlash = flashing.has(id);
  const want = 'expandable' + (isOpen ? ' open' : '') + (shouldFlash ? ' flash' : '');
  if (tr.className !== want) tr.className = want;

  // detail row
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

  const pl = p.players || [];
  const motdLines = (p.motd?.clean||[]).join(' / ') || (p.motd?.raw||[]).join(' / ') || '—';
  const versions  = (p.versions||[]).slice(0,8).join(', ') + ((p.versions||[]).length>8?'…':'');

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
    cols.map(c => `<label><input type="checkbox" data-col="${c.key}" ${visibleCols.has(c.key)?'checked':''}>${esc(c.label)}</label>`).join('')
    + `<div class="sep">ДЕЙСТВИЯ</div><button class="ghost" style="width:100%" onclick="resetCols()">СБРОСИТЬ</button>`;
  menu.querySelectorAll('input[type=checkbox]').forEach(cb => {
    cb.addEventListener('change', () => {
      const k = cb.dataset.col;
      if (cb.checked) visibleCols.add(k); else visibleCols.delete(k);
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
  visibleCols.clear();
  DEFAULT_COLS.forEach(k => visibleCols.add(k));
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

async function loadServers(){
  const r = await api('/api/servers');
  if (!r.ok) { console.error('loadServers', r.status); return; }
  servers = r.data || [];
  renderTable();
}

// ---------- SSE (batched states, dedup by checked_at) ----------
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
      for (const dto of dtos) {
        const idx = servers.findIndex(x => x.id === dto.id);

        // Дедуп: состояние пришло HTTP-ответом раньше SSE-батча — не мигаем.
        if (idx >= 0 && servers[idx].state && dto.state &&
            servers[idx].state.checked_at === dto.state.checked_at) {
          servers[idx] = dto; // обновим icon_url и прочие поля без рендера
          continue;
        }

        if (idx >= 0) servers[idx] = dto; else servers.push(dto);

        if (pendingPolls.has(dto.id)) continue;

        flashing.add(dto.id);
        setTimeout(() => {
          flashing.delete(dto.id);
          updateRowById(dto.id);
        }, 1600);
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
  const addr = $('ipAddr').value.trim();
  if (!addr) return;
  const box = $('ipResult');
  box.className = 'result';
  box.style.display = 'block';
  box.textContent = '► опрос…';
  const r = await api('/api/query', {
    method: 'POST',
    body: JSON.stringify({ address: addr, type: parseInt($('ipType').value, 10) }),
  });
  if (!r.ok) {
    box.className = 'result err';
    box.innerHTML = `<span style="color:var(--err)">${esc(r.data?.error || 'ошибка запроса')}</span>`;
    return;
  }
  if (r.data.error) {
    box.className = 'result err';
    box.innerHTML = `<span style="color:var(--err)">${esc(r.data.error)}</span>`;
    return;
  }

  const badge = r.data.from_cache
    ? `<div style="margin-bottom:8px"><span class="pill" style="background:var(--warn);color:#1a1305;border-color:var(--warn);font-weight:700">CACHE</span>
       <span style="color:var(--dim);font-size:11px;margin-left:8px">из кеша — реальный запрос был недавно</span></div>`
    : '';
  box.innerHTML = badge + renderServerCard(r.data.payload, { withRaw: true });
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
  await loadServers();
}

async function pollId(id, ev){
  if (ev) ev.stopPropagation();
  if (pendingPolls.has(id)) return;
  const i = servers.findIndex(x => x.id === id);
  if (i < 0) return;

  pendingPolls.add(id);
  updateRowById(id); // button → … через property sync

  try {
    const r = await api(`/api/servers/${id}/poll`, { method: 'POST' });
    if (r.ok && r.data) {
      servers[i].state = r.data;
      if (r.data.from_cache) {
        flashing.add(id);
        setTimeout(() => { flashing.delete(id); updateRowById(id); }, 800);
      }
    }
  } finally {
    pendingPolls.delete(id);
    updateRowById(id); // button → ОПРОС через property sync
  }
}

async function pollAll(){
  if (!servers.length) return;
  if (!confirm('Опрос всех серверов?')) return;
  const r = await api('/api/servers/poll-all', { method: 'POST' });
  if (!r.ok) alert(r.data?.error || 'Ошибка');
}

async function logout(){
  try { await fetch('/logout', { method: 'POST', credentials: 'include' }); } catch {}
  localStorage.removeItem(STORAGE_KEY);
  location.replace('/auth');
}

// ---------- init ----------
loadMe();
loadServers();
connectEvents();
setInterval(loadServers, 60_000);