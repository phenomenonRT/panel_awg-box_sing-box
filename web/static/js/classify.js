// Автоопределение типа записи для правил маршрутизации: IP/подсеть, домен (с поддоменами), точный домен,
// ключевое слово, регулярное выражение, порт и диапазон портов. Вставлять можно списком и даже ссылками.
const V4 = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;
const HOST = /^(?=.{1,253}$)([\p{L}\p{N}_]([\p{L}\p{N}_-]{0,61}[\p{L}\p{N}_])?\.)+[\p{L}\p{N}-]{2,63}$/u;

function isV6(s) {
  if (!s.includes(':') || !/^[0-9a-f:.]+$/i.test(s)) return false;
  try { new URL('http://[' + s + ']/'); return true; } catch { return false; }
}
function isCIDR(s) {
  const [ip, mask, extra] = s.split('/');
  if (extra !== undefined) return false;
  const v4 = V4.test(ip), v6 = !v4 && isV6(ip);
  if (!v4 && !v6) return false;
  if (mask === undefined) return true;
  if (!/^\d{1,3}$/.test(mask)) return false;
  return Number(mask) <= (v4 ? 32 : 128);
}

// Возвращает { type, value } или null, если запись не распознана.
export function classifyOne(raw) {
  let s = String(raw || '').trim();
  if (!s) return null;
  const pref = /^(full|domain|keyword|regexp|regex|suffix):(.+)$/i.exec(s);
  if (pref) {
    const k = pref[1].toLowerCase(), v = pref[2].trim();
    if (!v) return null;
    if (k === 'full') return { type: 'domain', value: v.toLowerCase() };
    if (k === 'keyword') return { type: 'domain_keyword', value: v.toLowerCase() };
    if (k === 'regexp' || k === 'regex') return { type: 'domain_regex', value: v };
    return { type: 'domain_suffix', value: v.replace(/^[.*]+/, '').toLowerCase() };
  }
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(s)) { // ссылка → берём только хост
    try { s = new URL(s).hostname; } catch { return null; }
    if (!s) return null;
  }
  s = s.replace(/^\[|\]$/g, '');
  if (isCIDR(s)) return { type: 'ip_cidr', value: s.toLowerCase() };
  if (/^\d{1,5}$/.test(s)) { const n = Number(s); return n >= 1 && n <= 65535 ? { type: 'port', value: String(n) } : null; }
  const pr = /^(\d{1,5})-(\d{1,5})$/.exec(s);
  if (pr) { const a = Number(pr[1]), b = Number(pr[2]); return a >= 1 && b <= 65535 && a < b ? { type: 'port_range', value: a + '-' + b } : null; }
  const d = s.replace(/^(\*\.|\.)+/, '').replace(/\.$/, '').toLowerCase();
  if (HOST.test(d)) return { type: 'domain_suffix', value: d };
  return null;
}

// Разбирает вставленный текст (через пробел, запятую, «;» или перенос строки) в группы по типам.
export function classify(text) {
  const groups = new Map(), bad = [];
  for (const tok of String(text || '').split(/[\s,;]+/)) {
    if (!tok) continue;
    const c = classifyOne(tok);
    if (!c) { bad.push(tok); continue; }
    if (!groups.has(c.type)) groups.set(c.type, []);
    const arr = groups.get(c.type);
    if (!arr.includes(c.value)) arr.push(c.value);
  }
  return { groups, bad };
}
