import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Divider, Form, Input, InputNumber, Select, Space, Switch, Tag } from 'antd';

import { HttpUtil } from '@/utils';
import type { NodeRecord } from '@/api/queries/useNodesQuery';
import {
  buildManagedDefaults,
  defaultManagedConfig,
  managedNodeBlockReason,
  MANAGED_PROTOCOL_LABELS,
  type ManagedFormValues,
  type ManagedNativeProtocol,
  ManagedFormSchema,
} from '@/lib/managed-protocols';
import '../managed/ManagedEndpointsPanel.css';

interface Props {
  protocol: ManagedNativeProtocol;
  mode: 'add' | 'edit';
  availableNodes: NodeRecord[];
  onSaved: () => void;
  onClose: () => void;
  setModalSaving?: (saving: boolean) => void;
  endpointId?: string;
  initialValues?: Partial<ManagedFormValues> & { config?: unknown };
}

const portProtocols = ['TCP', 'UDP'] as const;

function validationText(error: unknown): string {
  const err = error as { issues?: { message?: string }[] };
  return err.issues?.[0]?.message || 'Invalid managed endpoint configuration';
}

const redactedConfigKeys = /^(privateKey|publicKey|password|secret|token|raw|rawPath|rawConfig|clientConfig|ciphertext)$/i;

function sanitizeManagedConfig(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sanitizeManagedConfig);
  if (!value || typeof value !== 'object') return value;
  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>)
      .filter(([key]) => !redactedConfigKeys.test(key))
      .map(([key, nested]) => [key, sanitizeManagedConfig(nested)]),
  );
}

export function managedFormPayload(values: ManagedFormValues) {
  return {
    runtimeKind: values.runtimeKind,
    protocol: values.protocol,
    tag: values.tag,
    remark: values.remark || '',
    listen: values.listen || '',
    port: values.port,
    enable: values.enable,
    nodeId: values.nodeId ?? null,
    config: sanitizeManagedConfig(values.config),
  };
}

export default function ManagedInboundForm({
  protocol,
  mode,
  availableNodes,
  onSaved,
  onClose,
  setModalSaving,
  endpointId,
  initialValues,
}: Props) {
  const { t } = useTranslation();
  const [values, setValues] = useState<ManagedFormValues>(() => ({ ...buildManagedDefaults(protocol), ...initialValues, protocol, runtimeKind: protocol }) as ManagedFormValues);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const selectableNodes = useMemo(() => (availableNodes || []).filter((n) => n.enable), [availableNodes]);
  const selectedNode = selectableNodes.find((n) => n.id === values.nodeId);
  const blockReason = managedNodeBlockReason(selectedNode, protocol);

  useEffect(() => {
    const next = { ...buildManagedDefaults(protocol), ...initialValues, protocol, runtimeKind: protocol } as ManagedFormValues;
    setValues(next);
    setError('');
  }, [protocol, initialValues]);

  useEffect(() => {
    setModalSaving?.(saving);
  }, [saving, setModalSaving]);

  const setValue = (key: keyof ManagedFormValues, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: value }) as ManagedFormValues);
  };
  const setConfig = (key: string, value: unknown) => {
    setValues((prev) => ({ ...prev, config: { ...(prev.config as Record<string, unknown>), [key]: value } }) as ManagedFormValues);
  };
  const setFirstMieruBinding = (key: 'protocol' | 'portRange', value: unknown) => {
    setValues((prev) => {
      const config = { ...(prev.config as Record<string, unknown>) };
      const bindings = Array.isArray(config.portBindings) ? [...config.portBindings] as Record<string, unknown>[] : [{}];
      bindings[0] = { ...(bindings[0] ?? {}), [key]: value };
      if (key === 'portRange' && value) delete bindings[0].port;
      if (key === 'portRange' && !value) bindings[0].port = prev.port;
      config.portBindings = bindings;
      return { ...prev, config } as ManagedFormValues;
    });
  };

  const onPortChange = (port: number | null) => {
    const nextPort = Number(port) || 0;
    setValues((prev) => {
      const config = { ...(prev.config as Record<string, unknown>) };
      if (prev.protocol === 'amneziawg') config.listenPort = nextPort;
      if (prev.protocol === 'naiveproxy') config.port = nextPort;
      if (prev.protocol === 'mieru') {
        const bindings = Array.isArray(config.portBindings) ? [...config.portBindings] as Record<string, unknown>[] : [];
        if (bindings[0] && !bindings[0].portRange) bindings[0] = { ...bindings[0], port: nextPort };
        config.portBindings = bindings;
      }
      return { ...prev, port: nextPort, config } as ManagedFormValues;
    });
  };

  const resetProtocolDefaults = () => {
    setValues((prev) => ({ ...prev, config: defaultManagedConfig(protocol, prev.port) }) as ManagedFormValues);
  };

  const submit = async () => {
    setError('');
    if (blockReason) {
      setError(t('managedProtocols.capabilityUnavailable'));
      return;
    }
    const parsed = ManagedFormSchema.safeParse(values);
    if (!parsed.success) {
      setError(validationText(parsed.error));
      return;
    }
    setSaving(true);
    try {
      const payload = managedFormPayload(parsed.data);
      const msg = mode === 'edit' && endpointId
        ? await HttpUtil.patch(`/panel/api/managed-endpoints/${endpointId}`, payload)
        : await HttpUtil.post('/panel/api/managed-endpoints/create', payload);
      if (msg?.success) {
        onSaved();
        onClose();
      } else {
        setError(msg?.msg || t('managedProtocols.invalidConfig'));
      }
    } catch (err) {
      setError((err as Error).message || t('managedProtocols.invalidConfig'));
    } finally {
      setSaving(false);
    }
  };

  const cfg = values.config as Record<string, unknown>;
  const nodeOptions = selectableNodes.map((node) => ({
    value: node.id,
    label: node.name || `#${node.id}`,
  }));

  return (
    <div className="managed-inbound-form">
      <Alert
        type="info"
        showIcon
        message={t('managedProtocols.branch')}
        description={mode === 'edit'
          ? t('managedProtocols.editDescription')
          : t('managedProtocols.description', { protocol: MANAGED_PROTOCOL_LABELS[protocol] })}
      />
      {error && <Alert className="mt-12" type="error" showIcon message={error} />}
      {blockReason && <Alert className="mt-12" type="warning" showIcon message={t('managedProtocols.capabilityUnavailable')} />}

      <Form colon={false} labelCol={{ sm: { span: 8 } }} wrapperCol={{ sm: { span: 14 } }} labelWrap className="mt-12">
        <Form.Item label={t('pages.inbounds.protocol')}>
          <Space>
            <strong>{MANAGED_PROTOCOL_LABELS[protocol]}</strong>
            <Tag color="blue">{t('managedProtocols.badge')}</Tag>
          </Space>
        </Form.Item>
        <Form.Item label={t('managedProtocols.tag')} required>
          <Input aria-label={t('managedProtocols.tag')} value={values.tag} onChange={(e) => setValue('tag', e.target.value)} />
        </Form.Item>
        <Form.Item label={t('pages.inbounds.remark')}>
          <Input aria-label={t('pages.inbounds.remark')} value={values.remark} onChange={(e) => setValue('remark', e.target.value)} />
        </Form.Item>
        <Form.Item label={t('enable')}>
          <Switch checked={values.enable} onChange={(checked) => setValue('enable', checked)} />
        </Form.Item>
        {nodeOptions.length > 0 && (
          <Form.Item label={t('pages.inbounds.deployTo')}>
            <Select
              id="nodeId"
              allowClear
              placeholder={t('pages.inbounds.localPanel')}
              value={values.nodeId ?? undefined}
              options={nodeOptions}
              onChange={(value) => setValue('nodeId', value ?? null)}
            />
          </Form.Item>
        )}
        <Form.Item label={t('managedProtocols.listenIpv4')}>
          <Input aria-label={t('managedProtocols.listenIpv4')} value={values.listen} placeholder={t('managedProtocols.listenIpv4Placeholder')} onChange={(e) => setValue('listen', e.target.value)} />
        </Form.Item>
        <Form.Item label={t('pages.inbounds.port')} required>
          <InputNumber aria-label={t('pages.inbounds.port')} min={1} max={65535} value={values.port} onChange={onPortChange} />
        </Form.Item>
        <Divider />

        {protocol === 'amneziawg' && (
          <>
            <Form.Item label={t('managedProtocols.interface')}><Input aria-label={t('managedProtocols.interface')} value="awg0" disabled /></Form.Item>
            <Form.Item label={t('managedProtocols.ipv4Address')}><Input aria-label={t('managedProtocols.ipv4Address')} value={String(cfg.ipv4Address ?? '')} onChange={(e) => setConfig('ipv4Address', e.target.value)} /></Form.Item>
            <Form.Item label={t('managedProtocols.ipv4Pool')}><Input aria-label={t('managedProtocols.ipv4Pool')} value={String(cfg.ipv4Pool ?? '')} onChange={(e) => setConfig('ipv4Pool', e.target.value)} /></Form.Item>
            <Form.Item label={t('managedProtocols.mtu')}><InputNumber aria-label={t('managedProtocols.mtu')} min={576} max={9000} value={Number(cfg.mtu)} onChange={(v) => setConfig('mtu', Number(v) || 0)} /></Form.Item>
            <Form.Item label={t('managedProtocols.listenPort')}><InputNumber aria-label={t('managedProtocols.listenPort')} min={1} max={65535} value={Number(cfg.listenPort)} onChange={(v) => { setConfig('listenPort', Number(v) || 0); setValue('port', Number(v) || 0); }} /></Form.Item>
            <Form.Item label={t('managedProtocols.persistentKeepalive')}><InputNumber aria-label={t('managedProtocols.persistentKeepalive')} min={0} max={65535} value={Number(cfg.persistentKeepalive)} onChange={(v) => setConfig('persistentKeepalive', Number(v) || 0)} /></Form.Item>
            <Form.Item label={t('managedProtocols.clientAllowedIps')}><Input aria-label={t('managedProtocols.clientAllowedIps')} value={String(cfg.clientAllowedIPs ?? '')} onChange={(e) => setConfig('clientAllowedIPs', e.target.value)} /></Form.Item>
            <Form.Item label={t('managedProtocols.dnsIpv4')}><Input aria-label={t('managedProtocols.dnsIpv4')} value={String(cfg.dns ?? '')} onChange={(e) => setConfig('dns', e.target.value)} /></Form.Item>
            {(['jc', 'jmin', 'jmax', 's1', 's2', 's3', 's4'] as const).map((key) => (
              <Form.Item key={key} label={key === 'jc' ? 'Jc' : key.toUpperCase()}>
                <InputNumber aria-label={key === 'jc' ? 'Jc' : key.toUpperCase()} value={Number(cfg[key])} onChange={(v) => setConfig(key, Number(v) || 0)} />
              </Form.Item>
            ))}
            {(['h1', 'h2', 'h3', 'h4'] as const).map((key) => (
              <Form.Item key={key} label={key.toUpperCase()}>
                <Input aria-label={key.toUpperCase()} value={String(cfg[key] ?? '')} onChange={(e) => setConfig(key, e.target.value)} />
              </Form.Item>
            ))}
          </>
        )}

        {protocol === 'mieru' && (
          <>
            <Form.Item label={t('managedProtocols.transport')}>
              <Select
                aria-label={t('managedProtocols.transport')}
                value={String(((cfg.portBindings as { protocol?: string }[] | undefined)?.[0]?.protocol) ?? 'TCP')}
                options={portProtocols.map((value) => ({ value, label: value }))}
                onChange={(value) => setFirstMieruBinding('protocol', value)}
              />
            </Form.Item>
            <Form.Item label={t('managedProtocols.portRange')}>
              <Input aria-label={t('managedProtocols.portRange')} value={String(((cfg.portBindings as { portRange?: string }[] | undefined)?.[0]?.portRange) ?? '')} placeholder={t('managedProtocols.portRangePlaceholder')} onChange={(e) => setFirstMieruBinding('portRange', e.target.value || undefined)} />
            </Form.Item>
            <Form.Item label={t('managedProtocols.mtu')}>
              <InputNumber aria-label={t('managedProtocols.mtu')} min={1280} max={1500} value={Number(cfg.mtu)} onChange={(v) => setConfig('mtu', Number(v) || undefined)} />
            </Form.Item>
          </>
        )}

        {protocol === 'naiveproxy' && (
          <>
            <Form.Item label={t('managedProtocols.domain')} required><Input aria-label={t('managedProtocols.domain')} value={String(cfg.domain ?? '')} onChange={(e) => setConfig('domain', e.target.value)} /></Form.Item>
            <Form.Item label={t('managedProtocols.sni')}><Input aria-label={t('managedProtocols.sni')} value={String(cfg.sni ?? '')} onChange={(e) => setConfig('sni', e.target.value)} /></Form.Item>
            <Form.Item label={t('managedProtocols.tlsMode')}>
              <Select aria-label={t('managedProtocols.tlsMode')} value={String(cfg.tlsMode ?? 'acme')} options={[{ value: 'acme', label: 'ACME' }, { value: 'managed', label: t('managedProtocols.managedTls') }]} onChange={(value) => setConfig('tlsMode', value)} />
            </Form.Item>
            <Form.Item label={t('managedProtocols.acmeContact')}><Input aria-label={t('managedProtocols.acmeContact')} value={String(cfg.acmeEmail ?? '')} onChange={(e) => setConfig('acmeEmail', e.target.value)} /></Form.Item>
          </>
        )}

        <Form.Item wrapperCol={{ sm: { offset: 8, span: 14 } }}>
          <Space>
            <Button onClick={resetProtocolDefaults}>{t('managedProtocols.regenerate')}</Button>
            <Button type="primary" loading={saving} disabled={!!blockReason} onClick={submit}>
              {mode === 'edit' ? t('save') : t('create')}
            </Button>
          </Space>
        </Form.Item>
      </Form>
    </div>
  );
}
