import { useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
  Button,
  Empty,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Tag,
  Typography,
  message,
} from 'antd';
import {
  ClockCircleOutlined,
  CodeOutlined,
  FolderOpenOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import clsx from 'clsx';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import 'dayjs/locale/zh-cn';
import { useCLIProfiles, useCLISessions, useCreateCLISession } from '@/hooks';
import { useIsMobile } from '@/hooks/useMediaQuery';
import type { CLISession } from '@/types';
import { CLITerminalWorkbench } from '@/components/cli/CLITerminalWorkbench';
import { estimateTerminalCreateSize } from '@/components/cli/terminalUtils';
import { getExecutionHealthMeta } from '@/utils/executionHealth';

dayjs.extend(relativeTime);
dayjs.locale('zh-cn');

const { Paragraph, Text } = Typography;

const stateConfig: Record<string, { color: string; text: string }> = {
  running: { color: 'processing', text: '运行中' },
  paused: { color: 'warning', text: '已暂停' },
  completed: { color: 'success', text: '已完成' },
  terminated: { color: 'default', text: '已终止' },
  error: { color: 'error', text: '错误' },
  exited: { color: 'default', text: '已退出' },
  stopped: { color: 'default', text: '已停止' },
};

function compareSessions(a: CLISession, b: CLISession) {
  const aRunning = a.sessionState === 'running' ? 1 : 0;
  const bRunning = b.sessionState === 'running' ? 1 : 0;
  if (aRunning !== bRunning) {
    return bRunning - aRunning;
  }
  return dayjs(b.lastActiveAt).valueOf() - dayjs(a.lastActiveAt).valueOf();
}

function getSessionState(session: CLISession) {
  return stateConfig[session.sessionState] || { color: 'default', text: session.sessionState || '未知' };
}

function buildSessionRoute(sessionId?: string) {
  if (!sessionId) {
    return '/sessions';
  }
  return `/sessions/${encodeURIComponent(sessionId)}`;
}

export function CLISessions() {
  const [stateFilter, setStateFilter] = useState<string>('');
  const [createModal, setCreateModal] = useState(false);
  const [selectedCLIType, setSelectedCLIType] = useState('');
  const [toolbarContainer, setToolbarContainer] = useState<HTMLDivElement | null>(null);
  const [form] = Form.useForm();
  const isMobile = useIsMobile();
  const navigate = useNavigate();
  const { sessionId } = useParams();
  const { data, isLoading, refetch } = useCLISessions({ state: stateFilter, pageSize: 200 });
  const { data: profilesData } = useCLIProfiles();
  const createMutation = useCreateCLISession();

  const sessions = useMemo(() => {
    const list = [...(data?.data || [])];
    list.sort(compareSessions);
    return list;
  }, [data?.data]);

  const activeSession = useMemo(() => {
    if (sessionId) {
      return sessions.find(item => item.id === sessionId) || sessions[0] || null;
    }
    return sessions[0] || null;
  }, [sessionId, sessions]);

  const activeState = activeSession ? getSessionState(activeSession) : null;
  const activeHealth = activeSession
    ? getExecutionHealthMeta(activeSession.executionState, activeSession.executionReason)
    : null;
  const activeIssue = activeSession
    ? [
      Number.isFinite(activeSession.exitCode) ? `exit code ${activeSession.exitCode}` : '',
      activeSession.lastError || '',
    ].filter(Boolean).join(' | ')
    : '';
  const profiles = profilesData?.data ?? [];
  const runningCount = sessions.filter(item => item.sessionState === 'running').length;
  const cliTypeOptions = profiles.map(group => ({ value: group.cli_type, label: group.cli_type }));
  const selectedProfileGroup = profiles.find(group => group.cli_type === selectedCLIType);
  const profileOptions = selectedProfileGroup?.profiles.map(profile => ({
    value: profile.id,
    label: `${profile.name}${profile.description ? ` - ${profile.description}` : ''}`,
  })) ?? [];
  const requiresProfileSelection = profileOptions.length > 1;

  const handleCreate = async () => {
    try {
      const values = await form.validateFields();
      const group = profiles.find(item => item.cli_type === values.cli_type);
      const size = estimateTerminalCreateSize();
      const result = await createMutation.mutateAsync({
        cli_type: values.cli_type,
        profile: (group?.profiles?.length ?? 0) > 1
          ? values.profile
          : (group?.default_profile || group?.profiles?.[0]?.id || undefined),
        command: values.command,
        cols: size.cols,
        rows: size.rows,
      });
      message.success(`会话已创建: ${result.data.session_id}`);
      setCreateModal(false);
      form.resetFields();
      setSelectedCLIType('');
      await refetch();
      navigate(buildSessionRoute(result.data.session_id));
    } catch (error: any) {
      if (error?.message) {
        message.error(error.message);
      }
    }
  };

  const handleDeleted = async (deletedId: string) => {
    if (activeSession?.id === deletedId) {
      const fallback = sessions.find(item => item.id !== deletedId);
      navigate(buildSessionRoute(fallback?.id));
    }
    await refetch();
  };

  return (
    <div className="cli-page-shell">
      <div className="cli-page-workspace">
        <aside className="cli-session-list-panel">
          <div className="cli-session-list-header">
            <div>
              <h2>CLI 列表</h2>
              <p>{sessions.length} 个会话 · {runningCount} 个运行中</p>
            </div>
            {isLoading && <Tag color="gold">加载中</Tag>}
          </div>
          <div className="cli-session-list-filter">
            <Select
              allowClear
              value={stateFilter || undefined}
              placeholder="按状态筛选"
              style={{ width: '100%' }}
              onChange={value => setStateFilter(value || '')}
              options={[
                { value: 'running', label: '运行中' },
                { value: 'paused', label: '已暂停' },
                { value: 'completed', label: '已完成' },
                { value: 'terminated', label: '已终止' },
                { value: 'error', label: '错误' },
              ]}
            />
          </div>
          <div className="cli-session-list">
            {!isLoading && sessions.length === 0 && (
              <div className="cli-session-empty">
                <Empty description="暂无 CLI 会话" image={Empty.PRESENTED_IMAGE_SIMPLE}>
                  <Button type="primary" onClick={() => setCreateModal(true)}>
                    创建第一个会话
                  </Button>
                </Empty>
              </div>
            )}

            {sessions.map(item => {
              const itemState = getSessionState(item);
              const executionHealth = getExecutionHealthMeta(item.executionState, item.executionReason);
              const isActive = activeSession?.id === item.id;
              const profileLabel = item.profileName || item.profile;
              const issueText = [
                Number.isFinite(item.exitCode) ? `exit ${item.exitCode}` : '',
                item.lastError || '',
              ].filter(Boolean).join(' | ');
              return (
                <button
                  key={item.id}
                  type="button"
                  className={clsx('cli-session-card', isActive && 'is-active')}
                  onClick={() => navigate(buildSessionRoute(item.id))}
                >
                  <div className="cli-session-card-top">
                    <div>
                      <div className="cli-session-card-id">{item.id}</div>
                      <div className="cli-session-card-type">
                        <CodeOutlined />
                        <span>{item.cliType}</span>
                        <span className="cli-session-card-dot">•</span>
                        <span>{profileLabel}</span>
                      </div>
                    </div>
                    <div className="flex flex-col items-end gap-1">
                      <Tag color={itemState.color}>{itemState.text}</Tag>
                      {executionHealth && (
                        <Tag color={executionHealth.color}>{executionHealth.text}</Tag>
                      )}
                    </div>
                  </div>

                  <div className="cli-session-card-title">
                    {item.projectName || '未关联项目'}
                  </div>

                  <div className="cli-session-card-meta">
                    <span>
                      <FolderOpenOutlined />
                      {item.requirementTitle || '独立会话'}
                    </span>
                    {executionHealth?.reasonText && (
                      <span>{executionHealth.reasonText}</span>
                    )}
                    <span>
                      <ClockCircleOutlined />
                      {dayjs(item.lastActiveAt).fromNow()}
                    </span>
                  </div>
                  {issueText && (
                    <div className="cli-session-card-meta">
                      <span>{issueText}</span>
                    </div>
                  )}
                </button>
              );
            })}
          </div>
        </aside>

        <section className="cli-session-focus-panel">
          {activeSession ? (
            <>
              <div className={clsx('cli-focus-compact', isMobile && 'is-mobile-minimal')}>
                <div className="cli-focus-header-actions cli-focus-toolbar-line">
                  <Space wrap className="cli-focus-toolbar-summary">
                    {activeState && <Tag color={activeState.color}>{activeState.text}</Tag>}
                    {activeHealth && <Tag color={activeHealth.color}>{activeHealth.text}</Tag>}
                    <Tag color="gold">{activeSession.cliType}</Tag>
                    {!isMobile && activeHealth?.reasonText && <Tag>{activeHealth.reasonText}</Tag>}
                    {!isMobile && activeIssue && <Tag color="red">{activeIssue}</Tag>}
                  </Space>
                  {!isMobile && (
                  <div className="cli-focus-path cli-focus-path-inline">
                    <FolderOpenOutlined />
                    <Paragraph
                      copyable={{ text: activeSession.workDir }}
                      ellipsis={{ rows: 1 }}
                      className="cli-focus-path-text !mb-0"
                    >
                      {activeSession.workDir}
                    </Paragraph>
                  </div>
                  )}
                  {!isMobile && (
                  <Space wrap className="cli-focus-primary-actions">
                    <Button size="small" icon={<ReloadOutlined />} onClick={() => refetch()}>
                      刷新
                    </Button>
                    <Button size="small" type="primary" icon={<PlusOutlined />} onClick={() => setCreateModal(true)}>
                      创建会话
                    </Button>
                  </Space>
                  )}
                  {!isMobile && <div ref={setToolbarContainer} className="cli-focus-toolbar-slot" />}
                </div>
              </div>

              <CLITerminalWorkbench
                session={activeSession}
                toolbarContainer={toolbarContainer}
                onRefresh={() => refetch()}
                onDeleted={handleDeleted}
              />
            </>
          ) : (
            <div className="cli-focus-empty">
              <Empty
                description="从左侧卡片选择一个会话，或直接创建新会话"
                image={Empty.PRESENTED_IMAGE_SIMPLE}
              >
                <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateModal(true)}>
                  创建会话
                </Button>
              </Empty>
            </div>
          )}
        </section>
      </div>

      <Modal
        title="创建 CLI 会话"
        open={createModal}
        onOk={handleCreate}
        onCancel={() => {
          setCreateModal(false);
          form.resetFields();
          setSelectedCLIType('');
        }}
        okText="创建"
        confirmLoading={createMutation.isPending}
        width={480}
      >
        {profiles.length === 0 && (
          <div className="mb-4">
            <Text type="warning">
              未找到可用的 CLI Profiles，请检查后端配置
            </Text>
          </div>
        )}
        <Form form={form} layout="vertical">
          <Form.Item label="CLI 类型" name="cli_type" rules={[{ required: true, message: '请选择 CLI 类型' }]}>
            <Select
              placeholder="选择 CLI 类型"
              options={cliTypeOptions}
              onChange={(value) => {
                setSelectedCLIType(value);
                const nextGroup = profiles.find(group => group.cli_type === value);
                if ((nextGroup?.profiles?.length ?? 0) > 1) {
                  form.setFieldValue('profile', undefined);
                  return;
                }
                form.setFieldValue('profile', nextGroup?.default_profile || nextGroup?.profiles?.[0]?.id || undefined);
              }}
            />
          </Form.Item>
          {requiresProfileSelection ? (
            <Form.Item label="Profile" name="profile" rules={[{ required: true, message: '请选择 Profile' }]}>
              <Select
                placeholder="选择 Profile"
                options={profileOptions}
                disabled={!selectedCLIType}
              />
            </Form.Item>
          ) : selectedCLIType ? (
            <Form.Item label="账户">
              <Text type="secondary">
                当前 CLI 无需选择账户，将直接使用默认启动命令。
              </Text>
            </Form.Item>
          ) : null}
          <Form.Item label="命令（可选）" name="command">
            <Input placeholder="留空使用默认命令" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
