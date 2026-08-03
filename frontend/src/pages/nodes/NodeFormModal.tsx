import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Col,
  Form,
  Input,
  InputNumber,
  Modal,
  Row,
  Select,
  Space,
  Switch,
  Tag,
  Typography,
  message,
} from 'antd';
import { FormProvider, useForm, useWatch } from 'react-hook-form';
import type { NodeRecord } from '@/api/queries/useNodesQuery';
import type { RemoteInboundOption } from '@/api/queries/useNodeMutations';
import type { Msg } from '@/utils';
import { NodeFormSchema, type NodeFormValues, type NodePreflightResult, type ProbeResult } from '@/schemas/node';
import { FormField, rhfZodValidate } from '@/components/form/rhf';
import { useOutboundTagGroups } from '@/api/queries/useOutboundTags';
import './NodeFormModal.css';

type Mode = 'add' | 'edit';

interface NodeFormModalProps {
  open: boolean;
  mode: Mode;
  node: NodeRecord | null;
  testConnection: (payload: Partial<NodeRecord>) => Promise<Msg<ProbeResult>>;
  fetchFingerprint: (payload: Partial<NodeRecord>) => Promise<Msg<string>>;
  fetchInbounds: (payload: Partial<NodeRecord>) => Promise<Msg<RemoteInboundOption[]>>;
  preflight: (payload: Record<string, unknown>) => Promise<Msg<NodePreflightResult>>;
  save: (payload: Partial<NodeRecord>) => Promise<Msg<unknown>>;
  onOpenChange: (open: boolean) => void;
}

function defaultValues(): NodeFormValues {
  return {
    id: 0,
    name: '',
    remark: '',
    scheme: 'https',
    address: '',
    port: 2053,
    basePath: '/',
    apiToken: '',
    hasStoredToken: false,
    enable: true,
    allowPrivateAddress: false,
    tlsVerifyMode: 'verify',
    pinnedCertSha256: '',
    inboundSyncMode: 'all',
    inboundTags: [],
    outboundTag: '',
    sshUsername: 'root',
    sshPort: 22,
    sshAuthMethod: 'privateKey',
    sshPassword: '',
    sshPrivateKey: '',
    sshPrivateKeyPassphrase: '',
    sshHostKeyMode: 'known_hosts',
    sshKnownHosts: '',
    sshHostKeyFingerprint: '',
  };
}

export default function NodeFormModal({
  open,
  mode,
  node,
  testConnection,
  fetchFingerprint,
  fetchInbounds,
  preflight,
  save,
  onOpenChange,
}: NodeFormModalProps) {
  const { t } = useTranslation();
  const methods = useForm<NodeFormValues>({ defaultValues: defaultValues() });
  const [messageApi, messageContextHolder] = message.useMessage();

  const [submitting, setSubmitting] = useState(false);
  const [testing, setTesting] = useState(false);
  const [fetchingPin, setFetchingPin] = useState(false);
  const [fetchingInbounds, setFetchingInbounds] = useState(false);
  const [preflighting, setPreflighting] = useState(false);
  const [inboundOptions, setInboundOptions] = useState<RemoteInboundOption[]>([]);
  const [testResult, setTestResult] = useState<ProbeResult | null>(null);
  const [preflightResult, setPreflightResult] = useState<NodePreflightResult | null>(null);
  const scheme = useWatch({ control: methods.control, name: 'scheme' }) ?? 'https';
  const tlsVerifyMode = useWatch({ control: methods.control, name: 'tlsVerifyMode' }) ?? 'verify';
  const inboundSyncMode = useWatch({ control: methods.control, name: 'inboundSyncMode' }) ?? 'all';
  const sshAuthMethod = useWatch({ control: methods.control, name: 'sshAuthMethod' }) ?? 'privateKey';
  const sshHostKeyMode = useWatch({ control: methods.control, name: 'sshHostKeyMode' }) ?? 'known_hosts';
  const { data: outboundGroups } = useOutboundTagGroups({ excludeBlackhole: true });

  // Outbounds and balancers share one picker (like the panel-outbound selector);
  // when balancers exist they get a labeled group so it's clear the selection
  // routes through a balancer. Empty falls back to the placeholder ("Direct
  // connection") rather than a synthetic option, so it can't read as a second
  // "direct" next to a real freedom outbound.
  const outboundOptions = useMemo<
    ({ label: string; value: string } | { label: string; options: { label: string; value: string }[] })[]
  >(() => {
    const outOpts = (outboundGroups?.outbounds ?? []).map((tag) => ({ label: tag, value: tag }));
    if (!outboundGroups?.balancers.length) return outOpts;
    return [
      { label: t('pages.xray.Outbounds'), options: outOpts },
      { label: t('pages.xray.Balancers'), options: outboundGroups.balancers.map((tag) => ({ label: tag, value: tag })) },
    ];
  }, [outboundGroups, t]);

  useEffect(() => {
    if (!open) return;
    const base = defaultValues();
    const next: NodeFormValues = mode === 'edit' && node
      ? {
        ...base,
        ...(node as unknown as Partial<NodeFormValues>),
        id: node.id,
        scheme: (node.scheme as 'http' | 'https') || base.scheme,
        inboundSyncMode: (node.inboundSyncMode as 'all' | 'selected') || base.inboundSyncMode,
        inboundTags: node.inboundTags ?? [],
        apiToken: '',
        hasStoredToken: node.hasApiToken ?? false,
      }
      : base;
    if (next.scheme === 'http') next.tlsVerifyMode = 'skip';
    methods.reset(next);
    setInboundOptions((next.inboundTags || []).map((tag) => ({ tag })));
    setTestResult(null);
    setPreflightResult(null);
  }, [open, mode, node, methods]);

  const title = useMemo(
    () => (mode === 'edit' ? t('pages.nodes.editNode') : t('pages.nodes.addNode')),
    [mode, t],
  );

  const editingWithToken = mode === 'edit' && Boolean(node?.hasApiToken);

  function buildPayload(values: NodeFormValues): Partial<NodeRecord> {
    const token = values.apiToken.trim();
    const payload: Partial<NodeRecord> = {
      id: values.id || 0,
      name: values.name.trim(),
      remark: values.remark?.trim() || '',
      scheme: values.scheme,
      address: values.address.trim(),
      port: values.port,
      basePath: values.basePath.trim() || '/',
      enable: values.enable,
      allowPrivateAddress: values.allowPrivateAddress,
      tlsVerifyMode: values.tlsVerifyMode,
      pinnedCertSha256: values.tlsVerifyMode === 'pin' ? values.pinnedCertSha256.trim() : '',
      inboundSyncMode: values.inboundSyncMode,
      inboundTags: values.inboundSyncMode === 'selected' ? values.inboundTags : [],
      outboundTag: values.outboundTag || '',
    };
    if (token) payload.apiToken = token;
    return payload;
  }

  function buildPreflightPayload(values: NodeFormValues): Record<string, unknown> {
    const payload: Record<string, unknown> = {
      address: values.address.trim(),
      port: values.sshPort || 22,
      username: values.sshUsername?.trim() || 'root',
      authMethod: values.sshAuthMethod || 'privateKey',
      hostKeyMode: values.sshHostKeyMode || 'known_hosts',
      timeoutSeconds: 12,
    };
    if (values.sshAuthMethod === 'password') {
      payload.password = values.sshPassword || '';
    } else {
      payload.privateKey = values.sshPrivateKey || '';
      if (values.sshPrivateKeyPassphrase) payload.privateKeyPassphrase = values.sshPrivateKeyPassphrase;
    }
    if (values.sshHostKeyMode === 'known_hosts') payload.knownHosts = values.sshKnownHosts || '';
    if (values.sshHostKeyMode === 'pin') payload.hostKeyFingerprint = values.sshHostKeyFingerprint || '';
    return payload;
  }

  async function onTest() {
    if (!(await methods.trigger(['name', 'address', 'port']))) return;
    setTesting(true);
    setTestResult(null);
    try {
      const payload = buildPayload(methods.getValues());
      const msg = await testConnection(payload);
      if (msg?.success && msg.obj) {
        setTestResult(msg.obj);
      } else {
        setTestResult({ status: 'offline', error: msg?.msg || 'unknown error' });
      }
    } finally {
      setTesting(false);
    }
  }

  async function onFetchPin() {
    if (!(await methods.trigger(['name', 'address', 'port']))) return;
    setFetchingPin(true);
    try {
      const payload = buildPayload(methods.getValues());
      const msg = await fetchFingerprint(payload);
      if (msg?.success && msg.obj) {
        methods.setValue('pinnedCertSha256', msg.obj);
        messageApi.success(t('pages.nodes.pinFetched'));
      } else {
        messageApi.error(msg?.msg || t('pages.nodes.pinFetchFailed'));
      }
    } finally {
      setFetchingPin(false);
    }
  }

  async function onFetchInbounds() {
    if (!(await methods.trigger(['name', 'address', 'port', 'apiToken']))) return;
    setFetchingInbounds(true);
    try {
      const msg = await fetchInbounds(buildPayload(methods.getValues()));
      if (msg?.success && Array.isArray(msg.obj)) {
        setInboundOptions(msg.obj);
        messageApi.success(t('pages.nodes.inboundsLoaded', { count: msg.obj.length }));
      } else {
        messageApi.error(msg?.msg || t('pages.nodes.inboundsLoadFailed'));
      }
    } finally {
      setFetchingInbounds(false);
    }
  }

  async function onPreflight() {
    if (!(await methods.trigger(['address', 'sshPort', 'sshUsername']))) return;
    setPreflighting(true);
    setPreflightResult(null);
    try {
      const msg = await preflight(buildPreflightPayload(methods.getValues()));
      if (msg?.success && msg.obj) {
        setPreflightResult(msg.obj);
        messageApi.success(t('pages.nodes.preflightComplete'));
      } else {
        messageApi.error(msg?.msg || t('pages.nodes.preflightFailed'));
      }
    } finally {
      setPreflighting(false);
    }
  }

  async function onFinish(values: NodeFormValues) {
    const result = NodeFormSchema.safeParse(values);
    if (!result.success) {
      messageApi.error(t(result.error.issues[0]?.message ?? 'pages.nodes.toasts.fillRequired'));
      return;
    }
    setSubmitting(true);
    try {
      const payload = buildPayload(result.data);
      const test = await testConnection(payload);
      const probe = test?.success ? test.obj : null;
      if (!probe || probe.status !== 'online') {
        setTestResult(probe ?? { status: 'offline', error: test?.msg || t('pages.nodes.connectionFailed') });
        return;
      }
      setTestResult(probe);
      const msg = await save(payload);
      if (msg?.success) {
        onOpenChange(false);
      }
    } finally {
      setSubmitting(false);
    }
  }

  function close() {
    if (!submitting) onOpenChange(false);
  }

  return (
    <>
      {messageContextHolder}
      <Modal
        open={open}
        title={title}
        confirmLoading={submitting}
        okText={t('save')}
        cancelText={t('cancel')}
        mask={{ closable: false }}
        width="640px"
        onOk={methods.handleSubmit(onFinish)}
        onCancel={close}
      >
        <FormProvider {...methods}>
          <Form layout="vertical">
            <Row gutter={16}>
              <Col xs={24} md={12}>
                <FormField
                  label={t('pages.nodes.name')}
                  name="name"
                  rules={{ validate: rhfZodValidate(NodeFormSchema.shape.name) }}
                >
                  <Input placeholder={t('pages.nodes.namePlaceholder')} />
                </FormField>
              </Col>
              <Col xs={24} md={12}>
                <FormField label={t('pages.nodes.remark')} name="remark">
                  <Input />
                </FormField>
              </Col>
            </Row>

            <Row gutter={16}>
              <Col xs={24} md={6}>
                <FormField
                  label={t('pages.nodes.scheme')}
                  name="scheme"
                  onAfterChange={(value) => {
                    if (value === 'http') methods.setValue('tlsVerifyMode', 'skip');
                  }}
                >
                  <Select
                    options={[
                      { value: 'https', label: 'https' },
                      { value: 'http', label: 'http' },
                    ]}
                  />
                </FormField>
              </Col>
              <Col xs={24} md={12}>
                <FormField
                  label={t('pages.nodes.address')}
                  name="address"
                  rules={{ validate: rhfZodValidate(NodeFormSchema.shape.address) }}
                >
                  <Input placeholder={t('pages.nodes.addressPlaceholder')} />
                </FormField>
              </Col>
              <Col xs={24} md={6}>
                <FormField
                  label={t('pages.nodes.port')}
                  name="port"
                  rules={{ validate: rhfZodValidate(NodeFormSchema.shape.port) }}
                >
                  <InputNumber min={1} max={65535} style={{ width: '100%' }} />
                </FormField>
              </Col>
            </Row>

            <Row gutter={16}>
              <Col xs={24} md={12}>
                <FormField label={t('pages.nodes.basePath')} name="basePath">
                  <Input placeholder="/" />
                </FormField>
              </Col>
              <Col xs={24} md={12}>
                <FormField
                  label={t('pages.nodes.enable')}
                  name="enable"
                  valueProp="checked"
                >
                  <Switch />
                </FormField>
              </Col>
            </Row>

            <FormField
              label={t('pages.nodes.allowPrivateAddress')}
              name="allowPrivateAddress"
              valueProp="checked"
              tooltip={t('pages.nodes.allowPrivateAddressHint')}
            >
              <Switch />
            </FormField>

            <FormField
              label={t('pages.nodes.tlsVerifyMode')}
              name="tlsVerifyMode"
              tooltip={t('pages.nodes.tlsVerifyModeHint')}
            >
              <Select
                disabled={scheme === 'http'}
                options={[
                  { value: 'verify', label: t('pages.nodes.tlsVerify') },
                  { value: 'pin', label: t('pages.nodes.tlsPin') },
                  { value: 'skip', label: t('pages.nodes.tlsSkip') },
                  { value: 'mtls', label: t('pages.nodes.tlsMtls') },
                ]}
              />
            </FormField>

            {tlsVerifyMode === 'skip' && (
              <Alert
                type="warning"
                showIcon
                style={{ marginBottom: 16 }}
                title={t('pages.nodes.tlsSkipWarning')}
              />
            )}

            {tlsVerifyMode === 'mtls' && (
              <Alert
                type="info"
                showIcon
                style={{ marginBottom: 16 }}
                title={t('pages.nodes.mtlsFormHint')}
              />
            )}

            {tlsVerifyMode === 'pin' && (
              <FormField
                label={t('pages.nodes.pinnedCert')}
                name="pinnedCertSha256"
                tooltip={t('pages.nodes.pinnedCertHint')}
              >
                <Input.Search
                  placeholder={t('pages.nodes.pinnedCertPlaceholder')}
                  enterButton={t('pages.nodes.fetchPin')}
                  loading={fetchingPin}
                  onSearch={onFetchPin}
                />
              </FormField>
            )}

            <FormField
              label={t('pages.nodes.apiToken')}
              name="apiToken"
              rules={{ validate: rhfZodValidate(NodeFormSchema.shape.apiToken) }}
              tooltip={t('pages.nodes.apiTokenHint')}
              extra={editingWithToken ? t('pages.nodes.apiTokenKeepHint') : undefined}
            >
              <Input.Password
                placeholder={editingWithToken ? t('pages.nodes.apiTokenKeepHint') : t('pages.nodes.apiTokenPlaceholder')}
              />
            </FormField>

            <Typography.Title level={5}>{t('pages.nodes.sshPreflight')}</Typography.Title>
            <Row gutter={16}>
              <Col xs={24} md={12}>
                <FormField label={t('pages.nodes.sshUsername')} name="sshUsername">
                  <Input placeholder="root" />
                </FormField>
              </Col>
              <Col xs={24} md={12}>
                <FormField label={t('pages.nodes.sshPort')} name="sshPort">
                  <InputNumber min={1} max={65535} style={{ width: '100%' }} />
                </FormField>
              </Col>
            </Row>

            <Row gutter={16}>
              <Col xs={24} md={12}>
                <FormField label={t('pages.nodes.sshAuthMethod')} name="sshAuthMethod">
                  <Select
                    options={[
                      { value: 'privateKey', label: t('pages.nodes.sshPrivateKey') },
                      { value: 'password', label: t('pages.nodes.sshPassword') },
                    ]}
                  />
                </FormField>
              </Col>
              <Col xs={24} md={12}>
                <FormField label={t('pages.nodes.sshHostKeyMode')} name="sshHostKeyMode">
                  <Select
                    options={[
                      { value: 'known_hosts', label: t('pages.nodes.sshKnownHosts') },
                      { value: 'pin', label: t('pages.nodes.sshHostKeyPin') },
                      { value: 'insecure', label: t('pages.nodes.sshHostKeyInsecure') },
                    ]}
                  />
                </FormField>
              </Col>
            </Row>

            {sshAuthMethod === 'password' ? (
              <FormField label={t('pages.nodes.sshPassword')} name="sshPassword">
                <Input.Password />
              </FormField>
            ) : (
              <>
                <FormField label={t('pages.nodes.sshPrivateKey')} name="sshPrivateKey">
                  <Input.TextArea rows={5} style={{ fontFamily: 'monospace' }} />
                </FormField>
                <FormField label={t('pages.nodes.sshPrivateKeyPassphrase')} name="sshPrivateKeyPassphrase">
                  <Input.Password />
                </FormField>
              </>
            )}

            {sshHostKeyMode === 'known_hosts' && (
              <FormField label={t('pages.nodes.sshKnownHosts')} name="sshKnownHosts">
                <Input.TextArea rows={3} style={{ fontFamily: 'monospace' }} />
              </FormField>
            )}

            {sshHostKeyMode === 'pin' && (
              <FormField label={t('pages.nodes.sshHostKeyFingerprint')} name="sshHostKeyFingerprint">
                <Input placeholder="SHA256:..." />
              </FormField>
            )}

            {sshHostKeyMode === 'insecure' && (
              <Alert
                type="warning"
                showIcon
                style={{ marginBottom: 16 }}
                title={t('pages.nodes.sshHostKeyInsecureWarning')}
              />
            )}

            <div className="test-row">
              <Button type="default" loading={preflighting} onClick={onPreflight}>
                {t('pages.nodes.runPreflight')}
              </Button>
              {preflightResult && (
                <div className="test-result">
                  <Alert
                    type={preflightResult.provisioning?.canInstall ? 'success' : 'warning'}
                    showIcon
                    title={preflightResult.provisioning?.canInstall ? t('pages.nodes.preflightReady') : t('pages.nodes.preflightNeedsAttention')}
                    description={(
                      <Space direction="vertical" size={6}>
                        <Typography.Text>
                          {[preflightResult.hostname, preflightResult.os, preflightResult.arch].filter(Boolean).join(' / ')}
                        </Typography.Text>
                        <Space wrap>
                          <Tag color={preflightResult.root || preflightResult.sudo ? 'green' : 'red'}>{t('pages.nodes.preflightPrivilege')}</Tag>
                          <Tag color={preflightResult.systemd ? 'green' : 'red'}>systemd</Tag>
                          <Tag color={preflightResult.docker ? 'green' : 'gold'}>Docker</Tag>
                          <Tag>{t('pages.nodes.preflightDisk', { gb: (((preflightResult.freeDiskBytes ?? 0) / 1024 / 1024 / 1024).toFixed(1)) })}</Tag>
                        </Space>
                        {(preflightResult.occupiedPorts?.length ?? 0) > 0 && (
                          <Typography.Text type="secondary">
                            {t('pages.nodes.preflightPorts', { ports: preflightResult.occupiedPorts?.join(', ') })}
                          </Typography.Text>
                        )}
                        {(preflightResult.errors?.length ?? 0) > 0 && (
                          <Space direction="vertical" size={2}>
                            {preflightResult.errors?.map((err) => (
                              <Typography.Text type="danger" key={err.code}>{err.message}</Typography.Text>
                            ))}
                          </Space>
                        )}
                      </Space>
                    )}
                  />
                </div>
              )}
            </div>

            <FormField
              label={t('pages.nodes.outboundTag')}
              name="outboundTag"
              tooltip={t('pages.nodes.outboundTagHint')}
              transform={{ input: (v) => (v as string) || undefined }}
            >
              <Select
                allowClear
                showSearch
                placeholder={t('pages.nodes.outboundTagPlaceholder')}
                options={outboundOptions}
              />
            </FormField>

            <FormField
              label={t('pages.nodes.inboundSyncMode')}
              name="inboundSyncMode"
              tooltip={t('pages.nodes.inboundSyncModeHint')}
            >
              <Select
                options={[
                  { value: 'all', label: t('pages.nodes.allInbounds') },
                  { value: 'selected', label: t('pages.nodes.selectedInbounds') },
                ]}
              />
            </FormField>

            {inboundSyncMode === 'selected' && (
              <FormField
                label={t('pages.nodes.inboundTags')}
                name="inboundTags"
                tooltip={t('pages.nodes.inboundTagsHint')}
              >
                <Select
                  mode="multiple"
                  allowClear
                  loading={fetchingInbounds}
                  placeholder={t('pages.nodes.inboundTagsPlaceholder')}
                  popupRender={(menu) => (
                    <>
                      <Button type="text" block loading={fetchingInbounds} onClick={onFetchInbounds}>
                        {t('pages.nodes.loadInbounds')}
                      </Button>
                      {menu}
                    </>
                  )}
                  options={inboundOptions.map((inbound) => ({
                    value: inbound.tag,
                    label: `${inbound.remark || inbound.tag}${inbound.protocol ? ` (${inbound.protocol}:${inbound.port || 0})` : ''}`,
                  }))}
                />
              </FormField>
            )}

            <div className="test-row">
              <Button type="default" loading={testing} onClick={onTest}>
                {t('pages.nodes.testConnection')}
              </Button>
              {testResult && (
                <div className="test-result">
                  {testResult.status === 'online' ? (
                    <Alert
                      type="success"
                      showIcon
                      title={t('pages.nodes.connectionOk', { ms: testResult.latencyMs })}
                      description={testResult.xrayVersion ? `Xray ${testResult.xrayVersion}` : undefined}
                    />
                  ) : (
                    <Alert
                      type="error"
                      showIcon
                      title={t('pages.nodes.connectionFailed')}
                      description={testResult.error}
                    />
                  )}
                </div>
              )}
            </div>
          </Form>
        </FormProvider>
      </Modal>
    </>
  );
}
