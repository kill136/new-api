import React, { useState, useEffect, useCallback } from 'react';
import { Modal, Tabs, TabPane, Typography, Spin, Tag, Descriptions, Empty } from '@douyinfe/semi-ui';
import { IconChevronRight, IconChevronDown } from '@douyinfe/semi-icons';
import { API, showError } from '../../../../helpers';

const { Text } = Typography;

// Color scheme for JSON tokens
const jsonColors = {
  key: 'var(--semi-color-text-0)',
  string: '#e45649',
  number: '#986801',
  boolean: '#0184bc',
  null: '#999',
  bracket: 'var(--semi-color-text-2)',
  toggle: 'var(--semi-color-primary)',
};

const JsonValue = ({ value }) => {
  if (value === null || value === undefined) {
    return <span style={{ color: jsonColors.null, fontStyle: 'italic' }}>null</span>;
  }
  if (typeof value === 'boolean') {
    return <span style={{ color: jsonColors.boolean }}>{value ? 'true' : 'false'}</span>;
  }
  if (typeof value === 'number') {
    return <span style={{ color: jsonColors.number }}>{value}</span>;
  }
  if (typeof value === 'string') {
    // Truncate very long strings in display but keep them selectable
    const display = value.length > 500 ? value.slice(0, 500) + '...' : value;
    return (
      <span style={{ color: jsonColors.string }}>
        &quot;{display}&quot;
      </span>
    );
  }
  return null;
};

const JsonNode = ({ keyName, value, depth = 0, defaultExpanded = true }) => {
  const [expanded, setExpanded] = useState(depth < 2 ? defaultExpanded : false);
  const indent = depth * 20;

  const isObject = value !== null && typeof value === 'object' && !Array.isArray(value);
  const isArray = Array.isArray(value);

  if (!isObject && !isArray) {
    return (
      <div style={{ paddingLeft: indent, lineHeight: '24px', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
        {keyName !== null && (
          <span style={{ color: jsonColors.key, fontWeight: 500 }}>
            &quot;{keyName}&quot;
            <span style={{ color: jsonColors.bracket }}>: </span>
          </span>
        )}
        <JsonValue value={value} />
      </div>
    );
  }

  const entries = isArray ? value.map((v, i) => [i, v]) : Object.entries(value);
  const bracketOpen = isArray ? '[' : '{';
  const bracketClose = isArray ? ']' : '}';
  const isEmpty = entries.length === 0;
  const summary = isArray ? `${entries.length} items` : `${entries.length} keys`;

  return (
    <div>
      <div
        style={{
          paddingLeft: indent,
          lineHeight: '24px',
          cursor: 'pointer',
          userSelect: 'none',
          display: 'flex',
          alignItems: 'flex-start',
        }}
        onClick={() => setExpanded(!expanded)}
      >
        <span style={{ color: jsonColors.toggle, width: 16, flexShrink: 0, marginTop: 4 }}>
          {!isEmpty && (expanded ? <IconChevronDown size="small" /> : <IconChevronRight size="small" />)}
        </span>
        <span>
          {keyName !== null && (
            <span style={{ color: jsonColors.key, fontWeight: 500 }}>
              &quot;{keyName}&quot;
              <span style={{ color: jsonColors.bracket }}>: </span>
            </span>
          )}
          <span style={{ color: jsonColors.bracket }}>{bracketOpen}</span>
          {(!expanded || isEmpty) && (
            <>
              {isEmpty ? (
                <span style={{ color: jsonColors.bracket }}>{bracketClose}</span>
              ) : (
                <>
                  <span style={{ color: jsonColors.null, fontStyle: 'italic', fontSize: 12, marginLeft: 4 }}>
                    {summary}
                  </span>
                  <span style={{ color: jsonColors.bracket }}> {bracketClose}</span>
                </>
              )}
            </>
          )}
        </span>
      </div>
      {expanded && !isEmpty && (
        <>
          {entries.map(([k, v], i) => (
            <JsonNode
              key={isArray ? i : k}
              keyName={isArray ? null : String(k)}
              value={v}
              depth={depth + 1}
              defaultExpanded={depth < 1}
            />
          ))}
          <div style={{ paddingLeft: indent + 16, lineHeight: '24px', color: jsonColors.bracket }}>
            {bracketClose}
          </div>
        </>
      )}
    </div>
  );
};

const JsonViewer = ({ data, t }) => {
  if (!data) {
    return <Text type='tertiary'>N/A</Text>;
  }

  let parsed = null;
  let isJson = false;
  try {
    parsed = JSON.parse(data);
    isJson = true;
  } catch (e) {
    // not JSON
  }

  if (!isJson) {
    return (
      <pre
        style={{
          background: 'var(--semi-color-fill-0)',
          border: '1px solid var(--semi-color-border)',
          borderRadius: 8,
          padding: 16,
          fontSize: 12,
          lineHeight: 1.6,
          maxHeight: 500,
          overflow: 'auto',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-all',
          margin: 0,
        }}
      >
        {data}
      </pre>
    );
  }

  return (
    <div
      style={{
        background: 'var(--semi-color-fill-0)',
        border: '1px solid var(--semi-color-border)',
        borderRadius: 8,
        padding: 16,
        fontSize: 13,
        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
        maxHeight: 500,
        overflow: 'auto',
      }}
    >
      <JsonNode keyName={null} value={parsed} depth={0} defaultExpanded={true} />
    </div>
  );
};

const RequestLogDetailModal = ({
  visible,
  onClose,
  requestId,
  isAdminUser,
  t,
}) => {
  const [loading, setLoading] = useState(false);
  const [detail, setDetail] = useState(null);

  const fetchDetail = useCallback(async () => {
    setLoading(true);
    try {
      const url = isAdminUser
        ? `/api/log/detail?request_id=${encodeURIComponent(requestId)}`
        : `/api/log/self/detail?request_id=${encodeURIComponent(requestId)}`;
      const res = await API.get(url);
      const { success, message, data } = res.data;
      if (success) {
        setDetail(data);
      } else {
        showError(message || t('请求日志未找到'));
      }
    } catch (e) {
      showError(t('请求日志未找到'));
    }
    setLoading(false);
  }, [requestId, isAdminUser, t]);

  useEffect(() => {
    if (visible && requestId) {
      fetchDetail();
    } else {
      setDetail(null);
    }
  }, [visible, requestId, fetchDetail]);

  const metaData = detail
    ? [
        { key: 'Request ID', value: detail.request_id },
        { key: t('模型'), value: detail.model_name },
        { key: t('请求路径'), value: detail.endpoint },
        {
          key: t('状态码'),
          value: (
            <Tag
              color={detail.status_code >= 200 && detail.status_code < 300 ? 'green' : 'red'}
              shape='circle'
            >
              {detail.status_code}
            </Tag>
          ),
        },
        {
          key: t('用时'),
          value: `${detail.use_time}s`,
        },
        {
          key: t('流'),
          value: detail.is_stream ? (
            <Tag color='blue' shape='circle'>{t('流')}</Tag>
          ) : (
            <Tag color='purple' shape='circle'>{t('非流')}</Tag>
          ),
        },
      ]
    : [];

  return (
    <Modal
      title={t('请求详情')}
      visible={visible}
      onCancel={onClose}
      footer={null}
      width={800}
      style={{ maxWidth: '90vw' }}
      bodyStyle={{ maxHeight: '80vh', overflow: 'auto' }}
    >
      {loading ? (
        <div style={{ textAlign: 'center', padding: 40 }}>
          <Spin size='large' />
        </div>
      ) : detail ? (
        <div>
          <Descriptions
            data={metaData}
            style={{ marginBottom: 16 }}
          />
          <Tabs type='line' defaultActiveKey='request'>
            <TabPane tab={t('请求内容')} itemKey='request'>
              <div style={{ marginTop: 12 }}>
                <JsonViewer data={detail.request_body} t={t} />
              </div>
            </TabPane>
            <TabPane tab={t('响应内容')} itemKey='response'>
              <div style={{ marginTop: 12 }}>
                <JsonViewer data={detail.response_body} t={t} />
              </div>
            </TabPane>
          </Tabs>
        </div>
      ) : (
        <Empty description={t('请求日志未找到')} />
      )}
    </Modal>
  );
};

export default RequestLogDetailModal;
