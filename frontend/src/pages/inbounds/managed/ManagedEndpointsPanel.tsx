import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Alert, Button, Card, Descriptions, Form, Input, Modal, QRCode, Space, Table, Tag, message } from 'antd';
import {
  ApiOutlined,
  DownloadOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  StopOutlined,
  ToolOutlined,
  UserAddOutlined,
  UserOutlined,
} from '@ant-design/icons';

import { SizeFormatter } from '@/utils';
import {
  useManagedEndpointActions,
  useManagedEndpointQuery,
  useManagedEndpointsQuery,
  useManagedInstallPlansQuery,
  type ManagedEndpointClient,
  type ManagedExportResponse,
  type ManagedInstallPlan,
} from '@/api/queries/useManagedEndpointsQuery';
import { MANAGED_PROTOCOL_LABELS, type ManagedNativeProtocol } from '@/lib/managed-protocols';
import type { ManagedEndpoint } from '@/schemas/api/managed-endpoint';
import ManagedInboundForm from '../form/ManagedInboundForm';
import './ManagedEndpointsPanel.css';

const endpointActions = ['start', 'stop', 'restart', 'detect', 'health', 'install', 'update', 'uninstall'] as const;
const nonGreenStatuses = new Set(['draft', 'applying', 'degraded', 'failed', 'disabled', 'deleted', 'deleting', 'unknown', 'stale', 'unavailable']);

function statusColor(status: string): string {
  if (status === 'active') return 'green';
  if (status === 'disabled' || status === 'deleted') return 'default';
  if (status === 'failed' || status === 'degraded' || status === 'unknown' || status === 'stale' || status === 'unavailable') return 'red';
  if (status === 'applying' || status === 'draft') return 'gold';
  return nonGreenStatuses.has(status) ? 'red' : 'default';
}

function protocolLabel(protocol: string): string {
  return MANAGED_PROTOCOL_LABELS[protocol as ManagedNativeProtocol] || protocol;
}

function trafficCell(client: ManagedEndpointClient, t: (key: string) => string): string {
  if (client.traffic?.supported === false) return t('managedProtocols.trafficUnavailable');
  const up = client.traffic?.up;
  const down = client.traffic?.down;
  if (typeof up !== 'number' || typeof down !== 'number') return t('managedProtocols.trafficUnknown');
  return `${SizeFormatter.sizeFormat(up)} / ${SizeFormatter.sizeFormat(down)}`;
}

function actionIcon(action: string) {
  if (action === 'start') return <PlayCircleOutlined />;
  if (action === 'stop') return <StopOutlined />;
  if (action === 'restart') return <ReloadOutlined />;
  if (action === 'install' || action === 'update' || action === 'uninstall') return <ToolOutlined />;
  return <ApiOutlined />;
}

function actionLabel(action: string, t: (key: string) => string): string {
  const labels: Record<string, string> = {
    start: t('managedProtocols.startAction'),
    stop: t('managedProtocols.stopAction'),
    restart: t('managedProtocols.restartAction'),
    detect: t('managedProtocols.detectAction'),
    health: t('managedProtocols.healthAction'),
    install: t('managedProtocols.installAction'),
    update: t('managedProtocols.updateAction'),
    uninstall: t('managedProtocols.uninstallAction'),
  };
  return labels[action] || action;
}

function subscriptionLine(label: string, value: string | undefined, t: (key: string, options?: Record<string, string>) => string) {
  if (!value) return <div className="managed-unavailable">{t('managedProtocols.subscriptionUnavailable', { label })}</div>;
  return (
    <Space.Compact className="managed-copy-line">
      <Input readOnly value={value} />
      <Button onClick={() => { void navigator.clipboard?.writeText(value); void message.success(t('managedProtocols.copied')); }}>{t('managedProtocols.copy')}</Button>
    </Space.Compact>
  );
}

function pickActionBlockReason(endpoint: ManagedEndpoint, action: string): string {
  const anyEndpoint = endpoint as unknown as Record<string, unknown>;
  const plan = anyEndpoint.installPlan;
  const candidates: unknown[] = [plan, anyEndpoint.capabilities, anyEndpoint.actionPlan];
  if (plan && typeof plan === 'object') {
    const actionPlan = (plan as Record<string, unknown>)[action];
    candidates.unshift(actionPlan);
    const actions = (plan as Record<string, unknown>).actions;
    if (actions && typeof actions === 'object') candidates.unshift((actions as Record<string, unknown>)[action]);
  }
  for (const candidate of candidates) {
    if (!candidate || typeof candidate !== 'object') continue;
    const data = candidate as Record<string, unknown>;
    const blocked = data.blocked === true || data.available === false || data.allowed === false || data.supported === false;
    if (blocked) return String(data.reason || data.blockedReason || data.message || 'unavailable');
  }
  return '';
}

function planBlockReason(plan: ManagedInstallPlan | undefined, action: string): string {
  if (action !== 'install' && action !== 'update') return '';
  if (!plan) return '';
  if (plan.blocked || !plan.supported) return plan.reason || 'unavailable';
  return '';
}

function downloadExport(exported: ManagedExportResponse) {
  const content = exported.content ?? exported.subscriptions?.raw ?? '';
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = exported.filename || 'managed-client.txt';
  anchor.click();
  URL.revokeObjectURL(url);
}

interface ClientModalProps {
  endpoint: ManagedEndpoint | null;
  open: boolean;
  onClose: () => void;
}

function ManagedClientsModal({ endpoint, open, onClose }: ClientModalProps) {
  const { t } = useTranslation();
  const actions = useManagedEndpointActions();
  const [exported, setExported] = useState<ManagedExportResponse | null>(null);
  const [error, setError] = useState('');
  const [clientFormOpen, setClientFormOpen] = useState(false);
  const [editingClient, setEditingClient] = useState<ManagedEndpointClient | null>(null);
  const [username, setUsername] = useState('');
  const [subId, setSubId] = useState('');
  const [busyId, setBusyId] = useState('');
  const query = useQuery({
    queryKey: ['managed-endpoints', endpoint?.id, 'clients'],
    queryFn: () => actions.clients(endpoint!.id),
    enabled: open && !!endpoint,
  });
  const clients = query.data ?? [];

  const createClient = async () => {
    if (!endpoint) return;
    setError('');
    const payload = endpoint.protocol === 'naiveproxy' || endpoint.protocol === 'mieru'
      ? { subId: subId || undefined, username: username || undefined, enable: true }
      : { subId: subId || undefined, enable: true };
    const msg = await actions.createClient(endpoint.id, payload);
    if (msg?.success) {
      setClientFormOpen(false);
      setUsername('');
      setSubId('');
      await query.refetch();
    } else {
      setError(msg?.msg || t('managedProtocols.loadClientsFailed'));
    }
  };

  const openClientEdit = (client: ManagedEndpointClient) => {
    setEditingClient(client);
    setUsername(client.username || client.email || '');
    setSubId(client.subId || '');
  };

  const saveClientEdit = async () => {
    if (!endpoint || !editingClient) return;
    const payload = endpoint.protocol === 'naiveproxy' || endpoint.protocol === 'mieru'
      ? { subId: subId || undefined, username: username || undefined, enable: editingClient.enable ?? editingClient.enabled }
      : { subId: subId || undefined, enable: editingClient.enable ?? editingClient.enabled };
    const msg = await actions.patchClient(endpoint.id, editingClient.id, payload);
    if (msg?.success) {
      setEditingClient(null);
      setUsername('');
      setSubId('');
      await query.refetch();
    } else {
      setError(msg?.msg || t('managedProtocols.loadClientsFailed'));
    }
  };

  const runClientAction = async (client: ManagedEndpointClient, action: 'enable' | 'disable' | 'status') => {
    if (!endpoint) return;
    setBusyId(`${client.id}:${action}`);
    try {
      const msg = await actions.clientAction(endpoint.id, client.id, action);
      if (msg?.success) await query.refetch();
      else setError(msg?.msg || t('managedProtocols.loadClientsFailed'));
    } finally {
      setBusyId('');
    }
  };

  const deleteClient = async (client: ManagedEndpointClient) => {
    if (!endpoint) return;
    Modal.confirm({
      title: t('managedProtocols.deleteClientTitle', { name: client.email || client.username || client.id }),
      okType: 'danger',
      onOk: async () => {
        const msg = await actions.deleteClient(endpoint.id, client.id);
        if (msg?.success) await query.refetch();
        else setError(msg?.msg || t('managedProtocols.loadClientsFailed'));
      },
    });
  };

  const exportClient = async (client: ManagedEndpointClient) => {
    if (!endpoint) return;
    setBusyId(`${client.id}:export`);
    try {
      const result = await actions.exportClient(endpoint.id, client.id);
      if (result) setExported(result);
      else setError(t('managedProtocols.exportFailed'));
    } catch (err) {
      setError((err as Error).message || t('managedProtocols.exportFailed'));
    } finally {
      setBusyId('');
    }
  };

  return (
    <>
      <Modal open={open} onCancel={() => { setExported(null); onClose(); }} footer={null} title={endpoint ? `${protocolLabel(endpoint.protocol)} ${t('managedProtocols.clients')}` : t('managedProtocols.managedClients')} width={880}>
        <Space style={{ marginBottom: 12 }}>
          <Button icon={<UserAddOutlined />} onClick={() => setClientFormOpen(true)}>{t('managedProtocols.createClient')}</Button>
          <Button onClick={() => query.refetch()} loading={query.isFetching}>{t('managedProtocols.retry')}</Button>
        </Space>
        {error && <Alert className="managed-action-error" type="error" showIcon title={error} />}
        {query.error && <Alert type="error" showIcon title={(query.error as Error).message} />}
        <Table
          rowKey="id"
          size="small"
          loading={query.isFetching}
          dataSource={clients}
          columns={[
            { title: t('managedProtocols.client'), key: 'client', render: (_v, row: ManagedEndpointClient) => row.email || row.username || row.id },
            { title: t('managedProtocols.subId'), key: 'subId', render: (_v, row: ManagedEndpointClient) => row.subId || '-' },
            { title: t('managedProtocols.protocolUsername'), key: 'publicIdentity', render: (_v, row: ManagedEndpointClient) => row.publicIdentity || row.username || '-' },
            { title: t('managedProtocols.address'), key: 'address', render: (_v, row: ManagedEndpointClient) => row.address || '-' },
            { title: t('managedProtocols.enabled'), key: 'enable', render: (_v, row: ManagedEndpointClient) => <Tag color={(row.enable ?? row.enabled) ? 'green' : 'default'}>{(row.enable ?? row.enabled) ? t('managedProtocols.enabled') : t('managedProtocols.disabled')}</Tag> },
            { title: t('managedProtocols.status'), key: 'status', render: (_v, row: ManagedEndpointClient) => <Tag color={statusColor(row.status || 'unknown')}>{row.status || t('managedProtocols.unknown')}</Tag> },
            { title: t('managedProtocols.traffic'), key: 'traffic', render: (_v, row: ManagedEndpointClient) => trafficCell(row, t) },
            {
              title: t('managedProtocols.actions'),
              key: 'actions',
              render: (_v, row: ManagedEndpointClient) => (
                <Space wrap>
                  <Button size="small" loading={busyId === `${row.id}:enable`} onClick={() => runClientAction(row, 'enable')}>{t('managedProtocols.enableAction')}</Button>
                  <Button size="small" loading={busyId === `${row.id}:disable`} onClick={() => runClientAction(row, 'disable')}>{t('managedProtocols.disableAction')}</Button>
                  <Button size="small" loading={busyId === `${row.id}:status`} onClick={() => runClientAction(row, 'status')}>{t('managedProtocols.statusAction')}</Button>
                  <Button size="small" icon={<DownloadOutlined />} loading={busyId === `${row.id}:export`} onClick={() => exportClient(row)}>{t('managedProtocols.exportAction')}</Button>
                  <Button size="small" onClick={() => openClientEdit(row)}>{t('managedProtocols.editAction')}</Button>
                  <Button size="small" danger onClick={() => deleteClient(row)}>{t('managedProtocols.deleteAction')}</Button>
                </Space>
              ),
            },
          ]}
        />
      </Modal>
      <Modal open={clientFormOpen} title={t('managedProtocols.createClient')} onCancel={() => setClientFormOpen(false)} onOk={createClient}>
        <Alert type="info" showIcon title={t('managedProtocols.generatedCredentials')} />
        <Form className="mt-12" colon={false}>
          <Form.Item label={t('managedProtocols.subId')} htmlFor="managed-client-create-sub-id" required>
            <Input id="managed-client-create-sub-id" value={subId} onChange={(e) => setSubId(e.target.value)} />
          </Form.Item>
          {(endpoint?.protocol === 'naiveproxy' || endpoint?.protocol === 'mieru') && <Form.Item label={t('managedProtocols.protocolUsername')}>
            <Input value={username} onChange={(e) => setUsername(e.target.value)} />
          </Form.Item>}
        </Form>
      </Modal>
      <Modal open={!!editingClient} title={t('managedProtocols.editClient')} onCancel={() => setEditingClient(null)} onOk={saveClientEdit}>
        <Alert type="info" showIcon title={t('managedProtocols.noSecretsInEdit')} />
        <Form className="mt-12" colon={false}>
          <Form.Item label={t('managedProtocols.subId')} htmlFor="managed-client-edit-sub-id" required>
            <Input id="managed-client-edit-sub-id" value={subId} onChange={(e) => setSubId(e.target.value)} />
          </Form.Item>
          {(endpoint?.protocol === 'naiveproxy' || endpoint?.protocol === 'mieru') && <Form.Item label={t('managedProtocols.protocolUsername')}>
            <Input value={username} onChange={(e) => setUsername(e.target.value)} />
          </Form.Item>}
        </Form>
      </Modal>
      <Modal
        open={!!exported}
        title={t('managedProtocols.exportedProfile')}
        onCancel={() => setExported(null)}
        footer={<Button onClick={() => setExported(null)}>{t('managedProtocols.close')}</Button>}
        width={720}
        destroyOnHidden
      >
        <Descriptions bordered size="small" column={1}>
          <Descriptions.Item label={t('managedProtocols.raw')}>{subscriptionLine(t('managedProtocols.raw'), exported?.subscriptions?.raw, t)}</Descriptions.Item>
          <Descriptions.Item label={t('managedProtocols.json')}>{subscriptionLine(t('managedProtocols.json'), exported?.subscriptions?.json, t)}</Descriptions.Item>
          <Descriptions.Item label={t('managedProtocols.clash')}>{subscriptionLine(t('managedProtocols.clash'), exported?.subscriptions?.clash, t)}</Descriptions.Item>
        </Descriptions>
        {exported && (exported.content || exported.subscriptions?.raw) && (
          <Button className="managed-download-button" icon={<DownloadOutlined />} onClick={() => downloadExport(exported)}>
            {t('managedProtocols.download')}
          </Button>
        )}
        {exported?.content && <pre className="managed-export-block">{exported.content}</pre>}
        {(exported?.content || exported?.subscriptions?.raw) && (
          <div className="managed-qr-wrap">
            <QRCode value={exported.content || exported.subscriptions?.raw || ''} />
          </div>
        )}
      </Modal>
    </>
  );
}

export default function ManagedEndpointsPanel() {
  const { t } = useTranslation();
  const query = useManagedEndpointsQuery();
  const plansQuery = useManagedInstallPlansQuery();
  const actions = useManagedEndpointActions();
  const [selected, setSelected] = useState<ManagedEndpoint | null>(null);
  const [editing, setEditing] = useState<ManagedEndpoint | null>(null);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const detailQuery = useManagedEndpointQuery(editing?.id ?? '', !!editing);
  const rows = useMemo(
    () => (query.data ?? []).filter((row) => ['amneziawg', 'mieru', 'naiveproxy'].includes(row.protocol)),
    [query.data],
  );
  const plansByRuntime = useMemo(() => new Map((plansQuery.data ?? []).map((plan) => [plan.runtimeKind, plan])), [plansQuery.data]);

  const runAction = (endpoint: ManagedEndpoint, action: string) => {
    const blockReason = planBlockReason(plansByRuntime.get(endpoint.runtimeKind), action) || pickActionBlockReason(endpoint, action);
    if (blockReason && (action === 'install' || action === 'update')) {
      setError(t('managedProtocols.installBlocked', { reason: blockReason }));
      return;
    }
    Modal.confirm({
      title: t('managedProtocols.confirmActionTitle', { action: actionLabel(action, t), tag: endpoint.tag }),
      content: blockReason ? t('managedProtocols.blockedReason', { reason: blockReason }) : undefined,
      okText: action === 'uninstall' ? t('managedProtocols.uninstallAction') : t('managedProtocols.ok'),
      okType: action === 'uninstall' ? 'danger' : 'primary',
      onOk: async () => {
        setBusy(`${endpoint.id}:${action}`);
        setError('');
        try {
          const msg = await actions.endpointAction(endpoint.id, action);
          if (msg?.success) await query.refetch();
          else setError(msg?.msg || t('managedProtocols.unavailable'));
        } finally {
          setBusy('');
        }
      },
    });
  };

  const openEdit = (endpoint: ManagedEndpoint) => {
    setEditing(endpoint);
  };

  const deleteEndpoint = (endpoint: ManagedEndpoint) => {
    Modal.confirm({
      title: t('managedProtocols.deleteEndpointTitle', { tag: endpoint.tag }),
      content: t('managedProtocols.deleteEndpointDescription'),
      okText: t('delete'),
      okType: 'danger',
      onOk: async () => {
        setBusy(`${endpoint.id}:delete`);
        setError('');
        try {
          const msg = await actions.deleteEndpoint(endpoint.id);
          if (msg?.success) await query.refetch();
          else setError(msg?.msg || t('managedProtocols.unavailable'));
        } finally {
          setBusy('');
        }
      },
    });
  };

  return (
    <>
      <Card
        hoverable
        title={<Space><ApiOutlined /> {t('managedProtocols.title')}</Space>}
        extra={<Button onClick={() => query.refetch()} loading={query.isFetching}>{t('managedProtocols.retry')}</Button>}
      >
        {error && <Alert className="managed-action-error" type="error" showIcon title={error} />}
        {query.error && <Alert type="error" showIcon title={(query.error as Error).message} />}
        <Table
          size="small"
          rowKey="id"
          loading={query.isFetching}
          dataSource={rows}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          columns={[
            { title: t('pages.inbounds.protocol'), key: 'protocol', render: (_v, row: ManagedEndpoint) => <Space>{protocolLabel(row.protocol)}<Tag color="blue">{t('managedProtocols.badge')}</Tag></Space> },
            { title: t('managedProtocols.tag'), dataIndex: 'tag' },
            { title: t('managedProtocols.node'), key: 'node', render: (_v, row: ManagedEndpoint) => row.nodeName || (row.nodeId ? `#${row.nodeId}` : t('managedProtocols.localPanel')) },
            { title: t('managedProtocols.listenPort'), key: 'listen', render: (_v, row: ManagedEndpoint) => `${row.listen || '*'}:${row.port}` },
            { title: t('managedProtocols.status'), key: 'status', render: (_v, row: ManagedEndpoint) => <Tag color={statusColor(row.status || row.health?.status || 'unknown')}>{row.status || row.health?.status || t('managedProtocols.unknown')}</Tag> },
            { title: t('managedProtocols.clients'), dataIndex: 'clientCount' },
            {
              title: t('managedProtocols.actions'),
              key: 'actions',
              render: (_v, row: ManagedEndpoint) => (
                <Space wrap>
                  <Button size="small" onClick={() => openEdit(row)}>{t('managedProtocols.editAction')}</Button>
                  <Button size="small" icon={<UserOutlined />} onClick={() => setSelected(row)}>{t('managedProtocols.clients')}</Button>
                  {endpointActions.map((action) => (
                    <Button
                      key={action}
                      size="small"
                      icon={actionIcon(action)}
                      danger={action === 'uninstall'}
                      loading={busy === `${row.id}:${action}`}
                      disabled={!!(planBlockReason(plansByRuntime.get(row.runtimeKind), action) || pickActionBlockReason(row, action)) && (action === 'install' || action === 'update')}
                      onClick={() => runAction(row, action)}
                    >
                      {actionLabel(action, t)}
                    </Button>
                  ))}
                  <Button size="small" icon={<PauseCircleOutlined />} onClick={() => runAction(row, row.enable ? 'disable' : 'enable')}>
                    {row.enable ? t('managedProtocols.disableAction') : t('managedProtocols.enableAction')}
                  </Button>
                  <Button size="small" danger loading={busy === `${row.id}:delete`} onClick={() => deleteEndpoint(row)}>
                    {t('managedProtocols.deleteAction')}
                  </Button>
                  {(planBlockReason(plansByRuntime.get(row.runtimeKind), 'install') || pickActionBlockReason(row, 'install')) && <Tag color="red">{planBlockReason(plansByRuntime.get(row.runtimeKind), 'install') || pickActionBlockReason(row, 'install')}</Tag>}
                </Space>
              ),
            },
          ]}
        />
      </Card>
      <Modal open={!!editing} title={t('managedProtocols.editAction')} onCancel={() => setEditing(null)} footer={null} width={760}>
        {detailQuery.error && <Alert type="error" showIcon title={(detailQuery.error as Error).message} />}
        {detailQuery.isFetching && <Alert type="info" showIcon title={t('managedProtocols.loading')} />}
        {editing && (detailQuery.data || !detailQuery.isFetching) && (
          <ManagedInboundForm
            protocol={editing.protocol as ManagedNativeProtocol}
            mode="edit"
            endpointId={editing.id}
            availableNodes={[]}
            initialValues={({
              ...editing,
              ...(detailQuery.data ?? {}),
              config: ((detailQuery.data as unknown as { config?: unknown } | null)?.config) as never,
            }) as never}
            onSaved={() => {
              setEditing(null);
              void query.refetch();
            }}
            onClose={() => setEditing(null)}
          />
        )}
      </Modal>
      <ManagedClientsModal endpoint={selected} open={!!selected} onClose={() => setSelected(null)} />
    </>
  );
}
