import { apd, h, clear, pageHead, badge, note, field, fgrid, txt, sel, chk, num, lines, toast, toastErr, confirmBox } from '../ui.js';
import { get, post } from '../api.js';
import * as st from '../state.js';

const S = st.S;
const CORES = {
  amnezia: { name: 'amnezia-box', tag: 'sing-box с AmneziaWG: обход блокировок WireGuard', chips: ['всё из sing-box', 'AmneziaWG', 'сжатая сборка'], pkg: false },
};
const ORDER = ['amnezia'];

export default async function (root) {
  const s = S.settings;
  let alive = true, pollT = 0;
  const meterBox = h('div');
  const coreRows = h('div');
  const depBox = h('div');
  let depT = 0;
  const jobBox = h('div');
  const latest = {}; // id -> ответ api/core/latest: есть ли версия новее установленной
  apd(root, pageHead('Компоненты', 'Ядро amnezia-box: установка, обновление и удаление'),
    h('div', { class: 'bay' }, meterBox, coreRows, depBox), jobBox);

  // Хватает ли места под установку: свободное место + размер уже стоящей копии (она заменится).
  function space(id) {
    const sg = (S.system && S.system.storage) || {};
    const free = sg.free_mb, need = (sg.need_mb || {})[id] || 0;
    if (free == null || !need) return { ok: true };
    const have = free + ((sg.installed_mb || {})[id] || 0);
    return { ok: free <= 0 || have >= need, free, need, dir: sg.dir };
  }

  function drawMeter() {
    const sg = (S.system && S.system.storage) || {};
    const need = sg.need_mb || {};
    const free = sg.free_mb == null ? 0 : sg.free_mb;
    const marks = ORDER.map((id) => [CORES[id].name, need[id] || 0]).filter((m) => m[1] > 0);
    const scale = Math.max(...marks.map((m) => m[1])) * 1.25; // шкала по самому большому компоненту; больше свободного места — заполнено целиком
    const fill = h('i'); fill.style.width = Math.min(100, free / scale * 100) + '%';
    const track = h('div', { class: 'meter-track', role: 'img', 'aria-label': 'Свободно ' + free + ' МБ' }, fill);
    const labels = h('div', { class: 'meter-marks' });
    for (const [n, v] of marks.slice().sort((x, y) => x[1] - y[1])) {
      const t = h('b', { class: 'tick ' + (free >= v ? 'fit' : 'nofit'), title: n + ': около ' + v + ' МБ' }); t.style.left = Math.min(98, v / scale * 100) + '%';
      track.append(t);
      labels.append(h('span', { class: free >= v ? 'fit' : 'nofit' }, n + ' — ' + v + ' МБ'));
    }
    clear(meterBox).append(h('div', { class: 'meter' },
      h('div', { class: 'meter-head' }, h('b', 'Свободно ' + (sg.free_mb == null ? '—' : free + ' МБ')),
        h('span', { class: 'mute small' }, 'Ядра ставятся в ', h('code', sg.dir || '—'), ' — каталог меняется ниже')),
      track, labels));
  }

  function draw() {
    const cores = (S.system && S.system.cores) || {};
    drawMeter();
    clear(coreRows);
    for (const id of ORDER) {
      const c = CORES[id], info = cores[id] || {};
      const sel = s.core === id;
      const blocked = !!c.blocked;
      const sp = space(id);
      const lack = !sp.ok && !c.blocked;
      const lt = latest[id];
      const upToDate = !!(info.installed && lt && lt.known && !lt.update);
      const label = !info.installed ? 'Установить' : upToDate ? 'Переустановить' : lt && lt.latest ? 'Обновить до v' + lt.latest : 'Обновить';
      const btn = h('button', { class: 'btn' + (upToDate ? '' : ' primary') + (lack ? ' lowspace' : ''), disabled: lack, title: lack ? 'Не хватает места' : upToDate ? 'Установлена последняя версия. Кнопка скачает её заново.' : null, onclick: (e) => { e.stopPropagation(); install(id, ''); } }, label);
      if (blocked) { btn.disabled = true; btn.title = 'Установка заблокирована'; btn.classList.remove('lowspace'); }
      const pkgBtn = c.pkg ? h('button', { class: 'btn', disabled: blocked, title: 'Пакет из репозитория OpenWrt/Entware (opkg или apk): обычно компактнее релиза GitHub.', onclick: (e) => { e.stopPropagation(); install(id, '', 'package'); } }, 'Из пакетов системы') : null;
      const delBtn = h('button', { class: 'btn danger', onclick: async (e) => { e.stopPropagation(); remove(id); } }, 'Удалить');
      const pick = h('input', { type: 'radio', name: 'core', checked: sel, disabled: blocked, 'aria-label': 'Использовать ' + c.name, onchange: () => { s.core = id; st.touch(); draw(); } });
      coreRows.append(h('div', { class: 'mod' + (sel ? ' on' : '') + (blocked ? ' blocked' : ''), onclick: (e) => { if (blocked || e.target.closest('button,input,label,details')) return; pick.click(); } },
        h('div', { class: 'mod-head' }, h('label', { class: 'mod-name' }, pick, h('b', c.name)),
          h('span', { class: 'mod-tag' }, c.tag), h('span', { class: 'grow' }),
          blocked ? badge('заблокировано', 'err') : null, sel ? badge('активно', 'acc') : null,
          info.installed && lt && lt.known ? (lt.update ? badge('доступна v' + lt.latest, 'acc') : badge('последняя версия', 'ok')) : null,
          info.installed ? badge('установлено', 'ok') : badge('не установлено', 'warn')),
        h('div', { class: 'chips' }, c.chips.map((x) => h('span', x))),
        h('div', { class: 'mod-meta mono small' }, info.installed ? [h('span', 'v' + (info.version || '?').replace(/^v/, '')), h('span', info.path), lt && lt.latest && !lt.known ? h('span', 'на GitHub: v' + lt.latest) : null] : [h('span', 'нужно около ' + (sp.need || '?') + ' МБ')]),
        (lack && !blocked) ? note('warn', 'Не хватает места в ' + (sp.dir || 'каталоге установки') + ': свободно ' + sp.free + ' МБ, нужно около ' + sp.need + ' МБ. Скачанный файл при нехватке места удаляется. Подключите накопитель и укажите каталог установки ниже' + (c.pkg ? ' либо поставьте ядро «Из пакетов системы».' : '.')) : null,
        h('div', { class: 'mod-act' }, btn, pkgBtn, info.installed ? delBtn : null)));
    }
  }

  async function remove(core) {
    if (!(await confirmBox('Удалить ' + CORES[core].name + ' с роутера? Освободится место на флеш-памяти; настройки панели сохранятся.', 'Удалить', true))) return;
    try {
      const r = await post('api/core/remove', { core });
      toast('Удалено: ' + r.removed, 'ok');
      await st.loadSystem(); draw();
    } catch (e) { toastErr(e); }
  }

  async function install(core, version, source) {
    try { await post('api/core/install', { core, version, source: source || 'github' }); } catch (e) { toastErr(e); return; }
    toast('Загрузка начата', 'ok'); poll();
  }
  async function loadLatest() {
    const cores = (S.system && S.system.cores) || {};
    for (const id of ORDER) {
      if (CORES[id].blocked || !(cores[id] || {}).installed) continue;
      try { latest[id] = await get('api/core/latest?core=' + id); } catch (e) { delete latest[id]; }
    }
    if (alive) draw();
  }
  function poll() {
    clearTimeout(pollT);
    const tick = async () => {
      if (!alive) return;
      let j;
      try { j = await get('api/core/job'); } catch (e) { return; }
      drawJob(j);
      if (j && j.running) { pollT = setTimeout(tick, 700); return; }
      if (j && j.finished) {
        if (j.error) toast('Ошибка установки: ' + j.error, 'err'); else toast(j.message || ('Установлено: ' + (j.version || '')), 'ok');
        try { await st.loadSystem(); } catch (e) { /* ignore */ }
        if (alive) draw();
        loadLatest();
      }
    };
    tick();
  }
  function drawJob(j) {
    clear(jobBox);
    if (!j || (!j.running && !j.finished && !j.stage)) return;
    const bar = h('i'); bar.style.width = Math.min(100, j.percent || 0) + '%';
    jobBox.append(h('div', { class: 'card' }, h('h2', 'Установка ' + st.coreName(j.core)),
      j.error ? note('err', j.error) : null,
      j.warning ? note('warn', j.warning) : null,
      h('div', { class: 'row small', style: 'margin-bottom:6px' }, j.running ? h('span', { class: 'spin' }) : null, h('span', j.message || j.stage || ''), h('span', { class: 'grow' }), h('span', (j.percent || 0) + '%')),
      h('div', { class: 'progress', role: 'progressbar', 'aria-valuemin': '0', 'aria-valuemax': '100', 'aria-valuenow': String(j.percent || 0) }, bar)));
  }

  async function depLoad() {
    clearTimeout(depT);
    let k;
    try { k = await get('api/kmods'); } catch (e) { return; }
    if (!alive) return;
    clear(depBox);
    if (!k.installable) return;
    const j = k.job || {};
    const have = k.present || [];
    const why = { 'kmod-tun': 'устройство TUN', 'kmod-nft-tproxy': 'режим tproxy', 'kmod-nft-queue': 'auto_redirect в TUN', 'kmod-nft-socket': 'ускорение tproxy (необязательно)' };
    depBox.append(h('div', { class: 'mod' },
      h('div', { class: 'mod-head' }, h('b', { class: 'mod-name' }, 'Зависимые компоненты'), h('span', { class: 'mod-tag' }, 'Модули ядра для TUN и tproxy'), h('span', { class: 'grow' }),
        have.length ? badge('установлено: ' + have.length, 'ok') : badge('нет', 'warn')),
      h('div', { class: 'chips' }, have.length ? have.map((p) => h('span', p + ' — ' + (why[p] || ''))) : [h('span', 'ничего не установлено')]),
      h('div', { class: 'row', style: 'flex-wrap:wrap;gap:8px;margin-top:10px' },
        h('button', { class: 'btn danger', disabled: !have.length || !!j.running, onclick: async () => {
          if (!(await confirmBox('Удалить ' + have.join(', ') + '? Режимы TUN и tproxy перестанут работать, пока пакеты не будут установлены снова (страница «Входящие, TUN, сеть»). Удалятся только эти пакеты. Ядро не затрагивается.', 'Удалить', true))) return;
          try { await post('api/kmods/remove'); toast('Удаляю…', 'ok'); } catch (e) { toastErr(e); }
          depLoad();
        } }, j.running ? 'Выполняется…' : 'Удалить зависимые компоненты')),
      j.error ? note('err', j.error) : null,
      (j.running || j.finished) && j.title === 'Удаление зависимых компонентов' ? h('pre', { class: 'mono', style: 'max-height:220px;overflow:auto;white-space:pre-wrap;margin-top:10px' }, (j.output || []).join('\n')) : null));
    if (j.running && alive) depT = setTimeout(depLoad, 1500);
  }

  draw();
  depLoad();
  loadLatest();
  get('api/core/job').then((j) => { if (j && j.running) poll(); else drawJob(j && j.finished && j.error ? j : null); }).catch(() => {});

  const d = s.download;
  // Список зеркал: по строке на адрес, проверка кнопкой и добавление популярных.
  function mirrorEditor() {
    const area = lines(d, 'mirrors', { rows: 4, ph: 'https://ghfast.top/\nhttps://gh-proxy.com/', dedupe: true });
    const out = h('div', { class: 'mirror-res small' });
    let popular = [];
    const test = h('button', { type: 'button', class: 'btn sm', title: 'С роутера запросить небольшой файл с GitHub напрямую и через каждое зеркало', onclick: async () => {
      test.disabled = true; test.textContent = 'Проверяю…';
      clear(out).append(h('span', { class: 'mute' }, 'Запрашиваю файл через каждое зеркало…'));
      try {
        const r = await post('api/mirrors/test', { mirrors: d.mirrors });
        popular = r.popular || popular;
        clear(out).append((r.results || []).map((x) => h('div', { class: 'mirror-row' },
          h('span', { class: 'dot ' + (x.ok ? 'ok' : 'err') }),
          h('span', { class: 'mirror-name mono' }, x.mirror || 'GitHub напрямую'),
          h('span', { class: x.ok ? 'mute' : 'bad' }, x.ok ? x.ms + ' мс' : x.error))));
      } catch (e) { clear(out).append(note('err', e.message || String(e))); }
      test.disabled = false; test.textContent = 'Проверить зеркала';
    } }, 'Проверить зеркала');
    const addPop = h('button', { type: 'button', class: 'btn sm', title: 'Публичные зеркала сторонних сервисов: могут пропасть, после добавления проверьте их', onclick: () => {
      const list = popular.length ? popular : ['https://ghfast.top/', 'https://gh-proxy.com/', 'https://ghproxy.net/', 'https://github.moeyy.xyz/', 'https://hub.gitmirror.com/'];
      const have = new Set(d.mirrors.map((x) => x.toLowerCase().replace(/\/+$/, '')));
      for (const m of list) if (!have.has(m.toLowerCase().replace(/\/+$/, ''))) d.mirrors.push(m);
      st.touch(); area.value = d.mirrors.join('\n');
      toast('Добавлены популярные зеркала: проверьте их кнопкой справа', 'ok');
    } }, 'Добавить популярные');
    return h('div', { class: 'mirrors' }, area, h('div', { class: 'row', style: 'margin-top:6px' }, addPop, test), out);
  }
  let ifList = [];
  try { ifList = (await get('api/interfaces')) || []; } catch (e) { /* список необязателен */ }
  const viaBox = h('div', { class: 'stack' });
  function drawVia() {
    clear(viaBox);
    viaBox.append(sel(d, 'via', [['direct', 'Напрямую (с зеркалами)'], ['proxy', 'Через HTTP-прокси'], ['core', 'Через ядро (по правилам маршрутизации)'], ['iface', 'Через сетевой интерфейс']], { onchange: drawVia }));
    if (d.via === 'proxy') viaBox.append(txt(d, 'proxy', { ph: 'http://127.0.0.1:7890' }), h('div', { class: 'small mute' }, 'Формат: http(s)://host:port.'));
    if (d.via === 'core') viaBox.append(note('', 'Работает, пока запущено ядро. Если ядро остановлено, загрузка не удастся: выберите другой способ.'));
    if (d.via === 'iface') {
      const known = new Set(ifList.map((i) => i.name));
      const opts = [['', '— выберите интерфейс —'], ...ifList.map((i) => [i.name, i.name + (i.up ? '' : ' (не активен)') + ((i.addrs || []).length ? ' — ' + i.addrs.slice(0, 2).join(', ') : '')])];
      if (d.interface && !known.has(d.interface)) opts.push([d.interface, d.interface + ' (не найден)']);
      viaBox.append(sel(d, 'interface', opts));
    }
  }
  drawVia();
  apd(root, 
    h('div', { class: 'card' }, h('h2', 'Автозапуск'),
      chk(s, 'autostart', 'Запускать ядро вместе с панелью', 'При старте роутера панель сама поднимет выбранное ядро с сохранёнными настройками.')),
    h('div', { class: 'card' }, h('h2', 'Загрузка ядер и наборов правил'),
      note('', 'Релизы берутся с GitHub. Если GitHub недоступен, укажите зеркало или прокси.'),
      fgrid(
        field('Зеркала GitHub', mirrorEditor(), 'По одному адресу в строке. Если GitHub недоступен, панель пробует зеркала по порядку, а затем сам GitHub. Используются для ядра, обновления панели и наборов правил (в конфиг ядра попадает первое зеркало). Зеркало видит всё, что вы через него скачиваете, и может подменить файл: берите только те, которым доверяете.'),
        field('Откуда скачивать', viaBox, 'Как панель ходит в сеть за ядрами, обновлениями панели, подписками и наборами правил. «Через ядро» — трафик идёт на mixed-порт ядра, и дальше его направляют правила маршрутизации: добавьте правило для github.com (или набор), чтобы выбрать нужный узел. «Через интерфейс» — соединения привязываются к выбранному сетевому интерфейсу (например, к конкретному провайдеру или VPN).'),
        field('Каталог установки ядер', txt(d, 'bin_dir', { ph: (S.system && S.system.platform && S.system.platform.bin_dir) || '/opt/bin' }), 'Куда класть бинарники. Пусто — каталог по умолчанию для платформы. На роутерах с малой флеш-памятью выбирайте внешний накопитель.'),
        field('Свой бинарник amnezia-box', txt(d, 'amnezia_path', { ph: '/opt/bin/amnezia-box' }))),
      h('div', { class: 'row' }, h('p', { class: 'small mute grow' }, 'Изменения вступают в силу после сохранения настроек. Свободное место считается по сохранённому каталогу.'),
        h('button', { class: 'btn', onclick: async () => { try { await st.loadSystem(); draw(); toast('Место пересчитано', 'ok'); } catch (e) { toastErr(e); } } }, 'Пересчитать место'))));
  return () => { alive = false; clearTimeout(pollT); clearTimeout(depT); };
}
