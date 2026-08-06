import { describe, expect, it } from 'vitest';

import { deriveSpiderX } from '@/lib/xray/spider-x';

// Cross-language vectors shared with TestDeriveSpiderXMatchesFrontendVectors
// in internal/sub/service_sharelink_test.go: subscription links come from Go,
// panel links from this module, and the two must agree byte-for-byte.
describe('deriveSpiderX', () => {
  it('matches the Go deriveSpiderX vectors', () => {
    expect(deriveSpiderX('/seed', 'subAlice')).toBe('/seed');
    expect(deriveSpiderX('/', '')).toBe('/');
    expect(deriveSpiderX('', 'subAlice')).toBe('/');
  });

  it('returns the literal server-side spiderX for every client', () => {
    expect(deriveSpiderX('/seed', 'subAlice')).toBe(deriveSpiderX('/seed', 'subBob'));
    expect(deriveSpiderX('/seedA', 'subAlice')).toBe('/seedA');
    expect(deriveSpiderX('/seedB', 'subAlice')).toBe('/seedB');
  });

  it('falls back to / when spiderX is empty', () => {
    expect(deriveSpiderX('', '')).toBe('/');
  });
});
