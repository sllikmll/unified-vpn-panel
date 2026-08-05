import { describe, expect, it } from 'vitest';

import { ManagedEndpointViewSchema as GeneratedManagedEndpointViewSchema } from '@/generated/zod';
import {
  ManagedEndpointCapabilitiesSchema,
  ManagedEndpointListSchema,
} from '@/schemas/api/managed-endpoint';
import { parseNodeManagedProtocols } from '@/lib/managed-protocols';

describe('managed endpoint generated schemas', () => {
	it('reads managed runtimes from the node heartbeat contract', () => {
		const node = { runtimeCapabilities: ['amneziawg', 'mieru', 'naiveproxy'] } as never;
		expect(parseNodeManagedProtocols(node)).toEqual(['amneziawg', 'mieru', 'naiveproxy']);
	});
  it('parses projected Xray, MTProto, and native rows without secret fields', () => {
    const rows = ManagedEndpointListSchema.parse([
      {
        id: 'legacy-xray-1',
        source: 'legacy-inbound',
        inboundId: 1,
        runtimeKind: 'xray',
        protocol: 'vless',
        tag: 'vless-tag',
        remark: 'VLESS',
        listen: '',
        port: 443,
        enable: true,
        status: 'active',
        clientCount: 2,
        traffic: { up: 10, down: 20 },
        health: { status: 'active', message: '', checkedAt: 0 },
        secretSummary: { hasSecrets: false, fields: [] },
      },
      {
        id: 'legacy-mtproto-2',
        source: 'legacy-inbound',
        inboundId: 2,
        runtimeKind: 'mtproto',
        protocol: 'mtproto',
        tag: 'mt-tag',
        remark: 'MTProto',
        listen: '',
        port: 8443,
        enable: true,
        status: 'active',
        clientCount: 1,
        traffic: { up: 0, down: 0 },
        health: { status: 'active', message: '', checkedAt: 0 },
        secretSummary: { hasSecrets: true, fields: ['clients.secret'] },
      },
      {
        id: 'managed-3',
        source: 'managed-endpoint',
        nativeId: 3,
        runtimeKind: 'wireguard',
        protocol: 'wireguard',
        tag: 'wg-tag',
        remark: 'WireGuard',
        listen: '',
        port: 51820,
        enable: true,
        status: 'active',
        clientCount: 1,
        traffic: { up: 5, down: 6 },
        health: { status: 'active', message: '', checkedAt: 0 },
        secretSummary: { hasSecrets: true, fields: ['privateKey'] },
      },
    ]);

    expect(rows.map((row) => row.runtimeKind)).toEqual(['xray', 'mtproto', 'wireguard']);
    for (const row of rows) {
      expect('desiredConfig' in row).toBe(false);
      expect('observedConfig' in row).toBe(false);
    }
  });

  it('parses Phase 0 capabilities with native lifecycle unavailable', () => {
    const caps = ManagedEndpointCapabilitiesSchema.parse({
      runtimeKinds: [
        {
          runtimeKind: 'wireguard',
          protocols: ['wireguard'],
          serverLifecycle: false,
          clientCrud: false,
          traffic: false,
          detect: false,
          nativeExport: [],
          subscription: [],
          firewallPolicy: false,
        },
      ],
    });

    expect(caps.runtimeKinds[0].serverLifecycle).toBe(false);
    expect(caps.runtimeKinds[0].clientCrud).toBe(false);
  });

  it.each([
    'desiredConfig',
    'observedConfig',
    'clientConfig',
    'ciphertext',
    'lastError',
    'error',
    'rollbackToken',
  ])('rejects leaked internal field %s', (field) => {
    const payload = {
      id: 'managed-3',
      source: 'managed-endpoint',
      nativeId: 3,
      runtimeKind: 'wireguard',
      protocol: 'wireguard',
      tag: 'wg-tag',
      remark: 'WireGuard',
      listen: '',
      port: 51820,
      enable: true,
      status: 'active',
      clientCount: 1,
      traffic: { up: 5, down: 6 },
      health: { status: 'active', message: '', checkedAt: 0 },
      secretSummary: { hasSecrets: true, fields: ['privateKey'] },
      [field]: 'must-not-leak',
    };

    expect(() => ManagedEndpointListSchema.parse([payload])).toThrow();
  });

  it('generated schema rejects leaked internal fields directly', () => {
    const payload = {
      id: 'managed-3',
      source: 'managed-endpoint',
      nativeId: 3,
      runtimeKind: 'wireguard',
      protocol: 'wireguard',
      tag: 'wg-tag',
      remark: 'WireGuard',
      listen: '',
      port: 51820,
      enable: true,
      status: 'active',
      clientCount: 1,
      traffic: { up: 5, down: 6 },
      health: { status: 'active', message: '', checkedAt: 0 },
      secretSummary: { hasSecrets: true, fields: ['privateKey'] },
      desiredConfig: 'must-not-leak',
    };

    expect(() => GeneratedManagedEndpointViewSchema.parse(payload)).toThrow();
  });
});
