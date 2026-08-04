import { useQuery } from '@tanstack/react-query';

import { keys } from '@/api/queryKeys';
import {
  ManagedEndpointCapabilitiesSchema,
  ManagedEndpointListSchema,
  ManagedEndpointViewSchema,
  type ManagedEndpoint,
  type ManagedEndpointCapabilities,
} from '@/schemas/api/managed-endpoint';
import { HttpUtil } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';

async function fetchManagedEndpoints(): Promise<ManagedEndpoint[]> {
  const msg = await HttpUtil.get('/panel/api/managed-endpoints/list', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch managed endpoints');
  const validated = parseMsg(msg, ManagedEndpointListSchema, 'managed-endpoints/list');
  return Array.isArray(validated.obj) ? validated.obj : [];
}

async function fetchManagedEndpoint(id: string): Promise<ManagedEndpoint | null> {
  const msg = await HttpUtil.get(`/panel/api/managed-endpoints/${encodeURIComponent(id)}`, undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch managed endpoint');
  return parseMsg(msg, ManagedEndpointViewSchema, `managed-endpoints/${id}`).obj;
}

async function fetchManagedEndpointCapabilities(): Promise<ManagedEndpointCapabilities | null> {
  const msg = await HttpUtil.get('/panel/api/managed-endpoints/capabilities', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch managed endpoint capabilities');
  return parseMsg(msg, ManagedEndpointCapabilitiesSchema, 'managed-endpoints/capabilities').obj;
}

export function useManagedEndpointsQuery() {
  return useQuery({
    queryKey: keys.managedEndpoints.list(),
    queryFn: fetchManagedEndpoints,
  });
}

export function useManagedEndpointQuery(id: string, enabled = true) {
  return useQuery({
    queryKey: keys.managedEndpoints.detail(id),
    queryFn: () => fetchManagedEndpoint(id),
    enabled: enabled && id.length > 0,
  });
}

export function useManagedEndpointCapabilitiesQuery() {
  return useQuery({
    queryKey: keys.managedEndpoints.capabilities(),
    queryFn: fetchManagedEndpointCapabilities,
    staleTime: Infinity,
  });
}
