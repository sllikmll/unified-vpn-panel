import { describe, expect, it, vi, beforeEach, afterEach, beforeAll } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { Modal } from 'antd';

import ManagedEndpointsPanel from '@/pages/inbounds/managed/ManagedEndpointsPanel';
import { HttpUtil } from '@/utils';
import { renderWithProviders } from './test-utils';
import { readyI18n } from '@/i18n/react';

describe('ManagedEndpointsPanel', () => {
  beforeAll(async () => {
    await readyI18n();
  });

  beforeEach(() => {
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      if (url.endsWith('/list')) {
        return { success: true, msg: '', obj: [{
          id: 'managed-1',
          source: 'managed-endpoint',
          nativeId: 1,
          runtimeKind: 'amneziawg',
          protocol: 'amneziawg',
          tag: 'awg',
          remark: 'AWG',
          listen: '',
          port: 51820,
          enable: true,
          status: 'degraded',
          clientCount: 1,
          traffic: { up: 0, down: 0 },
          health: { status: 'degraded', message: 'stale', checkedAt: 0 },
          secretSummary: { hasSecrets: true, fields: ['privateKey'] },
          installPlan: { install: { blocked: true, reason: 'missing package manager' } },
        }] } as never;
      }
      if (url.endsWith('/managed-1')) {
        return { success: true, msg: '', obj: {
          id: 'managed-1',
          source: 'managed-endpoint',
          nativeId: 1,
          runtimeKind: 'amneziawg',
          protocol: 'amneziawg',
          tag: 'awg',
          remark: 'AWG',
          listen: '',
          port: 51820,
          enable: true,
          status: 'degraded',
          clientCount: 1,
          traffic: { up: 0, down: 0 },
          health: { status: 'degraded', message: 'stale', checkedAt: 0 },
          secretSummary: { hasSecrets: true, fields: ['privateKey'] },
          config: {
            interfaceName: 'awg0',
            listenPort: 51820,
            mtu: 1420,
            ipv4Address: '10.66.66.1/24',
            ipv4Pool: '10.66.66.0/24',
            dns: '1.1.1.1',
            clientAllowedIPs: '0.0.0.0/0',
            persistentKeepalive: 25,
            jc: 4,
            jmin: 50,
            jmax: 120,
            s1: 84,
            s2: 145,
            s3: 24,
            s4: 12,
            h1: '100-200',
            h2: '300-400',
            h3: '500-600',
            h4: '700-800',
            privateKey: 'SHOULD_NOT_SUBMIT',
            rawPath: '/secret',
          },
        } } as never;
      }
      if (url.endsWith('/clients')) {
        return { success: true, msg: '', obj: [{
          id: 'c1',
          email: 'phone',
          subId: 'sub-phone',
          publicIdentity: 'runtime-phone',
          address: '10.66.66.2/32',
          enable: true,
          status: 'unknown',
          traffic: { supported: false },
          privateKey: 'SHOULD_NOT_RENDER',
          password: 'SHOULD_NOT_RENDER',
        }] } as never;
      }
      if (url.endsWith('/export')) {
        return { success: true, msg: '', obj: {
          content: '[Interface]\\nPrivateKey = exported',
          filename: 'phone.conf',
          subscriptions: { raw: 'https://sub.example/raw' },
        } } as never;
      }
      return { success: true, msg: '', obj: { runtimeKinds: [] } } as never;
    });
  });

  afterEach(() => vi.restoreAllMocks());

  it('renders non-green degraded/unknown states and does not leak list secrets', async () => {
    renderWithProviders(<ManagedEndpointsPanel />);

    expect(await screen.findByText('degraded')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /Clients/i }));
    expect(await screen.findByText('unknown')).toBeTruthy();
    expect(await screen.findByText('sub-phone')).toBeTruthy();
    expect(await screen.findByText('10.66.66.2/32')).toBeTruthy();
    expect(screen.getByText('Traffic unavailable')).toBeTruthy();
    expect(document.body.textContent).not.toContain('SHOULD_NOT_RENDER');
  });

  it('submits subId when creating a managed client', async () => {
    const postSpy = vi.spyOn(HttpUtil, 'post').mockResolvedValue({ success: true, msg: '', obj: {} } as never);
    renderWithProviders(<ManagedEndpointsPanel />);

    fireEvent.click(await screen.findByRole('button', { name: /Clients/i }));
    fireEvent.click(await screen.findByRole('button', { name: /Create client/i }));
    fireEvent.change(screen.getByLabelText('Subscription ID'), { target: { value: 'sub-created' } });
    fireEvent.click(screen.getByRole('button', { name: /^OK$/i }));

    await waitFor(() => expect(postSpy).toHaveBeenCalledWith('/panel/api/managed-endpoints/managed-1/clients', expect.objectContaining({
      subId: 'sub-created',
      enable: true,
    })));
  });

  it('runs actions only after confirmation and waits for server response', async () => {
    const postSpy = vi.spyOn(HttpUtil, 'post').mockResolvedValue({ success: true, msg: '', obj: {} } as never);
    let confirmConfig: { onOk?: () => Promise<void> } | undefined;
    vi.spyOn(Modal, 'confirm').mockImplementation((config) => {
      confirmConfig = config as { onOk?: () => Promise<void> };
      return { destroy: vi.fn(), update: vi.fn() } as never;
    });
    renderWithProviders(<ManagedEndpointsPanel />);

    fireEvent.click(await screen.findByRole('button', { name: /Restart/i }));
    expect(postSpy).not.toHaveBeenCalled();
    await confirmConfig?.onOk?.();
    await waitFor(() => expect(postSpy).toHaveBeenCalledWith('/panel/api/managed-endpoints/managed-1/actions/restart'));
  });

  it('shows subscription URLs only when export API returns them', async () => {
    renderWithProviders(<ManagedEndpointsPanel />);

    fireEvent.click(await screen.findByRole('button', { name: /Clients/i }));
    fireEvent.click(await screen.findByRole('button', { name: /Export/i }));
    expect(await screen.findByDisplayValue('https://sub.example/raw')).toBeTruthy();
    expect(screen.getByText('JSON subscription unavailable')).toBeTruthy();
    expect(screen.getByText('Clash subscription unavailable')).toBeTruthy();
    expect(document.querySelector('.managed-qr-wrap canvas, .managed-qr-wrap svg')).toBeTruthy();
    expect(document.querySelector('.managed-qr-wrap')?.textContent).not.toContain('awg://server-returned');
  }, 15_000);

  it('edits full endpoint config from GET detail without submitting secret/raw fields', async () => {
    const patchSpy = vi.spyOn(HttpUtil, 'patch').mockResolvedValue({ success: true, msg: '', obj: {} } as never);
    renderWithProviders(<ManagedEndpointsPanel />);

    fireEvent.click(await screen.findByRole('button', { name: /^Edit$/i }));
    expect(await screen.findByDisplayValue('10.66.66.0/24')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('IPv4 pool'), { target: { value: '10.77.77.0/24' } });
    fireEvent.click(screen.getByRole('button', { name: /^Save$/i }));

    await waitFor(() => expect(patchSpy).toHaveBeenCalledWith('/panel/api/managed-endpoints/managed-1', expect.objectContaining({
      config: expect.objectContaining({ ipv4Pool: '10.77.77.0/24' }),
    })));
    const submitted = patchSpy.mock.calls[0][1] as { config: Record<string, unknown> };
    expect(submitted.config.privateKey).toBeUndefined();
    expect(submitted.config.rawPath).toBeUndefined();
  });

  it('deletes endpoint through endpoint DELETE, not runtime uninstall', async () => {
    const deleteSpy = vi.spyOn(HttpUtil, 'delete').mockResolvedValue({ success: true, msg: '', obj: {} } as never);
    let confirmConfig: { onOk?: () => Promise<void> } | undefined;
    vi.spyOn(Modal, 'confirm').mockImplementation((config) => {
      confirmConfig = config as { onOk?: () => Promise<void> };
      return { destroy: vi.fn(), update: vi.fn() } as never;
    });
    renderWithProviders(<ManagedEndpointsPanel />);

    fireEvent.click((await screen.findAllByRole('button', { name: /^Delete$/i }))[0]);
    await confirmConfig?.onOk?.();

    await waitFor(() => expect(deleteSpy).toHaveBeenCalledWith('/panel/api/managed-endpoints/managed-1'));
  });

  it('blocks install when backend install plan says blocked', async () => {
    const postSpy = vi.spyOn(HttpUtil, 'post').mockResolvedValue({ success: true, msg: '', obj: {} } as never);
    renderWithProviders(<ManagedEndpointsPanel />);

    const install = (await screen.findAllByRole('button', { name: /Install/i }))
      .find((button) => button.textContent === 'Install')!;
    expect(install.getAttribute('disabled')).not.toBeNull();
    fireEvent.click(install);
    expect(postSpy).not.toHaveBeenCalled();
    expect(await screen.findByText(/missing package manager/i)).toBeTruthy();
  });
});
