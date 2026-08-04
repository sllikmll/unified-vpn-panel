import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';

import { HttpUtil } from '@/utils';
import { Status } from '@/models/status';
import { useOverviewHistory } from '@/pages/index/useOverviewHistory';

beforeEach(() => {
  vi.mocked(HttpUtil.get).mockClear();
});

test('remote history does not seed from local server history endpoint', async () => {
  const status = new Status({ cpu: 10, mem: { current: 1, total: 2 } });

  const { result } = renderHook(() => useOverviewHistory(status, true, {
    targetKey: 'node:edge',
    seedLocal: false,
  }));

  await waitFor(() => expect(result.current.series.cpu).toEqual([10]));
  expect(HttpUtil.get).not.toHaveBeenCalledWith(
    expect.stringContaining('/panel/api/server/history/'),
    expect.anything(),
    expect.anything(),
  );
});

test('switching dashboard target resets and isolates rolling series', async () => {
  const first = new Status({ cpu: 10 });
  const second = new Status({ cpu: 90 });

  const { result, rerender } = renderHook(
    ({ status, keyName }) => useOverviewHistory(status, true, {
      targetKey: keyName,
      seedLocal: false,
    }),
    { initialProps: { status: first, keyName: 'node:first' } },
  );

  await waitFor(() => expect(result.current.series.cpu).toEqual([10]));

  await act(async () => {
    rerender({ status: second, keyName: 'node:second' });
  });

  await waitFor(() => expect(result.current.series.cpu).toEqual([90]));
});
