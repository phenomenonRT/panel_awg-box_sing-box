#!/usr/bin/env python3
"""Извлечение русских строк интерфейса и проверка словаря переводов.

  scripts/i18n.py extract            — печатает число уникальных фрагментов и пишет их в i18n-keys.json (рядом со скриптом не сохраняется)
  scripts/i18n.py check [lang]       — сравнивает web/static/i18n/<lang>.json с кодом: недостающие ключи, лишние, битые плейсхолдеры

Фрагмент — кусок строкового литерала между интерполяциями (${...}, %s, %d, ...) без крайних пробелов.
Интерфейс подменяет такие фрагменты внутри готовых текстов, поэтому склейки вида 'Текст ' + x + ' текст' переводятся по частям.
"""
import json, os, re, sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CYR = re.compile('[А-Яа-яЁё]')
VERB = re.compile(r'%[-+# 0]*\d*(?:\.\d+)?[a-zA-Z%]')


def tokenize_js(src):
    """Возвращает литералы строк (без комментариев); шаблонные строки делятся по ${...}."""
    out, i, n = [], 0, len(src)
    while i < n:
        c = src[i]
        if c == '/' and src[i:i+2] == '//':
            i = src.find('\n', i); i = n if i < 0 else i
        elif c == '/' and src[i:i+2] == '/*':
            j = src.find('*/', i + 2); i = n if j < 0 else j + 2
        elif c in '\'"':
            j, buf = i + 1, []
            while j < n and src[j] != c:
                if src[j] == '\\' and j + 1 < n:
                    e = src[j+1]
                    buf.append({'n': '\n', 't': '\t'}.get(e, e)); j += 2
                else:
                    buf.append(src[j]); j += 1
            out.append(''.join(buf)); i = j + 1
        elif c == '`':
            j, buf, depth = i + 1, [], 0
            while j < n:
                if src[j] == '\\' and j + 1 < n:
                    buf.append(src[j+1]); j += 2; continue
                if src[j] == '`': break
                if src[j:j+2] == '${':
                    out.append(''.join(buf)); buf = []
                    # пропускаем выражение с вложенными скобками и строками
                    j += 2; depth = 1
                    sub_start = j
                    while j < n and depth:
                        if src[j] == '{': depth += 1
                        elif src[j] == '}': depth -= 1
                        j += 1
                    out.extend(tokenize_js(src[sub_start:j-1]))
                    continue
                buf.append(src[j]); j += 1
            out.append(''.join(buf)); i = j + 1
        elif c == '/' and i > 0 and re.match(r'[\s(,=:!&|?{};]', src[i-1]) and src[i+1:i+2] not in ('/', '*', ' '):
            # литерал регулярного выражения — пропускаем (в нём бывает кириллица для проверок, не для показа)
            j = i + 1
            while j < n and src[j] != '/' and src[j] != '\n':
                j += 2 if src[j] == '\\' else 1
            i = j + 1
        else:
            i += 1
    return out


def tokenize_go(src):
    out, i, n = [], 0, len(src)
    while i < n:
        c = src[i]
        if src[i:i+2] == '//':
            i = src.find('\n', i); i = n if i < 0 else i
        elif src[i:i+2] == '/*':
            j = src.find('*/', i + 2); i = n if j < 0 else j + 2
        elif c == '"':
            j, buf = i + 1, []
            while j < n and src[j] != '"':
                if src[j] == '\\' and j + 1 < n:
                    e = src[j+1]
                    buf.append({'n': '\n', 't': '\t'}.get(e, e)); j += 2
                else:
                    buf.append(src[j]); j += 1
            out.append(''.join(buf)); i = j + 1
        elif c == '`':
            j = src.find('`', i + 1); j = n if j < 0 else j
            out.append(src[i+1:j]); i = j + 1
        elif c == "'":
            i += 3 if src[i+1:i+2] != '\\' else 4
        else:
            i += 1
    return out


def fragments(lit, go):
    parts = VERB.split(lit) if go else [lit]
    for p in parts:
        for q in re.split(r'\n', p):
            q = q.strip()
            if q and CYR.search(q):
                yield q


def files():
    for base, _, names in os.walk(os.path.join(ROOT, 'web', 'static')):
        for f in names:
            if f.endswith(('.js', '.html')):
                yield os.path.join(base, f), False
    for base, _, names in os.walk(os.path.join(ROOT, 'internal')):
        for f in names:
            if f.endswith('.go') and not f.endswith('_test.go'):
                yield os.path.join(base, f), True


def extract():
    keys = {}
    for path, go in files():
        src = open(path, encoding='utf-8').read()
        if path.endswith('.html'):
            lits = re.findall(r'>([^<>]*[А-Яа-яЁё][^<>]*)<', src) + re.findall(r'(?:title|alt|placeholder|aria-label)="([^"]*[А-Яа-яЁё][^"]*)"', src)
        else:
            lits = tokenize_go(src) if go else tokenize_js(src)
        for lit in lits:
            for fr in fragments(lit, go):
                keys.setdefault(fr, (os.path.relpath(path, ROOT), lit))
    return keys


def check(lang):
    keys = extract()
    p = os.path.join(ROOT, 'web', 'static', 'i18n', lang + '.json')
    have = json.load(open(p, encoding='utf-8')) if os.path.exists(p) else {}
    missing = [k for k in keys if k not in have]
    extra = [k for k in have if k not in keys]
    bad = []
    num = re.compile(r'\d+')
    for k, v in have.items():
        if k in keys and (sorted(num.findall(k)) != sorted(num.findall(v)) or VERB.findall(k) != VERB.findall(v)):
            bad.append(k)
    print(f'{lang}: всего ключей в коде {len(keys)}, переведено {len(keys) - len(missing)}, не хватает {len(missing)}, лишних {len(extra)}, подозрительных (числа/плейсхолдеры) {len(bad)}')
    for k in bad[:20]:
        print('  ?', k, '=>', have[k])
    return missing, extra, bad


if __name__ == '__main__':
    cmd = sys.argv[1] if len(sys.argv) > 1 else 'extract'
    if cmd == 'extract':
        k = extract()
        print(len(k), 'уникальных фрагментов,', sum(len(x) for x in k), 'символов')
        if len(sys.argv) > 2:  # с контекстом (файл и весь литерал) — для переводчика
            json.dump([{'k': a, 'f': b[0], 'c': b[1]} for a, b in k.items()], open(sys.argv[2], 'w', encoding='utf-8'), ensure_ascii=False, indent=0)
    elif cmd == 'check':
        m, e, b = check(sys.argv[2] if len(sys.argv) > 2 else 'en')
        sys.exit(1 if (m or b) and '--strict' in sys.argv else 0)
