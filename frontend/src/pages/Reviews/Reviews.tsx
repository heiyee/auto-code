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
  List,
  Avatar,
  Empty,
  Divider,
  Alert,
  Tooltip,
  Skeleton,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  EyeOutlined,
  ClockCircleOutlined,
  UserOutlined,
  AuditOutlined,
  CodeOutlined,
  RocketOutlined,
  ExclamationCircleOutlined,
  CommentOutlined,
} from '@ant-design/icons';
import { useIsMobile } from '@/hooks/useMediaQuery';
import { useReviews, useUpdateReview, useChangeSets } from '@/hooks';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import 'dayjs/locale/zh-cn';
import type { ReviewGate, GateType, GateStatus } from '@/types';

dayjs.extend(relativeTime);
dayjs.locale('zh-cn');

const gateTypeConfig: Record<GateType, { icon: React.ReactNode; text: string; color: string }> = {
  design_review: { icon: <AuditOutlined />, text: '设计审核', color: 'blue' },
  code_review: { icon: <CodeOutlined />, text: '代码审核', color: 'green' },
  acceptance_review: { icon: <CheckCircleOutlined />, text: '验收审核', color: 'purple' },
  release_approval: { icon: <RocketOutlined />, text: '发布审批', color: 'orange' },
};

const gateStatusConfig: Record<GateStatus, { color: string; text: string }> = {
  pending: { color: 'warning', text: '待审核' },
  approved: { color: 'success', text: '已通过' },
  rejected: { color: 'error', text: '已驳回' },
  waived: { color: 'purple', text: '已豁免' },
};

interface ReviewChangeSetProps {
  workflowRunId: string;
}

function ReviewChangeSet({ workflowRunId }: ReviewChangeSetProps) {
  const { data, isLoading } = useChangeSets({ workflowRunId });
  const changeSets = data?.data || [];
  const changeSet = changeSets[0];

  if (isLoading) return <Skeleton active paragraph={{ rows: 2 }} />;
  if (!changeSet) return null;

  return (
    <div>
      <h4 className="text-sm font-medium mb-2">变更摘要:</h4>
      <Card size="small" className="bg-gray-50">
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-center">
          <div>
            <div className="text-lg font-bold text-green-500">+{changeSet.fileStats.added}</div>
            <div className="text-xs text-gray-500">新增</div>
          </div>
          <div>
            <div className="text-lg font-bold text-blue-500">~{changeSet.fileStats.modified}</div>
            <div className="text-xs text-gray-500">修改</div>
          </div>
          <div>
            <div className="text-lg font-bold text-red-500">-{changeSet.fileStats.deleted}</div>
            <div className="text-xs text-gray-500">删除</div>
          </div>
          <div>
            <div className="text-lg font-bold text-gray-700">
              +{changeSet.fileStats.totalAdditions}/-{changeSet.fileStats.totalDeletions}
            </div>
            <div className="text-xs text-gray-500">行数</div>
          </div>
        </div>
        <Divider className="my-3" />
        <List
          size="small"
          dataSource={changeSet.files.slice(0, 5)}
          renderItem={file => (
            <List.Item className="!py-1">
              <Tag color={file.status === 'A' ? 'green' : file.status === 'D' ? 'red' : 'blue'}>
                {file.status}
              </Tag>
              <span className="flex-1 truncate">{file.path}</span>
              <span className="text-xs text-gray-400">+{file.additions}/-{file.deletions}</span>
            </List.Item>
          )}
        />
        {changeSet.files.length > 5 && (
          <div className="text-center text-xs text-gray-400 mt-2">
            还有 {changeSet.files.length - 5} 个文件...
          </div>
        )}
      </Card>
    </div>
  );
}

export function Reviews() {
  const [viewingGate, setViewingGate] = useState<ReviewGate | null>(null);
  const [reviewModalOpen, setReviewModalOpen] = useState(false);
  const [reviewDecision, setReviewDecision] = useState<'approved' | 'rejected' | 'waived'>('approved');
  const [reviewComment, setReviewComment] = useState('');
  const [reviewerName, setReviewerName] = useState('');
  const isMobile = useIsMobile();

  const { data, isLoading, refetch } = useReviews({ refetchInterval: 5000 });
  const updateReviewMutation = useUpdateReview();

  const allGates = data?.data || [];
  const pendingGates = allGates.filter(g => g.status === 'pending');
  const completedGates = allGates.filter(g => g.status !== 'pending');

  const handleReview = async () => {
    if (!viewingGate) return;
    try {
      await updateReviewMutation.mutateAsync({
        id: viewingGate.id,
        data: {
          status: reviewDecision,
          decision: reviewDecision === 'approved' ? 'pass' : reviewDecision === 'rejected' ? 'reject' : 'return_for_revision',
          reviewer: reviewerName || '当前用户',
          comment: reviewComment,
        },
      });
      message.success('审核结果已提交');
      setReviewModalOpen(false);
      setViewingGate(null);
      setReviewComment('');
      setReviewerName('');
      refetch();
    } catch {
      message.error('提交失败，请重试');
    }
  };

  const columns: ColumnsType<ReviewGate> = [
    {
      title: '审核类型',
      dataIndex: 'gateType',
      key: 'gateType',
      width: 120,
      render: (type: GateType) => {
        const config = gateTypeConfig[type];
        return (
          <Tag icon={config.icon} color={config.color}>
            {config.text}
          </Tag>
        );
      },
    },
    {
      title: '标题',
      dataIndex: 'title',
      key: 'title',
      ellipsis: true,
      render: (text, record) => (
        <a onClick={() => setViewingGate(record)} className="font-medium text-primary-500">
          {text || record.stageName}
        </a>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (status: GateStatus) => {
        const config = gateStatusConfig[status];
        return <Badge status={config.color as any} text={config.text} />;
      },
    },
    {
      title: '审核人',
      dataIndex: 'reviewer',
      key: 'reviewer',
      width: 100,
      responsive: ['md'],
      render: (reviewer) => reviewer ? (
        <span><Avatar size="small" icon={<UserOutlined />} className="mr-1" />{reviewer}</span>
      ) : '-',
    },
    {
      title: '创建时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 140,
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
      width: 120,
      fixed: 'right',
      render: (_, record) => (
        <Space size="small">
          <Button type="text" size="small" icon={<EyeOutlined />} onClick={() => setViewingGate(record)}>
            查看
          </Button>
          {record.status === 'pending' && (
            <Button type="primary" size="small" onClick={() => {
              setViewingGate(record);
              setReviewModalOpen(true);
            }}>
              审核
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
            审核中心
          </h1>
          <p className="text-gray-500 dark:text-gray-400 mt-1">
            管理设计审核、代码审核和发布审批
          </p>
        </div>
      </div>

      {/* Pending Reviews Alert */}
      {pendingGates.length > 0 && (
        <Alert
          message={`您有 ${pendingGates.length} 个待处理的审核项`}
          description="请及时处理待审核项，避免阻塞工作流执行"
          type="warning"
          showIcon
          icon={<ExclamationCircleOutlined />}
          action={
            <Button size="small" type="primary" onClick={() => setViewingGate(pendingGates[0])}>
              立即处理
            </Button>
          }
        />
      )}

      {/* Pending Reviews */}
      <Card
        title={
          <span>
            <ClockCircleOutlined className="mr-2" />
            待审核 ({pendingGates.length})
          </span>
        }
        styles={{ body: { padding: 0 } }}
      >
        {pendingGates.length > 0 ? (
          <Table
            columns={columns}
            dataSource={pendingGates}
            rowKey="id"
            loading={isLoading}
            pagination={false}
            size={isMobile ? 'small' : 'middle'}
          />
        ) : (
          <Empty description="暂无待审核项" className="py-8" />
        )}
      </Card>

      {/* Completed Reviews */}
      <Card
        title={
          <span>
            <CheckCircleOutlined className="mr-2" />
            已完成审核 ({completedGates.length})
          </span>
        }
        styles={{ body: { padding: 0 } }}
      >
        <Table
          columns={columns}
          dataSource={completedGates}
          rowKey="id"
          loading={isLoading}
          pagination={{ pageSize: 5, simple: isMobile }}
          size={isMobile ? 'small' : 'middle'}
        />
      </Card>

      {/* Review Detail Drawer */}
      <Drawer
        title={`审核详情 - ${viewingGate?.title || viewingGate?.stageName}`}
        open={!!viewingGate && !reviewModalOpen}
        onClose={() => setViewingGate(null)}
        width={isMobile ? '100%' : 600}
      >
        {viewingGate && (
          <div className="space-y-6">
            <Descriptions column={1} bordered size="small">
              <Descriptions.Item label="审核类型">
                <Tag icon={gateTypeConfig[viewingGate.gateType].icon} color={gateTypeConfig[viewingGate.gateType].color}>
                  {gateTypeConfig[viewingGate.gateType].text}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="状态">
                <Badge
                  status={gateStatusConfig[viewingGate.status].color as any}
                  text={gateStatusConfig[viewingGate.status].text}
                />
              </Descriptions.Item>
              <Descriptions.Item label="创建时间">
                {dayjs(viewingGate.createdAt).format('YYYY-MM-DD HH:mm:ss')}
              </Descriptions.Item>
              {viewingGate.resolvedAt && (
                <Descriptions.Item label="处理时间">
                  {dayjs(viewingGate.resolvedAt).format('YYYY-MM-DD HH:mm:ss')}
                </Descriptions.Item>
              )}
              {viewingGate.reviewer && (
                <Descriptions.Item label="审核人">
                  <Avatar size="small" icon={<UserOutlined />} className="mr-2" />
                  {viewingGate.reviewer}
                </Descriptions.Item>
              )}
              {viewingGate.comment && (
                <Descriptions.Item label="审核意见">
                  {viewingGate.comment}
                </Descriptions.Item>
              )}
            </Descriptions>

            {/* Blocking Items */}
            {viewingGate.blockingItems && viewingGate.blockingItems.length > 0 && (
              <div>
                <h4 className="text-sm font-medium mb-2">待确认项:</h4>
                <List
                  size="small"
                  dataSource={viewingGate.blockingItems}
                  renderItem={item => (
                    <List.Item>
                      <ExclamationCircleOutlined className="text-orange-500 mr-2" />
                      {item}
                    </List.Item>
                  )}
                />
              </div>
            )}

            {/* Change Set */}
            <ReviewChangeSet workflowRunId={viewingGate.workflowRunId} />

            {/* Action Button */}
            {viewingGate.status === 'pending' && (
              <Button type="primary" block onClick={() => setReviewModalOpen(true)}>
                开始审核
              </Button>
            )}
          </div>
        )}
      </Drawer>

      {/* Review Modal */}
      <Modal
        title="提交审核"
        open={reviewModalOpen}
        onCancel={() => {
          setReviewModalOpen(false);
          setReviewComment('');
          setReviewerName('');
        }}
        onOk={handleReview}
        confirmLoading={updateReviewMutation.isPending}
        okText="提交"
        cancelText="取消"
      >
        <div className="space-y-4">
          <div>
            <div className="mb-2 font-medium">审核结论:</div>
            <Radio.Group value={reviewDecision} onChange={(e) => setReviewDecision(e.target.value)}>
              <Radio.Button value="approved">
                <CheckCircleOutlined className="text-green-500 mr-1" />
                通过
              </Radio.Button>
              <Radio.Button value="waived">
                <CommentOutlined className="text-blue-500 mr-1" />
                豁免
              </Radio.Button>
              <Radio.Button value="rejected">
                <CloseCircleOutlined className="text-red-500 mr-1" />
                驳回
              </Radio.Button>
            </Radio.Group>
          </div>
          <div>
            <div className="mb-2 font-medium">审核人:</div>
            <Input
              value={reviewerName}
              onChange={(e) => setReviewerName(e.target.value)}
              placeholder="请输入审核人姓名..."
            />
          </div>
          <div>
            <div className="mb-2 font-medium">审核意见:</div>
            <Input.TextArea
              rows={4}
              value={reviewComment}
              onChange={(e) => setReviewComment(e.target.value)}
              placeholder="请输入审核意见..."
            />
          </div>
        </div>
      </Modal>
    </div>
  );
}
