import { useMemo } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { keys } from '@/api/queryKeys';
import type { ProtocolConnection } from '@/generated/types';
import { HttpUtil } from '@/utils';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } };

export interface ProtocolSpec {
  id: string;
  label: string;
  schemes: string[];
  mihomoSupported: boolean;
}

export interface ProtocolConnectionView extends ProtocolConnection {
  hasRaw: boolean;
  mihomoSupported: boolean;
  protocolLabel: string;
  usedBySelectors: string[];
}

interface ListResponse {
  connections: ProtocolConnectionView[];
  count: number;
  protocols: ProtocolSpec[];
}

export interface ImportProtocolConnectionPayload {
  protocol: string;
  name: string;
  content: string;
  selectors: string[];
}

export interface UpdateProtocolConnectionPayload {
  name?: string;
  enabled?: boolean;
  selectors?: string[];
}

async function fetchProtocolConnections(): Promise<ListResponse> {
  const msg = await HttpUtil.get<ListResponse>('/panel/api/proxy-connections', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch protocol connections');
  return msg.obj ?? { connections: [], count: 0, protocols: [] };
}

export function useProtocolConnectionsQuery() {
  const query = useQuery({
    queryKey: keys.protocolConnections.list(),
    queryFn: fetchProtocolConnections,
  });

  const data = query.data ?? { connections: [], count: 0, protocols: [] };
  return {
    connections: useMemo(() => data.connections, [data.connections]),
    protocols: useMemo(() => data.protocols, [data.protocols]),
    count: data.count,
    loading: query.isFetching,
    fetchError: query.error ? (query.error as Error).message : '',
    refetch: query.refetch,
  };
}

export function useProtocolConnectionMutations() {
  const queryClient = useQueryClient();
  const invalidate = () => queryClient.invalidateQueries({ queryKey: keys.protocolConnections.root() });

  const importMut = useMutation({
    mutationFn: (payload: ImportProtocolConnectionPayload) =>
      HttpUtil.post('/panel/api/proxy-connections/import', payload, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const updateMut = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: UpdateProtocolConnectionPayload }) =>
      HttpUtil.patch(`/panel/api/proxy-connections/${encodeURIComponent(id)}`, payload, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const deleteMut = useMutation({
    mutationFn: (id: string) =>
      HttpUtil.delete(`/panel/api/proxy-connections/${encodeURIComponent(id)}`),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const previewMut = useMutation({
    mutationFn: () => HttpUtil.post<{ block: string; configPreview: string }>('/panel/api/proxy-connections/preview', {}, JSON_HEADERS),
  });

  return {
    importConnection: (payload: ImportProtocolConnectionPayload) => importMut.mutateAsync(payload),
    update: (id: string, payload: UpdateProtocolConnectionPayload) => updateMut.mutateAsync({ id, payload }),
    remove: (id: string) => deleteMut.mutateAsync(id),
    preview: () => previewMut.mutateAsync(),
  };
}
