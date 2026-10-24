import { apd, h, clear, pageHead, note, toast, toastErr, confirmBox } from '../ui.js';
import { get, post } from '../api.js';

const pad = (n) => String(n).padStart(2, '0');
function clock(ms) {
  const d = new Date(ms);
  return pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
}
function rcodeBadge(e) {
  if (!e.rcode) return h('span', { class: 'badge' }, '…');
  if (e.rcode === 'NOERROR') return h('span', { class: 'badge ok' }, e.cached ? 'кэш' : 'ответ');
  if (e.rcode === 'rejected') return h('span', { class: 'badge' }, 'отклонён');
  return h('span', { class: 'badge warn' }, e.rcode);
}

export default async function (root) {
  const f = { client: '', q: '', pause: false };
  let alive = true, timer = 0, clients = [];
  const hint = h('div');
  const deviceSel = h('select', { 'aria-label': 'Устройство', style: 'max-width:300px', onchange: (e) => { f.client = e.target.value; load(); } });
  const body = h('tbody');
  const count = h('span', { class: 'small mute' });
  const label = (c) => (c.ip || 'неизвестно') + (c.name ? ' · ' + c.name : '') + ' (' + c.count + ')';

  function drawDevices() {
    const keep = f.client;
    clear(deviceSel).append(h('option', { value: '' }, 'Все устройства'),
      clients.slice().sort((a, b) => b.last - a.last).map((c) => h('option', { value: c.ip, selected: c.ip === keep }, label(c))));
    if (keep && !clients.some((c) => c.ip === keep)) deviceSel.append(h('option', { value: keep, selected: true }, keep + ' (нет запросов)'));
    deviceSel.value = keep;
  }
  const nameOf = (ip) => { const c = clients.find((x) => x.ip === ip); return c && c.name ? c.name : ''; };

  async function load() {
    if (!alive || f.pause) return;
    try {
      const r = await get('api/dns/queries?limit=500&client=' + encodeURIComponent(f.client) + '&q=' + encodeURIComponent(f.q));
      if (!alive) return;
      clients = r.clients || [];
      if (document.activeElement !== deviceSel) drawDevices();
      clear(hint);
      if (r.enabled === false) {
        clear(body); count.textContent = '';
        hint.append(note('warn', 'Журнал DNS-запросов выключен. Включите его на странице «Входящие, TUN, сеть» → «Общие» (галочка «Журнал DNS-запросов»), сохраните настройки и примените.'));
        return;
      }
      if (!r.debug) hint.append(note('warn', 'Запросы попадают в журнал только при уровне логов debug (сейчас: ' + (r.log_level || 'не задан') + '). Включите его на странице «Входящие, TUN, сеть» → «Общие», сохраните настройки и перезапустите ядро.'));
      const rows = r.entries || [];
      count.textContent = 'Показано запросов: ' + rows.length;
      clear(body);
      if (!rows.length) {
        body.append(h('tr', h('td', { colspan: 5, class: 'mute', style: 'text-align:center;padding:18px' }, 'Запросов пока нет. Ядро должно работать, а устройство — отправлять DNS-запросы через роутер.')));
        return;
      }
      for (const e of rows) {
        const dev = e.client ? [h('div', { class: 'mono' }, e.client), nameOf(e.client) ? h('div', { class: 'small mute' }, nameOf(e.client)) : null] : h('span', { class: 'mute' }, 'не определено');
        body.append(h('tr',
          h('td', { class: 'mono', style: 'white-space:nowrap' }, clock(e.t)),
          h('td', dev),
          h('td', { style: 'overflow-wrap:anywhere' }, h('div', { class: 'nb' }, e.domain), e.answers && e.answers.length ? h('div', { class: 'small mute mono' }, e.answers.join(', ')) : null),
          h('td', { class: 'mono' }, e.type),
          h('td', rcodeBadge(e), e.ms ? h('span', { class: 'small mute' }, ' ' + e.ms + ' мс') : null)));
      }
    } catch (e) { if (alive) toastErr(e); }
  }
  function tick() { clearTimeout(timer); load().finally(() => { if (alive) timer = setTimeout(tick, 2500); }); }

  apd(root, pageHead('DNS-запросы', 'Какие имена спрашивали устройства сети: выберите одно устройство, чтобы увидеть только его запросы'), hint,
    h('div', { class: 'row', style: 'margin-bottom:10px' },
      deviceSel,
      h('input', { type: 'text', style: 'flex:1;min-width:140px;max-width:300px', placeholder: 'Поиск по имени или IP-ответу', 'aria-label': 'Поиск', oninput: (e) => { f.q = e.target.value; load(); } }),
      h('button', { class: 'btn', onclick: (e) => { f.pause = !f.pause; e.target.textContent = f.pause ? 'Продолжить' : 'Пауза'; if (!f.pause) load(); } }, 'Пауза'),
      h('button', { class: 'btn', onclick: async () => {
        if (!(await confirmBox('Очистить журнал DNS-запросов?', 'Очистить', true))) return;
        try { await post('api/dns/queries/clear', {}); toast('Журнал очищен', 'ok'); load(); } catch (e) { toastErr(e); }
      } }, 'Очистить'), count),
    h('div', { class: 'tw' }, h('table', { class: 'dnslog' },
      h('thead', h('tr', h('th', 'Время'), h('th', 'Устройство'), h('th', 'Имя'), h('th', 'Тип'), h('th', 'Результат'))), body)));
  tick();
  return () => { alive = false; clearTimeout(timer); };
}
