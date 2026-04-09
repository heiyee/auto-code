import { useState } from 'react';
import {
  Card,
  Table,
  Button,
  Space,
  Input,
  Tag,
  Modal,
  Form,
  Select,
  Checkbox,
  Drawer,
  Descriptions,
  Tooltip,
  Popconfirm,
  message,
  Steps,
  Badge,
  Progress,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  PlusOutlined,
  EyeOutlined,
  EditOutlined,
  DeleteOutlined,
  PlayCircleOutlined,
  PauseCircleOutlined,
  ThunderboltOutlined,
  SyncOutlined,
  StepForwardOutlined,
  UpOutlined,
  DownOutlined,
} from '@ant-design/icons';
import {
  useRequirements,
  useProjects,
  useCreateRequirement,
  useUpdateRequirement,
  useDeleteRequirement,
  useRequirement,
  useWorkflows,
  useCLIProfiles,
} from '@/hooks';
import { useIsMobile } from '@/hooks/useMediaQuery';
import { useNavigate } from 'react-router-dom';
import dayjs from 'dayjs';
import type {
  Requirement,
  RequirementStatus,
  RequirementNoResponseAction,
  RequirementMutationInput,
} from '@/types';
import { getExecutionHealthMeta } from '@/utils/executionHealth';

const { Search } = Input;

const statusConfig: Record<RequirementStatus, { color: string; text: string }> = {
  planning: { color: 'default', text: '计划中' },
  running: { color: 'processing', text: '运行中' },
  paused: { color: 'warning', text: '已暂停' },
  failed: { color: 'error', text: '失败' },
  done: { color: 'success', text: '已完成' },
};

function getRequirementStatusMeta(status?: string) {
  const normalizedStatus = String(status || '').trim();
  return statusConfig[normalizedStatus as RequirementStatus] || {
    color: 'default',
    text: normalizedStatus || '未知状态',
  };
}

const statusSteps = [
  { title: '计划中' },
  { title: '运行中' },
  { title: '已完成' },
];

const workflowStatusConfig: Record<string, { color: string; text: string }> = {
  pending: { color: 'default', text: '等待中' },
  running: { color: 'processing', text: '执行中' },
  paused: { color: 'warning', text: '已暂停' },
  completed: { color: 'success', text: '已完成' },
  failed: { color: 'error', text: '失败' },
  canceled: { color: 'default', text: '已取消' },
};

const timeoutOptions = [
  { value: 0, label: '不启用' },
  { value: 5, label: '5 分钟' },
  { value: 10, label: '10 分钟' },
  { value: 15, label: '15 分钟' },
  { value: 20, label: '20 分钟' },
  { value: 30, label: '30 分钟' },
  { value: 45, label: '45 分钟' },
  { value: 60, label: '60 分钟' },
];

const errorActionOptions = [
  { value: 'none', label: '不处理' },
  { value: 'close_and_resend_requirement', label: '关闭并重新发送需求' },
] satisfies Array<{ value: RequirementNoResponseAction; label: string }>;

const idleActionOptions = [
  { value: 'none', label: '不处理' },
  { value: 'resend_requirement', label: '重新发送需求' },
] satisfies Array<{ value: RequirementNoResponseAction; label: string }>;

interface RequirementFormValues extends RequirementMutationInput {
  noResponseTimeoutMinutes: number;
  noResponseErrorAction: RequirementNoResponseAction;
  noResponseIdleAction: RequirementNoResponseAction;
}

function getWatchdogActionLabel(action?: string) {
  const value = String(action || '').trim();
  return [...errorActionOptions, ...idleActionOptions].find(item => item.value === value)?.label || value || '不处理';
}

function getWatchdogTriggerLabel(trigger?: string) {
  if (trigger === 'cli_error') {
    return 'CLI 报错/异常';
  }
  if (trigger === 'cli_idle') {
    return 'CLI 正常但无输出';
  }
  return trigger || '-';
}

function RequirementWorkflowInfo({ requirementId }: { requirementId: string }) {
  const navigate = useNavigate();
  const { data, isLoading } = useWorkflows({ requirementId });
  const workflows = data?.data || [];

  if (isLoading) return <SyncOutlined spin className="text-gray-400" />;
  if (workflows.length === 0) return <span className="text-gray-400 text-sm">无关联工作流</span>;

  const latest = workflows[0];
  const cfg = workflowStatusConfig[latest.status] || { color: 'default', text: latest.status };

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Badge status={cfg.color as any} text={cfg.text} />
        {latest.currentStage && (
          <span className="text-xs text-gray-500">当前阶段: {latest.currentStage}</span>
        )}
      </div>
      <Progress percent={latest.progress} size="small" status={latest.status === 'failed' ? 'exception' : 'active'} />
      <Button
        size="small"
        icon={<ThunderboltOutlined />}
        onClick={() => navigate('/workflows')}
      >
        查看工作流
      </Button>
    </div>
  );
}

export function Requirements() {
  const [statusFilter, setStatusFilter] = useState<string>('');
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [editingRequirement, setEditingRequirement] = useState<Requirement | null>(null);
  const [viewingRequirement, setViewingRequirement] = useState<Requirement | null>(null);
  const [activeActionKey, setActiveActionKey] = useState('');
  const [form] = Form.useForm();
  const isMobile = useIsMobile();

  const { data, isLoading, refetch } = useRequirements({ status: statusFilter });
  const { data: projectsData } = useProjects();
  const { data: cliProfilesData } = useCLIProfiles();
  const createMutation = useCreateRequirement();
  const updateMutation = useUpdateRequirement();
  const deleteMutation = useDeleteRequirement();

  const requirements = data?.data || [];
  const viewingRequirementId = viewingRequirement?.id || '';
  const { data: viewingRequirementData } = useRequirement(viewingRequirementId);
  const effectiveViewingRequirement = viewingRequirementData?.data || viewingRequirement;
  const viewingRequirementStatusMeta = effectiveViewingRequirement
    ? getRequirementStatusMeta(effectiveViewingRequirement.status)
    : null;
  const projects = projectsData?.data || [];
  const viewingRequirementHealth = effectiveViewingRequirement
    ? getExecutionHealthMeta(effectiveViewingRequirement.executionState, effectiveViewingRequirement.executionReason)
    : null;
  const cliTypeOptions = (cliProfilesData?.data ?? []).map(group => ({
    value: group.cli_type,
    label: group.cli_type === 'claude'
      ? 'Claude Code'
      : group.cli_type === 'codex'
        ? 'Codex'
        : group.cli_type === 'cursor'
          ? 'Cursor'
          : group.cli_type,
  }));
  const defaultCLIType = cliTypeOptions.find(option => option.value === 'codex')?.value ?? cliTypeOptions[0]?.value;

  const openCreateModal = () => {
    setEditingRequirement(null);
    form.resetFields();
    form.setFieldsValue({
      executionMode: 'manual',
      autoClearSession: true,
      cliType: defaultCLIType,
      noResponseTimeoutMinutes: 10,
      noResponseErrorAction: 'close_and_resend_requirement',
      noResponseIdleAction: 'resend_requirement',
      requiresDesignReview: false,
      requiresCodeReview: false,
      requiresAcceptanceReview: false,
      requiresReleaseApproval: false,
    });
    setCreateModalOpen(true);
  };

  const handleCreate = async (values: RequirementFormValues) => {
    try {
      if (editingRequirement) {
        await updateMutation.mutateAsync({ id: editingRequirement.id, data: values });
        message.success('需求更新成功');
      } else {
        await createMutation.mutateAsync(values);
        message.success('需求创建成功');
      }
      setCreateModalOpen(false);
      setEditingRequirement(null);
      form.resetFields();
      refetch();
    } catch {
      message.error('操作失败');
    }
  };

  const runRequirementAction = async <T,>(
    actionKey: string,
    loadingText: string,
    task: () => Promise<T>,
    successMessage: string,
    errorMessage: string,
  ) => {
    const messageKey = `requirement-action-${actionKey}`;
    setActiveActionKey(actionKey);
    message.open({ key: messageKey, type: 'loading', content: loadingText, duration: 0 });

    try {
      const result = await Promise.race<T>([
        task(),
        new Promise<T>((_, reject) => {
          window.setTimeout(() => reject(new Error('timeout')), 15000);
        }),
      ]);
      message.success({ key: messageKey, content: successMessage });
      return result;
    } catch (error) {
      const isTimeout = error instanceof Error && error.message === 'timeout';
      message.error({
        key: messageKey,
        content: isTimeout ? '请求超时，请稍后重试' : errorMessage,
      });
      throw error;
    } finally {
      setActiveActionKey(current => (current === actionKey ? '' : current));
    }
  };

  const shouldConfirmManualResume = (requirement: Requirement) => {
    if (requirement.status !== 'paused' && requirement.status !== 'failed') {
      return false;
    }
    if (requirement.retryBudgetExhausted) {
      return true;
    }
    if ((requirement.retryBudget ?? 0) <= 0) {
      return false;
    }
    return (requirement.autoRetryAttempts ?? 0) >= (requirement.retryBudget ?? 0);
  };

  const confirmManualResume = (requirement: Requirement) => new Promise<boolean>((resolve) => {
    if (!shouldConfirmManualResume(requirement)) {
      resolve(true);
      return;
    }
    Modal.confirm({
      title: '自动重试次数已达上限',
      content: `当前需求的自动重试次数已达到 ${requirement.autoRetryAttempts ?? 0}/${requirement.retryBudget ?? 0}。继续开始将作为人工确认重试，是否继续？`,
      okText: '继续开始',
      cancelText: '取消',
      onOk: () => resolve(true),
      onCancel: () => resolve(false),
    });
  });

  const handleStatusChange = async (
    requirement: Requirement,
    newStatus: RequirementStatus,
    options?: {
      successMessage?: string;
      errorMessage?: string;
      loadingMessage?: string;
      onSuccess?: () => void;
    },
  ) => {
    try {
      if (newStatus === 'running') {
        const confirmed = await confirmManualResume(requirement);
        if (!confirmed) {
          return;
        }
      }
      await runRequirementAction(
        `status-${requirement.id}-${newStatus}`,
        options?.loadingMessage || '正在更新需求状态...',
        () => updateMutation.mutateAsync({ id: requirement.id, data: { status: newStatus } }),
        options?.successMessage || '状态更新成功',
        options?.errorMessage || '更新失败',
      );
      options?.onSuccess?.();
      refetch();
    } catch {
      // handled by runRequirementAction
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deleteMutation.mutateAsync(id);
      message.success('需求删除成功');
      refetch();
    } catch {
      message.error('删除失败');
    }
  };

  const handleMoveRequirement = async (requirement: Requirement, targetSortOrder: number) => {
    try {
      await runRequirementAction(
        `move-${requirement.id}`,
        '正在更新需求顺序...',
        () => updateMutation.mutateAsync({ id: requirement.id, data: { sortOrder: targetSortOrder } }),
        '需求顺序已更新',
        '更新顺序失败',
      );
      refetch();
    } catch {
      // handled by runRequirementAction
    }
  };

  const handleEdit = (requirement: Requirement) => {
    setEditingRequirement(requirement);
    form.setFieldsValue({
      ...requirement,
      noResponseTimeoutMinutes: requirement.noResponseTimeoutMinutes ?? 0,
      noResponseErrorAction: requirement.noResponseErrorAction ?? 'none',
      noResponseIdleAction: requirement.noResponseIdleAction ?? 'none',
      requiresDesignReview: requirement.requiresDesignReview ?? false,
      requiresCodeReview: requirement.requiresCodeReview ?? false,
      requiresAcceptanceReview: requirement.requiresAcceptanceReview ?? false,
      requiresReleaseApproval: requirement.requiresReleaseApproval ?? false,
    });
    setCreateModalOpen(true);
  };

  const getCurrentStep = (status?: string) => {
    if (status === 'done') return 2;
    if (status === 'running') return 1;
    return 0;
  };

  const columns: ColumnsType<Requirement> = [
    {
      title: '顺序',
      key: 'sortOrder',
      width: 140,
      fixed: 'left',
      render: (_, record) => {
        const currentIndex = requirements.findIndex(item => item.id === record.id);
        const currentOrder = record.sortOrder ?? currentIndex + 1;
        const canMoveUp = currentIndex > 0;
        const canMoveDown = currentIndex >= 0 && currentIndex < requirements.length - 1;

        return (
          <Space size="small">
            <Tag className="!mr-0 min-w-[42px] text-center">{currentOrder}</Tag>
            <Button
              type="text"
              size="small"
              icon={<UpOutlined />}
              disabled={!canMoveUp || updateMutation.isPending}
              loading={activeActionKey === `move-${record.id}`}
              onClick={() => void handleMoveRequirement(record, currentOrder - 1)}
            />
            <Button
              type="text"
              size="small"
              icon={<DownOutlined />}
              disabled={!canMoveDown || updateMutation.isPending}
              loading={activeActionKey === `move-${record.id}`}
              onClick={() => void handleMoveRequirement(record, currentOrder + 1)}
            />
          </Space>
        );
      },
    },
    {
      title: '需求标题',
      dataIndex: 'title',
      key: 'title',
      ellipsis: true,
      render: (text, record) => (
        <a onClick={() => setViewingRequirement(record)} className="font-medium text-primary-500">
          {text}
        </a>
      ),
    },
    {
      title: '项目',
      dataIndex: 'projectName',
      key: 'projectName',
      width: 150,
      ellipsis: true,
      responsive: ['md'],
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (status: RequirementStatus) => {
        const config = getRequirementStatusMeta(status);
        return <Badge status={config.color as any} text={config.text} />;
      },
    },
    {
      title: '执行模式',
      dataIndex: 'executionMode',
      key: 'executionMode',
      width: 180,
      responsive: ['lg'],
      render: (_, record) => {
        const health = getExecutionHealthMeta(record.executionState, record.executionReason);
        return (
          <Space size={[4, 4]} wrap>
            <Tag color={record.executionMode === 'auto' ? 'green' : 'blue'}>
              {record.executionMode === 'auto' ? '自动' : '手动'}
            </Tag>
            {health && (
              <Tag color={health.color}>
                {health.text}
              </Tag>
            )}
          </Space>
        );
      },
    },
    {
      title: '创建时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 140,
      responsive: ['sm'],
      render: (date) => dayjs(date).format('MM-DD HH:mm'),
    },
    {
      title: '操作',
      key: 'actions',
      width: 220,
      fixed: 'right',
      render: (_, record) => (
        <Space size="small">
          <Tooltip title="查看">
            <Button
              type="text"
              size="small"
              icon={<EyeOutlined />}
              onClick={() => setViewingRequirement(record)}
            />
          </Tooltip>
          {record.status === 'planning' && (
            <Tooltip title="开始">
              <Button
                type="text"
                size="small"
                icon={<PlayCircleOutlined className="text-green-500" />}
                loading={activeActionKey === `status-${record.id}-running`}
                onClick={() => void handleStatusChange(record, 'running', {
                  loadingMessage: '正在开始需求...',
                })}
              />
            </Tooltip>
          )}
          {record.status === 'running' && (
            <Tooltip title="暂停">
              <Button
                type="text"
                size="small"
                icon={<PauseCircleOutlined className="text-orange-500" />}
                loading={activeActionKey === `status-${record.id}-paused`}
                onClick={() => void handleStatusChange(record, 'paused', {
                  loadingMessage: '正在暂停需求...',
                })}
              />
            </Tooltip>
          )}
          {record.status === 'paused' && (
            <Tooltip title="继续">
              <Button
                type="text"
                size="small"
                icon={<PlayCircleOutlined className="text-green-500" />}
                loading={activeActionKey === `status-${record.id}-running`}
                onClick={() => void handleStatusChange(record, 'running', {
                  loadingMessage: '正在恢复需求...',
                })}
              />
            </Tooltip>
          )}
          {record.status === 'failed' && (
            <Tooltip title="重试">
              <Button
                type="text"
                size="small"
                aria-label={`重试需求 ${record.title}`}
                icon={<PlayCircleOutlined className="text-green-500" />}
                loading={activeActionKey === `status-${record.id}-running`}
                onClick={() => void handleStatusChange(record, 'running', {
                  loadingMessage: '正在重试需求...',
                  successMessage: '重试已开始',
                  errorMessage: '重试失败',
                })}
              />
            </Tooltip>
          )}
          {record.status === 'failed' && (
            <Popconfirm
              title="跳过当前失败需求，并继续后续自动队列？"
              description="跳过后当前需求将标记为完成，后续需求会继续推进。"
              onConfirm={() => handleStatusChange(record, 'done', {
                loadingMessage: '正在跳过需求...',
                successMessage: '已跳过失败需求，后续队列将继续推进',
                errorMessage: '跳过失败',
              })}
              okText="确定跳过"
              cancelText="取消"
            >
              <Tooltip title="跳过并继续队列">
                <Button
                  type="text"
                  size="small"
                  aria-label={`跳过需求 ${record.title}`}
                  icon={<StepForwardOutlined className="text-blue-500" />}
                />
              </Tooltip>
            </Popconfirm>
          )}
          <Tooltip title="编辑">
            <Button
              type="text"
              size="small"
              icon={<EditOutlined />}
              onClick={() => handleEdit(record)}
            />
          </Tooltip>
          <Popconfirm
            title="确定要删除此需求吗？"
            onConfirm={() => handleDelete(record.id)}
            okText="确定"
            cancelText="取消"
          >
            <Tooltip title="删除">
              <Button type="text" size="small" danger icon={<DeleteOutlined />} />
            </Tooltip>
          </Popconfirm>
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
            需求管理
          </h1>
          <p className="text-gray-500 dark:text-gray-400 mt-1">
            管理项目的需求和任务
          </p>
        </div>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={openCreateModal}
          className="w-full sm:w-auto"
        >
          新建需求
        </Button>
      </div>

      {/* Filters */}
      <Card styles={{ body: { padding: isMobile ? 12 : 16 } }}>
        <div className="flex flex-col sm:flex-row gap-4 items-start sm:items-center">
          <Search
            placeholder="搜索需求..."
            allowClear
            onSearch={(value) => console.log('Search:', value)}
            className="w-full sm:w-64"
          />
          <Select
            placeholder="状态筛选"
            allowClear
            style={{ width: isMobile ? '100%' : 120 }}
            onChange={(value) => setStatusFilter(value || '')}
            options={[
              { value: 'planning', label: '计划中' },
              { value: 'running', label: '运行中' },
              { value: 'paused', label: '已暂停' },
              { value: 'failed', label: '失败' },
              { value: 'done', label: '已完成' },
            ]}
          />
        </div>
      </Card>

      {/* Table */}
      <Card styles={{ body: { padding: 0 } }}>
        <Table
          columns={columns}
          dataSource={requirements}
          rowKey="id"
          loading={isLoading}
          scroll={{ x: 700 }}
          pagination={false}
          size={isMobile ? 'small' : 'middle'}
        />
      </Card>

      {/* Create/Edit Modal */}
      <Modal
        title={editingRequirement ? '编辑需求' : '新建需求'}
        open={createModalOpen}
        onCancel={() => {
          setCreateModalOpen(false);
          setEditingRequirement(null);
          form.resetFields();
        }}
        footer={null}
        width={isMobile ? '100%' : 600}
        style={isMobile ? { top: 0, paddingBottom: 0 } : {}}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleCreate}
          className="mt-4"
        >
          <Form.Item
            name="projectId"
            label="所属项目"
            rules={[{ required: true, message: '请选择项目' }]}
          >
            <Select
              placeholder="请选择项目"
              options={projects.map(p => ({ value: p.id, label: p.name }))}
            />
          </Form.Item>
          <Form.Item
            name="title"
            label="需求标题"
            rules={[{ required: true, message: '请输入需求标题' }]}
          >
            <Input placeholder="请输入需求标题" />
          </Form.Item>
          <Form.Item
            name="description"
            label="需求描述"
            rules={[{ required: true, message: '请输入需求描述' }]}
          >
            <Input.TextArea rows={4} placeholder="详细描述需求内容..." />
          </Form.Item>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <Form.Item
              name="executionMode"
              label="执行模式"
              initialValue="manual"
            >
              <Select
                options={[
                  { value: 'manual', label: '手动执行' },
                  { value: 'auto', label: '自动执行' },
                ]}
              />
            </Form.Item>
            <Form.Item
              name="cliType"
              label="CLI 类型"
            >
              <Select
                placeholder={cliTypeOptions.length > 0 ? '请选择 CLI 类型' : '后端未配置 CLI 类型'}
                options={cliTypeOptions}
              />
            </Form.Item>
          </div>
          <Form.Item
            name="autoClearSession"
            label="自动清理会话"
            initialValue={true}
          >
            <Select
              options={[
                { value: true, label: '是' },
                { value: false, label: '否' },
              ]}
            />
          </Form.Item>
          <Card size="small" title="流程门禁配置" className="mb-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <Form.Item name="requiresDesignReview" valuePropName="checked" className="mb-0">
                <Checkbox>设计审核</Checkbox>
              </Form.Item>
              <Form.Item name="requiresCodeReview" valuePropName="checked" className="mb-0">
                <Checkbox>代码评审</Checkbox>
              </Form.Item>
              <Form.Item name="requiresAcceptanceReview" valuePropName="checked" className="mb-0">
                <Checkbox>人工验收</Checkbox>
              </Form.Item>
              <Form.Item name="requiresReleaseApproval" valuePropName="checked" className="mb-0">
                <Checkbox>发布审批</Checkbox>
              </Form.Item>
            </div>
          </Card>
          <Card size="small" title="执行看护策略" className="mb-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <Form.Item
                name="noResponseTimeoutMinutes"
                label="无响应阈值"
                initialValue={10}
                extra="基于最后一次输出时间计算，0 表示关闭自动处理。"
              >
                <Select options={timeoutOptions} />
              </Form.Item>
              <Form.Item
                name="noResponseErrorAction"
                label="CLI 报错/异常时"
                initialValue="close_and_resend_requirement"
              >
                <Select options={errorActionOptions} />
              </Form.Item>
            </div>
            <Form.Item
              name="noResponseIdleAction"
              label="CLI 正常但无输出时"
              initialValue="resend_requirement"
              className="mb-0"
            >
              <Select options={idleActionOptions} />
            </Form.Item>
          </Card>
          <Form.Item className="mb-0 text-right">
            <Space>
              <Button onClick={() => {
                setCreateModalOpen(false);
                setEditingRequirement(null);
                form.resetFields();
              }}>
                取消
              </Button>
              <Button type="primary" htmlType="submit" loading={createMutation.isPending}>
                {editingRequirement ? '更新' : '创建'}
              </Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>

      {/* View Drawer */}
      <Drawer
        title="需求详情"
        open={!!viewingRequirement}
        onClose={() => setViewingRequirement(null)}
        width={isMobile ? '100%' : 480}
      >
        {effectiveViewingRequirement && (
          <div className="space-y-6">
            <Steps
              current={getCurrentStep(effectiveViewingRequirement.status)}
              items={statusSteps}
              size="small"
            />
            <Descriptions column={1} bordered size="small">
              <Descriptions.Item label="需求标题">{effectiveViewingRequirement.title}</Descriptions.Item>
              <Descriptions.Item label="顺序">{effectiveViewingRequirement.sortOrder ?? '-'}</Descriptions.Item>
              <Descriptions.Item label="所属项目">{effectiveViewingRequirement.projectName}</Descriptions.Item>
              <Descriptions.Item label="状态">
                <Badge
                  status={viewingRequirementStatusMeta?.color as any}
                  text={viewingRequirementStatusMeta?.text}
                />
              </Descriptions.Item>
              <Descriptions.Item label="执行模式">
                <Space size={[4, 4]} wrap>
                  <Tag color={effectiveViewingRequirement.executionMode === 'auto' ? 'green' : 'blue'}>
                    {effectiveViewingRequirement.executionMode === 'auto' ? '自动' : '手动'}
                  </Tag>
                  {viewingRequirementHealth && (
                    <Tag color={viewingRequirementHealth.color}>
                      {viewingRequirementHealth.text}
                    </Tag>
                  )}
                </Space>
              </Descriptions.Item>
              <Descriptions.Item label="流程门禁">
                <Space size={[4, 4]} wrap>
                  {effectiveViewingRequirement.requiresDesignReview && <Tag color="blue">设计审核</Tag>}
                  {effectiveViewingRequirement.requiresCodeReview && <Tag color="green">代码评审</Tag>}
                  {effectiveViewingRequirement.requiresAcceptanceReview && <Tag color="purple">人工验收</Tag>}
                  {effectiveViewingRequirement.requiresReleaseApproval && <Tag color="orange">发布审批</Tag>}
                  {!effectiveViewingRequirement.requiresDesignReview &&
                    !effectiveViewingRequirement.requiresCodeReview &&
                    !effectiveViewingRequirement.requiresAcceptanceReview &&
                    !effectiveViewingRequirement.requiresReleaseApproval && (
                    <span className="text-gray-400">未配置门禁</span>
                  )}
                </Space>
              </Descriptions.Item>
              {viewingRequirementHealth && (
                <Descriptions.Item label="执行状态说明">
                  {viewingRequirementHealth.reasonText || viewingRequirementHealth.text}
                </Descriptions.Item>
              )}
              <Descriptions.Item label="CLI 类型">{effectiveViewingRequirement.cliType}</Descriptions.Item>
              <Descriptions.Item label="无响应阈值">
                {effectiveViewingRequirement.noResponseTimeoutMinutes && effectiveViewingRequirement.noResponseTimeoutMinutes > 0
                  ? `${effectiveViewingRequirement.noResponseTimeoutMinutes} 分钟`
                  : '不启用'}
              </Descriptions.Item>
              <Descriptions.Item label="CLI 报错策略">
                {getWatchdogActionLabel(effectiveViewingRequirement.noResponseErrorAction)}
              </Descriptions.Item>
              <Descriptions.Item label="CLI 无输出策略">
                {getWatchdogActionLabel(effectiveViewingRequirement.noResponseIdleAction)}
              </Descriptions.Item>
              {effectiveViewingRequirement.lastWatchdogEvent && (
                <Descriptions.Item label="最近看护动作">
                  <div className="space-y-1">
                    <div>{getWatchdogTriggerLabel(effectiveViewingRequirement.lastWatchdogEvent.triggerKind)}</div>
                    <div>{getWatchdogActionLabel(effectiveViewingRequirement.lastWatchdogEvent.action)}</div>
                    <div>结果：{effectiveViewingRequirement.lastWatchdogEvent.status}</div>
                    <div>{dayjs(effectiveViewingRequirement.lastWatchdogEvent.createdAt).format('YYYY-MM-DD HH:mm:ss')}</div>
                    {effectiveViewingRequirement.lastWatchdogEvent.detail && (
                      <div className="text-gray-500">{effectiveViewingRequirement.lastWatchdogEvent.detail}</div>
                    )}
                  </div>
                </Descriptions.Item>
              )}
              <Descriptions.Item label="描述">{effectiveViewingRequirement.description}</Descriptions.Item>
              <Descriptions.Item label="创建时间">
                {dayjs(effectiveViewingRequirement.createdAt).format('YYYY-MM-DD HH:mm:ss')}
              </Descriptions.Item>
              {effectiveViewingRequirement.startedAt && (
                <Descriptions.Item label="开始时间">
                  {dayjs(effectiveViewingRequirement.startedAt).format('YYYY-MM-DD HH:mm:ss')}
                </Descriptions.Item>
              )}
              {effectiveViewingRequirement.promptSentAt && (
                <Descriptions.Item label="提示词下发">
                  {dayjs(effectiveViewingRequirement.promptSentAt).format('YYYY-MM-DD HH:mm:ss')}
                </Descriptions.Item>
              )}
              {effectiveViewingRequirement.promptReplayedAt && (
                <Descriptions.Item label="提示词重发">
                  {dayjs(effectiveViewingRequirement.promptReplayedAt).format('YYYY-MM-DD HH:mm:ss')}
                </Descriptions.Item>
              )}
              {effectiveViewingRequirement.endedAt && (
                <Descriptions.Item label="完成时间">
                  {dayjs(effectiveViewingRequirement.endedAt).format('YYYY-MM-DD HH:mm:ss')}
                </Descriptions.Item>
              )}
              {effectiveViewingRequirement.lastOutputAt && (
                <Descriptions.Item label="最近输出">
                  {dayjs(effectiveViewingRequirement.lastOutputAt).format('YYYY-MM-DD HH:mm:ss')}
                </Descriptions.Item>
              )}
            </Descriptions>

            <div>
              <div className="text-sm font-medium text-gray-600 mb-2">工作流状态</div>
              <RequirementWorkflowInfo requirementId={effectiveViewingRequirement.id} />
            </div>

            <div className="flex gap-2">
              {effectiveViewingRequirement.status !== 'done' && effectiveViewingRequirement.status !== 'running' && (
                <Button
                  type="primary"
                  icon={<PlayCircleOutlined />}
                  onClick={() => {
                    handleStatusChange(effectiveViewingRequirement, 'running', {
                      loadingMessage: effectiveViewingRequirement.status === 'paused' ? '正在恢复需求...' : '正在开始需求...',
                      successMessage: effectiveViewingRequirement.status === 'failed' ? '重试已开始' : '状态更新成功',
                      errorMessage: effectiveViewingRequirement.status === 'failed' ? '重试失败' : '更新失败',
                      onSuccess: () => setViewingRequirement(current => current ? { ...current, status: 'running' } : current),
                    });
                  }}
                  loading={activeActionKey === `status-${effectiveViewingRequirement.id}-running`}
                >
                  {effectiveViewingRequirement.status === 'failed'
                    ? '手动重试'
                    : effectiveViewingRequirement.status === 'paused'
                      ? '继续执行'
                      : '开始执行'}
                </Button>
              )}
              {effectiveViewingRequirement.status === 'running' && (
                <Button
                  icon={<PauseCircleOutlined />}
                  onClick={() => {
                    handleStatusChange(effectiveViewingRequirement, 'paused', {
                      loadingMessage: '正在暂停需求...',
                      onSuccess: () => setViewingRequirement(current => current ? { ...current, status: 'paused' } : current),
                    });
                  }}
                  loading={activeActionKey === `status-${effectiveViewingRequirement.id}-paused`}
                >
                  暂停执行
                </Button>
              )}
              {effectiveViewingRequirement.status !== 'done' && effectiveViewingRequirement.status !== 'failed' && (
                <Button
                  onClick={() => {
                    handleStatusChange(effectiveViewingRequirement, 'done', {
                      loadingMessage: '正在标记完成...',
                      onSuccess: () => setViewingRequirement(current => current ? { ...current, status: 'done' } : current),
                    });
                  }}
                  loading={activeActionKey === `status-${effectiveViewingRequirement.id}-done`}
                >
                  标记完成
                </Button>
              )}
              {effectiveViewingRequirement.status === 'failed' && (
                <Popconfirm
                  title="跳过当前失败需求，并继续后续自动队列？"
                  description="跳过后会将当前需求标记为完成，后续需求可继续推进。"
                  onConfirm={() => {
                    handleStatusChange(effectiveViewingRequirement, 'done', {
                      loadingMessage: '正在跳过需求...',
                      successMessage: '已跳过失败需求，后续队列将继续推进',
                      errorMessage: '跳过失败',
                      onSuccess: () => setViewingRequirement(current => current ? { ...current, status: 'done' } : current),
                    });
                  }}
                  okText="确定跳过"
                  cancelText="取消"
                >
                  <Button icon={<StepForwardOutlined />}>
                    跳过并继续队列
                  </Button>
                </Popconfirm>
              )}
            </div>
          </div>
        )}
      </Drawer>
    </div>
  );
}
