export interface Env {
  MARKET_DATA: R2Bucket;
  DB: D1Database;
}

type LatestRow = {
  symbol: string;
  market: string;
  date: string;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  change: number;
  change_pct: number;
  source: string;
};

const jsonHeaders = {
  "content-type": "application/json; charset=utf-8",
  "cache-control": "public, max-age=60"
};

const corsHeaders = {
  "access-control-allow-origin": "*",
  "access-control-allow-methods": "GET, OPTIONS",
  "access-control-allow-headers": "Content-Type, Authorization",
  "access-control-max-age": "86400"
};

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    const parts = url.pathname.split("/").filter(Boolean);

    try {
      if (request.method === "OPTIONS") return corsPreflight(request);
      if (request.method !== "GET") return withCors(json({ error: "method_not_allowed" }, 405));
      if (parts[0] === "quote" && parts[1]) return withCors(await quote(env, parts[1]));
      if (parts[0] === "history" && parts[1]) return withCors(await history(env, parts[1], url.searchParams.get("range")));
      if (parts[0] === "daily" && parts[1] === "us" && parts[2]) return withCors(await r2JSON(env, `daily/us/${parts[2].slice(0, 4)}/${parts[2]}.json.gz`));
      if (parts[0] === "universe" && parts[1] === "us") return withCors(await r2JSON(env, "symbols/us/latest.json"));
      if (parts[0] === "context" && parts[1]) return withCors(await context(env, parts[1]));
      return withCors(json({ error: "not_found" }, 404));
    } catch (error) {
      return withCors(json({ error: "internal_error", message: error instanceof Error ? error.message : String(error) }, 500));
    }
  }
};

async function quote(env: Env, rawSymbol: string): Promise<Response> {
  const symbol = normalizeSymbol(rawSymbol);
  if (!symbol) return json({ error: "bad_symbol" }, 400);
  const row = await env.DB.prepare("SELECT * FROM latest_quotes WHERE symbol = ?").bind(symbol).first<LatestRow>();
  if (!row) return json({ error: "not_found", symbol }, 404);
  return json({
    symbol,
    date: row.date,
    price: row.close,
    change: row.change,
    changePct: row.change_pct,
    volume: row.volume,
    source: row.source
  });
}

async function history(env: Env, rawSymbol: string, range: string | null): Promise<Response> {
  const symbol = normalizeSymbol(rawSymbol);
  if (!symbol) return json({ error: "bad_symbol" }, 400);
  const object = await env.MARKET_DATA.get(`history/us/${symbol}/daily.json.gz`);
  if (!object) return json({ error: "not_found", symbol }, 404);
  const payload = await objectJSON<{ records: Array<{ date: string; close: number; volume: number }> }>(object);
  const records = filterRange(payload.records ?? [], range ?? "max");
  return json({ symbol, range: range ?? "max", records });
}

async function context(env: Env, rawSymbol: string): Promise<Response> {
  const symbol = normalizeSymbol(rawSymbol);
  if (!symbol) return json({ error: "bad_symbol" }, 400);
  const quoteRes = await env.DB.prepare("SELECT * FROM latest_quotes WHERE symbol = ?").bind(symbol).first<LatestRow>();
  const symRes = await env.DB.prepare("SELECT * FROM symbols WHERE symbol = ?").bind(symbol).first<Record<string, unknown>>();
  if (!quoteRes) return json({ error: "not_found", symbol }, 404);
  return json({
    symbol,
    name: symRes?.name ?? null,
    market: quoteRes.market,
    exchange: symRes?.exchange ?? null,
    latest: {
      date: quoteRes.date,
      price: quoteRes.close,
      changePct: quoteRes.change_pct,
      volume: quoteRes.volume
    },
    stats: null
  });
}

async function r2JSON(env: Env, key: string): Promise<Response> {
  const object = await env.MARKET_DATA.get(key);
  if (!object) return json({ error: "not_found", key }, 404);
  if (key.endsWith(".gz")) {
    return json(await objectJSON(object));
  }
  return new Response(object.body, { headers: jsonHeaders });
}

async function objectJSON<T = unknown>(object: R2ObjectBody): Promise<T> {
  if (object.key.endsWith(".gz")) {
    const stream = object.body.pipeThrough(new DecompressionStream("gzip"));
    return new Response(stream).json<T>();
  }
  return object.json<T>();
}

function filterRange<T extends { date: string }>(records: T[], range: string): T[] {
  if (range === "max") return records;
  const days = range === "1y" ? 365 : range === "6m" ? 183 : range === "3m" ? 92 : range === "1m" ? 31 : 0;
  if (!days) return records;
  const latest = records.at(-1)?.date;
  if (!latest) return records;
  const cutoff = new Date(latest);
  cutoff.setUTCDate(cutoff.getUTCDate() - days);
  return records.filter((record) => new Date(record.date) >= cutoff);
}

function normalizeSymbol(symbol: string): string | null {
  const normalized = symbol.toUpperCase();
  return /^[A-Z0-9.-]{1,16}$/.test(normalized) ? normalized : null;
}

function json(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), { status, headers: jsonHeaders });
}

function corsPreflight(request: Request): Response {
  const headers = new Headers(corsHeaders);
  const requestedHeaders = request.headers.get("Access-Control-Request-Headers");
  if (requestedHeaders) {
    headers.set("access-control-allow-headers", requestedHeaders);
  }
  return new Response(null, { status: 204, headers });
}

function withCors(response: Response): Response {
  const headers = new Headers(response.headers);
  for (const [name, value] of Object.entries(corsHeaders)) {
    headers.set(name, value);
  }
  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers
  });
}
