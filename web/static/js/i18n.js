// Язык интерфейса. Исходные тексты в коде русские; для других языков подгружается словарь i18n/<код>.json
// ({"русский фрагмент": "перевод"}), и тексты подменяются прямо в DOM, в том числе в уведомлениях и ответах сервера.
// Нет перевода — остаётся русский. Словарь собирается из кода: scripts/i18n.py extract / check.
const KEY = 'cp-lang';
// [код, короткая метка, название]; добавить язык = положить i18n/<код>.json и дописать сюда
export const LANGS = [['ru', 'RU', 'Русский'], ['en', 'EN', 'English']];
const CYR = /[А-Яа-яЁё]/;
const ATTRS = ['title', 'placeholder', 'aria-label', 'alt', 'data-tip'];
let cur = 'ru';
let exact = new Map();
let fragRe = null;
let frags = new Map();

function detect() {
  try { const s = localStorage.getItem(KEY); if (s && LANGS.some((l) => l[0] === s)) return s; } catch (e) { /* хранилище недоступно */ }
  const n = String(navigator.language || 'ru').toLowerCase();
  return /^(ru|uk|be|kk)/.test(n) ? 'ru' : 'en';
}

export const lang = () => cur;

export function setLang(code) {
  try { localStorage.setItem(KEY, code); } catch (e) { /* ignore */ }
  location.reload();
}

// Следующий язык по кругу — для кнопки в шапке.
export function nextLang() { return LANGS[(LANGS.findIndex((l) => l[0] === cur) + 1) % LANGS.length]; }

const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// Переводит строку: целиком (по точному совпадению) или по фрагментам, самые длинные первыми.
export function tr(s) {
  if (cur === 'ru' || typeof s !== 'string' || !CYR.test(s)) return s;
  const t = s.trim();
  const e = exact.get(t);
  if (e !== undefined) return s.replace(t, () => e);
  if (s.includes('\n')) return s.split('\n').map(tr).join('\n');
  if (!fragRe) return s;
  return s.replace(fragRe, (m, off, str) => {
    // фрагмент не должен быть частью другого русского слова
    if ((off > 0 && CYR.test(str[off - 1])) || CYR.test(str[off + m.length] || '')) return m;
    return frags.get(m) ?? m;
  });
}

function fixText(n) {
  const v = n.nodeValue;
  if (v && CYR.test(v)) { const r = tr(v); if (r !== v) n.nodeValue = r; }
}
function fixAttrs(el) {
  for (const a of ATTRS) {
    const v = el.getAttribute(a);
    if (v && CYR.test(v)) { const r = tr(v); if (r !== v) el.setAttribute(a, r); }
  }
}
function walk(node) {
  if (node.nodeType === 3) { fixText(node); return; }
  if (node.nodeType !== 1) return;
  const tag = node.tagName;
  fixAttrs(node);
  if (tag === 'SCRIPT' || tag === 'STYLE' || tag === 'TEXTAREA') return; // содержимое полей — данные пользователя
  for (let c = node.firstChild; c; c = c.nextSibling) walk(c);
}

export async function initI18n() {
  cur = detect();
  document.documentElement.lang = cur;
  if (cur === 'ru') return;
  let dict;
  try {
    const r = await fetch('i18n/' + cur + '.json');
    if (!r.ok) throw new Error('HTTP ' + r.status);
    dict = await r.json();
  } catch (e) { cur = 'ru'; document.documentElement.lang = 'ru'; return; } // словаря нет — остаёмся на русском
  const keys = Object.keys(dict).filter((k) => dict[k]);
  exact = new Map(keys.map((k) => [k, dict[k]]));
  frags = new Map(keys.map((k) => [k, dict[k]]));
  keys.sort((a, b) => b.length - a.length);
  try { fragRe = new RegExp(keys.map(esc).join('|'), 'g'); } catch (e) { fragRe = null; }

  document.title = tr(document.title);
  walk(document.body);
  new MutationObserver((recs) => {
    for (const r of recs) {
      if (r.type === 'childList') r.addedNodes.forEach(walk);
      else if (r.type === 'characterData') { if (r.target.parentNode && r.target.parentNode.tagName !== 'TEXTAREA') fixText(r.target); }
      else if (r.type === 'attributes') fixAttrs(r.target);
    }
    if (CYR.test(document.title)) document.title = tr(document.title);
  }).observe(document.documentElement, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ATTRS });
}
