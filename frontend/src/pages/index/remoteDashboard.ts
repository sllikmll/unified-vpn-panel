import { Status } from '@/models/status';
import type { NodeRecord } from '@/schemas/node';

export const LOCAL_DASHBOARD_TARGET = 'local';
export const REMOTE_HEARTBEAT_FRESH_MS = 30_000;

export interface DashboardTarget {
  key: string;
  label: string;
  node?: NodeRecord;
}

export function nodeDashboardKey(node: NodeRecord): string {
  const guid = (node.guid || '').trim();
  return guid ? `node:${guid}` : `node-id:${node.id}`;
}

export function dashboardTargets(nodes: NodeRecord[], localLabel: string): DashboardTarget[] {
  return [
    { key: LOCAL_DASHBOARD_TARGET, label: localLabel },
    ...nodes
      .filter((node) => !node.transitive && node.id > 0)
      .map((node) => ({
        key: nodeDashboardKey(node),
        label: node.name || node.remark || node.guid || `#${node.id}`,
        node,
      })),
  ];
}

export type RemoteSnapshotState =
  | { available: true; status: Status; reason: '' }
  | { available: false; status: null; reason: 'disabled' | 'offline' | 'stale' | 'unavailable' };

export function statusFromNodeSnapshot(node: NodeRecord): Status {
  return new Status({
    cpu: node.cpuPct ?? 0,
    cpuCores: node.cpuCores ?? 0,
    logicalPro: node.logicalPro ?? 0,
    cpuSpeedMhz: node.cpuSpeedMhz ?? 0,
    mem: { current: node.memCurrent ?? 0, total: node.memTotal ?? 0 },
    swap: { current: node.swapCurrent ?? 0, total: node.swapTotal ?? 0 },
    disk: { current: node.diskCurrent ?? 0, total: node.diskTotal ?? 0 },
    netIO: { up: node.netUp ?? 0, down: node.netDown ?? 0 },
    netTraffic: { sent: node.netTrafficSent ?? 0, recv: node.netTrafficRecv ?? 0 },
    tcpCount: node.tcpCount ?? 0,
    udpCount: node.udpCount ?? 0,
    uptime: node.uptimeSecs ?? 0,
    appUptime: node.appStatsUptime ?? 0,
    appStats: {
      mem: node.appStatsMem ?? 0,
      threads: node.appStatsThreads ?? 0,
      uptime: node.appStatsUptime ?? 0,
    },
    publicIP: {
      ipv4: node.publicIpV4 || 0,
      ipv6: node.publicIpV6 || 0,
    },
    xray: {
      state: node.xrayState || (node.xrayVersion ? 'running' : 'stop'),
      errorMsg: node.xrayError || '',
      version: node.xrayVersion || '',
    },
  });
}

export function remoteSnapshotState(
  node: NodeRecord | undefined,
  nowMs: number,
  freshMs = REMOTE_HEARTBEAT_FRESH_MS,
): RemoteSnapshotState {
  if (!node) return { available: false, status: null, reason: 'unavailable' };
  if (!node.enable) return { available: false, status: null, reason: 'disabled' };
  if (node.status !== 'online') return { available: false, status: null, reason: 'offline' };
  const lastMs = (node.lastHeartbeat || 0) * 1000;
  if (lastMs <= 0 || nowMs - lastMs > freshMs) {
    return { available: false, status: null, reason: 'stale' };
  }
  return { available: true, status: statusFromNodeSnapshot(node), reason: '' };
}
