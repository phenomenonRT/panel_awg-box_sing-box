import { h, clear, note, field, fgrid, txt, csv, chk, toast, modal, area } from './ui.js';
import * as st from './state.js';

const S = st.S;
const NAME_RE = /^[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?(\.[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?)*$/;
const isIP = (v) => /^(\d{1,3}\.){3}\d{1,3}$/.test(v) || (v.includes(':') && /^[0-9a-f:.]+$/i.test(v));

function problem(r, all) {
  const n = String(r.name || '').trim().toLowerCase().replace(/^\*\./, '').replace(/\.$/, '');
  if (!n) return 'Укажите имя';
  if (!NAME_RE.test(n)) return 'Имя может содержать буквы, цифры, «-», «_» и точки';
  if (all.some((x) => x !== r && !x.disabled && String(x.name || '').trim().toLowerCase().replace(/^\*\./, '').replace(/\.$/, '') === n)) return 'Такое имя уже есть';
  if (!(r.ips || []).length) return 'Укажите IP-адрес';
  const bad = (r.ips || []).find((ip) => !isIP(ip));
  return bad ? '«' + bad + '» — не IP-адрес' : '';
}

// Карточка «Локальные домены» для страницы DNS.
export function hostsCard() {
  const d = S.settings.dns;
  const list = h('div', { class: 'hosts' });
  const warn = h('div');

  function draw() {
    clear(warn);
    if (!d.enabled) warn.append(note('warn', 'DNS ядра выключен — локальные домены не работают. Включите его галочкой выше.'));
    clear(list);
    if (!d.hosts.length) list.append(note('', 'Локальных доменов пока нет. Добавьте имя и адрес, например nas.lan → 192.168.1.10: устройства, которые спрашивают DNS у ядра, получат этот адрес.'));
    d.hosts.forEach((r, i) => {
      const msg = h('div', { class: 'small', style: 'color:var(--err, #c0392b)' });
      const check = () => { msg.textContent = r.disabled ? '' : problem(r, d.hosts); };
      const ipIn = csv(r, 'ips', { ph: '192.168.1.10, fd00::10', onchange: check });
      const nameIn = txt(r, 'name', { ph: 'nas.lan', onchange: check });
      check();
      list.append(h('div', { class: 'card', style: 'margin-bottom:10px' + (r.disabled ? ';opacity:.6' : '') },
        fgrid(field('Имя', nameIn), field('IP-адреса', ipIn, 'Один или несколько адресов через запятую: IPv4 и IPv6.')),
        h('div', { class: 'row', style: 'align-items:center;gap:14px;margin-top:6px' },
          chk(r, 'subdomains', 'и все поддомены', 'Имя home.lan с этой галочкой отвечает ещё и для tv.home.lan, a.b.home.lan.'),
          chk({ get on() { return !r.disabled; }, set on(v) { r.disabled = !v; } }, 'on', 'включён', null, { onchange: () => { draw(); st.touch(); } }),
          h('span', { class: 'grow' }),
          h('button', { class: 'btn sm danger', 'aria-label': 'Удалить ' + (r.name || 'запись'), onclick: () => { d.hosts.splice(i, 1); st.touch(); draw(); } }, '✕')),
        field('Заметка', txt(r, 'note', { ph: 'необязательно' })), msg));
    });
  }

  function add() { d.hosts.push({ name: '', ips: [] }); st.touch(); draw(); const f = list.querySelector('.card:last-child input'); if (f) f.focus(); }

  function importHosts() {
    const t = { text: '' };
    modal({
      title: 'Импорт в формате hosts', wide: true,
      body: h('div', h('p', 'Одна запись на строку: IP-адрес и одно или несколько имён, как в /etc/hosts. Строки с # пропускаются.'),
        area(t, 'text', { rows: 8, ph: '192.168.1.10 nas.lan nas\n192.168.1.20 printer.lan' })),
      buttons: [{ text: 'Отмена' }, {
        text: 'Добавить', primary: true, onclick: () => {
          const by = new Map(d.hosts.map((r) => [String(r.name).toLowerCase(), r]));
          let n = 0;
          for (const raw of t.text.split('\n')) {
            const ln = raw.replace(/#.*/, '').trim();
            if (!ln) continue;
            const [ip, ...names] = ln.split(/\s+/);
            if (!isIP(ip)) continue;
            for (const nm of names.map((x) => x.toLowerCase())) {
              const ex = by.get(nm);
              if (ex) { if (!ex.ips.includes(ip)) ex.ips.push(ip); } else { const r = { name: nm, ips: [ip] }; d.hosts.push(r); by.set(nm, r); }
              n++;
            }
          }
          if (!n) { toast('Не нашёл ни одной записи «IP имя»', 'err'); return false; }
          st.touch(); draw(); toast('Добавлено записей: ' + n, 'ok');
        },
      }],
    });
  }

  const card = h('div', { class: 'card', style: 'margin-top:14px' }, h('h2', 'Локальные домены'),
    h('p', { class: 'small mute' }, 'Свои имена для устройств: ядро отвечает на них само, не обращаясь к DNS-серверам.'),
    warn,
    h('div', { class: 'row', style: 'margin-bottom:10px' },
      h('button', { class: 'btn primary', onclick: add }, '+ Добавить домен'),
      h('button', { class: 'btn', onclick: importHosts }, 'Импорт из hosts'),
      h('span', { class: 'mute small' }, 'Записей: ' + d.hosts.length)),
    list,
    h('p', { class: 'small mute' }, 'Работает для устройств, чьи DNS-запросы доходят до ядра (DNS роутера → ядро или перехват DNS). Эти ответы стоят выше всех DNS-правил и fakeip, так что имя всегда указывает на заданный адрес. Если у имени нет IPv6-адреса, ядро честно отвечает «записи нет» вместо запроса к внешнему DNS.'));
  draw();
  card.redraw = draw;
  return card;
}
