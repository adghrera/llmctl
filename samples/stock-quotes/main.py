"""Stock Quotes — llmctl agent tool sample.

Fetches current quotes from Yahoo Finance's public chart API (stdlib only,
no API key). Sandbox note: this tool declares "network": true in
llmctl-tool.json, so the sandboxed container gets outbound egress;
everything else (mem/pid caps, no-new-privileges) still applies.
"""

import json
import urllib.error
import urllib.parse
import urllib.request

CHART_URL = "https://query1.finance.yahoo.com/v8/finance/chart/{symbol}?interval=1d&range=5d"
USER_AGENT = (
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)


def _fetch_symbol(symbol: str) -> dict:
    url = CHART_URL.format(symbol=urllib.parse.quote(symbol))
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    with urllib.request.urlopen(req, timeout=15) as resp:
        data = json.loads(resp.read().decode("utf-8"))
    result = (data.get("chart") or {}).get("result")
    if not result:
        err = (data.get("chart") or {}).get("error") or {}
        raise ValueError(err.get("description") or "no data returned")
    return result[0]


def _fmt_num(v, digits=2):
    if v is None:
        return "n/a"
    try:
        return f"{float(v):,.{digits}f}"
    except (TypeError, ValueError):
        return "n/a"


def _quote_line(symbol: str) -> str:
    r = _fetch_symbol(symbol)
    meta = r.get("meta") or {}
    price = meta.get("regularMarketPrice")
    prev = meta.get("chartPreviousClose") or meta.get("previousClose")

    # same-day previous close from the daily series is more accurate
    indicators = ((r.get("indicators") or {}).get("quote") or [{}])[0]
    closes = [c for c in (indicators.get("close") or []) if c is not None]
    if len(closes) >= 2:
        prev = closes[-2]

    chg = (price - prev) if (price is not None and prev) else None
    pct = (chg / prev * 100.0) if (chg is not None and prev) else None
    arrow = "^" if (chg or 0) >= 0 else "v"

    lo = meta.get("regularMarketDayLow")
    hi = meta.get("regularMarketDayHigh")
    vol = meta.get("regularMarketVolume")
    exch = meta.get("fullExchangeName") or meta.get("exchangeName") or ""
    cur = meta.get("currency", "")

    line = f"- {symbol}: {_fmt_num(price)} {arrow} {_fmt_num(chg)} ({_fmt_num(pct)}%)"
    if lo is not None and hi is not None:
        line += f" | day range {_fmt_num(lo)}-{_fmt_num(hi)}"
    if vol is not None:
        line += f" | vol {int(vol):,}"
    if exch:
        line += f" | {exch}" + (f", {cur}" if cur else "")
    return line


def run(args):
    raw = str(args.get("symbols", "") or args.get("query", ""))
    symbols = [s.strip().upper() for s in raw.replace(" ", ",").split(",") if s.strip()]
    if not symbols:
        return 'error: provide ticker symbols, e.g. {"symbols": "AAPL, MSFT"}'
    if len(symbols) > 10:
        return "error: too many symbols (max 10 per call)"

    lines = ["Current quotes (Yahoo Finance):"]
    errors = 0
    for sym in symbols:
        try:
            lines.append(_quote_line(sym))
        except urllib.error.HTTPError as e:
            errors += 1
            if e.code == 404:
                lines.append(f"- {sym}: symbol not found")
            elif e.code == 429:
                lines.append(f"- {sym}: rate-limited (HTTP 429), try again in a minute")
            else:
                lines.append(f"- {sym}: HTTP {e.code}")
        except urllib.error.URLError as e:
            errors += 1
            lines.append(
                f"- {sym}: network failure ({e.reason}). "
                'If sandboxed, this tool needs "network": true in its manifest.'
            )
        except Exception as e:  # noqa: BLE001 - report, don't crash the agent loop
            errors += 1
            lines.append(f"- {sym}: {type(e).__name__}: {e}")
    if errors == len(symbols):
        return "error fetching all quotes:\n" + "\n".join(lines[1:])
    return "\n".join(lines)
