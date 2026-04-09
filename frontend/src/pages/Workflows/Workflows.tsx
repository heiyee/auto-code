import { useState } from 'react';
import {
  Card,
  Table,
  Button,
  Space,
  Tag,
  Drawer,
  Descriptions,
  Progress,
  Badge,
  Timeline,
  Tabs,
  List,
  Tooltip,
  Segmented,
  Statistic,
  Row,
  Col,
  Skeleton,
  Modal,
  Input,
  Radio,
  message,
  Alert,
  Divider,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  PlayCircleOutlined,
  PauseCircleOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  EyeOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
  ClockCircleOutlined,
  ExclamationCircleOutlined,
  UserOutlined,
  RobotOutlined,
  SyncOutlined,
  FileTextOutlined,
  QuestionCircleOutlined,
  AuditOutlined,
  DiffOutlined,
} from '@ant-design/icons';
import { useIsMobile } from '@/hooks/useMediaQuery';
import { useWorkflows, useWorkflow, useWorkflowTasks, useArtifacts, useArtifactContent, useDecisions, useResolveDecision, useReviews, useUpdateReview, useChangeSets, useUpdateRequirement } from '@/hooks';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import 'dayjs/locale/zh-cn';
import type { WorkflowRun, TaskItem, StageStatus, StageRun, Artifact, DecisionRequest, ReviewGate, GateStatus, GateType, RequestType, DecisionStatus, ChangeSet } from '@/types';
import { STAGE_DEFINITIONS } from '@/types';

dayjs.extend(relativeTime);
dayjs.locale('zh-cn');

const statusConfig: Record<WorkflowRun['status'], { color: string; text: string; icon: React.ReactNode }> = {
  pending: { color: 'default', text: '等待中', icon: <ClockCircleOutlined /> },
  running: { color: 'processing', text: '执行中', icon: <SyncOutlined spin /> },
  paused: { color: 'warning', text: '已暂停', icon: <PauseCircleOutlined /> },
  completed: { color: 'success', text: '已完成', icon: <CheckCircleOutlined /> },
  failed: { color: 'error', text: '失败', icon: <CloseCircleOutlined /> },
  canceled: { color: 'default', text: '已取消', icon: <CloseCircleOutlined /> },
};

const stageStatusConfig: Record<StageStatus, { color: string; text: string }> = {
  pending: { color: 'default', text: '等待中' },
  running: { color: 'processing', text: '执行中' },
  stalled: { color: 'warning', text: '停滞' },
  verifying: { color: 'blue', text: '校验中' },
  awaiting_input: { color: 'orange', text: '等待输入' },
  awaiting_review: { color: 'purple', text: '等待审核' },
  blocked: { color: 'error', text: '阻塞' },
  partial: { color: 'gold', text: '部分完成' },
  completed: { color: 'success', text: '已完成' },
  failed: { color: 'error', text: '失败' },
  interrupted: { color: 'magenta', text: '中断' },
  canceled: { color: 'default', text: '已取消' },
};

const taskStatusConfig: Record<TaskItem['status'], { color: string; text: string }> = {
  planned: { color: 'default', text: '计划中' },
  running: { color: 'processing', text: '执行中' },
  done: { color: 'success', text: '已完成' },
  waived: { color: 'purple', text: '已豁免' },
  blocked: { color: 'error', text: '阻塞' },
  failed: { color: 'error', text: '失败' },
};

const decisionStatusConfig: Record<DecisionStatus, { color: string; text: string }> = {
  pending: { color: 'warning', text: '待处理' },
  resolved: { color: 'success', text: '已处理' },
  expired: { color: 'default', text: '已过期' },
};

const decisionTypeConfig: Record<RequestType, { text: string; color: string }> = {
  clarification: { text: '需澄清', color: 'blue' },
  option_selection: { text: '选项选择', color: 'purple' },
  risk_confirmation: { text: '风险确认', color: 'red' },
  scope_confirmation: { text: '范围确认', color: 'orange' },
};

const reviewStatusConfig: Record<GateStatus, { color: string; text: string }> = {
  pending: { color: 'warning', text: '待审核' },
  approved: { color: 'success', text: '已通过' },
  rejected: { color: 'error', text: '已驳回' },
  waived: { color: 'purple', text: '已豁免' },
};

const reviewTypeConfig: Record<GateType, { text: string; color: string }> = {
  design_review: { text: '设计审核', color: 'blue' },
  code_review: { text: '代码审核', color: 'green' },
  acceptance_review: { text: '验收审核', color: 'purple' },
  release_approval: { text: '发布审批', color: 'orange' },
};

// ── Inline Decisions Panel ────────────────────────────────────────────────────
function WorkflowDecisionsPanel({ workflowId }: { workflowId: string }) {
  const [resolveTarget, setResolveTarget] = useState<DecisionRequest | null>(null);
  const [selectedOption, setSelectedOption] = useState('');
  const [deciderName, setDeciderName] = useState('');
  const { data, refetch } = useDecisions({ workflowRunId: workflowId, refetchInterval: 5000 });
  const resolveDecision = useResolveDecision();
  const decisions = data?.data || [];

  const handleResolve = async () => {
    if (!resolveTarget || !selectedOption) { message.warning('请选择一个选项'); return; }
    try {
      await resolveDecision.mutateAsync({ id: resolveTarget.id, decision: selectedOption, decider: deciderName || '当前用户' });
      message.success('决策已提交');
      setResolveTarget(null);
      setSelectedOption('');
      setDeciderName('');
      refetch();
    } catch {
      message.error('提交失败');
    }
  };

  if (decisions.length === 0) {
    return <div className="text-gray-400 py-4 text-sm">暂无决策项</div>;
  }

  const pending = decisions.filter(d => d.status === 'pending');
  return (
    <>
      {pending.length > 0 && (
        <Alert
          className="mb-3"
          type="error"
          showIcon
          icon={<QuestionCircleOutlined />}
          message={`${pending.length} 个待处理决策正在阻塞工作流`}
        />
      )}
      <List
        size="small"
        dataSource={decisions}
        renderItem={item => (
          <List.Item
            actions={item.status === 'pending' ? [
              <Button key="resolve" type="primary" size="small" onClick={() => {
                setResolveTarget(item);
                setSelectedOption(item.recommendedOption || '');
              }}>处理</Button>
            ] : []}
          >
            <List.Item.Meta
              title={
                <div className="flex items-center gap-2">
                  <Tag color={decisionTypeConfig[item.requestType]?.color}>{decisionTypeConfig[item.requestType]?.text}</Tag>
                  <span>{item.title}</span>
                  <Badge status={decisionStatusConfig[item.status]?.color as any} text={decisionStatusConfig[item.status]?.text} />
                </div>
              }
              description={<span className="text-xs text-gray-500">{item.question}</span>}
            />
          </List.Item>
        )}
      />
      <Modal
        title="处理决策"
        open={!!resolveTarget}
        onCancel={() => { setResolveTarget(null); setSelectedOption(''); setDeciderName(''); }}
        onOk={handleResolve}
        okText="提交决策"
        cancelText="取消"
        confirmLoading={resolveDecision.isPending}
      >
        {resolveTarget && (
          <div className="space-y-4 py-2">
            <div className="bg-gray-50 p-3 rounded text-sm">{resolveTarget.question}</div>
            {resolveTarget.options && resolveTarget.options.length > 0 ? (
              <Radio.Group value={selectedOption} onChange={e => setSelectedOption(e.target.value)} className="w-full">
                <div className="space-y-2">
                  {resolveTarget.options.map(opt => (
                    <Radio key={opt.value} value={opt.value}>
                      {opt.label}
                      {opt.value === resolveTarget.recommendedOption && <Tag color="green" className="ml-2 text-xs">推荐</Tag>}
                    </Radio>
                  ))}
                </div>
              </Radio.Group>
            ) : (
              <Input value={selectedOption} onChange={e => setSelectedOption(e.target.value)} placeholder="请输入决策内容" />
            )}
            <Input value={deciderName} onChange={e => setDeciderName(e.target.value)} placeholder="决策人（可选）" prefix={<UserOutlined />} />
          </div>
        )}
      </Modal>
    </>
  );
}

// ── Inline Reviews Panel ──────────────────────────────────────────────────────
function WorkflowReviewsPanel({ workflowId }: { workflowId: string }) {
  const [reviewTarget, setReviewTarget] = useState<ReviewGate | null>(null);
  const [reviewDecision, setReviewDecision] = useState<'approved' | 'rejected' | 'waived'>('approved');
  const [reviewComment, setReviewComment] = useState('');
  const [reviewerName, setReviewerName] = useState('');
  const { data, refetch } = useReviews({ workflowRunId: workflowId, refetchInterval: 5000 });
  const updateReview = useUpdateReview();
  const reviews = data?.data || [];

  const handleReview = async () => {
    if (!reviewTarget) return;
    try {
      await updateReview.mutateAsync({
        id: reviewTarget.id,
        data: {
          status: reviewDecision,
          decision: reviewDecision === 'approved' ? 'pass' : reviewDecision === 'rejected' ? 'reject' : 'return_for_revision',
          reviewer: reviewerName || '当前用户',
          comment: reviewComment,
        },
      });
      message.success('审核结果已提交');
      setReviewTarget(null);
      setReviewComment('');
      setReviewerName('');
      refetch();
    } catch {
      message.error('提交失败');
    }
  };

  if (reviews.length === 0) {
    return <div className="text-gray-400 py-4 text-sm">暂无审核项</div>;
  }

  const pending = reviews.filter(r => r.status === 'pending');
  return (
    <>
      {pending.length > 0 && (
        <Alert
          className="mb-3"
          type="warning"
          showIcon
          icon={<AuditOutlined />}
          message={`${pending.length} 个审核项待处理`}
        />
      )}
      <List
        size="small"
        dataSource={reviews}
        renderItem={item => (
          <List.Item
            actions={item.status === 'pending' ? [
              <Button key="review" type="primary" size="small" onClick={() => {
                setReviewTarget(item);
                setReviewDecision('approved');
              }}>审核</Button>
            ] : []}
          >
            <List.Item.Meta
              title={
                <div className="flex items-center gap-2">
                  <Tag color={reviewTypeConfig[item.gateType]?.color}>{reviewTypeConfig[item.gateType]?.text}</Tag>
                  <span>{item.title || item.stageName}</span>
                  <Badge status={reviewStatusConfig[item.status]?.color as any} text={reviewStatusConfig[item.status]?.text} />
                </div>
              }
              description={item.reviewer ? <span className="text-xs text-gray-500">审核人: {item.reviewer}</span> : null}
            />
          </List.Item>
        )}
      />
      <Modal
        title="提交审核"
        open={!!reviewTarget}
        onCancel={() => { setReviewTarget(null); setReviewComment(''); setReviewerName(''); }}
        onOk={handleReview}
        okText="提交"
        cancelText="取消"
        confirmLoading={updateReview.isPending}
      >
        {reviewTarget && (
          <div className="space-y-4 py-2">
            <div className="font-medium">{reviewTarget.title || reviewTarget.stageName}</div>
            {reviewTarget.description && <div className="text-sm text-gray-500">{reviewTarget.description}</div>}
            <div>
              <div className="text-sm font-medium mb-2">审核结论：</div>
              <Radio.Group value={reviewDecision} onChange={e => setReviewDecision(e.target.value)}>
                <Radio value="approved"><Tag color="green">通过</Tag></Radio>
                <Radio value="rejected"><Tag color="red">驳回</Tag></Radio>
                <Radio value="waived"><Tag color="purple">豁免</Tag></Radio>
              </Radio.Group>
            </div>
            <Input value={reviewerName} onChange={e => setReviewerName(e.target.value)} placeholder="审核人（可选）" prefix={<UserOutlined />} />
            <Input.TextArea value={reviewComment} onChange={e => setReviewComment(e.target.value)} placeholder="审核意见（可选）" rows={3} />
          </div>
        )}
      </Modal>
    </>
  );
}

// ── Inline ChangeSets Panel ───────────────────────────────────────────────────
function WorkflowChangeSetsPanel({ workflowId }: { workflowId: string }) {
  const { data, isLoading } = useChangeSets({ workflowRunId: workflowId });
  const changeSets: ChangeSet[] = data?.data || [];

  if (isLoading) return <Skeleton active paragraph={{ rows: 4 }} />;
  if (changeSets.length === 0) return <div className="text-gray-400 py-4 text-sm">暂无代码变更数据</div>;

  const latest = changeSets[0];
  return (
    <div className="space-y-4 mt-2">
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-center">
        <div><div className="text-lg font-bold text-green-500">+{latest.fileStats.added}</div><div className="text-xs text-gray-500">新增</div></div>
        <div><div className="text-lg font-bold text-blue-500">~{latest.fileStats.modified}</div><div className="text-xs text-gray-500">修改</div></div>
        <div><div className="text-lg font-bold text-red-500">-{latest.fileStats.deleted}</div><div className="text-xs text-gray-500">删除</div></div>
        <div><div className="text-lg font-bold text-gray-700">+{latest.fileStats.totalAdditions}/-{latest.fileStats.totalDeletions}</div><div className="text-xs text-gray-500">行数</div></div>
      </div>
      {latest.summary && <div className="text-sm text-gray-600 bg-gray-50 p-3 rounded">{latest.summary}</div>}
      <Divider className="!my-2" />
      <List
        size="small"
        dataSource={latest.files.slice(0, 20)}
        locale={{ emptyText: '无文件变更' }}
        renderItem={file => (
          <List.Item className="!py-1">
            <Tag color={file.status === 'A' ? 'green' : file.status === 'D' ? 'red' : 'blue'} className="!mr-2">{file.status}</Tag>
            <span className="flex-1 truncate text-xs font-mono">{file.path}</span>
            <span className="text-xs text-gray-400 ml-2">+{file.additions}/-{file.deletions}</span>
          </List.Item>
        )}
      />
      {latest.files.length > 20 && <div className="text-center text-xs text-gray-400">还有 {latest.files.length - 20} 个文件...</div>}
    </div>
  );
}

interface ArtifactContentViewerProps {
  artifactId: string;
  artifactTitle: string;
  open: boolean;
  onClose: () => void;
}

function ArtifactContentViewer({ artifactId, artifactTitle, open, onClose }: ArtifactContentViewerProps) {
  const { data: content, isLoading } = useArtifactContent(artifactId);
  return (
    <Modal
      title={`产物内容 - ${artifactTitle}`}
      open={open}
      onCancel={onClose}
      footer={null}
      width={800}
    >
      {isLoading ? (
        <Skeleton active paragraph={{ rows: 10 }} />
      ) : (
        <pre className="bg-gray-50 p-4 rounded text-xs overflow-auto max-h-[60vh] whitespace-pre-wrap">
          {content || '（无内容）'}
        </pre>
      )}
    </Modal>
  );
}

interface WorkflowDetailContentProps {
  workflowId: string;
}

function WorkflowDetailContent({ workflowId }: WorkflowDetailContentProps) {
  const isMobile = useIsMobile();
  const [viewingArtifactId, setViewingArtifactId] = useState<string | null>(null);
  const [viewingArtifactTitle, setViewingArtifactTitle] = useState('');
  const { data: workflowData, isLoading: workflowLoading } = useWorkflow(workflowId, { refetchInterval: 5000 });
  const { data: tasksData } = useWorkflowTasks(workflowId);
  const { data: artifactsData } = useArtifacts({ workflowRunId: workflowId });

  const workflow = workflowData?.data;
  const stages: StageRun[] = (workflow as any)?.stages || [];
  const tasks: TaskItem[] = tasksData?.data || [];
  const artifacts: Artifact[] = artifactsData?.data || [];

  if (workflowLoading) {
    return <Skeleton active paragraph={{ rows: 8 }} />;
  }

  if (!workflow) {
    return null;
  }

  const getStageTasks = (stageId: string) => tasks.filter(t => t.stageRunId === stageId);

  const renderStageTimeline = () => {
    const sorted = [...stages].sort((a, b) => a.order - b.order);
    return (
      <Timeline
        items={sorted.map(stage => ({
          color: stageStatusConfig[stage.status]?.color === 'success' ? 'green' :
                 stageStatusConfig[stage.status]?.color === 'processing' ? 'blue' :
                 stageStatusConfig[stage.status]?.color === 'error' ? 'red' : 'gray',
          dot: stage.status === 'running' ? <SyncOutlined spin /> : undefined,
          children: (
            <div className="pb-4">
              <div className="flex items-center gap-2 mb-1">
                <span className="font-medium">{stage.displayName}</span>
                <Tag color={stageStatusConfig[stage.status]?.color}>
                  {stageStatusConfig[stage.status]?.text}
                </Tag>
                {stage.ownerType === 'human' && (
                  <Tag icon={<UserOutlined />} color="purple">人工</Tag>
                )}
              </div>
              {stage.startedAt && (
                <div className="text-xs text-gray-500 mb-2">
                  开始: {dayjs(stage.startedAt).format('YYYY-MM-DD HH:mm:ss')}
                  {stage.endedAt && ` · 耗时: ${Math.round(dayjs(stage.endedAt).diff(dayjs(stage.startedAt), 'minute'))}分钟`}
                </div>
              )}
              {stage.status === 'running' && (
                <div className="mt-2">
                  <div className="text-xs text-gray-500 mb-1">任务进度:</div>
                  <List
                    size="small"
                    dataSource={getStageTasks(stage.id)}
                    locale={{ emptyText: '暂无任务' }}
                    renderItem={task => (
                      <List.Item className="!py-1 !px-2 text-sm">
                        <div className="flex items-center justify-between w-full">
                          <span>{task.title}</span>
                          <Tag color={taskStatusConfig[task.status]?.color} className="!m-0">
                            {taskStatusConfig[task.status]?.text}
                          </Tag>
                        </div>
                      </List.Item>
                    )}
                  />
                </div>
              )}
            </div>
          ),
        }))}
      />
    );
  };

  return (
    <>
    <Tabs
      defaultActiveKey="stages"
      items={[
        {
          key: 'stages',
          label: '阶段进度',
          children: (
            <div className="pt-4">
              {stages.length > 0 ? renderStageTimeline() : <div className="text-gray-400 py-4">暂无阶段数据</div>}
            </div>
          ),
        },
        {
          key: 'info',
          label: '基本信息',
          children: (
            <Descriptions column={isMobile ? 1 : 2} bordered size="small" className="mt-4">
              <Descriptions.Item label="项目">{workflow.projectName}</Descriptions.Item>
              <Descriptions.Item label="需求">{workflow.requirementTitle}</Descriptions.Item>
              <Descriptions.Item label="状态">
                <Badge
                  status={statusConfig[workflow.status].color as any}
                  text={statusConfig[workflow.status].text}
                />
              </Descriptions.Item>
              <Descriptions.Item label="风险等级">
                <Tag color={workflow.riskLevel === 'critical' ? 'magenta' : workflow.riskLevel}>
                  {workflow.riskLevel.toUpperCase()}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="触发方式">
                <Tag icon={workflow.triggerMode === 'auto' ? <RobotOutlined /> : <UserOutlined />}>
                  {workflow.triggerMode === 'auto' ? '自动' : '手动'}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="进度">
                <Progress percent={workflow.progress} size="small" />
              </Descriptions.Item>
              <Descriptions.Item label="开始时间" span={2}>
                {dayjs(workflow.startedAt).format('YYYY-MM-DD HH:mm:ss')}
              </Descriptions.Item>
              {workflow.endedAt && (
                <Descriptions.Item label="结束时间" span={2}>
                  {dayjs(workflow.endedAt).format('YYYY-MM-DD HH:mm:ss')}
                </Descriptions.Item>
              )}
              {workflow.lastError && (
                <Descriptions.Item label="错误信息" span={2}>
                  <span className="text-red-500">{workflow.lastError}</span>
                </Descriptions.Item>
              )}
            </Descriptions>
          ),
        },
        {
          key: 'artifacts',
          label: '产物文件',
          children: (
            <List
              className="mt-4"
              dataSource={artifacts}
              locale={{ emptyText: '暂无产物文件' }}
              renderItem={artifact => (
                <List.Item
                  actions={[
                    <Button
                      key="view"
                      type="link"
                      size="small"
                      icon={<FileTextOutlined />}
                      onClick={() => {
                        setViewingArtifactId(artifact.id);
                        setViewingArtifactTitle(artifact.title);
                      }}
                    >
                      查看
                    </Button>,
                  ]}
                >
                  <List.Item.Meta
                    title={artifact.title}
                    description={`${artifact.path} · v${artifact.version}`}
                  />
                  <Tag color={artifact.status === 'approved' ? 'green' : 'blue'}>
                    {artifact.status}
                  </Tag>
                </List.Item>
              )}
            />
          ),
        },
        {
          key: 'decisions',
          label: <span><QuestionCircleOutlined /> 决策项</span>,
          children: (
            <div className="pt-4">
              <WorkflowDecisionsPanel workflowId={workflowId} />
            </div>
          ),
        },
        {
          key: 'reviews',
          label: <span><AuditOutlined /> 审核项</span>,
          children: (
            <div className="pt-4">
              <WorkflowReviewsPanel workflowId={workflowId} />
            </div>
          ),
        },
        {
          key: 'changes',
          label: <span><DiffOutlined /> 代码变更</span>,
          children: (
            <div className="pt-4">
              <WorkflowChangeSetsPanel workflowId={workflowId} />
            </div>
          ),
        },
      ]}
    />
    {viewingArtifactId && (
      <ArtifactContentViewer
        artifactId={viewingArtifactId}
        artifactTitle={viewingArtifactTitle}
        open={!!viewingArtifactId}
        onClose={() => setViewingArtifactId(null)}
      />
    )}
    </>
  );
}

export function Workflows() {
  const [viewingWorkflowId, setViewingWorkflowId] = useState<string | null>(null);
  const [viewMode, setViewMode] = useState<'list' | 'board'>('list');
  const [activeActionKey, setActiveActionKey] = useState('');
  const isMobile = useIsMobile();

  const { data: workflowsData, isLoading } = useWorkflows({ refetchInterval: 8000 });
  const updateRequirement = useUpdateRequirement();
  const workflows = workflowsData?.data || [];

  const runWorkflowRequirementAction = async (
    actionKey: string,
    loadingText: string,
    requirementId: string,
    status: 'running' | 'paused',
    successMessage: string,
    errorMessage: string,
  ) => {
    const messageKey = `workflow-action-${actionKey}`;
    setActiveActionKey(actionKey);
    message.open({ key: messageKey, type: 'loading', content: loadingText, duration: 0 });
    try {
      await Promise.race([
        updateRequirement.mutateAsync({ id: requirementId, data: { status } }),
        new Promise((_, reject) => {
          window.setTimeout(() => reject(new Error('timeout')), 15000);
        }),
      ]);
      message.success({ key: messageKey, content: successMessage });
    } catch (error) {
      const isTimeout = error instanceof Error && error.message === 'timeout';
      message.error({ key: messageKey, content: isTimeout ? '请求超时，请稍后重试' : errorMessage });
    } finally {
      setActiveActionKey(current => (current === actionKey ? '' : current));
    }
  };

  const running = workflows.filter(w => w.status === 'running').length;
  const completed = workflows.filter(w => w.status === 'completed').length;
  const pausedOrFailed = workflows.filter(w => ['paused', 'failed'].includes(w.status)).length;

  const columns: ColumnsType<WorkflowRun> = [
    {
      title: '工作流 ID',
      dataIndex: 'id',
      key: 'id',
      width: 100,
      render: (id, record) => (
        <a onClick={() => setViewingWorkflowId(record.id)} className="font-mono text-primary-500">
          {id.slice(0, 8)}
        </a>
      ),
    },
    {
      title: '项目 / 需求',
      key: 'project',
      ellipsis: true,
      render: (_, record) => (
        <div>
          <div className="font-medium">{record.projectName}</div>
          <div className="text-xs text-gray-500">{record.requirementTitle}</div>
        </div>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (status: WorkflowRun['status']) => {
        const config = statusConfig[status];
        return <Badge status={config.color as any} text={config.text} />;
      },
    },
    {
      title: '进度',
      dataIndex: 'progress',
      key: 'progress',
      width: 120,
      responsive: ['md'],
      render: (progress: number) => (
        <Progress percent={progress} size="small" status={progress === 100 ? 'success' : 'active'} />
      ),
    },
    {
      title: '当前阶段',
      dataIndex: 'currentStage',
      key: 'currentStage',
      width: 120,
      responsive: ['lg'],
      render: (stage) => {
        const def = STAGE_DEFINITIONS.find(s => s.name === stage);
        return <span>{def?.icon} {def?.displayName || stage}</span>;
      },
    },
    {
      title: '风险等级',
      dataIndex: 'riskLevel',
      key: 'riskLevel',
      width: 90,
      responsive: ['lg'],
      render: (level) => {
        const colors: Record<string, string> = { low: 'green', medium: 'orange', high: 'red', critical: 'magenta' };
        return <Tag color={colors[level]}>{level.toUpperCase()}</Tag>;
      },
    },
    {
      title: '开始时间',
      dataIndex: 'startedAt',
      key: 'startedAt',
      width: 120,
      responsive: ['sm'],
      render: (date) => dayjs(date).format('MM-DD HH:mm'),
    },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      fixed: 'right',
      render: (_, record) => (
        <Space size="small">
          <Tooltip title="查看详情">
            <Button type="text" size="small" icon={<EyeOutlined />} onClick={() => setViewingWorkflowId(record.id)} />
          </Tooltip>
          {record.status === 'paused' && (
            <Tooltip title="恢复执行">
              <Button
                type="text"
                size="small"
                icon={<PlayCircleOutlined className="text-green-500" />}
                loading={activeActionKey === `workflow-${record.id}-running`}
                onClick={() => void runWorkflowRequirementAction(
                  `workflow-${record.id}-running`,
                  '正在恢复需求执行...',
                  record.requirementId,
                  'running',
                  '工作流已恢复执行',
                  '恢复失败',
                )}
              />
            </Tooltip>
          )}
          {record.status === 'running' && (
            <Tooltip title="暂停">
              <Button
                type="text"
                size="small"
                icon={<PauseCircleOutlined className="text-orange-500" />}
                loading={activeActionKey === `workflow-${record.id}-paused`}
                onClick={() => void runWorkflowRequirementAction(
                  `workflow-${record.id}-paused`,
                  '正在暂停需求执行...',
                  record.requirementId,
                  'paused',
                  '工作流已暂停',
                  '暂停失败',
                )}
              />
            </Tooltip>
          )}
          {record.status === 'failed' && (
            <Tooltip title="重试">
              <Button
                type="text"
                size="small"
                icon={<ReloadOutlined className="text-blue-500" />}
                loading={activeActionKey === `workflow-${record.id}-retry`}
                onClick={() => void runWorkflowRequirementAction(
                  `workflow-${record.id}-retry`,
                  '正在重试需求执行...',
                  record.requirementId,
                  'running',
                  '工作流已重新开始',
                  '重试失败',
                )}
              />
            </Tooltip>
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
            工作流管理
          </h1>
          <p className="text-gray-500 dark:text-gray-400 mt-1">
            管理自动化研发工作流的执行和状态
          </p>
        </div>
      </div>

      {/* Stats */}
      <Row gutter={[16, 16]}>
        <Col xs={12} sm={6}>
          <Card>
            <Statistic
              title="运行中"
              value={running}
              prefix={<SyncOutlined spin className="text-blue-500" />}
            />
          </Card>
        </Col>
        <Col xs={12} sm={6}>
          <Card>
            <Statistic
              title="已完成"
              value={completed}
              prefix={<CheckCircleOutlined className="text-green-500" />}
            />
          </Card>
        </Col>
        <Col xs={12} sm={6}>
          <Card>
            <Statistic
              title="暂停/失败"
              value={pausedOrFailed}
              prefix={<ExclamationCircleOutlined className="text-orange-500" />}
            />
          </Card>
        </Col>
        <Col xs={12} sm={6}>
          <Card>
            <Statistic
              title="总计"
              value={workflows.length}
              prefix={<ThunderboltOutlined className="text-purple-500" />}
            />
          </Card>
        </Col>
      </Row>

      {/* Filters */}
      <Card styles={{ body: { padding: isMobile ? 12 : 16 } }}>
        <div className="flex flex-col sm:flex-row gap-4 items-start sm:items-center justify-between">
          <Segmented
            value={viewMode}
            onChange={(v) => setViewMode(v as 'list' | 'board')}
            options={[
              { value: 'list', label: '列表视图' },
              { value: 'board', label: '看板视图' },
            ]}
          />
          <Space>
            <Button icon={<ThunderboltOutlined />}>批量操作</Button>
          </Space>
        </div>
      </Card>

      {/* Table */}
      <Card styles={{ body: { padding: 0 } }}>
        <Table
          columns={columns}
          dataSource={workflows}
          rowKey="id"
          loading={isLoading}
          scroll={{ x: 800 }}
          pagination={{
            showSizeChanger: false,
            simple: isMobile,
          }}
          size={isMobile ? 'small' : 'middle'}
        />
      </Card>

      {/* Detail Drawer */}
      <Drawer
        title={`工作流详情 - ${viewingWorkflowId?.slice(0, 8)}`}
        open={!!viewingWorkflowId}
        onClose={() => setViewingWorkflowId(null)}
        width={isMobile ? '100%' : 720}
      >
        {viewingWorkflowId && (
          <WorkflowDetailContent workflowId={viewingWorkflowId} />
        )}
      </Drawer>
    </div>
  );
}
