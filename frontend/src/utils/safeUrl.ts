/**
 * Returns an absolute http(s) URL with a host, and "" for anything else.
 *
 * Some URLs the UI binds to `href` are chosen by whoever can reach the server
 * with an ingestion key — an OpenLineage facet, for instance — so a
 * `javascript:` value would run in this origin and inherit the HttpOnly
 * session cookie. The server drops those before it derives a link
 * (`backend/plugin/openlineage/airflow_links.go`); this is the second gate, and
 * the one that covers any future server-supplied URL.
 *
 * The parser rejects a relative or malformed value outright, and a special
 * scheme (`http:`/`https:`) cannot be parsed without a host, so the protocol
 * check is the whole whitelist.
 */
export function safeExternalUrl(value: string | undefined | null): string {
  const trimmed = value?.trim() ?? "";
  if (!trimmed) {
    return "";
  }

  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return "";
  }

  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    return "";
  }

  return parsed.href;
}
