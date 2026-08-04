import { afterEach, describe, expect, it, vi } from 'vitest';

import { buildProtocolExportURL, fetchProtocolConnectionReveal } from '@/api/queries/useProtocolConnections';
import { HttpUtil, Msg } from '@/utils';

afterEach(() => {
  vi.restoreAllMocks();
});

describe('protocol connection reveal', () => {
	it('builds the export URL under the configured panel base path', () => {
		expect(buildProtocolExportURL('/xui/')).toBe('/xui/panel/api/proxy-connections/export.yaml');
		expect(buildProtocolExportURL('')).toBe('panel/api/proxy-connections/export.yaml');
	});

  it('fetches a single revealed connection on demand with an encoded id', async () => {
    const get = vi.spyOn(HttpUtil, 'get').mockResolvedValue(new Msg(true, '', {
      id: 'wg/private',
      mihomoYaml: '- name: wg\n  private-key: actual-secret\n',
    }));

    const connection = await fetchProtocolConnectionReveal('wg/private');

    expect(get).toHaveBeenCalledWith('/panel/api/proxy-connections/wg%2Fprivate/reveal', undefined, { silent: true });
    expect(connection.mihomoYaml).toContain('actual-secret');
  });

  it('rejects an unsuccessful reveal response', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(new Msg(false, 'reveal denied', null));

    await expect(fetchProtocolConnectionReveal('wg')).rejects.toThrow('reveal denied');
  });
});
