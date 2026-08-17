import { useQuery, useQueryClient } from '@tanstack/react-query';

import { keys } from '@/api/queryKeys';
import { i18n } from '@/i18n/react';
import {
  ManagedEndpointCapabilitiesSchema,
  ManagedEndpointListSchema,
  ManagedEndpointViewSchema,
  ManagedInstallPlanListSchema,
  type ManagedEndpoint,
  type ManagedEndpointCapabilities,
  type ManagedInstallPlan,
} from '@/schemas/api/managed-endpoint';
import { HttpUtil } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';

async function fetchManagedEndpoints(): Promise<ManagedEndpoint[]> {
  const msg = await HttpUtil.get('/panel/api/managed-endpoints/list', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || i18n.t('managedProtocols.fetchEndpointsFailed'));
  const validated = parseMsg(msg, ManagedEndpointListSchema, 'managed-endpoints/list');
  return Array.isArray(validated.obj) ? validated.obj : [];
}

async function fetchManagedEndpoint(id: string): Promise<ManagedEndpoint | null> {
  const msg = await HttpUtil.get(`/panel/api/managed-endpoints/${encodeURIComponent(id)}`, undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || i18n.t('managedProtocols.fetchEndpointFailed'));
  return parseMsg(msg, ManagedEndpointViewSchema, `managed-endpoints/${id}`).obj;
}

async function fetchManagedEndpointCapabilities(): Promise<ManagedEndpointCapabilities | null> {
  const msg = await HttpUtil.get('/panel/api/managed-endpoints/capabilities', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || i18n.t('managedProtocols.fetchCapabilitiesFailed'));
  return parseMsg(msg, ManagedEndpointCapabilitiesSchema, 'managed-endpoints/capabilities').obj;
}

async function fetchManagedInstallPlans(): Promise<ManagedInstallPlan[]> {
  const msg = await HttpUtil.get('/panel/api/managed-endpoints/install-plan', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || i18n.t('managedProtocols.fetchCapabilitiesFailed'));
  const validated = parseMsg(msg, ManagedInstallPlanListSchema, 'managed-endpoints/install-plan');
  return Array.isArray(validated.obj) ? validated.obj : [];
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

export function useManagedInstallPlansQuery() {
  return useQuery({
    queryKey: keys.managedEndpoints.installPlans(),
    queryFn: fetchManagedInstallPlans,
    staleTime: 60_000,
  });
}

export type { ManagedInstallPlan };

export interface ManagedEndpointClient {
  id: string;
  clientId?: number;
  subId?: string;
  email?: string;
  username?: string;
  publicIdentity?: string;
  address?: string;
  enable: boolean;
  enabled?: boolean;
  status?: string;
  trafficUp?: number;
  trafficDown?: number;
  latestHandshake?: number;
  lastOnline?: number;
  traffic?: { up?: number; down?: number; supported?: boolean };
  subscriptions?: Partial<Record<'raw' | 'json' | 'clash', string>>;
  hasCredential?: boolean;
}

export interface ManagedClientPayload {
  subId?: string;
  email?: string;
  username?: string;
  address?: string;
  enable?: boolean;
  quotas?: { days?: number; megabytes?: number }[];
  allowPrivateIP?: boolean;
  allowLoopbackIP?: boolean;
}

export interface ManagedExportResponse {
  content?: string;
  filename?: string;
  subscriptions?: Partial<Record<'raw' | 'json' | 'clash', string>>;
}

export function useManagedEndpointActions() {
  const queryClient = useQueryClient();
  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: keys.managedEndpoints.root() });
  };
  return {
    create: async (payload: unknown) => {
      const msg = await HttpUtil.post('/panel/api/managed-endpoints/create', payload);
      if (msg?.success) await invalidate();
      return msg;
    },
    patch: async (id: string, payload: unknown) => {
      const msg = await HttpUtil.patch(`/panel/api/managed-endpoints/${id}`, payload);
      if (msg?.success) await invalidate();
      return msg;
    },
    deleteEndpoint: async (id: string) => {
      const msg = await HttpUtil.delete(`/panel/api/managed-endpoints/${id}`);
      if (msg?.success) await invalidate();
      return msg;
    },
    endpointAction: async (id: string, action: string) => {
      const msg = await HttpUtil.post(`/panel/api/managed-endpoints/${id}/actions/${action}`);
      if (msg?.success) await invalidate();
      return msg;
    },
    clients: async (id: string): Promise<ManagedEndpointClient[]> => {
      const msg = await HttpUtil.get<ManagedEndpointClient[]>(`/panel/api/managed-endpoints/${id}/clients`, undefined, { silent: true });
      if (!msg?.success) throw new Error(msg?.msg || i18n.t('managedProtocols.loadClientsFailed'));
      return Array.isArray(msg.obj) ? msg.obj : [];
    },
    createClient: async (id: string, payload: ManagedClientPayload) => {
      const msg = await HttpUtil.post(`/panel/api/managed-endpoints/${id}/clients`, payload);
      if (msg?.success) await invalidate();
      return msg;
    },
    patchClient: async (id: string, clientId: string, payload: ManagedClientPayload) => {
      const msg = await HttpUtil.patch(`/panel/api/managed-endpoints/${id}/clients/${clientId}`, payload);
      if (msg?.success) await invalidate();
      return msg;
    },
    deleteClient: async (id: string, clientId: string) => {
      const msg = await HttpUtil.delete(`/panel/api/managed-endpoints/${id}/clients/${clientId}`);
      if (msg?.success) await invalidate();
      return msg;
    },
    clientAction: async (id: string, clientId: string, action: 'enable' | 'disable' | 'status') => {
      const msg = await HttpUtil.post(`/panel/api/managed-endpoints/${id}/clients/${clientId}/actions/${action}`);
      if (msg?.success) await invalidate();
      return msg;
    },
    exportClient: async (id: string, clientId: string): Promise<ManagedExportResponse | null> => {
      const msg = await HttpUtil.get<ManagedExportResponse>(`/panel/api/managed-endpoints/${id}/clients/${clientId}/export`, undefined, { silentSuccess: true });
      if (!msg?.success) throw new Error(msg?.msg || i18n.t('managedProtocols.exportFailed'));
      return (msg.obj ?? null) as ManagedExportResponse | null;
    },
  };
}
