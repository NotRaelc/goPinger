// ============================================================
// MOCK. Всё, что ниже — заглушка под будущее API.
// Когда появятся POST/GET/PATCH/DELETE /api/servers и /api/query —
// заменить loadServers/saveServer/delServer/pollId/checkAddr
// на соответствующие fetch.
// ============================================================

const $ = id => document.getElementById(id);
const STORAGE_KEY = 'gp_user';
const COLS_KEY    = 'gp_columns';
const TYPE_NAME   = {0:'AUTO',1:'SOURCE',2:'MC-JAVA',3:'MC-LEGACY',4:'MC-BEDROCK'};

// ---------- MOCK DATA ----------
let nextId = 4;
let servers = [
  { id:1, address:'play.example.com:25565', type:2, interval_sec:60, label:'prod',
    enabled:true, last:{ ok:true, checked_at:Date.now(), payload:{
      game_kind:'minecraft-java', online:true, name:'Paper 1.20.4',
      ip:'play.example.com', port:25565, online_players:12, max_players:100,
      versions:['1.20.4'], protocol:765, software:'Paper',
      motd:{ clean:['Добро пожаловать!'] },
      players:[{name:'Steve',time:-1},{name:'Alex',time:-1}],
    }}},
  { id:2, address:'mc.hypixel.net', type:2, interval_sec:120, label:'',
    enabled:true, last:{ ok:true, checked_at:Date.now(), payload:{
      game_kind:'minecraft-java', online:true, name:'Hypixel',
      ip:'mc.hypixel.net', port:25565, online_players:45000, max_players:50000,
      versions:['1.8-1.20'], protocol:47, software:'Custom',
      motd:{ clean:['Hypixel Network','► play.hypixel.net'] },
      players:[],
    }}},
  { id:3, address:'10.0.0.1:27015', type:1, interval_sec:60, label:'local',
    enabled:false, last:{ ok:false, checked_at:Date.now(), error:'connection refused' }},
];

let expanded  = new Set();
let editingId = null;

// ---------- utils ----------
const esc      = s => String(s??'').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const fmtTime  = ts => ts ? new Date(ts).toLocaleTimeString() : '—';
const trunc    = s => s==null||s==='' ? '—' : `<span class="cell-trunc" title="${esc(s)}">${esc(s)}</span>`;
const statusCell = last => {
  const ok  = last?.ok;
  const cls = ok===true?'st-ok':ok===false?'st-fail':'st-wait';
  const txt = ok===true?'● ОНЛАЙН':ok===false?'● ОФФЛАЙН':'○ ЖДЁТ';
  return `<span class="${cls}">${txt}</span>`;
};
const playersCell = last => {
  const p = last?.payload;
  return p?.online_players!=null ? `${p.online_players}/${p.max_players}` : '—';
};

// ---------- JSON highlight ----------
function highlightJSON(obj){
  let json;
  try { json = typeof obj === 'string' ? obj : JSON.stringify(obj, null, 2); }
  catch { json = String(obj); }
  json = json.replace(/[&<>]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;'}[c]));
  return json.replace(
    /("(?:\\u[a-fA-F0-9]{4}|\\[^u]|[^\\"])*"\s*:?|\btrue\b|\bfalse\b|\bnull\b|-?\d+(?:\.\d+)?(?:[eE][+\-]?\d+)?)/g,
    (m) => {
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
  { key:'address', label:'АДРЕС', width:'auto', always:true,
    render:s=>`<a href="#" onclick="toggleExpand(${s.id},event);return false">${esc(s.address)}</a>
      ${s.label?`<span class="pill" style="margin-left:6px">${esc(s.label)}</span>`:''}
      ${s.enabled===false?`<span class="pill" style="color:var(--warn);border-color:var(--warn)">OFF</span>`:''}` },
  { key:'label',    label:'МЕТКА',    width:'120px', render:s=>s.label?esc(s.label):'—' },
  { key:'type',     label:'ТИП',      width:'110px', render:s=>`<span class="pill">${TYPE_NAME[s.type]||'?'}</span>` },
  { key:'interval', label:'ИНТЕРВАЛ', width:'90px',  render:s=>s.interval_sec+'s' },
  { key:'status',   label:'СТАТУС',   width:'110px', render:s=>statusCell(s.last) },
  { key:'players',  label:'ИГРОКИ',   width:'110px', render:s=>playersCell(s.last) },
  { key:'name',     label:'ИМЯ',      width:'160px', render:s=>trunc(s.last?.payload?.name) },
  { key:'motd',     label:'MOTD',     width:'200px', render:s=>trunc((s.last?.payload?.motd?.clean||[]).join(' / ')) },
  { key:'version',  label:'ВЕРСИЯ',   width:'150px', render:s=>trunc((s.last?.payload?.versions||[]).slice(0,3).join(', ')) },
  { key:'protocol', label:'PROTO',    width:'80px',  render:s=>s.last?.payload?.protocol ?? '—' },
  { key:'software', label:'SOFTWARE', width:'130px', render:s=>trunc(s.last?.payload?.software) },
  { key:'gamemode', label:'GAMEMODE', width:'120px', render:s=>trunc(s.last?.payload?.gamemode) },
  { key:'map',      label:'MAP',      width:'140px', render:s=>trunc(s.last?.payload?.map) },
  { key:'ip',       label:'IP:PORT',  width:'170px',
    render:s=>{const p=s.last?.payload; if(!p) return '—'; return esc(p.ip||'')+(p.port?':'+p.port:'');} },
  { key:'checked',  label:'ОПРОС',    width:'110px', render:s=>fmtTime(s.last?.checked_at) },
  { key:'actions',  label:'ДЕЙСТВИЯ', width:'230px', always:true,
    render:s=>`<div class="actions">
      <button class="ghost sm" onclick="pollId(${s.id},event)">ОПРОС</button>
      <button class="ghost sm" onclick="editServer(${s.id},event)">ИЗМ</button>
      <button class="danger sm" onclick="delServer(${s.id},event)">✕</button>
    </div>` },
];
const DEFAULT_COLS = ['idx','address','type','interval','status','players','checked','actions'];
const visibleCols  = new Set(loadCols());

function loadCols(){
  try{
    const raw = localStorage.getItem(COLS_KEY);
    if(!raw) return DEFAULT_COLS;
    const arr = JSON.parse(raw);
    return Array.isArray(arr)&&arr.length ? arr : DEFAULT_COLS;
  }catch{ return DEFAULT_COLS; }
}
function saveCols(){ localStorage.setItem(COLS_KEY, JSON.stringify([...visibleCols])); }

// ---------- TABLE ----------
function loadServers(){ renderTable(); }

function activeColumns(){ return COLUMNS.filter(c => c.always || visibleCols.has(c.key)); }

function renderTable(){
  const cols = activeColumns();
  $('thead').innerHTML = '<tr>' + cols.map(c =>
    `<th style="width:${c.width}">${esc(c.label)}</th>`).join('') + '</tr>';

  const tbody = $('rows');
  tbody.innerHTML = '';

  if(!servers.length){
    tbody.innerHTML = `<tr><td colspan="${cols.length}" style="color:var(--dim)">нет серверов — добавь первый</td></tr>`;
    return;
  }

  servers.forEach((s,i)=>{
    const tr = document.createElement('tr');
    tr.className = 'expandable' + (expanded.has(s.id)?' open':'');
    tr.innerHTML = cols.map(c => {
      const cell = document.createElement('td');
      cell.innerHTML = c.render(s,i) ?? '';
      return cell.outerHTML;
    }).join('');
    tr.addEventListener('click', ev => {
      if(ev.target.closest('button,a,input,select')) return;
      toggleExpand(s.id);
    });
    tbody.appendChild(tr);

    if(expanded.has(s.id)){
      const dr = document.createElement('tr');
      dr.className = 'detail-row';
      dr.innerHTML = `<td colspan="${cols.length}">${renderDetail(s)}</td>`;
      tbody.appendChild(dr);
    }
  });
}

function renderDetail(s){
  const last = s.last || {};
  if(last.payload) return renderServerCard(last.payload, {withRaw:true});
  if(last.error)   return `<div class="sec-title">ОШИБКА</div>
    <pre class="json">${esc(last.error)}</pre>`;
  return `<span style="color:var(--dim)">ещё не опрашивался</span>`;
}

function toggleExpand(id, ev){
  if(ev) ev.stopPropagation();
  if(expanded.has(id)) expanded.delete(id); else expanded.add(id);
  renderTable();
}

// ---------- SERVER CARD ----------
function renderServerCard(p, {withRaw=false}={}){
  if(!p) return '<i style="color:var(--dim)">пусто</i>';
  const pl = p.players||[];
  const motdLines = (p.motd?.clean||[]).join(' / ') || (p.motd?.raw||[]).join(' / ') || '—';
  const versions  = (p.versions||[]).slice(0,8).join(', ') + ((p.versions||[]).length>8?'…':'');
  let h = `<div class="kv">
    <b>GameKind</b><span>${esc(p.game_kind||'—')}</span>
    <b>Online</b><span style="color:${p.online?'var(--acc)':'var(--err)'}">${p.online?'YES':'NO'}</span>
    <b>Name</b><span>${esc(p.name||'—')}</span>
    <b>IP : Port</b><span>${esc(p.ip||'')}${p.port?':'+p.port:''}</span>
    ${p.hostname?`<b>Hostname</b><span>${esc(p.hostname)}</span>`:''}
    <b>Players</b><span>${p.online_players??'—'} / ${p.max_players??'—'}</span>
    <b>MOTD</b><span>${esc(motdLines)}</span>
    ${versions?`<b>Versions</b><span>${esc(versions)}</span>`:''}
    ${p.protocol?`<b>Protocol</b><span>${p.protocol}</span>`:''}
    ${p.software?`<b>Software</b><span>${esc(p.software)}</span>`:''}
    ${p.gamemode?`<b>Gamemode</b><span>${esc(p.gamemode)}</span>`:''}
    ${p.map?`<b>Map</b><span>${esc(p.map)}</span>`:''}
    ${p.game_id>=0?`<b>Game ID</b><span>${p.game_id}</span>`:''}
    ${p.serverid?`<b>ServerID</b><span>${esc(p.serverid)}</span>`:''}
  </div>`;
  if(pl.length){
    h += `<div class="sec-title">ИГРОКИ (${pl.length})</div>
      <div style="max-height:180px;overflow:auto;font-size:11px">
        ${pl.slice(0,80).map(x =>
          `<div>${esc(x.name)} ${x.time>=0?`<span style="color:var(--dim)">${x.time}s</span>`:''}</div>`
        ).join('')}
        ${pl.length>80?`<div style="color:var(--dim)">… и ещё ${pl.length-80}</div>`:''}
      </div>`;
  }
  if(withRaw){
    h += `<details class="raw"><summary>RAW JSON</summary>
      <pre class="json">${highlightJSON(p)}</pre></details>`;
  }
  return h;
}

// ---------- COLUMN PICKER ----------
function renderColPicker(){
  const menu = $('colPickerMenu');
  const cols = COLUMNS.filter(c => !c.always);
  menu.innerHTML =
    cols.map(c => `
      <label>
        <input type="checkbox" data-col="${c.key}" ${visibleCols.has(c.key)?'checked':''}>
        ${esc(c.label)}
      </label>`).join('') +
    `<div class="sep">ДЕЙСТВИЯ</div>
     <button class="ghost" style="width:100%" onclick="resetCols()">СБРОСИТЬ</button>`;

  menu.querySelectorAll('input[type=checkbox]').forEach(cb => {
    cb.addEventListener('change', () => {
      const k = cb.dataset.col;
      if(cb.checked) visibleCols.add(k); else visibleCols.delete(k);
      saveCols();
      renderTable();
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
  DEFAULT_COLS.forEach(k=>visibleCols.add(k));
  saveCols();
  renderColPicker();
  renderTable();
}
document.addEventListener('click', ev => {
  if(!ev.target.closest('.colpicker')) $('colPickerMenu').classList.remove('on');
});

// ---------- IP CHECK (mock) ----------
async function checkAddr(){
  const addr = $('ipAddr').value.trim();
  const box  = $('ipResult');
  if(!addr) return;
  box.className = 'result';
  box.style.display = 'block';
  box.textContent = '► опрос…';

  await new Promise(r => setTimeout(r, 400));

  const mock = {
    game_kind: 'minecraft-java',
    online: true,
    name: 'Mock 1.20.4',
    ip: addr.split(':')[0],
    port: 25565,
    online_players: 3,
    max_players: 20,
    versions: ['1.20.4'],
    protocol: 765,
    motd: { clean: ['mocked response'] },
    players: [],
    game_id: -1,
  };
  box.innerHTML = renderServerCard(mock, {withRaw:true}) +
    `<div style="margin-top:8px;color:var(--dim);font-size:11px">
      ⚠ это mock. /api/query ещё не реализован на бекенде.</div>`;
}

// ---------- CRUD (mock) ----------
function openAdd(){
  editingId = null;
  $('addTitle').textContent = 'НОВЫЙ СЕРВЕР';
  $('fAddr').value=''; $('fType').value='0';
  $('fInterval').value=60; $('fLabel').value='';
  $('mAdd').classList.add('on');
  $('fAddr').focus();
}
function editServer(id, ev){
  if(ev) ev.stopPropagation();
  const s = servers.find(x=>x.id===id); if(!s) return;
  editingId = id;
  $('addTitle').textContent = 'РЕДАКТИРОВАНИЕ #'+id;
  $('fAddr').value=s.address; $('fType').value=String(s.type);
  $('fInterval').value=s.interval_sec; $('fLabel').value=s.label||'';
  $('mAdd').classList.add('on');
}
function closeModal(id){ $(id).classList.remove('on'); }

async function saveServer(){
  const body = {
    address:      $('fAddr').value.trim(),
    type:         parseInt($('fType').value,10),
    interval_sec: parseInt($('fInterval').value,10) || 60,
    label:        $('fLabel').value.trim(),
  };
  if(!body.address) return alert('Введите адрес');

  if(editingId){
    const s = servers.find(x=>x.id===editingId);
    if(s) Object.assign(s, body);
  } else {
    servers.push({ id: nextId++, enabled: true, last: null, ...body });
  }
  closeModal('mAdd');
  renderTable();
}

async function delServer(id, ev){
  if(ev) ev.stopPropagation();
  if(!confirm('Удалить сервер #'+id+'?')) return;
  servers = servers.filter(s => s.id !== id);
  expanded.delete(id);
  renderTable();
}

async function pollId(id, ev){
  if(ev) ev.stopPropagation();
  const s = servers.find(x=>x.id===id);
  if(!s) return;
  s.last = { ok: null, checked_at: Date.now() };
  renderTable();

  // имитация опроса, ~400ms
  setTimeout(() => {
    s.last = {
      ok: true, checked_at: Date.now(), payload: {
        game_kind:'minecraft-java', online:true, name:'Mock Server',
        ip:s.address.split(':')[0], port:25565,
        online_players:5, max_players:20,
        versions:['1.20.4'], protocol:765,
        motd:{ clean:['mocked'] }, players:[],
      },
    };
    renderTable();
  }, 400);
}

// ---------- LOGOUT ----------
async function logout(){
  try {
    await fetch('/logout', { method: 'POST', credentials: 'include' });
  } catch (e) {
    console.error('logout:', e);
  }
  localStorage.removeItem(STORAGE_KEY);
  location.replace('/auth');
}

// ---------- INIT ----------
const username = localStorage.getItem(STORAGE_KEY);
if (username) $('who').textContent = username;
loadServers();