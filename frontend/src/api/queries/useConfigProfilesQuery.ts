import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';

import { keys } from '@/api/queryKeys';
import { ConfigProfileListSchema, type ConfigProfile } from '@/schemas/api/config-profile';
import { HttpUtil } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';

export type { ConfigProfile };

async function fetchConfigProfiles(): Promise<ConfigProfile[]> {
  const msg = await HttpUtil.get('/panel/api/profiles/list', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch profiles');
  const validated = parseMsg(msg, ConfigProfileListSchema, 'profiles/list');
  return Array.isArray(validated.obj) ? validated.obj : [];
}

export function useConfigProfilesQuery() {
  const query = useQuery({
    queryKey: keys.profiles.list(),
    queryFn: fetchConfigProfiles,
  });

  const profiles = useMemo(() => query.data ?? [], [query.data]);

  return {
    profiles,
    loading: query.isFetching,
    fetched: query.data !== undefined || query.isError,
    fetchError: query.error ? (query.error as Error).message : '',
    refetch: query.refetch,
  };
}
