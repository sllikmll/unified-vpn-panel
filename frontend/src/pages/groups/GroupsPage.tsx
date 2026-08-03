import { lazy, useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Col,
  ConfigProvider,
  Dropdown,
  Form,
  Input,
  InputNumber,
  Layout,
  Modal,
  Result,
  Row,
  Select,
  Space,
  Spin,
  Statistic,
  Switch,
  Table,
  Tag,
  Tooltip,
  message,
} from 'antd';
import type { MenuProps, TableColumnsType } from 'antd';
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  ClockCircleOutlined,
  DeleteOutlined,
  EditOutlined,
  LinkOutlined,
  MoreOutlined,
  PieChartOutlined,
  PlusOutlined,
  RetweetOutlined,
  TagsOutlined,
  TeamOutlined,
  UsergroupAddOutlined,
  UsergroupDeleteOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';

import { useTheme } from '@/hooks/useTheme';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { usePageTitle } from '@/hooks/usePageTitle';
import { useClients } from '@/hooks/useClients';
import { HttpUtil, SizeFormatter } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import AppSidebar from '@/layouts/AppSidebar';
import { LazyMount } from '@/components/utility';
import { keys } from '@/api/queryKeys';
import { useInboundOptions } from '@/api/queries/useInboundOptions';
import {
  ClientRecordSchema,
  GroupSummaryListSchema,
  type ClientRecord,
  type GroupSummary,
} from '@/schemas/client';
import { parseMsg } from '@/utils/zodValidate';

const ClientRecordListSchema = z.array(ClientRecordSchema).nullable().transform((v) => v ?? []);

const SubLinksModal = lazy(() => import('../clients/SubLinksModal'));
const ClientBulkAdjustModal = lazy(() => import('../clients/ClientBulkAdjustModal'));
const GroupAddClientsModal = lazy(() => import('./GroupAddClientsModal'));
const GroupRemoveClientsModal = lazy(() => import('./GroupRemoveClientsModal'));

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

interface GroupFormState {
  name: string;
  description: string;
  enable: boolean;
  assignedInboundIds: number[];
  defaultTotalGB: number;
  defaultExpiryTime: number;
}

const emptyGroupForm: GroupFormState = {
  name: '',
  description: '',
  enable: true,
  assignedInboundIds: [],
  defaultTotalGB: 0,
  defaultExpiryTime: 0,
};

function groupToForm(group: GroupSummary): GroupFormState {
  return {
    name: group.name,
    description: group.description || '',
    enable: group.enable !== false,
    assignedInboundIds: group.assignedInboundIds || [],
    defaultTotalGB: Math.max(0, group.defaultTotalGB || 0),
    defaultExpiryTime: Math.max(0, group.defaultExpiryTime || 0),
  };
}

async function fetchGroups(): Promise<GroupSummary[]> {
  const msg = await HttpUtil.get('/panel/api/clients/groups', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to load groups');
  const validated = parseMsg(msg, GroupSummaryListSchema, 'clients/groups');
  return validated.obj ?? [];
}

async function fetchEmailsForGroup(name: string): Promise<string[]> {
  const msg = await HttpUtil.get<string[]>(
    `/panel/api/clients/groups/${encodeURIComponent(name)}/emails`,
    undefined,
    { silent: true },
  );
  if (!msg?.success || !Array.isArray(msg.obj)) return [];
  return msg.obj;
}

export default function GroupsPage() {
  usePageTitle();
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const { isMobile } = useMediaQuery();
  const [modal, modalContextHolder] = Modal.useModal();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => { setMessageInstance(messageApi); }, [messageApi]);
  const queryClient = useQueryClient();

  const { subSettings, bulkAdjust, bulkAddToGroup, bulkRemoveFromGroup, bulkDelete } = useClients({ list: false });
  const inboundOptionsQuery = useInboundOptions();

  const groupsQuery = useQuery({
    queryKey: keys.clients.groups(),
    queryFn: fetchGroups,
  });
  const groups = useMemo(() => groupsQuery.data ?? [], [groupsQuery.data]);
  const loading = groupsQuery.isFetching;
  const fetched = groupsQuery.data !== undefined || groupsQuery.isError;
  const fetchError = groupsQuery.error ? (groupsQuery.error as Error).message : '';

  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: keys.clients.root() });
  }, [queryClient]);

  const createMut = useMutation({
    mutationFn: (body: GroupFormState) =>
      HttpUtil.post('/panel/api/clients/groups/create', {
        name: body.name,
        description: body.description,
        enable: body.enable,
        assignedInboundIds: body.assignedInboundIds,
        policy: {
          defaultTotalGB: body.defaultTotalGB,
          defaultExpiryTime: body.defaultExpiryTime,
        },
      }, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const updateMut = useMutation({
    mutationFn: (body: { oldName: string } & GroupFormState) =>
      HttpUtil.post('/panel/api/clients/groups/update', {
        oldName: body.oldName,
        name: body.name,
        description: body.description,
        enable: body.enable,
        assignedInboundIds: body.assignedInboundIds,
        policy: {
          defaultTotalGB: body.defaultTotalGB,
          defaultExpiryTime: body.defaultExpiryTime,
        },
      }, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const deleteMut = useMutation({
    mutationFn: (body: { name: string }) =>
      HttpUtil.post('/panel/api/clients/groups/delete', body, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const groupResetMut = useMutation({
    mutationFn: (body: { name: string }) =>
      HttpUtil.post('/panel/api/clients/groups/resetTraffic', body, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState<GroupFormState>(emptyGroupForm);

  const [renameOpen, setRenameOpen] = useState(false);
  const [renameTarget, setRenameTarget] = useState<GroupSummary | null>(null);
  const [renameForm, setRenameForm] = useState<GroupFormState>(emptyGroupForm);

  const [subLinksOpen, setSubLinksOpen] = useState(false);
  const [adjustOpen, setAdjustOpen] = useState(false);
  const [addClientsOpen, setAddClientsOpen] = useState(false);
  const [removeClientsOpen, setRemoveClientsOpen] = useState(false);
  const [groupEmails, setGroupEmails] = useState<string[]>([]);
  const [groupForAction, setGroupForAction] = useState<GroupSummary | null>(null);

  const allClientsQuery = useQuery<ClientRecord[]>({
    queryKey: keys.clients.all(),
    queryFn: async () => {
      const msg = await HttpUtil.get('/panel/api/clients/list', undefined, { silent: true });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to load clients');
      const validated = parseMsg(msg, ClientRecordListSchema, 'clients/list');
      return validated.obj ?? [];
    },
    enabled: addClientsOpen || removeClientsOpen || subLinksOpen,
    staleTime: 30_000,
  });
  const allClients = allClientsQuery.data ?? [];

  const totalGroups = groups.length;
  const totalClients = useMemo(
    () => groups.reduce((acc, g) => acc + (g.clientCount || 0), 0),
    [groups],
  );
  const totalTraffic = useMemo(
    () => groups.reduce((acc, g) => acc + (g.trafficUsed || 0), 0),
    [groups],
  );
  const totalUpload = useMemo(
    () => groups.reduce((acc, g) => acc + (g.up || 0), 0),
    [groups],
  );
  const totalDownload = useMemo(
    () => groups.reduce((acc, g) => acc + (g.down || 0), 0),
    [groups],
  );

  function openCreate() {
    setCreateForm(emptyGroupForm);
    setCreateOpen(true);
  }

  async function confirmCreate() {
    const name = createForm.name.trim();
    if (!name) return;
    if (groups.some((g) => g.name.toLowerCase() === name.toLowerCase())) {
      messageApi.error(t('pages.groups.renameCollision', { name }));
      return;
    }
    const msg = await createMut.mutateAsync({ ...createForm, name });
    if (msg?.success) {
      messageApi.success(t('pages.groups.createSuccess', { name }));
      setCreateOpen(false);
    }
  }

  function openRename(g: GroupSummary) {
    setRenameTarget(g);
    setRenameForm(groupToForm(g));
    setRenameOpen(true);
  }

  async function confirmRename() {
    if (!renameTarget) return;
    const next = renameForm.name.trim();
    if (!next) {
      return;
    }
    const sameMetadata =
      next === renameTarget.name &&
      renameForm.description === (renameTarget.description || '') &&
      renameForm.enable === (renameTarget.enable !== false) &&
      JSON.stringify([...renameForm.assignedInboundIds].sort()) === JSON.stringify([...(renameTarget.assignedInboundIds || [])].sort()) &&
      renameForm.defaultTotalGB === (renameTarget.defaultTotalGB || 0) &&
      renameForm.defaultExpiryTime === (renameTarget.defaultExpiryTime || 0);
    if (sameMetadata) {
      setRenameOpen(false);
      return;
    }
    if (groups.some((g) => g.name.toLowerCase() === next.toLowerCase() && g.name !== renameTarget.name)) {
      messageApi.error(t('pages.groups.renameCollision', { name: next }));
      return;
    }
    const msg = await updateMut.mutateAsync({ ...renameForm, oldName: renameTarget.name, name: next });
    if (msg?.success) {
      const affected = (msg.obj as { affected?: number } | undefined)?.affected ?? 0;
      messageApi.success(t('pages.groups.updateSuccess', { count: affected }));
      setRenameOpen(false);
    }
  }

  function onDelete(g: GroupSummary) {
    modal.confirm({
      title: t('pages.groups.deleteConfirmTitle', { name: g.name }),
      content: t('pages.groups.deleteConfirmContent', { count: g.clientCount }),
      okText: t('delete'),
      okType: 'danger',
      cancelText: t('cancel'),
      onOk: async () => {
        const msg = await deleteMut.mutateAsync({ name: g.name });
        if (msg?.success) {
          const affected = (msg.obj as { affected?: number } | undefined)?.affected ?? 0;
          messageApi.success(t('pages.groups.deleteSuccess', { count: affected }));
        }
      },
    });
  }

  async function openSubLinksFor(g: GroupSummary) {
    if (!g.clientCount) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    const emails = await fetchEmailsForGroup(g.name);
    if (emails.length === 0) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    setGroupForAction(g);
    setGroupEmails(emails);
    setSubLinksOpen(true);
  }

  async function openAdjustFor(g: GroupSummary) {
    if (!g.clientCount) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    const emails = await fetchEmailsForGroup(g.name);
    if (emails.length === 0) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    setGroupForAction(g);
    setGroupEmails(emails);
    setAdjustOpen(true);
  }

  function openAddClientsFor(g: GroupSummary) {
    setGroupForAction(g);
    setAddClientsOpen(true);
  }

  function openRemoveClientsFor(g: GroupSummary) {
    if (!g.clientCount) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    setGroupForAction(g);
    setRemoveClientsOpen(true);
  }

  function onDeleteClients(g: GroupSummary) {
    if (!g.clientCount) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    modal.confirm({
      title: t('pages.groups.deleteClientsConfirmTitle', { name: g.name }),
      content: t('pages.groups.deleteClientsConfirmContent', { count: g.clientCount }),
      okText: t('delete'),
      okType: 'danger',
      cancelText: t('cancel'),
      onOk: async () => {
        const emails = await fetchEmailsForGroup(g.name);
        if (emails.length === 0) return;
        const msg = await bulkDelete(emails);
        if (msg?.success) {
          const ok = msg.obj?.deleted ?? 0;
          const skipped = msg.obj?.skipped ?? [];
          const failed = skipped.length;
          if (failed === 0) {
            messageApi.success(t('pages.groups.deleteClientsSuccess', { count: ok }));
          } else {
            const firstError = skipped[0]?.reason ?? msg?.msg ?? '';
            messageApi.warning(firstError
              ? `${t('pages.groups.deleteClientsMixed', { ok, failed })} — ${firstError}`
              : t('pages.groups.deleteClientsMixed', { ok, failed }));
          }
        }
      },
    });
  }

  function onResetTraffic(g: GroupSummary) {
    if (!g.clientCount) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    modal.confirm({
      title: t('pages.groups.resetConfirmTitle', { name: g.name }),
      content: t('pages.groups.resetConfirmContent'),
      okText: t('reset'),
      okType: 'danger',
      cancelText: t('cancel'),
      onOk: async () => {
        const msg = await groupResetMut.mutateAsync({ name: g.name });
        if (msg?.success) {
          messageApi.success(t('pages.groups.resetSuccess', { name: g.name }));
        }
      },
    });
  }

  function rowActions(row: GroupSummary): MenuProps['items'] {
    return [
      {
        key: 'subLinks',
        icon: <LinkOutlined />,
        label: t('pages.clients.subLinksSelected', { count: row.clientCount || 0 }),
        disabled: !row.clientCount,
        onClick: () => openSubLinksFor(row),
      },
      {
        key: 'adjust',
        icon: <ClockCircleOutlined />,
        label: t('pages.clients.adjustSelected', { count: row.clientCount || 0 }),
        disabled: !row.clientCount,
        onClick: () => openAdjustFor(row),
      },
      {
        key: 'reset',
        icon: <RetweetOutlined />,
        label: t('pages.groups.resetTraffic'),
        disabled: !row.clientCount,
        onClick: () => onResetTraffic(row),
      },
      {
        key: 'addClients',
        icon: <UsergroupAddOutlined />,
        label: t('pages.groups.addToGroup'),
        onClick: () => openAddClientsFor(row),
      },
      {
        key: 'rename',
        icon: <EditOutlined />,
        label: t('pages.groups.rename'),
        onClick: () => openRename(row),
      },
      { type: 'divider' },
      {
        key: 'removeClients',
        icon: <UsergroupDeleteOutlined />,
        label: t('pages.groups.removeFromGroup'),
        danger: true,
        disabled: !row.clientCount,
        onClick: () => openRemoveClientsFor(row),
      },
      {
        key: 'deleteClients',
        icon: <DeleteOutlined />,
        label: t('pages.groups.deleteClients'),
        danger: true,
        disabled: !row.clientCount,
        onClick: () => onDeleteClients(row),
      },
      {
        key: 'delete',
        icon: <DeleteOutlined />,
        label: t('pages.groups.deleteGroupOnly'),
        danger: true,
        onClick: () => onDelete(row),
      },
    ];
  }

  const columns: TableColumnsType<GroupSummary> = [
    {
      title: t('pages.clients.actions'),
      key: 'actions',
      width: 90,
      render: (_v, row) => (
        <Space size={4}>
          <Dropdown trigger={['click']} menu={{ items: rowActions(row) }}>
            <Button aria-label={t('more')} size="small" type="text" style={{ fontSize: 16 }} icon={<MoreOutlined />} />
          </Dropdown>
          <Tooltip title={t('pages.groups.rename')}>
            <Button aria-label={t('pages.groups.rename')} size="small" type="text" style={{ fontSize: 16 }} icon={<EditOutlined />} onClick={() => openRename(row)} />
          </Tooltip>
        </Space>
      ),
    },
    {
      title: t('pages.groups.name'),
      dataIndex: 'name',
      key: 'name',
      render: (name: string, row) => (
        <Space direction="vertical" size={2}>
          <Space size={6} wrap>
            <Tag color="geekblue" style={{ margin: 0, fontSize: 13 }}>{name}</Tag>
            <Tag color={row.enable === false ? 'default' : 'success'}>{row.enable === false ? t('disabled') : t('enable')}</Tag>
          </Space>
          {row.description ? <span style={{ color: 'rgba(0,0,0,0.55)' }}>{row.description}</span> : null}
        </Space>
      ),
    },
    {
      title: t('pages.groups.assignedInbounds'),
      dataIndex: 'assignedInboundIds',
      key: 'assignedInboundIds',
      width: 220,
      render: (ids: number[]) => {
        if (!ids?.length) return <span style={{ color: 'rgba(0,0,0,0.45)' }}>—</span>;
        const byId = new Map((inboundOptionsQuery.data ?? []).map((ib) => [ib.id, ib]));
        return (
          <Space size={[4, 4]} wrap>
            {ids.map((id) => {
              const ib = byId.get(id);
              return <Tag key={id}>{ib ? `${ib.remark || ib.tag} · ${ib.protocol}` : `#${id}`}</Tag>;
            })}
          </Space>
        );
      },
    },
    {
      title: t('pages.groups.defaults'),
      key: 'defaults',
      width: 220,
      render: (_value, row) => (
        <Space size={[4, 4]} wrap>
          {row.defaultTotalGB > 0 ? <Tag>{SizeFormatter.sizeFormat(row.defaultTotalGB)}</Tag> : null}
          {row.defaultExpiryTime > 0 ? <Tag>{new Date(row.defaultExpiryTime).toLocaleDateString()}</Tag> : null}
          {row.defaultTotalGB <= 0 && row.defaultExpiryTime <= 0 ? <span style={{ color: 'rgba(0,0,0,0.45)' }}>—</span> : null}
        </Space>
      ),
    },
    {
      title: t('pages.groups.clientCount'),
      dataIndex: 'clientCount',
      key: 'clientCount',
      width: 180,
      render: (count: number) => <span>{count || 0}</span>,
    },
    {
      title: t('pages.groups.upload'),
      dataIndex: 'up',
      key: 'up',
      width: 140,
      render: (bytes: number) => <span>{SizeFormatter.sizeFormat(bytes || 0)}</span>,
    },
    {
      title: t('pages.groups.download'),
      dataIndex: 'down',
      key: 'down',
      width: 140,
      render: (bytes: number) => <span>{SizeFormatter.sizeFormat(bytes || 0)}</span>,
    },
    {
      title: t('pages.groups.trafficUsed'),
      dataIndex: 'trafficUsed',
      key: 'trafficUsed',
      width: 160,
      render: (bytes: number) => <span>{SizeFormatter.sizeFormat(bytes || 0)}</span>,
    },
  ];

  const pageClass = useMemo(() => {
    const classes = ['groups-page'];
    if (isDark) classes.push('is-dark');
    if (isUltra) classes.push('is-ultra');
    return classes.join(' ');
  }, [isDark, isUltra]);

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      {modalContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Spin spinning={!fetched} delay={200} description={t('loading')} size="large">
              {!fetched ? (
                <div className="loading-spacer" />
              ) : fetchError ? (
                <Result
                  status="error"
                  title={t('somethingWentWrong')}
                  subTitle={fetchError}
                  extra={<Button type="primary" loading={loading} onClick={() => groupsQuery.refetch()}>{t('refresh')}</Button>}
                />
              ) : (
                <Row gutter={[isMobile ? 8 : 16, isMobile ? 8 : 12]}>
                  <Col span={24}>
                    <Card size="small" hoverable className="summary-card">
                      <Row gutter={[16, isMobile ? 16 : 12]}>
                        <Col xs={12} sm={12} md={6}>
                          <Statistic
                            title={t('pages.groups.totalGroups')}
                            value={String(totalGroups)}
                            prefix={<TagsOutlined />}
                          />
                        </Col>
                        <Col xs={12} sm={12} md={6}>
                          <Statistic
                            title={t('pages.groups.totalGroupedClients')}
                            value={String(totalClients)}
                            prefix={<TeamOutlined />}
                          />
                        </Col>
                        <Col xs={12} sm={12} md={6}>
                          <Statistic
                            title={t('pages.groups.totalUpDown')}
                            value={0}
                            formatter={() => (
                              <span>
                                <ArrowUpOutlined /> {SizeFormatter.sizeFormat(totalUpload)}
                                {' / '}
                                <ArrowDownOutlined /> {SizeFormatter.sizeFormat(totalDownload)}
                              </span>
                            )}
                          />
                        </Col>
                        <Col xs={12} sm={12} md={6}>
                          <Statistic
                            title={t('pages.groups.totalTraffic')}
                            value={SizeFormatter.sizeFormat(totalTraffic)}
                            prefix={<PieChartOutlined />}
                          />
                        </Col>
                      </Row>
                    </Card>
                  </Col>

                  <Col span={24}>
                    <Card
                      size="small"
                      hoverable
                      title={
                        <div className="card-toolbar">
                          <Button aria-label={t('pages.groups.addGroup')} type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                            {!isMobile && t('pages.groups.addGroup')}
                          </Button>
                        </div>
                      }
                    >
                      <Table<GroupSummary>
                        dataSource={groups}
                        columns={columns}
                        rowKey="name"
                        size="small"
                        pagination={false}
                        loading={loading}
                        locale={{
                          emptyText: (
                            <div className="card-empty">
                              <TagsOutlined style={{ fontSize: 32, marginBottom: 8 }} />
                              <div>{t('noData')}</div>
                            </div>
                          ),
                        }}
                      />
                    </Card>
                  </Col>
                </Row>
              )}
            </Spin>
          </Layout.Content>
        </Layout>

        <Modal
          open={createOpen}
          title={t('pages.groups.addGroup')}
          okText={t('create')}
          cancelText={t('cancel')}
          confirmLoading={createMut.isPending}
          onCancel={() => setCreateOpen(false)}
          onOk={confirmCreate}
          destroyOnHidden
        >
          <Form layout="vertical">
            <Form.Item label={t('pages.groups.name')}>
              <Input
                value={createForm.name}
                onChange={(e) => setCreateForm((v) => ({ ...v, name: e.target.value }))}
                onPressEnter={confirmCreate}
                placeholder={t('pages.clients.groupPlaceholder')}
                autoFocus
              />
            </Form.Item>
            <Form.Item label={t('pages.groups.descriptionLabel')}>
              <Input.TextArea
                value={createForm.description}
                onChange={(e) => setCreateForm((v) => ({ ...v, description: e.target.value }))}
                autoSize={{ minRows: 2, maxRows: 4 }}
              />
            </Form.Item>
            <Form.Item label={t('pages.groups.enabled')} valuePropName="checked">
              <Switch checked={createForm.enable} onChange={(enable) => setCreateForm((v) => ({ ...v, enable }))} />
            </Form.Item>
            <Form.Item label={t('pages.groups.assignedInbounds')}>
              <Select
                mode="multiple"
                value={createForm.assignedInboundIds}
                onChange={(assignedInboundIds) => setCreateForm((v) => ({ ...v, assignedInboundIds }))}
                options={(inboundOptionsQuery.data ?? []).map((ib) => ({
                  value: ib.id,
                  label: `${ib.remark || ib.tag} · ${ib.protocol}`,
                }))}
                loading={inboundOptionsQuery.isFetching}
              />
            </Form.Item>
            <Row gutter={12}>
              <Col span={12}>
                <Form.Item label={t('pages.groups.defaultTraffic')}>
                  <InputNumber min={0} value={createForm.defaultTotalGB} onChange={(defaultTotalGB) => setCreateForm((v) => ({ ...v, defaultTotalGB: Number(defaultTotalGB || 0) }))} style={{ width: '100%' }} />
                </Form.Item>
              </Col>
              <Col span={12}>
                <Form.Item label={t('pages.groups.defaultExpiry')}>
                  <InputNumber min={0} value={createForm.defaultExpiryTime} onChange={(defaultExpiryTime) => setCreateForm((v) => ({ ...v, defaultExpiryTime: Number(defaultExpiryTime || 0) }))} style={{ width: '100%' }} />
                </Form.Item>
              </Col>
            </Row>
          </Form>
        </Modal>

        <Modal
          open={renameOpen}
          title={renameTarget ? t('pages.groups.editTitle', { name: renameTarget.name }) : ''}
          okText={t('save')}
          cancelText={t('cancel')}
          confirmLoading={updateMut.isPending}
          onCancel={() => setRenameOpen(false)}
          onOk={confirmRename}
          destroyOnHidden
        >
          <Form layout="vertical">
            <Form.Item label={t('pages.groups.name')}>
              <Input
                value={renameForm.name}
                onChange={(e) => setRenameForm((v) => ({ ...v, name: e.target.value }))}
                onPressEnter={confirmRename}
                placeholder={t('pages.clients.groupPlaceholder')}
                autoFocus
              />
            </Form.Item>
            <Form.Item label={t('pages.groups.descriptionLabel')}>
              <Input.TextArea
                value={renameForm.description}
                onChange={(e) => setRenameForm((v) => ({ ...v, description: e.target.value }))}
                autoSize={{ minRows: 2, maxRows: 4 }}
              />
            </Form.Item>
            <Form.Item label={t('pages.groups.enabled')} valuePropName="checked">
              <Switch checked={renameForm.enable} onChange={(enable) => setRenameForm((v) => ({ ...v, enable }))} />
            </Form.Item>
            <Form.Item label={t('pages.groups.assignedInbounds')}>
              <Select
                mode="multiple"
                value={renameForm.assignedInboundIds}
                onChange={(assignedInboundIds) => setRenameForm((v) => ({ ...v, assignedInboundIds }))}
                options={(inboundOptionsQuery.data ?? []).map((ib) => ({
                  value: ib.id,
                  label: `${ib.remark || ib.tag} · ${ib.protocol}`,
                }))}
                loading={inboundOptionsQuery.isFetching}
              />
            </Form.Item>
            <Row gutter={12}>
              <Col span={12}>
                <Form.Item label={t('pages.groups.defaultTraffic')}>
                  <InputNumber min={0} value={renameForm.defaultTotalGB} onChange={(defaultTotalGB) => setRenameForm((v) => ({ ...v, defaultTotalGB: Number(defaultTotalGB || 0) }))} style={{ width: '100%' }} />
                </Form.Item>
              </Col>
              <Col span={12}>
                <Form.Item label={t('pages.groups.defaultExpiry')}>
                  <InputNumber min={0} value={renameForm.defaultExpiryTime} onChange={(defaultExpiryTime) => setRenameForm((v) => ({ ...v, defaultExpiryTime: Number(defaultExpiryTime || 0) }))} style={{ width: '100%' }} />
                </Form.Item>
              </Col>
            </Row>
          </Form>
        </Modal>

        <LazyMount when={subLinksOpen}>
          <SubLinksModal
            open={subLinksOpen}
            emails={groupEmails}
            clients={allClients}
            subSettings={subSettings}
            onOpenChange={setSubLinksOpen}
          />
        </LazyMount>

        <LazyMount when={adjustOpen}>
          <ClientBulkAdjustModal
            open={adjustOpen}
            count={groupEmails.length}
            onOpenChange={setAdjustOpen}
            onSubmit={async (addDays, addBytes) => {
              const msg = await bulkAdjust(groupEmails, addDays, addBytes);
              if (msg?.success) {
                const obj = msg.obj ?? { adjusted: 0 };
                messageApi.success(
                  t('pages.groups.adjustSuccess', {
                    count: obj.adjusted ?? 0,
                    name: groupForAction?.name ?? '',
                  }),
                );
                return obj;
              }
              return null;
            }}
          />
        </LazyMount>

        <LazyMount when={addClientsOpen}>
          <GroupAddClientsModal
            open={addClientsOpen}
            groupName={groupForAction?.name ?? null}
            candidates={allClients.filter((c) => c.group !== groupForAction?.name)}
            onClose={() => setAddClientsOpen(false)}
            onSubmit={async (emails) => {
              const msg = await bulkAddToGroup(emails, groupForAction?.name ?? '');
              if (msg?.success) {
                return (msg.obj as { affected?: number } | undefined) ?? { affected: 0 };
              }
              return null;
            }}
          />
        </LazyMount>

        <LazyMount when={removeClientsOpen}>
          <GroupRemoveClientsModal
            open={removeClientsOpen}
            groupName={groupForAction?.name ?? null}
            members={allClients.filter((c) => c.group === groupForAction?.name)}
            onClose={() => setRemoveClientsOpen(false)}
            onSubmit={async (emails) => {
              const msg = await bulkRemoveFromGroup(emails);
              if (msg?.success) {
                return (msg.obj as { affected?: number } | undefined) ?? { affected: 0 };
              }
              return null;
            }}
          />
        </LazyMount>
      </Layout>
    </ConfigProvider>
  );
}
