import { describe, expect, it } from 'vitest';

import {
  LOCAL_DASHBOARD_TARGET,
  dashboardTargets,
  nodeDashboardKey,
  remoteSnapshotState,
  statusFromNodeSnapshot,
} from '@/pages/index/remoteDashboard';
import type { NodeRecord } from '@/schemas/node';

const nowSec = 1_700_000_000;

function node(overrides: Partial<NodeRecord> = {}): NodeRecord {
  return {
    id: 7,
    name: 'edge',
    enable: true,
    status: 'online',
    lastHeartbeat: nowSec,
    cpuPct: 25,
    cpuCores: 4,
    logicalPro: 8,
    cpuSpeedMhz: 2400,
    memCurrent: 100,
    memTotal: 200,
    swapCurrent: 10,
    swapTotal: 20,
    diskCurrent: 300,
    diskTotal: 600,
    netUp: 1000,
    netDown: 2000,
    netTrafficSent: 3000,
    netTrafficRecv: 4000,
    tcpCount: 5,
    udpCount: 6,
    uptimeSecs: 123,
    appStatsMem: 700,
    appStatsThreads: 8,
    appStatsUptime: 900,
    publicIpV4: '203.0.113.7',
    publicIpV6: '2001:db8::7',
    xrayVersion: '26.6.27',
    xrayState: 'running',
    panelVersion: '0.0.1',
    ...overrides,
  };
}

describe('remote dashboard mapping', () => {
  it('builds target options from local plus direct nodes only', () => {
    const direct = node({ guid: 'direct-guid' });
    const transitive = node({ id: 0, name: 'child', guid: 'child-guid', transitive: true });

    expect(dashboardTargets([direct, transitive], 'Local server')).toEqual([
      { key: LOCAL_DASHBOARD_TARGET, label: 'Local server' },
      { key: 'node:direct-guid', label: 'edge', node: direct },
    ]);
    expect(nodeDashboardKey(node({ guid: '' }))).toBe('node-id:7');
  });

  it('maps a successful node heartbeat snapshot into the Status model', () => {
    const status = statusFromNodeSnapshot(node());

    expect(status.cpu.percent).toBe(25);
    expect(status.cpuCores).toBe(4);
    expect(status.logicalPro).toBe(8);
    expect(status.mem.current).toBe(100);
    expect(status.mem.total).toBe(200);
    expect(status.disk.percent).toBe(50);
    expect(status.netIO.up).toBe(1000);
    expect(status.netTraffic.recv).toBe(4000);
    expect(status.tcpCount).toBe(5);
    expect(status.appStats.threads).toBe(8);
    expect(status.publicIP.ipv4).toBe('203.0.113.7');
    expect(status.xray.version).toBe('26.6.27');
  });

  it('rejects disabled, offline, stale, and missing remote snapshots', () => {
    expect(remoteSnapshotState(node({ enable: false }), nowSec * 1000).reason).toBe('disabled');
    expect(remoteSnapshotState(node({ status: 'offline' }), nowSec * 1000).reason).toBe('offline');
    expect(remoteSnapshotState(node({ lastHeartbeat: nowSec - 31 }), nowSec * 1000).reason).toBe('stale');
    expect(remoteSnapshotState(undefined, nowSec * 1000).reason).toBe('unavailable');
    expect(remoteSnapshotState(node(), nowSec * 1000).available).toBe(true);
  });
});
