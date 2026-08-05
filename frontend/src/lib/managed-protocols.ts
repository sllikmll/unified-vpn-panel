import { z } from 'zod';

import { RandomUtil } from '@/utils';
import type { NodeRecord } from '@/api/queries/useNodesQuery';

export const MANAGED_PROTOCOLS = ['amneziawg', 'mieru', 'naiveproxy'] as const;
export type ManagedNativeProtocol = typeof MANAGED_PROTOCOLS[number];
export type ManagedRuntimeKind = ManagedNativeProtocol;

export const MANAGED_PROTOCOL_LABELS: Record<ManagedNativeProtocol, string> = {
  amneziawg: 'AmneziaWG 2.0',
  mieru: 'Mieru',
  naiveproxy: 'NaiveProxy',
};

const ipv4Cidr = z.string().trim().regex(/^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/, 'IPv4 CIDR is required');
const ipv4Csv = z.string().trim().min(1, 'IPv4 value is required').refine((value) => !value.includes(':'), 'IPv6 is not supported here');
const hValue = z.string().trim().refine((value) => {
  if (!value) return true;
  if (/^\d+$/.test(value)) return true;
  return /^\d+\s*-\s*\d+$/.test(value);
}, 'H value must be an integer or low-high range');
const domain = z.string().trim().min(1).regex(/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+\.?$/i, 'DNS domain is required');

export const ManagedBaseSchema = z.object({
  protocol: z.enum(MANAGED_PROTOCOLS),
  runtimeKind: z.enum(MANAGED_PROTOCOLS),
  tag: z.string().trim().min(1, 'Tag is required'),
  remark: z.string().optional().default(''),
  enable: z.boolean(),
  listen: z.string().trim().refine((value) => value === '' || (/^(\d{1,3}\.){3}\d{1,3}$/.test(value) && !value.includes(':')), 'Listen must be IPv4 or blank'),
  port: z.number().int().min(1).max(65535),
  nodeId: z.number().int().nullable().optional(),
});

export const AwgConfigSchema = z.object({
  interfaceName: z.literal('awg0'),
  listenPort: z.number().int().min(1).max(65535),
  mtu: z.number().int().min(576).max(9000),
  ipv4Address: ipv4Cidr,
  ipv4Pool: ipv4Cidr,
  dns: ipv4Csv,
  clientAllowedIPs: ipv4Csv,
  persistentKeepalive: z.number().int().min(0).max(65535),
  jc: z.number().int().min(1).max(128),
  jmin: z.number().int().min(0),
  jmax: z.number().int().min(0),
  s1: z.number().int().min(0).max(1500),
  s2: z.number().int().min(0).max(1500),
  s3: z.number().int().min(0).max(64),
  s4: z.number().int().min(0).max(32),
  h1: hValue,
  h2: hValue,
  h3: hValue,
  h4: hValue,
}).superRefine((value, ctx) => {
  if (value.jmin > value.jmax) {
    ctx.addIssue({ code: 'custom', path: ['jmax'], message: 'Jmax must be greater than or equal to Jmin' });
  }
  if (value.s1 + 56 === value.s2) {
    ctx.addIssue({ code: 'custom', path: ['s2'], message: 'S2 must not equal S1 + 56' });
  }
});

export const MieruConfigSchema = z.object({
  portBindings: z.array(z.object({
    protocol: z.enum(['TCP', 'UDP']),
    port: z.number().int().min(1).max(65535).optional(),
    portRange: z.string().trim().regex(/^\d+-\d+$/, 'Use begin-end').optional(),
  })).min(1),
  mtu: z.number().int().min(1280).max(1500).optional(),
}).superRefine((value, ctx) => {
  value.portBindings.forEach((binding, index) => {
    if (!binding.port && !binding.portRange) {
      ctx.addIssue({ code: 'custom', path: ['portBindings', index, 'port'], message: 'Port or range is required' });
    }
    if (binding.port && binding.portRange) {
      ctx.addIssue({ code: 'custom', path: ['portBindings', index, 'portRange'], message: 'Use either port or range' });
    }
  });
});

export const NaiveConfigSchema = z.object({
  domain,
  sni: domain.optional().or(z.literal('')),
  listenIP: z.string().trim().regex(/^(\d{1,3}\.){3}\d{1,3}$/, 'Listen IP must be IPv4'),
  port: z.number().int().min(1).max(65535),
  tlsMode: z.literal('acme'),
  acmeEmail: z.string().trim().email().optional().or(z.literal('')),
});

export const ManagedFormSchema = ManagedBaseSchema.and(z.discriminatedUnion('protocol', [
  z.object({ protocol: z.literal('amneziawg'), config: AwgConfigSchema }),
  z.object({ protocol: z.literal('mieru'), config: MieruConfigSchema }),
  z.object({ protocol: z.literal('naiveproxy'), config: NaiveConfigSchema }),
]));

export type ManagedFormValues = z.infer<typeof ManagedFormSchema>;

function randomInt(min: number, max: number): number {
  const range = max - min + 1;
  const cryptoApi = globalThis.crypto;
  if (cryptoApi?.getRandomValues) {
    const values = new Uint32Array(1);
    cryptoApi.getRandomValues(values);
    return min + (values[0] % range);
  }
  return min + Math.floor(Math.random() * range);
}

function generatedHRanges(): Pick<z.infer<typeof AwgConfigSchema>, 'h1' | 'h2' | 'h3' | 'h4'> {
  const ranges: string[] = [];
  let cursor = randomInt(100_000, 800_000);
  for (let i = 0; i < 4; i += 1) {
    const width = randomInt(4096, 65535);
    const gap = randomInt(8192, 131072);
    const start = cursor + gap;
    const end = start + width;
    ranges.push(`${start}-${end}`);
    cursor = end;
  }
  return { h1: ranges[0], h2: ranges[1], h3: ranges[2], h4: ranges[3] };
}

export function defaultManagedConfig(protocol: ManagedNativeProtocol, port: number): ManagedFormValues['config'] {
  if (protocol === 'amneziawg') {
    return {
      interfaceName: 'awg0',
      listenPort: port || 51820,
      mtu: 1420,
      ipv4Address: '10.66.66.1/24',
      ipv4Pool: '10.66.66.0/24',
      dns: '1.1.1.1',
      clientAllowedIPs: '0.0.0.0/0',
      persistentKeepalive: 25,
      jc: randomInt(3, 10),
      jmin: 40,
      jmax: 120,
      s1: 80,
      s2: 149,
      s3: 24,
      s4: 12,
      ...generatedHRanges(),
    };
  }
  if (protocol === 'mieru') {
    return {
      portBindings: [{ protocol: 'TCP', port: port || 2999 }],
      mtu: 1400,
    };
  }
  return {
    domain: '',
    sni: '',
    listenIP: '0.0.0.0',
    port: port || 443,
    tlsMode: 'acme',
    acmeEmail: '',
  };
}

export function buildManagedDefaults(protocol: ManagedNativeProtocol = 'amneziawg'): ManagedFormValues {
  const port = protocol === 'naiveproxy' ? 443 : protocol === 'mieru' ? 2999 : 51820;
  return {
    protocol,
    runtimeKind: protocol,
    tag: `${protocol}-${RandomUtil.randomLowerAndNum(5)}`,
    remark: '',
    enable: true,
    listen: protocol === 'naiveproxy' ? '0.0.0.0' : '',
    port,
    nodeId: null,
    config: defaultManagedConfig(protocol, port),
  } as ManagedFormValues;
}

export function isManagedNativeProtocol(value: string): value is ManagedNativeProtocol {
  return (MANAGED_PROTOCOLS as readonly string[]).includes(value);
}

export function parseNodeManagedProtocols(node: NodeRecord | undefined): string[] {
  if (!node) return [];
  const record = node as unknown as { runtimeCapabilities?: unknown; managedProtocols?: unknown; capabilities?: unknown };
  if (Array.isArray(record.runtimeCapabilities)) return record.runtimeCapabilities.map(String);
  const raw = record.managedProtocols;
  if (Array.isArray(raw)) return raw.map(String);
  const caps = record.capabilities;
  if (typeof caps === 'string' && caps.trim()) {
    try {
      const parsed = JSON.parse(caps) as { managedProtocols?: unknown; protocols?: unknown };
      if (Array.isArray(parsed.managedProtocols)) return parsed.managedProtocols.map(String);
      if (Array.isArray(parsed.protocols)) return parsed.protocols.map(String);
    } catch {
      return [];
    }
  }
  if (caps && typeof caps === 'object') {
    const parsed = caps as { managedProtocols?: unknown; protocols?: unknown };
    if (Array.isArray(parsed.managedProtocols)) return parsed.managedProtocols.map(String);
    if (Array.isArray(parsed.protocols)) return parsed.protocols.map(String);
  }
  return [];
}

export function nodeSupportsManagedProtocol(node: NodeRecord | undefined, protocol: ManagedNativeProtocol): boolean {
  if (!node) return true;
  const protocols = parseNodeManagedProtocols(node);
  return protocols.length > 0 && protocols.includes(protocol);
}

export function managedNodeBlockReason(node: NodeRecord | undefined, protocol: ManagedNativeProtocol): string {
  if (!node) return '';
  const protocols = parseNodeManagedProtocols(node);
  if (protocols.length === 0) return 'capability unknown/unavailable';
  if (protocols.includes(protocol)) return '';
  return 'capability unknown/unavailable';
}
