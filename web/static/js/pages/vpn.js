import { apd, h, clear, pageHead, badge, note, chk } from '../ui.js';
import { get } from '../api.js';
import * as st from '../state.js';
import { jump } from './nodes.js';

const S = st.S;
// Имя системного интерфейса считается так же, как в генераторе конфигурации.
const ifName = (n) => n.toLowerCase().replace(/[^a-z0-9._!@-]/g, '').slice(0, 15) || 'wg0';

export default async function (root, nav) {
  const s = S.settings;
  const tbl = h('div');
  const sys = h('div');
  let live = [];
  let alive = true, timer = 0;

  const go = (q) => { Object.assign(jump, q); nav.go('nodes'); };

  function state(n) {
    if (!n.system) return h('span', { class: 'small mute' }, 'обычный исходящий');
    if (!S.status || S.status.state !== 'running') return h('span', { class: 'small mute' }, 'ядро не запущено');
    const i = live.find((x) => x.name === ifName(n.name));
    if (!i) return badge('нет в системе', 'warn');
    return badge(i.up ? 'активен' : 'не активен', i.up ? 'ok' : 'warn');
  }

  function drawTable() {
    const wg = s.nodes.filter((n) => n.type === 'wireguard' || n.type === 'awg');
    clear(tbl).append(wg.length
      ? h('div', { class: 'tw' }, h('table', h('thead', h('tr', h('th', 'Узел'), h('th', 'Интерфейс'), h('th', 'Состояние'), h('th', 'Адреса'), h('th', 'MTU'), h('th', 'Системный интерфейс'), h('th'))),
        h('tbody', wg.map((n) => h('tr',
          h('td', n.name, ' ', n.type === 'awg' ? badge('AWG') : null, h('div', { class: 'small mute mono' }, (n.server || '—') + (n.port ? ':' + n.port : ''))),
          h('td', { class: 'mono' }, n.system ? ifName(n.name) : '—'),
          h('td', state(n)),
          h('td', { class: 'mono' }, (n.addresses || []).join(', ') || '—'),
          h('td', n.mtu || 'авто'),
          h('td', chk(n, 'system', 'создавать', null, { onchange: () => { drawTable(); st.touch(); } })),
          h('td', { class: 'act' }, h('button', { class: 'btn sm', onclick: () => go({ edit: n.name }) }, 'Изменить')))))))
      : note('', 'WireGuard-узлов нет. Импортируйте .conf на странице «Узлы и VPN» или добавьте узел кнопкой ниже.'));
  }

  function drawSys() {
    const used = new Set(s.nodes.filter((n) => n.system && (n.type === 'wireguard' || n.type === 'awg')).map((n) => ifName(n.name)));
    const rows = live.filter((i) => used.has(i.name) || /^(wg|awg|tun|utun|usque|warp|nord|proton)/i.test(i.name));
    clear(sys).append(rows.length
      ? h('div', { class: 'tw' }, h('table', h('thead', h('tr', h('th', 'Интерфейс'), h('th', 'Состояние'), h('th', 'Адреса'), h('th', 'Кто создал'))),
        h('tbody', rows.map((i) => h('tr', h('td', { class: 'mono' }, i.name), h('td', badge(i.up ? 'активен' : 'не активен', i.up ? 'ok' : 'warn')),
          h('td', { class: 'mono' }, (i.addrs || []).join(', ') || '—'), h('td', used.has(i.name) ? 'эта панель (ядро)' : 'система или другая программа'))))))
      : h('p', { class: 'mute' }, 'VPN-интерфейсов в системе сейчас нет.'));
  }

  async function load() {
    try { live = (await get('api/interfaces')) || []; } catch (e) { live = []; }
    if (!alive) return;
    drawTable(); drawSys();
  }

  apd(root, pageHead('VPN-интерфейсы (WireGuard)', 'Туннели WireGuard и AmneziaWG и их сетевые интерфейсы в системе'),
    h('div', { class: 'card' }, h('h2', 'Туннели'),
      h('p', 'По умолчанию WireGuard-узел — обычный исходящий: трафик попадает в него только по правилам маршрутизации. Если включить «создавать» (', h('code', 'system=true'),
        '), ядро создаст настоящий сетевой интерфейс в системе: его видно в ', h('code', 'ip link'), ', и им можно пользоваться из правил роутера и других программ. Изменения вступают в силу после «Применить».'),
      note('warn', 'Системный интерфейс требует прав root.'),
      tbl,
      h('div', { class: 'row', style: 'margin-top:10px' },
        h('button', { class: 'btn primary', onclick: () => go({ add: 'wireguard' }) }, '+ WireGuard'),
        h('button', { class: 'btn', onclick: () => go({ add: 'awg' }) }, '+ AmneziaWG'),
        h('button', { class: 'btn', onclick: () => nav.go('nodes') }, 'Импорт .conf'),
        h('button', { class: 'btn', onclick: () => { nav.go('network'); } }, 'Свои интерфейсы'))),
    h('div', { class: 'card', style: 'margin-top:14px' }, h('h2', 'Интерфейсы в системе'), sys,
      h('p', { class: 'small mute' }, 'Внешние VPN (например, настроенные в OpenWrt) подключаются как узел типа «Системный VPN (интерфейс)» на странице «Узлы и VPN».')));
  drawTable(); drawSys();
  await load();
  timer = setInterval(() => { if (!document.hidden) load(); }, 5000);
  return () => { alive = false; clearInterval(timer); };
}
