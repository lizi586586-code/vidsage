/** Node 18 combines Set-Cookie headers; Expires dates contain a comma. */
export function responseCookies(headers: Headers): string[] {
  const native = headers as Headers & { getSetCookie?: () => string[] };
  const values = native.getSetCookie?.() ??
    (headers.get('set-cookie') ? [headers.get('set-cookie')!] : []);
  return values.flatMap(value => value.split(/,(?=\s*[^\s;,=]+\s*=)/))
    .map(value => value.trim());
}

export function updateCookies(jar: Map<string, string>, headers: Headers): void {
  for (const value of responseCookies(headers)) {
    const first = value.split(';', 1)[0];
    const splitAt = first.indexOf('=');
    if (splitAt <= 0) continue;
    const name = first.slice(0, splitAt).trim();
    const content = first.slice(splitAt + 1).trim();
    if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(name) || /[\r\n;]/.test(content)) continue;
    const maxAge = value.match(/;\s*max-age\s*=\s*(-?\d+)\s*(?:;|$)/i);
    const expires = value.match(/;\s*expires\s*=\s*([^;]+)/i);
    // Max-Age takes precedence over Expires, including positive lifetimes.
    const expired = maxAge ? Number(maxAge[1]) <= 0 :
      Boolean(expires && Date.parse(expires[1]) <= Date.now());
    if (expired || !content) jar.delete(name);
    else jar.set(name, content);
  }
}
