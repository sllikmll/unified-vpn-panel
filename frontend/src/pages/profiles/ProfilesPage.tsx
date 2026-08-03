import { useCallback, useEffect, useMemo, useState } from 'react';
import { Controller, FormProvider } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Col,
  ConfigProvider,
  Input,
  InputNumber,
  Layout,
  Modal,
  Result,
  Row,
  Space,
  Spin,
  Statistic,
  Switch,
  Table,
  Tag,
  Tooltip,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  CheckCircleOutlined,
  CopyOutlined,
  DeleteOutlined,
  EditOutlined,
  FileTextOutlined,
  PlusOutlined,
  StopOutlined,
} from '@ant-design/icons';

import { useConfigProfileMutations } from '@/api/queries/useConfigProfileMutations';
import { useConfigProfilesQuery, type ConfigProfile } from '@/api/queries/useConfigProfilesQuery';
import { FormField, useZodForm } from '@/components/form/rhf';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { useTheme } from '@/hooks/useTheme';
import AppSidebar from '@/layouts/AppSidebar';
import { ConfigProfileFormSchema, type ConfigProfileFormValues } from '@/schemas/api/config-profile';
import { setMessageInstance } from '@/utils/messageBus';
import type { Msg } from '@/utils';
import './ProfilesPage.css';

const DEFAULT_PROFILE = JSON.stringify({
  inbounds: [
    {
      protocol: 'vless',
      port: 443,
      listen: '',
      streamSettings: {
        network: 'tcp',
        security: 'reality',
      },
      settings: {
        clients: [],
        decryption: 'none',
      },
    },
  ],
}, null, 2);

function formatJson(value: string): string {
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
}

function profileToForm(profile: ConfigProfile | null): ConfigProfileFormValues {
  if (!profile) {
    return {
      name: '',
      description: '',
      enabled: true,
      version: 1,
      profile: DEFAULT_PROFILE,
    };
  }
  return {
    name: profile.name,
    description: profile.description || '',
    enabled: profile.enabled,
    version: profile.version || 1,
    profile: formatJson(profile.profile),
  };
}

interface ProfileModalProps {
  open: boolean;
  mode: 'create' | 'edit';
  profile: ConfigProfile | null;
  save: (values: ConfigProfileFormValues) => Promise<Msg<unknown>>;
  onOpenChange: (open: boolean) => void;
}

function ProfileModal({ open, mode, profile, save, onOpenChange }: ProfileModalProps) {
  const { t } = useTranslation();
  const [saving, setSaving] = useState(false);
  const form = useZodForm(ConfigProfileFormSchema, {
    defaultValues: profileToForm(profile),
  });

  useEffect(() => {
    if (open) form.reset(profileToForm(profile));
  }, [open, profile, form]);

  const onSubmit = form.handleSubmit(async (values) => {
    setSaving(true);
    try {
      const msg = await save(values);
      if (msg.success) {
        onOpenChange(false);
      }
    } finally {
      setSaving(false);
    }
  });

  return (
    <Modal
      open={open}
      title={t(mode === 'edit' ? 'pages.profiles.editProfile' : 'pages.profiles.createProfile')}
      okText={t('save')}
      cancelText={t('cancel')}
      confirmLoading={saving}
      width={820}
      onOk={() => onSubmit()}
      onCancel={() => onOpenChange(false)}
      destroyOnHidden
    >
      <FormProvider {...form}>
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          <FormField
            name="name"
            label={t('pages.profiles.fields.name')}
          >
            <Input maxLength={128} />
          </FormField>
          <FormField
            name="description"
            label={t('pages.profiles.fields.descriptionLabel')}
          >
            <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} maxLength={1024} />
          </FormField>
          <Row gutter={12}>
            <Col xs={12}>
              <FormField
                name="version"
                label={t('pages.profiles.fields.version')}
                transform={{ output: (value) => Number(value) || 1 }}
              >
                <InputNumber min={1} precision={0} style={{ width: '100%' }} />
              </FormField>
            </Col>
            <Col xs={12}>
              <Controller
                name="enabled"
                control={form.control}
                render={({ field }) => (
                  <div style={{ paddingTop: 30 }}>
                    <Switch checked={field.value} onChange={field.onChange} />
                    <span style={{ marginInlineStart: 8 }}>{t('enabled')}</span>
                  </div>
                )}
              />
            </Col>
          </Row>
          <FormField
            name="profile"
            label={t('pages.profiles.fields.profile')}
          >
            <Input.TextArea
              className="profile-json-editor"
              autoSize={{ minRows: 14, maxRows: 24 }}
              spellCheck={false}
            />
          </FormField>
          <Button onClick={() => form.setValue('profile', formatJson(form.getValues('profile')), { shouldValidate: true })}>
            {t('pages.profiles.formatJson')}
          </Button>
        </Space>
      </FormProvider>
    </Modal>
  );
}

export default function ProfilesPage() {
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const { isMobile } = useMediaQuery();
  const [modal, modalContextHolder] = Modal.useModal();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => { setMessageInstance(messageApi); }, [messageApi]);

  const { profiles, loading, fetched, fetchError, refetch } = useConfigProfilesQuery();
  const { create, update, clone, remove, setEnable } = useConfigProfileMutations();
  const [formOpen, setFormOpen] = useState(false);
  const [formMode, setFormMode] = useState<'create' | 'edit'>('create');
  const [editingProfile, setEditingProfile] = useState<ConfigProfile | null>(null);

  const summary = useMemo(() => {
    const total = profiles.length;
    const enabled = profiles.filter((p) => p.enabled).length;
    return { total, enabled, disabled: total - enabled };
  }, [profiles]);

  const openCreate = useCallback(() => {
    setFormMode('create');
    setEditingProfile(null);
    setFormOpen(true);
  }, []);

  const openEdit = useCallback((profile: ConfigProfile) => {
    setFormMode('edit');
    setEditingProfile(profile);
    setFormOpen(true);
  }, []);

  const onSave = useCallback(async (values: ConfigProfileFormValues) => {
    if (formMode === 'edit' && editingProfile) {
      return update(editingProfile.id, values);
    }
    return create(values);
  }, [create, editingProfile, formMode, update]);

  const onClone = useCallback((profile: ConfigProfile) => {
    modal.confirm({
      title: t('pages.profiles.cloneConfirmTitle', { name: profile.name }),
      okText: t('copy'),
      cancelText: t('cancel'),
      onOk: async () => {
        const msg = await clone(profile.id, `${profile.name} copy`);
        if (msg?.success) messageApi.success(t('pages.profiles.toasts.clone'));
      },
    });
  }, [clone, messageApi, modal, t]);

  const onDelete = useCallback((profile: ConfigProfile) => {
    modal.confirm({
      title: t('pages.profiles.deleteConfirmTitle', { name: profile.name }),
      content: t('pages.profiles.deleteConfirmContent'),
      okText: t('delete'),
      okType: 'danger',
      cancelText: t('cancel'),
      onOk: async () => {
        const msg = await remove(profile.id);
        if (msg?.success) messageApi.success(t('pages.profiles.toasts.delete'));
      },
    });
  }, [messageApi, modal, remove, t]);

  const columns: ColumnsType<ConfigProfile> = [
    {
      title: t('pages.profiles.fields.actions'),
      key: 'actions',
      width: 132,
      render: (_, profile) => (
        <Space size={2}>
          <Tooltip title={t('edit')}>
            <Button size="small" type="text" icon={<EditOutlined />} aria-label={t('edit')} onClick={() => openEdit(profile)} />
          </Tooltip>
          <Tooltip title={t('copy')}>
            <Button size="small" type="text" icon={<CopyOutlined />} aria-label={t('copy')} onClick={() => onClone(profile)} />
          </Tooltip>
          <Tooltip title={t('delete')}>
            <Button size="small" type="text" danger icon={<DeleteOutlined />} aria-label={t('delete')} onClick={() => onDelete(profile)} />
          </Tooltip>
        </Space>
      ),
    },
    {
      title: t('pages.profiles.fields.enabled'),
      key: 'enabled',
      width: 92,
      render: (_, profile) => (
        <Switch size="small" checked={profile.enabled} onChange={(next) => setEnable(profile.id, next)} />
      ),
    },
    {
      title: t('pages.profiles.fields.name'),
      dataIndex: 'name',
      key: 'name',
      render: (_, profile) => (
        <div className="profile-name-cell">
          <span className="profile-name">{profile.name}</span>
          {profile.description ? <span className="profile-desc">{profile.description}</span> : null}
        </div>
      ),
    },
    {
      title: t('pages.profiles.fields.version'),
      dataIndex: 'version',
      key: 'version',
      width: 100,
      render: (version: number) => <Tag>v{version}</Tag>,
    },
    {
      title: t('pages.profiles.fields.profile'),
      dataIndex: 'profile',
      key: 'profile',
      render: (profile: string) => <div className="profile-json-preview">{profile}</div>,
    },
  ];

  const pageClass = useMemo(() => {
    const classes = ['profiles-page'];
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
            <Spin spinning={!fetched} delay={200} size="large">
              {!fetched ? (
                <div className="loading-spacer" />
              ) : fetchError ? (
                <Result
                  status="error"
                  title={t('somethingWentWrong')}
                  subTitle={fetchError}
                  extra={<Button type="primary" loading={loading} onClick={() => refetch()}>{t('refresh')}</Button>}
                />
              ) : (
                <Row gutter={[isMobile ? 8 : 16, isMobile ? 8 : 12]}>
                  <Col span={24}>
                    <Card size="small" hoverable className="summary-card">
                      <Row gutter={[16, 12]}>
                        <Col xs={8}>
                          <Statistic title={t('pages.profiles.summary.total')} value={String(summary.total)} prefix={<FileTextOutlined />} />
                        </Col>
                        <Col xs={8}>
                          <Statistic title={t('pages.profiles.summary.enabled')} value={String(summary.enabled)} prefix={<CheckCircleOutlined style={{ color: 'var(--ant-color-success)' }} />} />
                        </Col>
                        <Col xs={8}>
                          <Statistic title={t('pages.profiles.summary.disabled')} value={String(summary.disabled)} prefix={<StopOutlined style={{ color: 'var(--ant-color-text-quaternary)' }} />} />
                        </Col>
                      </Row>
                    </Card>
                  </Col>
                  <Col span={24}>
                    <Card
                      size="small"
                      hoverable
                      title={(
                        <div className="card-toolbar">
                          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                            {!isMobile && t('pages.profiles.createProfile')}
                          </Button>
                        </div>
                      )}
                      className="profiles-card"
                    >
                      <Table<ConfigProfile>
                        rowKey="id"
                        size="small"
                        loading={loading}
                        columns={columns}
                        dataSource={profiles}
                        pagination={false}
                        scroll={{ x: 'max-content' }}
                        locale={{
                          emptyText: (
                            <div className="card-empty">
                              <FileTextOutlined style={{ fontSize: 32, marginBottom: 8 }} />
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
        <ProfileModal
          open={formOpen}
          mode={formMode}
          profile={editingProfile}
          save={onSave}
          onOpenChange={setFormOpen}
        />
      </Layout>
    </ConfigProvider>
  );
}
