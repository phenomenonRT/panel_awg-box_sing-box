import { apd, h, clear, pageHead, note, field, fgrid, txt, num, dur, sel, chk, toast, help } from '../ui.js';
import { ruleEditor } from '../rules.js';
import * as st from '../state.js';
import { transparentNote } from '../transparent.js';

const S = st.S;
const SB = 'https://raw.githubusercontent.com/SagerNet/';
const ID = 'https://raw.githubusercontent.com/itdoginfo/allow-domains/main/';
const MCT = 'https://raw.githubusercontent.com/avgustvishne/CDN-Cloud-MagiTrickle/main/data/';
const ASN = 'https://asn.web2core.workers.dev/';
const DEFAULT_RULESET_INTERVAL = 0;
const MCT_PROFILES = [
  ['FULL', 'full', 'presets/full'], ['BALANCED', 'balanced', 'presets/balanced'],
  ['PERFORMANCE', 'performance', 'presets/performance'], ['MINIMAL', 'minimal', 'presets/minimal'],
  ['CDN', 'cdn', 'presets/cdn'], ['CLOUD', 'cloud', 'presets/cloud'],
  ['VIDEO', 'video', 'presets/video'], ['VPN', 'vpn', 'presets/vpn'],
  ['ASN ALL', 'asn-all', 'asn-all'], ['STABLE', 'stable', 'presets/stable'],
];
const ASN_PRESETS = [
  ['AWS IPv4', 'aws-v4', 'AS16509,AS14618?v4'], ['AWS IPv6', 'aws-v6', 'AS16509,AS14618?v6'],
  ['Fastly CDN IPv4', 'fastly-v4', 'AS54113?v4'],
  ['Melbicom IPv4 (AS56630)', 'melbicom-as56630-v4', 'AS56630?v4'],
  ['Melbicom IPv4 (AS8849)', 'melbicom-as8849-v4', 'AS8849?v4'],
  ['Melbicom IPv6 (AS56630)', 'melbicom-as56630-v6', 'AS56630?v6'],
  ['FT/BuyVM IPv4', 'buyvm-v4', 'AS53667?v4'], ['FT/BuyVM IPv6', 'buyvm-v6', 'AS53667?v6'],
  ['Vultr IPv4', 'vultr-v4', 'AS20473?v4'], ['Vultr IPv6', 'vultr-v6', 'AS20473?v6'],
  ['Contabo IPv4', 'contabo-v4', 'AS51167?v4'], ['Contabo IPv6', 'contabo-v6', 'AS51167?v6'],
  ['Akamai IPv4', 'akamai-v4', 'AS20940?v4'], ['Akamai IPv6', 'akamai-v6', 'AS20940?v6'],
  ['Scaleway IPv4', 'scaleway-v4', 'AS12876?v4'], ['Scaleway IPv6', 'scaleway-v6', 'AS12876?v6'],
  ['Cloudflare by ASN IPv4', 'cloudflare-asn-v4', 'AS13335,AS14789,AS132892,AS133877,AS139242,AS202623,AS203898,AS209242,AS394536,AS395747,AS400095?v4'],
  ['Hetzner by ASN IPv4', 'hetzner-asn-v4', 'AS24940,AS213230,AS212317,AS215859?v4'],
  ['OVH by ASN IPv4', 'ovh-asn-v4', 'AS16276,AS35540,AS199949?v4'],
  ['Akamai by ASN (Linode, Apple Private Relay, Prolexic) IPv4', 'akamai-asn-v4', 'AS16625,AS20940,AS12222,AS18680,AS18717,AS22207,AS23903,AS32787,AS35994,AS36183,AS63949?v4'],
  ['DigitalOcean by ASN IPv4', 'digitalocean-asn-v4', 'AS14061,AS133165,AS394362,AS393406,AS135340,AS201229,AS202018,AS202109?v4'],
  ['Microsoft Azure by ASN IPv4', 'microsoft-azure-asn-v4', 'AS8075,AS12076,AS54489,AS3598,AS6584,AS8068?v4'],
  ['Edgio by ASN IPv4', 'edgio-asn-v4', 'AS15133,AS22822?v4'],
  ['Oracle Cloud by ASN IPv4', 'oracle-cloud-asn-v4', 'AS31898,AS23871,AS39648?v4'],
  ['Alibaba Cloud by ASN IPv4', 'alibaba-cloud-asn-v4', 'AS37963,AS45102,AS24429,AS45090?v4'],
  ['CDN77 by ASN IPv4', 'cdn77-asn-v4', 'AS60068?v4'],
];
// [подпись, тип, имя]
const PRESETS = [
  ['Реклама', 'geosite', 'category-ads-all'], ['Россия (домены)', 'geosite', 'category-ru'], ['Россия (IP)', 'geoip', 'ru'],
  ['Локальные (домены)', 'geosite', 'private'], ['Локальные (IP)', 'geoip', 'private'], ['Telegram (домены)', 'geosite', 'telegram'], ['Telegram (IP)', 'geoip', 'telegram'],
  ['YouTube', 'geosite', 'youtube'], ['Google', 'geosite', 'google'], ['Netflix', 'geosite', 'netflix'], ['OpenAI', 'geosite', 'openai'],
  ['Discord', 'geosite', 'discord'], ['GitHub', 'geosite', 'github'], ['Twitter / X', 'geosite', 'twitter'], ['Китай (IP)', 'geoip', 'cn'],
  // обычные текстовые списки itdoginfo/allow-domains: панель сама скачивает и обновляет их
  ['itdoginfo: Россия inside (заблокированное в РФ)', 'list', 'itdog-russia-inside', ID + 'Russia/inside-raw.lst'],
  ['itdoginfo: Россия outside (российское для зарубежья)', 'list', 'itdog-russia-outside', ID + 'Russia/outside-raw.lst'],
  ['itdoginfo: Новости', 'list', 'itdog-news', ID + 'Categories/news.lst'],
  ['itdoginfo: Геоблок', 'list', 'itdog-geoblock', ID + 'Categories/geoblock.lst'],
  ['itdoginfo: Хостинги (Hetzner, OVH, DO, Cloudflare, AWS, Akamai)', 'list', 'itdog-hodca', ID + 'Categories/hodca.lst'],
  ['itdoginfo: Discord', 'list', 'itdog-discord', ID + 'Services/discord.lst'],
  ['itdoginfo: Meta', 'list', 'itdog-meta', ID + 'Services/meta.lst'],
  ['itdoginfo: Telegram', 'list', 'itdog-telegram', ID + 'Services/telegram.lst'],
  ['itdoginfo: TikTok', 'list', 'itdog-tiktok', ID + 'Services/tiktok.lst'],
  ['itdoginfo: Twitter / X', 'list', 'itdog-twitter', ID + 'Services/twitter.lst'],
  ['itdoginfo: YouTube', 'list', 'itdog-youtube', ID + 'Services/youtube.lst'],
  ['itdoginfo: Подсети Cloudflare', 'list', 'itdog-ip-cloudflare', ID + 'Subnets/IPv4/cloudflare.lst'],
  ['itdoginfo: Подсети Discord', 'list', 'itdog-ip-discord', ID + 'Subnets/IPv4/discord.lst'],
  ['itdoginfo: Подсети Meta', 'list', 'itdog-ip-meta', ID + 'Subnets/IPv4/meta.lst'],
  ['itdoginfo: Подсети Telegram', 'list', 'itdog-ip-telegram', ID + 'Subnets/IPv4/telegram.lst'],
  ['itdoginfo: Подсети Twitter / X', 'list', 'itdog-ip-twitter', ID + 'Subnets/IPv4/twitter.lst'],
  ...MCT_PROFILES.flatMap(([label, slug, path]) => [
    [`MagiTrickle: ${label} IPv4`, 'list', `magitrickle-${slug}-ipv4`, `${MCT}${path}-v4.txt`],
    [`MagiTrickle: ${label} IPv6`, 'list', `magitrickle-${slug}-ipv6`, `${MCT}${path}-v6.txt`],
  ]),
  ...ASN_PRESETS.map(([label, slug, path]) => [`ASN: ${label}`, 'list', `asn-${slug}`, `${ASN}${path}`]),
];
function presetURL(core, kind, name) {
  return SB + (kind === 'geosite' ? 'sing-geosite/rule-set/geosite-' : 'sing-geoip/rule-set/geoip-') + name + '.srs';
}
function presetSource(preset) {
  if (preset[1] !== 'list') return 'SagerNet/sing-box (geosite и geoip)';
  if (preset[2].startsWith('itdog-')) return 'itdoginfo/allow-domains';
  if (preset[2].startsWith('magitrickle-')) return 'CDN-Cloud-MagiTrickle';
  if (preset[2].startsWith('asn-')) return 'ASN — asn.web2core.workers.dev';
  return 'Другие источники';
}
// быстрые шаблоны правил: [подпись, тип, значения, куда]
const QUICK = [
  ['Локальные сети → напрямую', 'private', [], 'direct'], ['Реклама → блок', 'geosite', ['category-ads-all'], 'block'],
  ['Россия → напрямую', 'geosite', ['category-ru'], 'direct'], ['Россия (IP) → напрямую', 'geoip', ['ru'], 'direct'],
  ['Telegram → прокси', 'geosite', ['telegram'], '@proxy'], ['YouTube → прокси', 'geosite', ['youtube'], '@proxy'],
];

export default async function (root) {
  const s = S.settings;
  const outs = () => st.outbounds();
  const proxyOut = () => (s.groups.find((g) => g.type === 'selector') || s.groups[0] || s.nodes[0] || { name: 'direct' }).name;
  const ed = ruleEditor({ rules: s.rules, outs, defaultOut: proxyOut });
  const setsBox = h('div');
  const core = () => s.core;

  // Набор сам ничего не маршрутизирует: трафик направляет правило «Набор правил → куда». Здесь это правило
  // создаётся и меняется одним выбором «Куда направить», а в списке правил выше оно остаётся видимым.
  const ruleOf = (name) => s.rules.find((x) => x.type === 'rule_set' && x.values && x.values.length === 1 && x.values[0] === name);
  const usedElsewhere = (name) => s.rules.filter((x) => x.type === 'rule_set' && x.values && x.values.includes(name) && !(x.values.length === 1)).length;
  const dest = () => [['', 'не направлять (набор только подключён)'], ...outs()];
  function setRule(name, out) {
    const r = ruleOf(name);
    if (!out) { if (r) s.rules.splice(s.rules.indexOf(r), 1); }
    else if (r) r.outbound = out;
    else s.rules.push({ type: 'rule_set', values: [name], outbound: out });
    st.touch(); ed.redraw();
  }

  function drawSets() {
    clear(setsBox);
    if (!s.rule_sets.length) setsBox.append(h('div', { class: 'empty' }, 'Наборов пока нет. Выберите шаблон выше и укажите, куда направлять трафик из него.'));
    s.rule_sets.forEach((r, i) => {
      let prev = r.name;
      const cur = ruleOf(r.name);
      const more = usedElsewhere(r.name);
      const addedAt = Number(r.added_at);
      const addedDate = Number.isFinite(addedAt) && addedAt > 0 ? new Date(addedAt).toLocaleDateString() : '';
      const source = String(r.url || '').trim();
      let sourceRow = null;
      if (source) {
        let sourceLink = source;
        try {
          const parsed = new URL(source);
          if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
            sourceLink = h('a', { href: parsed.href, target: '_blank', rel: 'noopener noreferrer', title: parsed.href }, parsed.href);
          }
        } catch { /* Keep invalid or incomplete addresses visible as plain text. */ }
        sourceRow = h('div', { class: 'small mute rs-source' }, 'Источник данных: ', sourceLink);
      }
      const outSel = h('select', { 'aria-label': 'Куда направить трафик набора ' + r.name, onchange: (e) => { if (!r.name) { toast('Сначала укажите имя набора', 'err'); e.target.value = ''; return; } setRule(r.name, e.target.value); drawSets(); } },
        dest().map(([v, t]) => h('option', { value: v, selected: v === (cur ? cur.outbound : '') }, t)));
      const status = cur ? h('span', { class: 'badge ok' }, 'направляется → ' + cur.outbound)
        : more ? h('span', { class: 'badge' }, 'используется в составном правиле')
          : h('span', { class: 'badge warn' }, 'не используется — трафик не направляется');
      setsBox.append(h('div', { class: 'rule' },
        h('div', { class: 'row', style: 'flex-wrap:wrap;gap:8px 12px;align-items:center;margin-bottom:8px' },
          h('b', r.name || 'новый набор'), addedDate ? h('span', { class: 'small mute' }, 'добавлен ' + addedDate) : null, status, h('span', { class: 'grow' }),
          h('button', { class: 'btn sm danger', onclick: () => { const old = ruleOf(r.name); if (old) s.rules.splice(s.rules.indexOf(old), 1); s.rule_sets.splice(i, 1); st.touch(); ed.redraw(); drawSets(); } }, 'Удалить')),
        sourceRow,
        fgrid(field('Куда направить трафик', outSel, 'Создаёт правило «этот набор → выбранный выход». Правила работают сверху вниз: новое добавляется в конец списка правил, а всё, что не подошло, уходит в «Final».'),
          field('Адрес списка (URL)', txt(r, 'url', { ph: 'https://…/set.srs' }), 'Файл .srs или .json — ядро скачивает и обновляет его само. Обычный список (.lst, .txt: домен в строке, можно подсети) панель скачивает и преобразует сама.')),
        h('details', { style: 'margin-top:6px' }, h('summary', { class: 'small' }, 'Дополнительно: имя, формат, обновление'),
          fgrid(field('Имя', txt(r, 'name', { ph: 'my-set', onchange: () => { const c = ruleOf(prev); if (c) c.values = [r.name]; prev = r.name; ed.redraw(); } }), 'Внутреннее имя набора; в правилах он выбирается по нему.'),
            field('Формат', sel(r, 'format', [['', 'определить по URL'], ['srs', 'srs (бинарный)'], ['json', 'json (текстовый)'], ['text', 'список (домен в строке)']]), 'Обычно определяется по окончанию ссылки.'),
            field('Обновлять раз в', dur(r, 'interval', [['сек', 1], ['мин', 60], ['ч', 3600], ['сут', 86400], ['мес', 30 * 86400]], { min: 0 }), 'По умолчанию 0: скачать при первом запуске и больше не обновлять.')))));
    });
  }

  const presetSel = { out: '' };
  const selectedPresets = new Set();
  const presetSummaryText = h('span', 'Выбрать шаблоны');
  const presetSummary = h('summary', { class: 'btn', 'aria-label': 'Выбрать шаблоны наборов правил' }, presetSummaryText,
    h('span', { class: 'rs-chevron', 'aria-hidden': 'true' }, '▾'));
  let previousSource = '';
  const presetOptions = [];
  PRESETS.forEach((p, i) => {
    const source = presetSource(p);
    if (source !== previousSource) {
      if (previousSource) presetOptions.push(h('hr', { class: 'rs-preset-divider' }));
      presetOptions.push(h('div', { class: 'small mute rs-preset-source' }, source));
      previousSource = source;
    }
    presetOptions.push(h('label', { class: 'check rs-preset-option' },
      h('input', { type: 'checkbox', onchange: (e) => {
        if (e.target.checked) selectedPresets.add(i); else selectedPresets.delete(i);
        updatePresetSummary();
      } }), p[0]));
  });
  const presetPicker = h('details', { class: 'rs-picker' }, presetSummary,
    h('div', { class: 'rs-preset-menu' }, presetOptions));
  function updatePresetSummary() {
    const label = selectedPresets.size ? 'Выбрано шаблонов: ' + selectedPresets.size : 'Выбрать шаблоны';
    presetSummaryText.textContent = label;
    presetSummary.setAttribute('aria-label', label);
  }
  const g = s.general;
  apd(root, pageHead('Маршрутизация', 'Правила применяются сверху вниз: побеждает первое подходящее'), transparentNote(),
    h('div', { class: 'card' }, h('h2', 'Общее'),
      fgrid(field('Final — исходящий по умолчанию', sel(s, 'final', outs()), 'Куда отправлять трафик, не попавший ни под одно правило.'),
        field('Скачивать наборы правил через', sel(g, 'update_via', [['direct', 'direct — напрямую'], ...outs().filter((o) => o[0] !== 'direct' && o[0] !== 'block')]), 'Если GitHub заблокирован, укажите здесь рабочий узел или группу.')),
      chk(g, 'rule_sets_ram', 'Хранить рабочие копии наборов в RAM', 'Первая успешная версия сохраняется во флеш-память, рабочая копия — в /tmp (RAM). При интервале 0 источник скачивается только один раз; при ненулевом интервале текстовые списки проверяются по расписанию, изменения остаются в RAM. После перезагрузки восстанавливается флеш-копия. Расход RAM примерно равен суммарному размеру активных наборов.'),
      chk(g, 'bypass_lan', 'Локальные сети — напрямую (bypass LAN)', 'Добавляет правило «частные адреса → direct» перед остальными, чтобы доступ к роутеру и устройствам дома не уходил в прокси.'),
      chk(g, 'sniff', 'Определять домен и протокол (sniff)', 'Ядро подсматривает SNI/Host в первых пакетах. Нужно для доменных правил при прозрачном прокси и для правил по протоколу.')),
    h('div', { class: 'card' }, h('h2', 'Правила'),
      h('div', { class: 'row', style: 'margin-bottom:10px' },
        h('button', { class: 'btn primary', onclick: () => ed.add() }, '+ Добавить правило'),
        h('select', { 'aria-label': 'Шаблон правила', style: 'width:auto', onchange: (e) => {
          const q = QUICK[e.target.value]; e.target.value = ''; if (!q) return;
          s.rules.push({ type: q[1], values: q[2].slice(), outbound: q[3] === '@proxy' ? proxyOut() : q[3] }); st.touch(); ed.redraw();
        } }, h('option', { value: '' }, 'Добавить по шаблону…'), QUICK.map((q, i) => h('option', { value: i }, q[0])))),
      ed.el),
    h('div', { class: 'card' }, h('h2', 'Наборы правил'),
      h('p', { class: 'small mute' }, 'Набор — это готовый список доменов или подсетей (Telegram, YouTube, реклама…). Выберите набор и то, куда направлять его трафик: правило создастся само и появится в списке «Правила» выше. Ссылки на списки (.srs) подставляются автоматически.'),
      h('p', { class: 'small mute' }, 'Добавлены профили IPv4/IPv6 от ', h('a', { href: 'https://github.com/avgustvishne/CDN-Cloud-MagiTrickle', target: '_blank', rel: 'noopener noreferrer' }, 'CDN-Cloud-MagiTrickle'), '. Их списки обновляются по ссылкам источника.'),
      h('p', { class: 'small mute' }, 'Добавлены также отдельные ASN-подписки от ', h('a', { href: 'https://asn.web2core.workers.dev/', target: '_blank', rel: 'noopener noreferrer' }, 'asn.web2core.workers.dev'), '.'),
      h('div', { class: 'row', style: 'margin-bottom:10px;flex-wrap:wrap;gap:8px' },
        presetPicker,
        h('span', { class: 'small mute' }, '→'),
        h('select', { 'aria-label': 'Куда направить', style: 'width:auto', onchange: (e) => { presetSel.out = e.target.value; } },
          outs().filter((o) => o[0] !== undefined).map(([v, t]) => h('option', { value: v, selected: v === proxyOut() }, t))),
        h('button', { class: 'btn primary', onclick: () => {
          if (!selectedPresets.size) { toast('Выберите один или несколько шаблонов', 'err'); return; }
          const out = presetSel.out || proxyOut();
          const addedAt = Date.now();
          let added = 0, skipped = 0;
          for (const i of selectedPresets) {
            const p = PRESETS[i]; const isList = p[1] === 'list'; const name = isList ? p[2] : p[1] + '-' + p[2];
            if (s.rule_sets.some((r) => r.name === name)) { skipped++; continue; }
            s.rule_sets.push(isList ? { name, url: p[3], format: 'text', interval: DEFAULT_RULESET_INTERVAL, added_at: addedAt } : { name, url: presetURL(core(), p[1], p[2]), format: 'srs', behavior: p[1] === 'geoip' ? 'ipcidr' : 'domain', interval: DEFAULT_RULESET_INTERVAL, added_at: addedAt });
            const existing = ruleOf(name);
            if (existing) existing.outbound = out;
            else s.rules.push({ type: 'rule_set', values: [name], outbound: out });
            added++;
          }
          selectedPresets.clear();
          for (const checkbox of presetPicker.querySelectorAll('input[type=checkbox]')) checkbox.checked = false;
          updatePresetSummary();
          presetPicker.open = false;
          if (added) { st.touch(); ed.redraw(); drawSets(); }
          if (added && skipped) toast('Добавлено наборов: ' + added + ', уже были: ' + skipped, 'ok');
          else if (added) toast('Добавлено наборов: ' + added + ' → ' + out, 'ok');
          else toast('Выбранные наборы уже добавлены', 'err');
        } }, 'Добавить'),
        h('button', { class: 'btn', onclick: () => { s.rule_sets.push({ name: 'set-' + (s.rule_sets.length + 1), url: '', format: '', behavior: '', interval: DEFAULT_RULESET_INTERVAL, added_at: Date.now() }); st.touch(); drawSets(); } }, 'Свой набор по ссылке')),
      setsBox));
  drawSets();
}
