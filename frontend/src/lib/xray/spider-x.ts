// Mirrors deriveSpiderX in internal/sub/service.go byte-for-byte so panel links
// and subscription links agree with the live Xray Reality runtime. spiderX is a
// server-side auth path, not a per-client derivation seed.
export function deriveSpiderX(seed: string, _clientKey: string): string {
  if (seed) return seed;
  return '/';
}
