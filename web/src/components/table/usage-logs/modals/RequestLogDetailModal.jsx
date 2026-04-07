import React, { useState, useEffect } from 'react';
import { Modal, Tabs, TabPane, Typography, Spin, Tag, Descriptions, Empty } from '@douyinfe/semi-ui';
import { API, showError } from '../../../../helpers';

const { Text } = Typography;

const JsonViewer = ({ data }) => {
  if (!data) {
    return <Text type='tertiary'>N/A</Text>;
  }

  let formatted = data;
  try {
    const parsed = JSON.parse(data);
    formatted = JSON.stringify(parsed, null, 2);
  } catch (e) {
    // not JSON, show as-is
  }

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
      {formatted}
    </pre>
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

  useEffect(() => {
    if (visible && requestId) {
      fetchDetail();
    } else {
      setDetail(null);
    }
  }, [visible, requestId]);

  const fetchDetail = async () => {
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
  };

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
                <JsonViewer data={detail.request_body} />
              </div>
            </TabPane>
            <TabPane tab={t('响应内容')} itemKey='response'>
              <div style={{ marginTop: 12 }}>
                <JsonViewer data={detail.response_body} />
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
