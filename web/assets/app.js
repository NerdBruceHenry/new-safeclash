'use strict';
// 安全约定：所有来自订阅/核心的数据只通过 textContent 写入页面，绝不使用 innerHTML。

const $ = (id) => document.getElementById(id);
let state = null;
let curTab = 'home';

function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (k === 'class') el.className = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (v !== undefined && v !== null && v !== false) el.setAttribute(k, v === true ? '' : v);
  }
  for (const c of children.flat()) {
    if (c === null || c === undefined || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

async function api(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let j;
  try { j = JSON.parse(text); } catch { j = null; }
  if (!res.ok) throw new Error((j && j.error) || text || res.statusText);
  return j && 'data' in j ? j.data : j;
}
const core = (method, path, body) => api(method, '/api/mihomo' + path, body);

function toast(msg, ms = 2500) {
  const t = $('toast');
  t.textContent = msg;
  t.classList.add('show');
  clearTimeout(t._h);
  t._h = setTimeout(() => t.classList.remove('show'), ms);
}

async function run(btn, fn, okMsg) {
  if (btn) btn.disabled = true;
  try {
    await fn();
    if (okMsg) toast(okMsg);
  } catch (e) {
    toast('失败：' + e.message, 5000);
  } finally {
    if (btn) btn.disabled = false;
    refresh();
  }
}

const fmtBytes = (n) => {
  if (!n) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), 4);
  return (n / 1024 ** i).toFixed(i ? 1 : 0) + ' ' + u[i];
};
const fmtTime = (s) => (!s || s.startsWith('0001') ? '从未' : new Date(s).toLocaleString());

// ---------- 状态 ----------
async function refresh() {
  try {
    state = await api('GET', '/api/status');
  } catch (e) {
    $('statusText').textContent = '界面与程序断开';
    $('dot').className = 'dot err';
    return;
  }
  const s = state.settings;
  $('dot').className = 'dot ' + (state.running ? 'on' : state.core_error ? 'err' : '');
  $('statusText').textContent = state.running ? '运行中' : '已停止';
  $('powerBtn').textContent = state.running ? '停止' : '启动';
  $('powerBtn').className = state.running ? 'danger' : 'primary';
  document.querySelectorAll('#modeSeg button').forEach((b) => b.classList.toggle('active', b.dataset.mode === s.mode));
  $('sysProxy').checked = s.system_proxy;
  $('tun').checked = s.tun;
  const ts = state.tun;
  $('tunState').textContent = (state.admin ? '当前权限：管理员' : '当前权限：普通用户') +
    (s.tun && state.running && ts.checked ? (ts.active ? ' · ✔ 已生效' : ' · ✘ 未生效') : '');
  const prof = s.profiles.find((p) => p.id === s.active_profile);
  $('curProfile').textContent = prof ? prof.name : '未选择';
  $('coreInfo').textContent = state.core && state.core.version ? 'mihomo ' + state.core.version : '未安装';
  $('coreSource').textContent = state.core_ok
    ? (state.core.source === 'signed-update' ? '✔ 签名验证 + 哈希完整' : '⚠ 手动导入（未经签名）')
    : '';
  $('appInfo').textContent = 'SafeClash ' + state.app_version;
  const u = state.update;
  $('updInfo').textContent = !state.signed_build ? '开发版：自动更新未启用' : '上次检查：' + fmtTime(u.last_check);
  $('portInfo').textContent = '127.0.0.1:' + s.mixed_port + (s.allow_lan ? '（已允许局域网）' : '');
  $('dataDir').textContent = '数据目录：' + state.data_dir;

  const alerts = $('alerts');
  alerts.replaceChildren();
  if (state.core_error) alerts.append(h('div', { class: 'alert bad' }, state.core_error));
  if (s.tun && state.running && state.tun.checked && !state.tun.active)
    alerts.append(h('div', { class: 'alert bad' }, 'TUN 未能启动，目前只有遵循系统代理的应用在走代理。原因：' + state.tun.error));
  if (u.last_error) alerts.append(h('div', { class: 'alert warn' }, '更新：' + u.last_error));
  if ((u.app_available || u.core_available) && u.security)
    alerts.append(h('div', { class: 'alert bad' }, '有安全修复更新可用，请到「设置」中立即更新。'));
  else if (u.app_available || u.core_available)
    alerts.append(h('div', { class: 'alert warn' }, '有新版本可用：应用 ' + (u.latest_app || '') + ' / 核心 ' + (u.latest_core || '')));
  if (s.allow_lan) alerts.append(h('div', { class: 'alert warn' }, '已允许局域网连接：同一网络中的设备可使用你的代理端口。'));

  if (curTab === 'profiles') renderProfiles();
}

// ---------- 顶栏 ----------
$('powerBtn').onclick = (e) => run(e.target, () => api('POST', state.running ? '/api/core/stop' : '/api/core/start'));
document.querySelectorAll('#modeSeg button').forEach((b) => {
  b.onclick = () => run(null, () => api('POST', '/api/settings', { mode: b.dataset.mode }));
});
$('sysProxy').onchange = (e) => run(null, () => api('POST', '/api/settings', { system_proxy: e.target.checked }));
$('tun').onchange = (e) => run(null, () => api('POST', '/api/settings', { tun: e.target.checked }));

document.querySelectorAll('#tabs button').forEach((b) => {
  b.onclick = () => {
    curTab = b.dataset.tab;
    document.querySelectorAll('#tabs button').forEach((x) => x.classList.toggle('active', x === b));
    document.querySelectorAll('.tab').forEach((t) => t.classList.toggle('active', t.id === 'tab-' + curTab));
    onTab();
  };
});

function onTab() {
  if (curTab === 'proxies') loadProxies();
  if (curTab === 'profiles') renderProfiles();
  if (curTab === 'settings') fillSettings();
  if (curTab === 'logs') startLogs();
}

// ---------- 流式接口（流量、日志） ----------
async function stream(path, onLine, signal) {
  const res = await fetch('/api/mihomo' + path, { signal });
  if (!res.ok) throw new Error(res.statusText);
  const reader = res.body.getReader();
  const dec = new TextDecoder();
  let buf = '';
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    let i;
    while ((i = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, i).trim();
      buf = buf.slice(i + 1);
      if (line) try { onLine(JSON.parse(line)); } catch {}
    }
  }
}

async function trafficLoop() {
  for (;;) {
    if (state && state.running) {
      try {
        await stream('/traffic', (t) => {
          $('up').textContent = fmtBytes(t.up) + '/s';
          $('down').textContent = fmtBytes(t.down) + '/s';
        });
      } catch {}
    }
    $('up').textContent = $('down').textContent = '0 B/s';
    await new Promise((r) => setTimeout(r, 2000));
  }
}

// ---------- 代理 ----------
const collapsed = new Set();
async function loadProxies() {
  const box = $('groups');
  if (!state || !state.running) {
    box.replaceChildren(h('p', { class: 'muted' }, '核心未运行'));
    return;
  }
  let data;
  try { data = await core('GET', '/proxies'); } catch (e) { box.replaceChildren(h('p', { class: 'muted' }, e.message)); return; }
  const P = data.proxies;
  const groupTypes = ['Selector', 'URLTest', 'Fallback', 'LoadBalance', 'Relay'];
  const order = (P.GLOBAL && P.GLOBAL.all) || Object.keys(P);
  const names = order.filter((n) => P[n] && groupTypes.includes(P[n].type));
  if (state.settings.mode === 'global') names.unshift('GLOBAL');

  box.replaceChildren(...names.map((name) => {
    const g = P[name];
    const delayOf = (n) => {
      const hist = P[n] && P[n].history;
      return hist && hist.length ? hist[hist.length - 1].delay : null;
    };
    const nodes = h('div', { class: 'nodes' }, (g.all || []).map((n) => {
      const d = delayOf(n);
      const cls = d === null ? '' : d === 0 ? 'd-bad' : d < 300 ? 'd-good' : d < 800 ? 'd-mid' : 'd-bad';
      return h('div', {
        class: 'node' + (g.now === n ? ' sel' : ''),
        title: n,
        onclick: () => g.type === 'Selector' &&
          run(null, async () => { await core('PUT', '/proxies/' + encodeURIComponent(name), { name: n }); loadProxies(); }),
      },
      h('span', { class: 'n' }, n),
      h('span', { class: 't' }, h('span', {}, (P[n] && P[n].type) || ''), h('span', { class: cls }, d === null ? '' : d === 0 ? '超时' : d + ' ms')));
    }));
    if (collapsed.has(name)) nodes.style.display = 'none';
    const testBtn = h('button', {
      onclick: (e) => {
        e.stopPropagation();
        run(e.target, async () => {
          await core('GET', '/group/' + encodeURIComponent(name) + '/delay?url=' +
            encodeURIComponent('https://www.gstatic.com/generate_204') + '&timeout=5000').catch(() => {});
          loadProxies();
        });
      },
    }, '测速');
    return h('div', { class: 'group' },
      h('div', { class: 'ghead', onclick: () => { collapsed.has(name) ? collapsed.delete(name) : collapsed.add(name); loadProxies(); } },
        h('b', {}, name), h('span', { class: 'muted' }, g.type), h('span', { class: 'muted grow' }, g.now ? '→ ' + g.now : ''), testBtn),
      nodes);
  }));
}
$('refreshProxies').onclick = loadProxies;

// ---------- 订阅 ----------
function renderProfiles() {
  const s = state.settings;
  $('profileList').replaceChildren(...s.profiles.map((p) => {
    const used = p.upload + p.download;
    const info = h('div', { class: 'info' },
      h('b', {}, p.name), s.active_profile === p.id ? h('span', { class: 'muted' }, '  · 使用中') : null,
      h('div', { class: 'muted' }, p.url ? '订阅 · 更新于 ' + fmtTime(p.updated_at) + (p.interval_hours ? ' · 每 ' + p.interval_hours + ' 小时自动更新' : ' · 不自动更新') : '本地配置'),
      p.total ? [h('div', { class: 'bar' }, h('i', { style: 'width:' + Math.min(100, (used / p.total) * 100) + '%' })),
        h('div', { class: 'muted' }, '已用 ' + fmtBytes(used) + ' / ' + fmtBytes(p.total) + (p.expire ? ' · 到期 ' + new Date(p.expire * 1000).toLocaleDateString() : ''))] : null,
      p.last_error ? h('div', { class: 'warn' }, '上次更新失败：' + p.last_error) : null);
    const btn = (label, fn, cls) => h('button', { class: cls || '', onclick: (e) => run(e.target, fn, label + '完成') }, label);
    return h('div', { class: 'card prof' + (s.active_profile === p.id ? ' active' : '') }, info,
      s.active_profile !== p.id ? btn('使用', () => api('POST', '/api/profiles/' + p.id + '/select'), 'primary') : null,
      p.url ? btn('更新', () => api('POST', '/api/profiles/' + p.id + '/update')) : null,
      p.url ? h('button', { onclick: () => {
        const v = prompt('自动更新间隔（小时，0 = 关闭）', p.interval_hours);
        if (v !== null) run(null, () => api('POST', '/api/profiles/' + p.id + '/interval', { hours: parseInt(v, 10) || 0 }));
      } }, '间隔') : null,
      h('button', { onclick: async () => {
        try { $('logBox').textContent = await api('GET', '/api/profiles/' + p.id + '/content'); document.querySelector('[data-tab=logs]').click(); stopLogs(); }
        catch (e) { toast(e.message); }
      } }, '查看'),
      btn('删除', () => confirm('删除配置「' + p.name + '」？') ? api('DELETE', '/api/profiles/' + p.id) : Promise.resolve(), 'danger'));
  }));
}
$('addSub').onclick = (e) => run(e.target, async () => {
  await api('POST', '/api/profiles', { name: $('subName').value, url: $('subUrl').value });
  $('subName').value = $('subUrl').value = '';
}, '订阅已添加');
$('addLocal').onclick = (e) => run(e.target, async () => {
  await api('POST', '/api/profiles', { name: $('subName').value, content: $('subContent').value });
  $('subContent').value = '';
}, '配置已导入');

// ---------- 连接 ----------
async function connLoop() {
  for (;;) {
    if (curTab === 'conns' && state && state.running) {
      try {
        const data = await core('GET', '/connections');
        const f = $('connFilter').value.toLowerCase();
        const list = (data.connections || []).filter((c) => {
          const m = c.metadata;
          return !f || [m.host, m.destinationIP, c.rule, c.rulePayload, (c.chains || []).join(' ')].join(' ').toLowerCase().includes(f);
        });
        $('connCount').textContent = list.length + ' 个连接 · 总上传 ' + fmtBytes(data.uploadTotal) + ' · 总下载 ' + fmtBytes(data.downloadTotal);
        $('connBody').replaceChildren(...list.map((c) => {
          const m = c.metadata;
          return h('tr', {},
            h('td', { class: 'host', title: m.host || m.destinationIP }, (m.host || m.destinationIP) + ':' + m.destinationPort),
            h('td', {}, m.network), h('td', {}, c.rule + (c.rulePayload ? ' ' + c.rulePayload : '')),
            h('td', {}, (c.chains || []).slice().reverse().join(' → ')),
            h('td', {}, fmtBytes(c.upload)), h('td', {}, fmtBytes(c.download)),
            h('td', {}, h('button', { onclick: () => core('DELETE', '/connections/' + encodeURIComponent(c.id)) }, '断开')));
        }));
      } catch {}
    }
    await new Promise((r) => setTimeout(r, 2000));
  }
}
$('closeAll').onclick = (e) => run(e.target, () => core('DELETE', '/connections'), '已断开全部连接');

// ---------- 日志 ----------
let logCtl = null, logPaused = false;
function stopLogs() { if (logCtl) logCtl.abort(); logCtl = null; }
function startLogs() {
  stopLogs();
  if (!state || !state.running || logPaused) return;
  const box = $('logBox');
  logCtl = new AbortController();
  stream('/logs?level=' + $('logLevel').value, (l) => {
    const atBottom = box.scrollTop + box.clientHeight >= box.scrollHeight - 20;
    box.append(new Date().toLocaleTimeString() + ' [' + l.type + '] ' + l.payload + '\n');
    if (box.childNodes.length > 2000) box.firstChild.remove();
    if (atBottom) box.scrollTop = box.scrollHeight;
  }, logCtl.signal).catch(() => {});
}
$('logLevel').onchange = startLogs;
$('logPause').onclick = (e) => { logPaused = !logPaused; e.target.textContent = logPaused ? '继续' : '暂停'; logPaused ? stopLogs() : startLogs(); };
$('logClear').onclick = () => { $('logBox').textContent = ''; };
$('coreLogs').onclick = async () => { stopLogs(); $('logBox').textContent = (await api('GET', '/api/core/logs')).join('\n'); };

// ---------- 设置 ----------
const boolKeys = ['allow_lan', 'ipv6', 'auto_start', 'auto_connect', 'auto_update', 'update_via_proxy'];
const numKeys = ['mixed_port', 'update_interval_hours'];
function fillSettings() {
  const s = state.settings;
  boolKeys.forEach((k) => { $('s_' + k).checked = s[k]; });
  numKeys.forEach((k) => { $('s_' + k).value = s[k]; });
  $('s_log_level').value = s.log_level;
  $('s_bypass').value = (s.bypass || []).join('\n');
  showUpd(state.update);
}
$('saveSettings').onclick = (e) => run(e.target, () => {
  const p = { log_level: $('s_log_level').value, bypass: $('s_bypass').value.split('\n').map((x) => x.trim()).filter(Boolean) };
  boolKeys.forEach((k) => { p[k] = $('s_' + k).checked; });
  numKeys.forEach((k) => { p[k] = parseInt($('s_' + k).value, 10); });
  return api('POST', '/api/settings', p);
}, '设置已保存');
function showUpd(u) {
  const lines = [];
  if (u.latest_app) lines.push('最新应用版本：' + u.latest_app + (u.app_available ? '（可更新）' : ''));
  if (u.latest_core) lines.push('最新核心版本：' + u.latest_core + (u.core_available ? '（可更新）' : ''));
  if (u.security) lines.push('⚠ 本次发布包含安全修复');
  if (u.notes) lines.push('说明：' + u.notes);
  if (u.message) lines.push(u.message);
  if (u.last_error) lines.push('错误：' + u.last_error);
  $('updBox').textContent = lines.join('\n');
}
$('checkUpd').onclick = (e) => run(e.target, async () => showUpd(await api('POST', '/api/update/check')));
$('applyUpd').onclick = (e) => run(e.target, async () => showUpd(await api('POST', '/api/update/apply')), '更新完成');
$('quitBtn').onclick = () => {
  if (!confirm('退出将停止代理并恢复系统代理设置，确定？')) return;
  api('POST', '/api/quit').finally(() => { document.body.replaceChildren(h('main', {}, h('h2', {}, 'SafeClash 已退出，可以关闭此页面。'))); });
};

refresh();
setInterval(refresh, 3000);
trafficLoop();
connLoop();
