import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Col,
  Empty,
  Input,
  Modal,
  Row,
  Segmented,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  Tooltip,
  Upload,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { UploadProps } from 'antd';
import {
  CopyOutlined,
  DeleteOutlined,
  DownloadOutlined,
  EyeOutlined,
  ImportOutlined,
  ReloadOutlined,
  SaveOutlined,
} from '@ant-design/icons';

import {
  buildProtocolExportURL,
  useProtocolConnectionMutations,
  useProtocolConnectionsQuery,
  type ProtocolConnectionView,
} from '@/api/queries/useProtocolConnections';
import AppSidebar from '@/layouts/AppSidebar';
import { setMessageInstance } from '@/utils/messageBus';
import './ProtocolLibraryPage.css';

const DEFAULT_PROTOCOLS = ['wireguard', 'amnezia', 'hysteria2', 'vless', 'trojan', 'mieru', 'naiveproxy', 'vmess', 'shadowsocks'];

function selectorText(values?: string[]): string {
  return (values || []).join(', ');
}

function parseSelectors(raw: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  raw.split(',').map((v) => v.trim()).filter(Boolean).forEach((item) => {
    if (!seen.has(item)) {
      seen.add(item);
      out.push(item);
    }
  });
  return out;
}

async function copyText(text: string) {
  await navigator.clipboard.writeText(text);
}

export default function ProtocolLibraryPage() {
  const { t } = useTranslation();
  const [messageApi, contextHolder] = message.useMessage();
  setMessageInstance(messageApi);

  const { connections, protocols, loading, fetchError, refetch } = useProtocolConnectionsQuery();
  const mutations = useProtocolConnectionMutations();
  const exportURL = buildProtocolExportURL(window.X_UI_BASE_PATH || '');
  const specs = protocols.length ? protocols : DEFAULT_PROTOCOLS.map((id) => ({ id, label: id, schemes: [], mihomoSupported: id !== 'mieru' }));
  const [active, setActive] = useState(specs[0]?.id || 'wireguard');
  const [name, setName] = useState('');
  const [content, setContent] = useState('');
  const [selectors, setSelectors] = useState('GLOBAL, AI Selector');
  const [saving, setSaving] = useState(false);
  const [preview, setPreview] = useState('');

  const filtered = useMemo(
    () => connections.filter((conn) => conn.protocol === active),
    [connections, active],
  );

  const activeSpec = specs.find((spec) => spec.id === active);

  const uploadProps: UploadProps = {
    accept: '.conf,.txt,.json,.yaml,.yml',
    showUploadList: false,
    beforeUpload: async (file) => {
      setContent(await file.text());
      if (!name) setName(file.name.replace(/\.[^.]+$/, ''));
      return false;
    },
  };

  const importConnection = async () => {
    setSaving(true);
    try {
      const msg = await mutations.importConnection({
        protocol: active,
        name,
        content,
        selectors: parseSelectors(selectors),
      });
      if (msg.success) {
        setContent('');
        setName('');
      }
    } finally {
      setSaving(false);
    }
  };

  const updateConnection = async (conn: ProtocolConnectionView, patch: { enabled?: boolean; selectors?: string[] }) => {
    await mutations.update(conn.id, patch);
  };

  const removeConnection = (conn: ProtocolConnectionView) => {
    Modal.confirm({
      title: t('pages.protocolLibrary.deleteConfirm'),
      content: conn.name,
      okText: t('delete'),
      cancelText: t('cancel'),
      okButtonProps: { danger: true },
      onOk: () => mutations.remove(conn.id),
    });
  };

  const loadPreview = async () => {
    const msg = await mutations.preview();
    if (msg.success) setPreview(msg.obj?.configPreview || msg.obj?.block || '');
  };

  const copyConnectionYAML = async (conn: ProtocolConnectionView) => {
    const revealed = await mutations.reveal(conn.id);
    await copyText(revealed.mihomoYaml || '');
  };

  const columns: ColumnsType<ProtocolConnectionView> = [
    {
      title: t('pages.protocolLibrary.fields.enabled'),
      width: 92,
      render: (_, conn) => <Switch size="small" checked={conn.enabled} onChange={(enabled) => updateConnection(conn, { enabled })} />,
    },
    {
      title: t('pages.protocolLibrary.fields.name'),
      render: (_, conn) => (
        <div className="protocol-library-name">
          <span>{conn.name}</span>
          <Space size={4} wrap>
            <Tag>{conn.protocolLabel || conn.protocol}</Tag>
            <Tag color={conn.mihomoSupported ? 'green' : 'orange'}>
              {conn.mihomoSupported ? t('pages.protocolLibrary.mihomoReady') : t('pages.protocolLibrary.storedOnly')}
            </Tag>
          </Space>
        </div>
      ),
    },
    {
      title: t('pages.protocolLibrary.fields.selectors'),
      render: (_, conn) => (
        <Input
          size="small"
          defaultValue={selectorText(conn.selectors)}
          onBlur={(event) => updateConnection(conn, { selectors: parseSelectors(event.target.value) })}
        />
      ),
    },
    {
      title: t('pages.protocolLibrary.fields.actions'),
      width: 190,
      render: (_, conn) => (
        <Space size={2}>
          <Tooltip title={t('pages.protocolLibrary.copyYaml')}>
            <Button type="text" icon={<CopyOutlined />} aria-label={t('pages.protocolLibrary.copyYaml')} onClick={() => copyConnectionYAML(conn)} />
          </Tooltip>
          <Tooltip title={t('pages.protocolLibrary.copyId')}>
            <Button type="text" icon={<EyeOutlined />} aria-label={t('pages.protocolLibrary.copyId')} onClick={() => copyText(conn.id)} />
          </Tooltip>
          <Tooltip title={t('delete')}>
            <Button type="text" danger icon={<DeleteOutlined />} aria-label={t('delete')} onClick={() => removeConnection(conn)} />
          </Tooltip>
        </Space>
      ),
    },
  ];

  return (
    <>
      {contextHolder}
      <AppSidebar />
      <main className="page-shell protocol-library-page">
        <div className="page-header-row">
          <div>
            <h1>{t('pages.protocolLibrary.title')}</h1>
            <p>{t('pages.protocolLibrary.subtitle')}</p>
          </div>
          <Space wrap>
            <Tooltip title={t('refresh')}>
              <Button icon={<ReloadOutlined />} onClick={() => refetch()} />
            </Tooltip>
            <Button icon={<DownloadOutlined />} href={exportURL}>
              {t('pages.protocolLibrary.exportYaml')}
            </Button>
          </Space>
        </div>

        <Segmented
          className="protocol-tabs"
          value={active}
          onChange={(value) => setActive(String(value))}
          options={specs.map((spec) => ({ value: spec.id, label: spec.label }))}
        />

        <Row gutter={[16, 16]} className="protocol-library-grid">
          <Col xs={24} xl={9}>
            <Card title={t('pages.protocolLibrary.importTitle')} extra={<Tag>{activeSpec?.label || active}</Tag>}>
              <Space direction="vertical" size={12} style={{ width: '100%' }}>
                <Input
                  value={name}
                  maxLength={128}
                  placeholder={t('pages.protocolLibrary.namePlaceholder')}
                  onChange={(event) => setName(event.target.value)}
                />
                <Input
                  value={selectors}
                  placeholder={t('pages.protocolLibrary.selectorsPlaceholder')}
                  onChange={(event) => setSelectors(event.target.value)}
                />
                <Input.TextArea
                  value={content}
                  spellCheck={false}
                  autoSize={{ minRows: 12, maxRows: 22 }}
                  placeholder={t('pages.protocolLibrary.contentPlaceholder')}
                  onChange={(event) => setContent(event.target.value)}
                />
                <Space wrap>
                  <Upload {...uploadProps}>
                    <Button icon={<ImportOutlined />}>{t('pages.protocolLibrary.importFile')}</Button>
                  </Upload>
                  <Button type="primary" icon={<SaveOutlined />} loading={saving} disabled={!content.trim()} onClick={importConnection}>
                    {t('pages.protocolLibrary.saveConnection')}
                  </Button>
                </Space>
              </Space>
            </Card>
          </Col>

          <Col xs={24} xl={15}>
            <Card
              title={t('pages.protocolLibrary.connectionsTitle')}
              extra={<Button icon={<EyeOutlined />} onClick={loadPreview}>{t('pages.protocolLibrary.preview')}</Button>}
            >
              <Spin spinning={loading}>
                {fetchError ? <Empty description={fetchError} /> : (
                  <Table
                    rowKey="id"
                    size="middle"
                    columns={columns}
                    dataSource={filtered}
                    pagination={{ pageSize: 8, hideOnSinglePage: true }}
                    locale={{ emptyText: t('pages.protocolLibrary.empty') }}
                  />
                )}
              </Spin>
            </Card>
          </Col>
        </Row>

        <Card className="protocol-library-preview" title={t('pages.protocolLibrary.previewTitle')}>
          <Input.TextArea
            value={preview}
            readOnly
            spellCheck={false}
            autoSize={{ minRows: 8, maxRows: 18 }}
            placeholder={t('pages.protocolLibrary.previewPlaceholder')}
          />
        </Card>
      </main>
    </>
  );
}
