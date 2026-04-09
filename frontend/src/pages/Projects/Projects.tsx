import { useEffect, useState } from 'react';
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
  Drawer,
  Descriptions,
  Tooltip,
  Popconfirm,
  message,
  Segmented,
  Switch,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  PlusOutlined,
  EyeOutlined,
  EditOutlined,
  DeleteOutlined,
  AppstoreOutlined,
  BarsOutlined,
  GithubOutlined,
  FolderOutlined,
  ThunderboltOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
} from '@ant-design/icons';
import {
  useProjects,
  useCreateProject,
  useUpdateProject,
  useDeleteProject,
  useInspectLocalRepo,
  useSolutionTemplates,
  useBootstrapSolution,
  useCLIProfiles,
} from '@/hooks';
import { useIsMobile } from '@/hooks/useMediaQuery';
import { useNavigate } from 'react-router-dom';
import dayjs from 'dayjs';
import type { GitLocalRepoInspection, Project } from '@/types';

const { Search } = Input;

type ProjectFormValues = {
  name: string;
  repository: string;
  branch: string;
  workDir: string;
  automationPaused?: boolean;
  description?: string;
};

type BranchOption = {
  value: string;
  label: string;
};

function buildBranchOptions(branches: string[], fallbackBranch = ''): BranchOption[] {
  const seen = new Set<string>();
  const values = [...branches, fallbackBranch]
    .map(branch => String(branch || '').trim())
    .filter(branch => {
      if (!branch || seen.has(branch)) {
        return false;
      }
      seen.add(branch);
      return true;
    });

  return values.map(branch => ({
    value: branch,
    label: branch,
  }));
}

function resolveLocalRepoInspection(
  workDir: string,
  payload: GitLocalRepoInspection,
  currentRepository: string,
  currentBranch: string,
) {
  const branches = Array.isArray(payload.branches)
    ? payload.branches.map(branch => String(branch || '').trim()).filter(Boolean)
    : [];
  if (branches.length === 0) {
    throw new Error('未读取到本地分支，请确认该目录是 Git 仓库');
  }

  const preferredBranch = payload.currentBranch && payload.currentBranch !== 'HEAD'
    ? payload.currentBranch
    : currentBranch || branches[0];

  return {
    repository: payload.remoteURL || currentRepository || workDir,
    selectedBranch: preferredBranch,
    branchOptions: buildBranchOptions(branches, preferredBranch),
  };
}

export function Projects() {
  const navigate = useNavigate();
  const [searchText, setSearchText] = useState('');
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [bootstrapModalOpen, setBootstrapModalOpen] = useState(false);
  const [editingProject, setEditingProject] = useState<Project | null>(null);
  const [viewingProject, setViewingProject] = useState<Project | null>(null);
  const [viewMode, setViewMode] = useState<'list' | 'card'>('list');
  const [branchOptions, setBranchOptions] = useState<BranchOption[]>([]);
  const [lastLoadedWorkDir, setLastLoadedWorkDir] = useState('');
  const [branchLoadError, setBranchLoadError] = useState('');
  const [form] = Form.useForm();
  const [bootstrapForm] = Form.useForm();
  const isMobile = useIsMobile();
  const workDirValue = Form.useWatch('workDir', form);
  const trimmedWorkDir = String(workDirValue || '').trim();

  const { data, isLoading, refetch } = useProjects({ search: searchText });
  const { data: templatesData } = useSolutionTemplates();
  const { data: cliProfilesData } = useCLIProfiles();
  const createMutation = useCreateProject();
  const updateMutation = useUpdateProject();
  const deleteMutation = useDeleteProject();
  const inspectLocalRepo = useInspectLocalRepo();
  const bootstrapMutation = useBootstrapSolution();

  const projects = data?.data || [];
  const pagination = data ? { total: data.total, page: data.page, pageSize: data.pageSize } : null;
  const templates = templatesData?.data ?? [];
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
  const submittingProject = createMutation.isPending || updateMutation.isPending;

  const closeCreateModal = () => {
    setCreateModalOpen(false);
    setEditingProject(null);
    setBranchOptions([]);
    setLastLoadedWorkDir('');
    setBranchLoadError('');
    form.resetFields();
  };

  const resetProjectForm = (project: Project | null) => {
    form.resetFields();
    if (project) {
      form.setFieldsValue(project);
    }
    setBranchOptions(buildBranchOptions([], String(project?.branch || '').trim()));
    setLastLoadedWorkDir('');
    setBranchLoadError('');
  };

  const applyRepoInspection = (workDir: string, payload: GitLocalRepoInspection) => {
    const resolved = resolveLocalRepoInspection(
      workDir,
      payload,
      String(form.getFieldValue('repository') || '').trim(),
      String(form.getFieldValue('branch') || '').trim(),
    );

    setBranchOptions(resolved.branchOptions);
    form.setFieldsValue({
      repository: resolved.repository,
      branch: resolved.selectedBranch,
    });
    setLastLoadedWorkDir(workDir);
    setBranchLoadError('');
  };

  const loadProjectGitInfo = async (silent = false) => {
    const workDir = String(form.getFieldValue('workDir') || '').trim();
    if (!workDir) {
      if (!silent) {
        message.error('请先填写本地工作目录');
      }
      return;
    }

    try {
      const payload = await inspectLocalRepo.mutateAsync(workDir);
      if (String(form.getFieldValue('workDir') || '').trim() !== workDir) {
        return;
      }
      applyRepoInspection(workDir, payload);
      if (!silent) {
        message.success('已根据工作目录读取仓库地址和可用分支');
      }
    } catch (error) {
      if (String(form.getFieldValue('workDir') || '').trim() !== workDir) {
        return;
      }
      const errorMessage = error instanceof Error ? error.message : '读取本地分支失败';
      setBranchLoadError(errorMessage);
      if (!silent) {
        message.error(errorMessage);
      }
    }
  };

  useEffect(() => {
    if (!createModalOpen || !trimmedWorkDir || trimmedWorkDir === lastLoadedWorkDir) {
      return;
    }

    const timer = window.setTimeout(async () => {
      try {
        const payload = await inspectLocalRepo.mutateAsync(trimmedWorkDir);
        if (String(form.getFieldValue('workDir') || '').trim() !== trimmedWorkDir) {
          return;
        }
        applyRepoInspection(trimmedWorkDir, payload);
      } catch (error) {
        if (String(form.getFieldValue('workDir') || '').trim() !== trimmedWorkDir) {
          return;
        }
        const errorMessage = error instanceof Error ? error.message : '读取本地分支失败';
        setBranchLoadError(errorMessage);
      }
    }, 400);

    return () => window.clearTimeout(timer);
  }, [createModalOpen, trimmedWorkDir, lastLoadedWorkDir, inspectLocalRepo, form]);

  const handleCreate = async (values: ProjectFormValues) => {
    try {
      if (editingProject) {
        await updateMutation.mutateAsync({ id: editingProject.id, data: values });
        message.success('项目更新成功');
      } else {
        await createMutation.mutateAsync(values);
        message.success('项目创建成功');
      }
      closeCreateModal();
      refetch();
    } catch {
      message.error('操作失败');
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deleteMutation.mutateAsync(id);
      message.success('项目删除成功');
      refetch();
    } catch {
      message.error('删除失败');
    }
  };

  const handleAutomationPauseChange = async (project: Project, paused: boolean) => {
    try {
      await updateMutation.mutateAsync({ id: project.id, data: { automationPaused: paused } });
      message.success(paused ? '项目自动推进已暂停' : '项目自动推进已恢复');
      if (viewingProject?.id === project.id) {
        setViewingProject({ ...project, automationPaused: paused });
      }
      refetch();
    } catch {
      message.error(paused ? '暂停失败' : '恢复失败');
    }
  };

  const handleEdit = (project: Project) => {
    setEditingProject(project);
    resetProjectForm(project);
    setCreateModalOpen(true);
  };

  const openBootstrapModal = () => {
    bootstrapForm.resetFields();
    bootstrapForm.setFieldsValue({
      templateId: templates[0]?.id,
      branch: 'main',
      cliType: defaultCLIType,
      autoClearSession: true,
    });
    setBootstrapModalOpen(true);
  };

  const handleBootstrap = async (values: {
    appName: string;
    templateId?: string;
    repository?: string;
    branch?: string;
    workDir?: string;
    cliType?: string;
    autoClearSession?: boolean;
  }) => {
    try {
      const result = await bootstrapMutation.mutateAsync(values);
      const count = result.data.requirements.length;
      message.success(`已创建项目并拆分 ${count} 条自动需求，系统已开始自动推进`);
      setBootstrapModalOpen(false);
      bootstrapForm.resetFields();
      refetch();
      navigate('/requirements');
    } catch {
      message.error('生成失败');
    }
  };

  const columns: ColumnsType<Project> = [
    {
      title: '项目名称',
      dataIndex: 'name',
      key: 'name',
      ellipsis: true,
      render: (text, record) => (
        <a onClick={() => setViewingProject(record)} className="font-medium text-primary-500">
          {text}
        </a>
      ),
    },
    {
      title: '仓库地址',
      dataIndex: 'repository',
      key: 'repository',
      ellipsis: true,
      responsive: ['md'],
      render: (text) => (
        <Tooltip title={text}>
          <a href={text} target="_blank" rel="noopener noreferrer" className="text-gray-600 dark:text-gray-400">
            <GithubOutlined className="mr-1" />
            {text.replace('https://github.com/', '')}
          </a>
        </Tooltip>
      ),
    },
    {
      title: '分支',
      dataIndex: 'branch',
      key: 'branch',
      width: 100,
      responsive: ['lg'],
      render: (branch) => <Tag color="blue">{branch}</Tag>,
    },
    {
      title: '自动推进',
      key: 'automationPaused',
      width: 120,
      responsive: ['md'],
      render: (_, record) => (
        <Switch
          size="small"
          checked={!record.automationPaused}
          checkedChildren="运行"
          unCheckedChildren="暂停"
          loading={updateMutation.isPending}
          onClick={(checked, event) => {
            event?.stopPropagation();
            void handleAutomationPauseChange(record, !checked);
          }}
        />
      ),
    },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      key: 'updatedAt',
      width: 150,
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
          <Tooltip title="查看">
            <Button
              type="text"
              size="small"
              icon={<EyeOutlined />}
              onClick={() => setViewingProject(record)}
            />
          </Tooltip>
          <Tooltip title="编辑">
            <Button
              type="text"
              size="small"
              icon={<EditOutlined />}
              onClick={() => handleEdit(record)}
            />
          </Tooltip>
          <Popconfirm
            title="确定要删除此项目吗？"
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

  // Card view component
  const ProjectCard = ({ project }: { project: Project }) => (
    <Card
      hoverable
      className="h-full"
      onClick={() => setViewingProject(project)}
      styles={{ body: { padding: 16 } }}
    >
      <div className="space-y-3">
        <div className="flex items-start justify-between">
          <h3 className="font-medium text-gray-800 dark:text-white truncate flex-1">
            {project.name}
          </h3>
          <Space size={[4, 4]} wrap>
            <Tag color="blue">{project.branch}</Tag>
            <Tag color={project.automationPaused ? 'red' : 'green'}>
              {project.automationPaused ? '自动暂停' : '自动运行'}
            </Tag>
          </Space>
        </div>
        {project.description && (
          <p className="text-sm text-gray-500 dark:text-gray-400 line-clamp-2">
            {project.description}
          </p>
        )}
        <div className="flex items-center text-sm text-gray-400 gap-2">
          <GithubOutlined />
          <span className="truncate">
            {project.repository.replace('https://github.com/', '')}
          </span>
        </div>
        <div className="flex items-center text-sm text-gray-400">
          <FolderOutlined className="mr-2" />
          <span className="truncate">{project.workDir}</span>
        </div>
        <div className="pt-2 border-t flex justify-between items-center">
          <span className="text-xs text-gray-400">
            更新于 {dayjs(project.updatedAt).format('MM-DD HH:mm')}
          </span>
          <Space size="small">
            <Button
              type="text"
              size="small"
              icon={<EditOutlined />}
              onClick={(e) => {
                e.stopPropagation();
                handleEdit(project);
              }}
            />
            <Button
              type="text"
              size="small"
              danger
              icon={<DeleteOutlined />}
              onClick={(e) => {
                e.stopPropagation();
                handleDelete(project.id);
              }}
            />
          </Space>
        </div>
      </div>
    </Card>
  );

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-xl sm:text-2xl font-bold text-gray-800 dark:text-white">
            项目管理
          </h1>
          <p className="text-gray-500 dark:text-gray-400 mt-1">
            管理您的所有项目
          </p>
        </div>
        <Space className="w-full sm:w-auto" wrap>
          <Button
            icon={<ThunderboltOutlined />}
            onClick={openBootstrapModal}
            className="w-full sm:w-auto"
          >
            一键生成真实应用
          </Button>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => {
              setEditingProject(null);
              resetProjectForm(null);
              setCreateModalOpen(true);
            }}
            className="w-full sm:w-auto"
          >
            新建项目
          </Button>
        </Space>
      </div>

      {/* Search & View Toggle */}
      <Card styles={{ body: { padding: isMobile ? 12 : 16 } }}>
        <div className="flex flex-col sm:flex-row gap-4 items-start sm:items-center justify-between">
          <Search
            placeholder="搜索项目名称..."
            allowClear
            onSearch={setSearchText}
            className="w-full sm:w-64"
          />
          {!isMobile && (
            <Segmented
              value={viewMode}
              onChange={(v) => setViewMode(v as 'list' | 'card')}
              options={[
                { value: 'list', icon: <BarsOutlined /> },
                { value: 'card', icon: <AppstoreOutlined /> },
              ]}
            />
          )}
        </div>
      </Card>

      {/* Content */}
      {isMobile || viewMode === 'list' ? (
        <Card styles={{ body: { padding: 0 } }}>
          <Table
            columns={columns}
            dataSource={projects}
            rowKey="id"
            loading={isLoading}
            scroll={{ x: 600 }}
            pagination={{
              total: pagination?.total,
              pageSize: pagination?.pageSize,
              current: pagination?.page,
              showSizeChanger: false,
              simple: isMobile,
            }}
            size={isMobile ? 'small' : 'middle'}
          />
        </Card>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {projects.map((project) => (
            <ProjectCard key={project.id} project={project} />
          ))}
        </div>
      )}

      {/* Create/Edit Modal */}
      <Modal
        title={editingProject ? '编辑项目' : '新建项目'}
        open={createModalOpen}
        onCancel={closeCreateModal}
        footer={null}
        width={isMobile ? '100%' : 520}
        style={isMobile ? { top: 0, paddingBottom: 0 } : {}}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleCreate}
          className="mt-4"
        >
          <Form.Item
            name="name"
            label="项目名称"
            rules={[{ required: true, message: '请输入项目名称' }]}
          >
            <Input placeholder="请输入项目名称" />
          </Form.Item>
          <Form.Item
            name="workDir"
            label="工作目录"
            rules={[{ required: true, message: '请输入工作目录' }]}
            extra={branchLoadError || '填写本地 Git 仓库目录后会自动读取仓库地址和可用分支'}
            validateStatus={branchLoadError ? 'error' : undefined}
          >
            <Input
              placeholder="/workspace/project"
              onChange={(event) => {
                const nextWorkDir = event.target.value.trim();
                if (nextWorkDir === lastLoadedWorkDir) {
                  return;
                }
                setBranchLoadError('');
                setBranchOptions([]);
                setLastLoadedWorkDir('');
                form.setFieldsValue({
                  branch: undefined,
                  repository: '',
                });
              }}
            />
          </Form.Item>
          <div className="mb-4 flex justify-end">
            <Button
              onClick={() => void loadProjectGitInfo()}
              loading={inspectLocalRepo.isPending}
              disabled={!trimmedWorkDir}
            >
              读取仓库和分支
            </Button>
          </div>
          <Form.Item
            name="repository"
            label="仓库地址"
            rules={[{ required: true, message: '请输入仓库地址' }]}
          >
            <Input placeholder="自动探测后可手动调整" />
          </Form.Item>
          <Form.Item
            name="branch"
            label="分支"
            rules={[{ required: true, message: '请选择分支' }]}
          >
            <Select
              showSearch
              options={branchOptions}
              placeholder={
                !trimmedWorkDir
                  ? '请先填写工作目录'
                  : inspectLocalRepo.isPending
                    ? '正在读取本地分支...'
                    : branchOptions.length > 0
                      ? '请选择分支'
                      : '未读取到可用分支'
              }
              disabled={!trimmedWorkDir || inspectLocalRepo.isPending || branchOptions.length === 0}
              optionFilterProp="label"
            />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} placeholder="项目描述（可选）" />
          </Form.Item>
          <Form.Item
            name="automationPaused"
            label="自动推进"
            valuePropName="checked"
            initialValue={false}
          >
            <Switch checkedChildren="暂停" unCheckedChildren="运行" />
          </Form.Item>
          <Form.Item className="mb-0 text-right">
            <Space>
              <Button onClick={closeCreateModal}>
                取消
              </Button>
              <Button type="primary" htmlType="submit" loading={submittingProject}>
                {editingProject ? '更新' : '创建'}
              </Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="一键生成真实应用"
        open={bootstrapModalOpen}
        onCancel={() => {
          setBootstrapModalOpen(false);
          bootstrapForm.resetFields();
        }}
        footer={null}
        width={isMobile ? '100%' : 640}
        style={isMobile ? { top: 0, paddingBottom: 0 } : {}}
      >
        <Form
          form={bootstrapForm}
          layout="vertical"
          onFinish={handleBootstrap}
          className="mt-4"
        >
          <Form.Item
            name="appName"
            label="应用名称"
            rules={[{ required: true, message: '请输入应用名称' }]}
          >
            <Input placeholder="例如：智能工单平台" />
          </Form.Item>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <Form.Item
              name="templateId"
              label="应用模板"
            >
              <Select
                placeholder="请选择模板"
                options={templates.map(item => ({
                  value: item.id,
                  label: `${item.name}（${item.requirementCount} 阶段）`,
                }))}
              />
            </Form.Item>
            <Form.Item
              name="cliType"
              label="自动执行 CLI"
            >
              <Select
                placeholder={cliTypeOptions.length > 0 ? '请选择 CLI' : '后端未配置 CLI 类型'}
                options={cliTypeOptions}
              />
            </Form.Item>
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <Form.Item name="branch" label="分支" initialValue="main">
              <Input placeholder="main" />
            </Form.Item>
            <Form.Item name="workDir" label="工作目录">
              <Input placeholder="默认：workspace/应用名" />
            </Form.Item>
          </div>
          <Form.Item name="repository" label="仓库地址">
            <Input placeholder="默认：local://应用名" />
          </Form.Item>
          <Form.Item
            name="autoClearSession"
            label="阶段完成后自动清理会话"
            initialValue={true}
          >
            <Select
              options={[
                { value: true, label: '是' },
                { value: false, label: '否' },
              ]}
            />
          </Form.Item>
          <Form.Item className="mb-0 text-right">
            <Space>
              <Button onClick={() => setBootstrapModalOpen(false)}>
                取消
              </Button>
              <Button type="primary" htmlType="submit" loading={bootstrapMutation.isPending}>
                生成并自动推进
              </Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>

      {/* View Drawer */}
      <Drawer
        title="项目详情"
        open={!!viewingProject}
        onClose={() => setViewingProject(null)}
        width={isMobile ? '100%' : 480}
      >
        {viewingProject && (
          <Descriptions column={1} bordered size="small">
            <Descriptions.Item label="项目名称">{viewingProject.name}</Descriptions.Item>
            <Descriptions.Item label="仓库地址">
              <a href={viewingProject.repository} target="_blank" rel="noopener noreferrer">
                {viewingProject.repository}
              </a>
            </Descriptions.Item>
            <Descriptions.Item label="分支">
              <Tag color="blue">{viewingProject.branch}</Tag>
            </Descriptions.Item>
            <Descriptions.Item label="自动推进">
              <Space>
                <Tag color={viewingProject.automationPaused ? 'red' : 'green'}>
                  {viewingProject.automationPaused ? '已暂停' : '运行中'}
                </Tag>
                <Button
                  size="small"
                  icon={viewingProject.automationPaused ? <PlayCircleOutlined /> : <PauseCircleOutlined />}
                  loading={updateMutation.isPending}
                  onClick={() => void handleAutomationPauseChange(viewingProject, !viewingProject.automationPaused)}
                >
                  {viewingProject.automationPaused ? '恢复自动推进' : '暂停自动推进'}
                </Button>
              </Space>
            </Descriptions.Item>
            <Descriptions.Item label="工作目录">{viewingProject.workDir}</Descriptions.Item>
            <Descriptions.Item label="描述">{viewingProject.description || '-'}</Descriptions.Item>
            <Descriptions.Item label="创建时间">
              {dayjs(viewingProject.createdAt).format('YYYY-MM-DD HH:mm:ss')}
            </Descriptions.Item>
            <Descriptions.Item label="更新时间">
              {dayjs(viewingProject.updatedAt).format('YYYY-MM-DD HH:mm:ss')}
            </Descriptions.Item>
          </Descriptions>
        )}
      </Drawer>
    </div>
  );
}
