import { useState } from 'react';
import {
  Card,
  Table,
  Button,
  Space,
  Tag,
  Drawer,
  Descriptions,
  Badge,
  Modal,
  Input,
  Radio,
  message,
  Tooltip,
  Statistic,
  Row,
  Col,
  Alert,
  Tabs,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  CheckCircleOutlined,
  EyeOutlined,
  ClockCircleOutlined,
  ExclamationCircleOutlined,
  QuestionCircleOutlined,
} from '@ant-design/icons';
import { useIsMobile } from '@/hooks/useMediaQuery';
import { useDecisions, useResolveDecision } from '@/hooks';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import 'dayjs/locale/zh-cn';
import type { DecisionRequest, DecisionStatus, RequestType } from '@/types';

dayjs.extend(relativeTime);
dayjs.locale('zh-cn');

const requestTypeConfig: Record<RequestType, { text: string; color: string }> = {
  clarification: { text: '需澄清', color: 'blue' },
  option_selection: { text: '选项选择', color: 'purple' },
  risk_confirmation: { text: '风险确认', color: 'red' },
  scope_confirmation: { text: '范围确认', color: 'orange' },
};

const statusConfig: Record<DecisionStatus, { color: string; text: string }> = {
  pending: { color: 'warning', text: '待处理' },
  resolved: { color: 'success', text: '已处理' },
  expired: { color: 'default', text: '已过期' },
};

export function Decisions() {
  const [viewingDecision, setViewingDecision] = useState<DecisionRequest | null>(null);
  const [resolveModalOpen, setResolveModalOpen] = useState(false);
  const [selectedOption, setSelectedOption] = useState('');
  const [deciderName, setDeciderName] = useState('');
  const isMobile = useIsMobile();

  const { data, isLoading, refetch } = useDecisions({ refetchInterval: 5000 });
  const resolveDecisionMutation = useResolveDecision();

  const allDecisions = data?.data || [];
  const pendingDecisions = allDecisions.filter(d => d.status === 'pending');
  const resolvedDecisions = allDecisions.filter(d => d.status !== 'pending');

  const handleResolve = async () => {
    if (!viewingDecision || !selectedOption) {
      message.warning('请选择一个选项');
      return;
    }
    try {
      await resolveDecisionMutation.mutateAsync({
        id: viewingDecision.id,
        decision: selectedOption,
        decider: deciderName || '当前用户',
      });
      message.success('决策已提交');
      setResolveModalOpen(false);
      setViewingDecision(null);
      setSelectedOption('');
      setDeciderName('');
      refetch();
    } catch {
      message.error('提交失败，请重试');
    }
  };

  const openResolveModal = (record: DecisionRequest) => {
    setViewingDecision(record);
    setSelectedOption(record.recommendedOption || '');
    setResolveModalOpen(true);
  };

  const columns: ColumnsType<DecisionRequest> = [
    {
      title: '类型',
      dataIndex: 'requestType',
      key: 'requestType',
      width: 110,
      render: (type: RequestType) => {
        const config = requestTypeConfig[type];
        return <Tag color={config.color}>{config.text}</Tag>;
      },
    },
    {
      title: '标题 / 问题',
      key: 'title',
      ellipsis: true,
      render: (_, record) => (
        <a onClick={() => setViewingDecision(record)} className="font-medium text-primary-500">
          {record.title}
          <div className="text-xs text-gray-400 font-normal truncate">{record.question}</div>
        </a>
      ),
    },
    {
      title: '工作流',
      dataIndex: 'workflowRunId',
      key: 'workflowRunId',
      width: 100,
      responsive: ['md'],
      render: (id) => (
        <span className="font-mono text-xs text-gray-500">{id?.slice(0, 8)}</span>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (status: DecisionStatus) => {
        const config = statusConfig[status];
        return <Badge status={config.color as any} text={config.text} />;
      },
    },
    {
      title: '创建时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 130,
      responsive: ['sm'],
      render: (date) => (
        <Tooltip title={dayjs(date).format('YYYY-MM-DD HH:mm:ss')}>
          <span>{dayjs(date).fromNow()}</span>
        </Tooltip>
      ),
    },
    {
      title: '操作',
      key: 'actions',
      width: 130,
      fixed: 'right',
      render: (_, record) => (
        <Space size="small">
          <Button type="text" size="small" icon={<EyeOutlined />} onClick={() => setViewingDecision(record)}>
            查看
          </Button>
          {record.status === 'pending' && (
            <Button type="primary" size="small" onClick={() => openResolveModal(record)}>
              处理
            </Button>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-xl sm:text-2xl font-bold text-gray-800 dark:text-white">
            决策中心
          </h1>
          <p className="text-gray-500 dark:text-gray-400 mt-1">
            处理工作流中的待决策项，避免阻塞自动化流程
          </p>
        </div>
      </div>

      {/* Pending alert */}
      {pendingDecisions.length > 0 && (
        <Alert
          message={`您有 ${pendingDecisions.length} 个待处理决策`}
          description="待处理的决策会阻塞工作流继续执行，请及时处理。"
          type="warning"
          showIcon
          icon={<ExclamationCircleOutlined />}
        />
      )}

      {/* Stats */}
      <Row gutter={[16, 16]}>
        <Col xs={8}>
          <Card>
            <Statistic
              title="待处理"
              value={pendingDecisions.length}
              prefix={<ClockCircleOutlined className="text-orange-500" />}
            />
          </Card>
        </Col>
        <Col xs={8}>
          <Card>
            <Statistic
              title="已处理"
              value={resolvedDecisions.length}
              prefix={<CheckCircleOutlined className="text-green-500" />}
            />
          </Card>
        </Col>
        <Col xs={8}>
          <Card>
            <Statistic
              title="总计"
              value={allDecisions.length}
              prefix={<QuestionCircleOutlined className="text-purple-500" />}
            />
          </Card>
        </Col>
      </Row>

      {/* Table with tabs */}
      <Card styles={{ body: { padding: 0 } }}>
        <Tabs
          defaultActiveKey="pending"
          className="px-4 pt-2"
          items={[
            {
              key: 'pending',
              label: `待处理 (${pendingDecisions.length})`,
              children: (
                <Table
                  columns={columns}
                  dataSource={pendingDecisions}
                  rowKey="id"
                  loading={isLoading}
                  scroll={{ x: 700 }}
                  pagination={{ showSizeChanger: false, simple: isMobile }}
                  size={isMobile ? 'small' : 'middle'}
                  locale={{ emptyText: '暂无待处理决策' }}
                />
              ),
            },
            {
              key: 'resolved',
              label: `已处理 (${resolvedDecisions.length})`,
              children: (
                <Table
                  columns={columns}
                  dataSource={resolvedDecisions}
                  rowKey="id"
                  loading={isLoading}
                  scroll={{ x: 700 }}
                  pagination={{ showSizeChanger: false, simple: isMobile }}
                  size={isMobile ? 'small' : 'middle'}
                  locale={{ emptyText: '暂无已处理决策' }}
                />
              ),
            },
          ]}
        />
      </Card>

      {/* Detail Drawer */}
      <Drawer
        title="决策详情"
        open={!!viewingDecision && !resolveModalOpen}
        onClose={() => setViewingDecision(null)}
        width={isMobile ? '100%' : 520}
        extra={
          viewingDecision?.status === 'pending' && (
            <Button type="primary" onClick={() => openResolveModal(viewingDecision)}>
              处理决策
            </Button>
          )
        }
      >
        {viewingDecision && (
          <div className="space-y-4">
            <Descriptions column={1} bordered size="small">
              <Descriptions.Item label="类型">
                <Tag color={requestTypeConfig[viewingDecision.requestType]?.color}>
                  {requestTypeConfig[viewingDecision.requestType]?.text}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="状态">
                <Badge
                  status={statusConfig[viewingDecision.status]?.color as any}
                  text={statusConfig[viewingDecision.status]?.text}
                />
              </Descriptions.Item>
              <Descriptions.Item label="标题">{viewingDecision.title}</Descriptions.Item>
              <Descriptions.Item label="问题">{viewingDecision.question}</Descriptions.Item>
              {viewingDecision.context && (
                <Descriptions.Item label="上下文">
                  <span className="whitespace-pre-wrap text-sm">{viewingDecision.context}</span>
                </Descriptions.Item>
              )}
              {viewingDecision.recommendedOption && (
                <Descriptions.Item label="推荐选项">
                  <Tag color="green">{viewingDecision.recommendedOption}</Tag>
                </Descriptions.Item>
              )}
              {viewingDecision.decision && (
                <Descriptions.Item label="决策结果">
                  <Tag color="blue">{viewingDecision.decision}</Tag>
                </Descriptions.Item>
              )}
              {viewingDecision.decider && (
                <Descriptions.Item label="决策人">{viewingDecision.decider}</Descriptions.Item>
              )}
              <Descriptions.Item label="工作流 ID">
                <span className="font-mono text-xs">{viewingDecision.workflowRunId}</span>
              </Descriptions.Item>
              <Descriptions.Item label="创建时间">
                {dayjs(viewingDecision.createdAt).format('YYYY-MM-DD HH:mm:ss')}
              </Descriptions.Item>
              {viewingDecision.resolvedAt && (
                <Descriptions.Item label="处理时间">
                  {dayjs(viewingDecision.resolvedAt).format('YYYY-MM-DD HH:mm:ss')}
                </Descriptions.Item>
              )}
            </Descriptions>

            {viewingDecision.options && viewingDecision.options.length > 0 && (
              <div>
                <h4 className="font-medium mb-2">可选选项：</h4>
                <div className="space-y-2">
                  {viewingDecision.options.map(opt => (
                    <div
                      key={opt.value}
                      className={`p-3 rounded border text-sm ${
                        opt.value === viewingDecision.decision
                          ? 'border-blue-400 bg-blue-50'
                          : opt.value === viewingDecision.recommendedOption
                          ? 'border-green-400 bg-green-50'
                          : 'border-gray-200'
                      }`}
                    >
                      <span className="font-mono text-xs text-gray-400 mr-2">[{opt.value}]</span>
                      {opt.label}
                      {opt.value === viewingDecision.recommendedOption && (
                        <Tag color="green" className="ml-2 text-xs">推荐</Tag>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </Drawer>

      {/* Resolve Modal */}
      <Modal
        title="处理决策"
        open={resolveModalOpen}
        onCancel={() => {
          setResolveModalOpen(false);
          setSelectedOption('');
          setDeciderName('');
        }}
        onOk={handleResolve}
        okText="提交决策"
        cancelText="取消"
        confirmLoading={resolveDecisionMutation.isPending}
        width={isMobile ? '100%' : 520}
      >
        {viewingDecision && (
          <div className="space-y-4 py-2">
            <div>
              <div className="text-sm font-medium text-gray-700 mb-1">问题：</div>
              <div className="text-gray-600 bg-gray-50 p-3 rounded">{viewingDecision.question}</div>
            </div>

            {viewingDecision.options && viewingDecision.options.length > 0 ? (
              <div>
                <div className="text-sm font-medium text-gray-700 mb-2">选择选项：</div>
                <Radio.Group
                  value={selectedOption}
                  onChange={e => setSelectedOption(e.target.value)}
                  className="w-full"
                >
                  <div className="space-y-2">
                    {viewingDecision.options.map(opt => (
                      <Radio key={opt.value} value={opt.value} className="w-full">
                        <span>
                          {opt.label}
                          {opt.value === viewingDecision.recommendedOption && (
                            <Tag color="green" className="ml-2 text-xs">推荐</Tag>
                          )}
                        </span>
                      </Radio>
                    ))}
                  </div>
                </Radio.Group>
              </div>
            ) : (
              <div>
                <div className="text-sm font-medium text-gray-700 mb-2">输入决策：</div>
                <Input
                  value={selectedOption}
                  onChange={e => setSelectedOption(e.target.value)}
                  placeholder="请输入决策内容"
                />
              </div>
            )}

            <div>
              <div className="text-sm font-medium text-gray-700 mb-2">决策人（可选）：</div>
              <Input
                value={deciderName}
                onChange={e => setDeciderName(e.target.value)}
                placeholder="请输入您的姓名"
                prefix={<span className="text-gray-400 text-xs">👤</span>}
              />
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
