import { describe, expect, it, vi, beforeEach, afterEach, beforeAll } from 'vitest';
import { act, fireEvent, screen, waitFor } from '@testing-library/react';

import InboundFormModal from '@/pages/inbounds/form/InboundFormModal';
import { managedFormPayload } from '@/pages/inbounds/form/ManagedInboundForm';
import * as inboundAdapter from '@/lib/xray/inbound-form-adapter';
import { HttpUtil } from '@/utils';
import type { NodeRecord } from '@/api/queries/useNodesQuery';
import { managedNodeBlockReason, nodeSupportsManagedProtocol } from '@/lib/managed-protocols';
import { readyI18n } from '@/i18n/react';
import {
  chooseSelectOption,
  fieldLabels,
  listSelectOptions,
  renderWithProviders,
} from './test-utils';

const nodes = [
  { id: 1, name: 'edge-a', enable: true, status: 'online', capabilities: JSON.stringify({ managedProtocols: ['amneziawg', 'mieru'] }) },
  { id: 2, name: 'edge-b', enable: true, status: 'online', capabilities: JSON.stringify({ managedProtocols: ['naiveproxy'] }) },
] as unknown as NodeRecord[];

beforeAll(async () => {
  await readyI18n();
});

function renderModal() {
  return renderWithProviders(
    <InboundFormModal
      open
      mode="add"
      dbInbound={null}
      dbInbounds={[]}
      availableNodes={nodes}
      onClose={() => {}}
      onSaved={() => {}}
    />,
  );
}

async function flush() {
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
}

describe('managed native protocol inbound form branch', () => {
  beforeEach(() => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue({
      success: true,
      msg: '',
      obj: {
        runtimeKinds: [
          { runtimeKind: 'amneziawg', protocols: ['amneziawg'], serverLifecycle: true, clientCrud: true, detect: true, nativeExport: ['conf'], subscription: [], traffic: false, firewallPolicy: false },
          { runtimeKind: 'mieru', protocols: ['mieru'], serverLifecycle: true, clientCrud: true, detect: true, nativeExport: ['url'], subscription: [], traffic: false, firewallPolicy: false },
          { runtimeKind: 'naiveproxy', protocols: ['naiveproxy'], serverLifecycle: true, clientCrud: true, detect: true, nativeExport: ['url'], subscription: [], traffic: false, firewallPolicy: false },
        ],
      },
    } as never);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows managed protocols in the protocol selector and switches away from Xray-only tabs', async () => {
    renderModal();

    await waitFor(() => expect(listSelectOptions('protocol')).toEqual(expect.arrayContaining([
      'AmneziaWG 2.0 (managed)',
      'Mieru (managed)',
      'NaiveProxy (managed)',
    ])));

    chooseSelectOption('protocol', 'AmneziaWG 2.0 (managed)');
    await flush();

    expect(screen.getByText('Managed native protocol')).toBeTruthy();
    expect(fieldLabels()).toEqual(expect.arrayContaining(['Interface', 'IPv4 pool', 'Client allowed IPs', 'Jc', 'H4']));
    expect(fieldLabels()).not.toContain('Transmission');
    expect(fieldLabels()).not.toContain('Sniffing');
  }, 15_000);

  it('blocks unsupported selected nodes with a visible capability reason', async () => {
    renderModal();

    chooseSelectOption('protocol', 'NaiveProxy (managed)');
    await flush();
    chooseSelectOption('nodeId', 'edge-a');
    await flush();

    expect(screen.getByText(/capability unknown\/unavailable/i)).toBeTruthy();
    expect(screen.getByRole('button', { name: /Create|Add|create/i }).getAttribute('disabled')).not.toBeNull();
  });

  it('posts managed create payload without using the Xray form serializer', async () => {
    const serializeSpy = vi.spyOn(inboundAdapter, 'formValuesToWirePayload');
    const postSpy = vi.spyOn(HttpUtil, 'post').mockResolvedValue({ success: true, msg: '', obj: { id: 'managed-1' } } as never);
    renderModal();

    chooseSelectOption('protocol', 'Mieru (managed)');
    await flush();
    fireEvent.change(screen.getByLabelText('Tag'), { target: { value: 'mieru-9443' } });
    fireEvent.change(screen.getByLabelText('Listen IPv4'), { target: { value: '127.0.0.1' } });
    fireEvent.change(screen.getByLabelText('Port'), { target: { value: '9443' } });
    fireEvent.click(screen.getByRole('button', { name: /Create|Add|create/i }));

    await waitFor(() => expect(postSpy).toHaveBeenCalledWith('/panel/api/managed-endpoints/create', expect.objectContaining({
      runtimeKind: 'mieru',
      protocol: 'mieru',
      tag: 'mieru-9443',
      listen: '127.0.0.1',
      port: 9443,
      config: expect.objectContaining({
        portBindings: expect.arrayContaining([expect.objectContaining({ protocol: 'TCP', port: 9443 })]),
      }),
    })));
    expect(serializeSpy).not.toHaveBeenCalled();
  });

  it('validates AWG obfuscation bounds before submit', async () => {
    const postSpy = vi.spyOn(HttpUtil, 'post').mockResolvedValue({ success: true, msg: '', obj: {} } as never);
    renderModal();

    chooseSelectOption('protocol', 'AmneziaWG 2.0 (managed)');
    await flush();
    fireEvent.change(screen.getByLabelText('Tag'), { target: { value: 'awg-51820' } });
    fireEvent.change(screen.getByLabelText('H1'), { target: { value: 'bad/path' } });
    fireEvent.click(screen.getByRole('button', { name: /Create|Add|create/i }));

    await waitFor(() => expect(screen.getByText(/H value must be/i)).toBeTruthy());
    expect(postSpy).not.toHaveBeenCalled();
  });
});

describe('managed edit payload sanitization', () => {
  it('preserves editable AWG config and strips secret/raw fields', () => {
    const payload = managedFormPayload({
      protocol: 'amneziawg',
      runtimeKind: 'amneziawg',
      tag: 'awg',
      remark: '',
      enable: true,
      listen: '',
      port: 51820,
      nodeId: null,
      config: {
        interfaceName: 'awg0',
        listenPort: 51820,
        mtu: 1420,
        ipv4Address: '10.66.66.1/24',
        ipv4Pool: '10.66.66.0/24',
        dns: '1.1.1.1',
        clientAllowedIPs: '0.0.0.0/0',
        persistentKeepalive: 25,
        jc: 7,
        jmin: 40,
        jmax: 120,
        s1: 80,
        s2: 149,
        s3: 24,
        s4: 12,
        h1: '100-200',
        h2: '300-400',
        h3: '500-600',
        h4: '700-800',
        privateKey: 'redacted',
        rawPath: '/secret',
      } as never,
    });

    expect(payload.config).toMatchObject({ jc: 7, h4: '700-800' });
    expect(payload.config).not.toHaveProperty('privateKey');
    expect(payload.config).not.toHaveProperty('rawPath');
  });

  it('preserves editable Mieru config and strips nested secret/raw fields', () => {
    const payload = managedFormPayload({
      protocol: 'mieru',
      runtimeKind: 'mieru',
      tag: 'mieru',
      remark: '',
      enable: true,
      listen: '',
      port: 2999,
      nodeId: null,
      config: {
        portBindings: [{ protocol: 'UDP', portRange: '4000-4010', password: 'redacted', rawConfig: '{}' }],
        mtu: 1400,
      } as never,
    });

    expect(payload.config).toMatchObject({ portBindings: [{ protocol: 'UDP', portRange: '4000-4010' }], mtu: 1400 });
    expect(JSON.stringify(payload.config)).not.toContain('redacted');
    expect(JSON.stringify(payload.config)).not.toContain('rawConfig');
  });

  it('preserves editable NaiveProxy config and strips secret/raw fields', () => {
    const payload = managedFormPayload({
      protocol: 'naiveproxy',
      runtimeKind: 'naiveproxy',
      tag: 'naive',
      remark: '',
      enable: true,
      listen: '0.0.0.0',
      port: 443,
      nodeId: null,
      config: {
        domain: 'example.com',
        sni: 'example.com',
        listenIP: '0.0.0.0',
        port: 443,
        tlsMode: 'managed',
        acmeEmail: '',
        secret: 'redacted',
        raw: 'redacted',
      } as never,
    });

    expect(payload.config).toMatchObject({ domain: 'example.com', tlsMode: 'managed' });
    expect(payload.config).not.toHaveProperty('secret');
    expect(payload.config).not.toHaveProperty('raw');
  });
});

describe('managed remote node capability gate', () => {
  it('allows local panel from local capabilities', () => {
    expect(nodeSupportsManagedProtocol(undefined, 'amneziawg')).toBe(true);
    expect(managedNodeBlockReason(undefined, 'amneziawg')).toBe('');
  });

  it('blocks unknown remote capabilities', () => {
    const node = { id: 3, name: 'edge-empty', enable: true, capabilities: JSON.stringify({ managedProtocols: [] }) } as unknown as NodeRecord;
    expect(nodeSupportsManagedProtocol(node, 'amneziawg')).toBe(false);
    expect(managedNodeBlockReason(node, 'amneziawg')).toBe('capability unknown/unavailable');
  });

  it('blocks malformed remote capabilities', () => {
    const node = { id: 4, name: 'edge-bad', enable: true, capabilities: '{bad-json' } as unknown as NodeRecord;
    expect(nodeSupportsManagedProtocol(node, 'mieru')).toBe(false);
    expect(managedNodeBlockReason(node, 'mieru')).toBe('capability unknown/unavailable');
  });

  it('blocks unsupported remote capabilities and allows supported ones', () => {
    const node = { id: 5, name: 'edge-some', enable: true, capabilities: JSON.stringify({ managedProtocols: ['mieru'] }) } as unknown as NodeRecord;
    expect(nodeSupportsManagedProtocol(node, 'amneziawg')).toBe(false);
    expect(managedNodeBlockReason(node, 'amneziawg')).toBe('capability unknown/unavailable');
    expect(nodeSupportsManagedProtocol(node, 'mieru')).toBe(true);
    expect(managedNodeBlockReason(node, 'mieru')).toBe('');
  });
});
