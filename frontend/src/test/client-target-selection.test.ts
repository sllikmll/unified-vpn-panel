import { describe, expect, it } from 'vitest';

import { normalizeClientTargetSelection } from '@/pages/clients/client-targets';

describe('normalizeClientTargetSelection', () => {
  it('switches atomically from legacy to managed targets', () => {
    expect(normalizeClientTargetSelection(
      ['legacy:1', 'legacy:2', 'managed:9'],
      ['legacy:1', 'legacy:2'],
    )).toEqual(['managed:9']);
  });

  it('switches atomically from managed to legacy targets', () => {
    expect(normalizeClientTargetSelection(
      ['managed:9', 'legacy:1'],
      ['managed:9'],
    )).toEqual(['legacy:1']);
  });

  it('preserves ordinary removals', () => {
    expect(normalizeClientTargetSelection(['managed:9'], ['managed:9', 'managed:10']))
      .toEqual(['managed:9']);
  });
});
