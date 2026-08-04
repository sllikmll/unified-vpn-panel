// Mirror of web/service/panel.go isNewerVersion: parse a vMAJOR.MINOR.PATCH tag
// and report whether `latest` is ahead of `current`. When either side isn't a
// clean three-part numeric tag, fall back to a normalized string inequality —
// the same heuristic the Go side uses so the node "update available" badge
// agrees with what the server would decide.
function parseVersionParts(version: string): [number, number, number] | null {
  const parts = version.trim().replace(/^v/, '').split('.');
  if (parts.length !== 3) return null;
  const out: number[] = [];
  for (const part of parts) {
    if (!/^\d+$/.test(part)) return null;
    out.push(Number(part));
  }
  return [out[0], out[1], out[2]];
}

// Format a panel version for display. Dev builds report a "dev+<commit>"
// identity (see config.GetPanelVersion); show those — and any other
// non-numeric label — verbatim. Semantic versions get a single normalized "v"
// prefix, so a raw "v0.0.1" tag and a bare "0.0.1" both render as "v0.0.1"
// instead of doubling up to "vv0.0.1".
export function formatPanelVersion(version: string | undefined | null): string {
  const v = (version || '').trim();
  if (!v) return '';
  const normalized = v.replace(/^v/i, '');
  return /^\d/.test(normalized) ? `v${normalized}` : v;
}

export function isPanelUpdateAvailable(latest: string, current: string): boolean {
  if (!latest || !current) return false;
  const a = parseVersionParts(latest);
  const b = parseVersionParts(current);
  if (!a || !b) {
    return latest.trim().replace(/^v/, '') !== current.trim().replace(/^v/, '');
  }
  for (let i = 0; i < 3; i++) {
    if (a[i] > b[i]) return true;
    if (a[i] < b[i]) return false;
  }
  return false;
}
