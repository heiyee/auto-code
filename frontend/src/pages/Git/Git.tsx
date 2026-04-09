import { useState } from 'react';
import {
  Card,
  Select,
  Button,
  Tag,
  Modal,
  Input,
  message,
  Spin,
  Tooltip,
  List,
  Empty,
  Dropdown,
} from 'antd';
import type { MenuProps } from 'antd';
import {
  BranchesOutlined,
  PlusOutlined,
  CloudDownloadOutlined,
  CloudUploadOutlined,
  CheckOutlined,
  MinusOutlined,
  ReloadOutlined,
  SwapOutlined,
} from '@ant-design/icons';
import { useIsMobile } from '@/hooks/useMediaQuery';
import {
  useProjects,
  useGitStatus,
  useGitBranches,
  useGitDiff,
  useGitStage,
  useGitUnstage,
  useGitCommit,
  useGitCheckout,
  useGitCreateBranch,
  useGitPull,
  useGitPush,
} from '@/hooks';

const gitStatusColor: Record<string, string> = {
  modified: 'orange',
  added: 'green',
  deleted: 'red',
  untracked: 'default',
  staged: 'blue',
  conflict: 'volcano',
};

export function Git() {
  const [selectedProjectId, setSelectedProjectId] = useState('');
  const [selectedFilePath, setSelectedFilePath] = useState('');
  const [commitMessage, setCommitMessage] = useState('');
  const [createBranchModal, setCreateBranchModal] = useState(false);
  const [newBranchName, setNewBranchName] = useState('');
  const [pullModal, setPullModal] = useState(false);
  const [pushModal, setPushModal] = useState(false);
  const [remoteBranch, setRemoteBranch] = useState('origin');
  const isMobile = useIsMobile();

  const { data: projectsData } = useProjects({ pageSize: 100 });
  const projects = projectsData?.data ?? [];

  const { data: gitStatusData, isLoading: statusLoading, refetch: refetchStatus } = useGitStatus(
    selectedProjectId,
    { refetchInterval: 10000 }
  );
  const { data: branchesData, refetch: refetchBranches } = useGitBranches(selectedProjectId);
  const { data: diffData, isLoading: diffLoading } = useGitDiff(selectedProjectId, selectedFilePath);

  const stageMutation = useGitStage();
  const unstageMutation = useGitUnstage();
  const commitMutation = useGitCommit();
  const checkoutMutation = useGitCheckout();
  const createBranchMutation = useGitCreateBranch();
  const pullMutation = useGitPull();
  const pushMutation = useGitPush();

  const gitStatus = gitStatusData?.data;
  const branches = branchesData?.data;
  const currentBranch = gitStatus?.currentBranch || branches?.currentBranch || '-';

  const handleStage = async (path: string) => {
    try {
      await stageMutation.mutateAsync({ projectId: selectedProjectId, path });
    } catch (e: any) {
      message.error(e?.message || '暂存失败');
    }
  };

  const handleUnstage = async (path: string) => {
    try {
      await unstageMutation.mutateAsync({ projectId: selectedProjectId, path });
    } catch (e: any) {
      message.error(e?.message || '取消暂存失败');
    }
  };

  const handleCommit = async () => {
    if (!commitMessage.trim()) {
      message.warning('请输入提交信息');
      return;
    }
    try {
      const result = await commitMutation.mutateAsync({ projectId: selectedProjectId, message: commitMessage });
      message.success(`提交成功：${result.data.commitHash?.slice(0, 7) ?? ''}`);
      setCommitMessage('');
    } catch (e: any) {
      message.error(e?.message || '提交失败');
    }
  };

  const handleCheckout = async (branch: string) => {
    try {
      await checkoutMutation.mutateAsync({ projectId: selectedProjectId, branch });
      message.success(`已切换到 ${branch}`);
      refetchBranches();
    } catch (e: any) {
      message.error(e?.message || '切换分支失败');
    }
  };

  const handleCreateBranch = async () => {
    if (!newBranchName.trim()) return;
    try {
      await createBranchMutation.mutateAsync({ projectId: selectedProjectId, name: newBranchName.trim(), checkout: true });
      message.success(`分支 ${newBranchName} 已创建并切换`);
      setCreateBranchModal(false);
      setNewBranchName('');
      refetchBranches();
      refetchStatus();
    } catch (e: any) {
      message.error(e?.message || '创建分支失败');
    }
  };

  const handlePull = async () => {
    try {
      const result = await pullMutation.mutateAsync({ projectId: selectedProjectId, remote: remoteBranch, branch: currentBranch });
      message.success(result.data.output || '拉取成功');
      setPullModal(false);
      refetchStatus();
    } catch (e: any) {
      message.error(e?.message || '拉取失败');
    }
  };

  const handlePush = async () => {
    try {
      const result = await pushMutation.mutateAsync({ projectId: selectedProjectId, remote: remoteBranch, branch: currentBranch });
      message.success(result.data.output || '推送成功');
      setPushModal(false);
    } catch (e: any) {
      message.error(e?.message || '推送失败');
    }
  };

  const branchMenuItems: MenuProps['items'] = (branches?.branches ?? [])
    .filter(b => !b.current)
    .map(b => ({
      key: b.name,
      label: b.name,
      onClick: () => handleCheckout(b.name),
    }));

  const stagedFiles = gitStatus?.staged ?? [];
  const changedFiles = gitStatus?.changes ?? [];
  const untrackedFiles = (gitStatus?.untracked ?? []).map(p => ({ path: p, status: 'untracked' }));
  const unstaged = [...changedFiles, ...untrackedFiles];

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-xl sm:text-2xl font-bold text-gray-800 dark:text-white">Git 管理</h1>
        <p className="text-gray-500 dark:text-gray-400 mt-1">管理代码变更、分支和提交</p>
      </div>

      {/* Project selector */}
      <Card styles={{ body: { padding: isMobile ? 12 : 16 } }}>
        <div className="flex flex-col sm:flex-row gap-3 items-start sm:items-center flex-wrap">
          <span className="text-gray-600 whitespace-nowrap">选择项目：</span>
          <Select
            placeholder="请选择项目"
            value={selectedProjectId || undefined}
            onChange={(v) => { setSelectedProjectId(v); setSelectedFilePath(''); }}
            style={{ minWidth: 240 }}
            options={projects.map(p => ({ value: p.id, label: `${p.name} (${p.branch})` }))}
            showSearch
            optionFilterProp="label"
          />
          {selectedProjectId && (
            <>
              <div className="flex items-center gap-2">
                <BranchesOutlined className="text-gray-500" />
                <Tag color="blue" className="text-sm">{currentBranch}</Tag>
                <Dropdown menu={{ items: branchMenuItems }} trigger={['click']} disabled={!branchMenuItems.length}>
                  <Button size="small" icon={<SwapOutlined />}>切换分支</Button>
                </Dropdown>
                <Button size="small" icon={<PlusOutlined />} onClick={() => setCreateBranchModal(true)}>
                  新建分支
                </Button>
              </div>
              <div className="flex gap-2 ml-auto">
                <Button size="small" icon={<ReloadOutlined />} onClick={() => { refetchStatus(); refetchBranches(); }}>
                  刷新
                </Button>
                <Button size="small" icon={<CloudDownloadOutlined />} onClick={() => setPullModal(true)} loading={pullMutation.isPending}>
                  Pull
                </Button>
                <Button size="small" icon={<CloudUploadOutlined />} onClick={() => setPushModal(true)} loading={pushMutation.isPending}>
                  Push
                </Button>
              </div>
            </>
          )}
        </div>
      </Card>

      {selectedProjectId && (
        <div className={`flex gap-4 ${isMobile ? 'flex-col' : 'flex-row'}`}>
          {/* Left: Changed files */}
          <div className="flex flex-col gap-4" style={{ width: isMobile ? '100%' : 320, flexShrink: 0 }}>
            {/* Staged */}
            <Card
              title={<span className="text-sm font-medium">已暂存 ({stagedFiles.length})</span>}
              styles={{ body: { padding: 0 } }}
              size="small"
            >
              <Spin spinning={statusLoading}>
                {stagedFiles.length === 0 ? (
                  <Empty className="py-4" description="无暂存变更" imageStyle={{ height: 40 }} />
                ) : (
                  <List
                    size="small"
                    dataSource={stagedFiles}
                    renderItem={(item) => (
                      <List.Item
                        className={`cursor-pointer hover:bg-gray-50 px-3 ${selectedFilePath === item.path ? 'bg-blue-50' : ''}`}
                        onClick={() => setSelectedFilePath(item.path)}
                        actions={[
                          <Tooltip title="取消暂存" key="unstage">
                            <Button
                              type="text"
                              size="small"
                              icon={<MinusOutlined />}
                              loading={unstageMutation.isPending}
                              onClick={(e) => { e.stopPropagation(); handleUnstage(item.path); }}
                            />
                          </Tooltip>,
                        ]}
                      >
                        <div className="flex items-center gap-2 overflow-hidden">
                          <Tag color={gitStatusColor[item.status] ?? 'default'} className="text-xs shrink-0">
                            {item.status[0].toUpperCase()}
                          </Tag>
                          <span className="font-mono text-xs truncate" title={item.path}>
                            {item.path.split('/').pop()}
                          </span>
                        </div>
                      </List.Item>
                    )}
                  />
                )}
              </Spin>
            </Card>

            {/* Unstaged */}
            <Card
              title={<span className="text-sm font-medium">未暂存 ({unstaged.length})</span>}
              styles={{ body: { padding: 0 } }}
              size="small"
            >
              <Spin spinning={statusLoading}>
                {unstaged.length === 0 ? (
                  <Empty className="py-4" description="无变更" imageStyle={{ height: 40 }} />
                ) : (
                  <List
                    size="small"
                    dataSource={unstaged}
                    renderItem={(item) => (
                      <List.Item
                        className={`cursor-pointer hover:bg-gray-50 px-3 ${selectedFilePath === item.path ? 'bg-blue-50' : ''}`}
                        onClick={() => setSelectedFilePath(item.path)}
                        actions={[
                          <Tooltip title="暂存" key="stage">
                            <Button
                              type="text"
                              size="small"
                              icon={<CheckOutlined />}
                              loading={stageMutation.isPending}
                              onClick={(e) => { e.stopPropagation(); handleStage(item.path); }}
                            />
                          </Tooltip>,
                        ]}
                      >
                        <div className="flex items-center gap-2 overflow-hidden">
                          <Tag color={gitStatusColor[item.status] ?? 'default'} className="text-xs shrink-0">
                            {item.status[0].toUpperCase()}
                          </Tag>
                          <span className="font-mono text-xs truncate" title={item.path}>
                            {item.path.split('/').pop()}
                          </span>
                        </div>
                      </List.Item>
                    )}
                  />
                )}
              </Spin>
            </Card>

            {/* Commit */}
            <Card title={<span className="text-sm font-medium">提交</span>} size="small">
              <div className="flex flex-col gap-2">
                <Input.TextArea
                  placeholder="提交信息..."
                  value={commitMessage}
                  onChange={e => setCommitMessage(e.target.value)}
                  rows={3}
                />
                <Button
                  type="primary"
                  block
                  loading={commitMutation.isPending}
                  onClick={handleCommit}
                  disabled={stagedFiles.length === 0}
                >
                  提交 ({stagedFiles.length} 个文件)
                </Button>
              </div>
            </Card>
          </div>

          {/* Right: Diff viewer */}
          <Card
            title={
              selectedFilePath
                ? <span className="font-mono text-sm">{selectedFilePath}</span>
                : '选择文件查看 Diff'
            }
            className="flex-1 min-w-0"
            styles={{ body: { padding: 0 } }}
          >
            {!selectedFilePath && (
              <Empty className="py-16" description="点击左侧文件查看变更内容" />
            )}
            {selectedFilePath && (
              <Spin spinning={diffLoading}>
                {diffData?.data?.diff ? (
                  <div className="overflow-auto" style={{ maxHeight: '70vh' }}>
                    <pre className="text-xs font-mono p-4 whitespace-pre-wrap leading-5">
                      {diffData.data.diff.split('\n').map((line, i) => {
                        let cls = '';
                        if (line.startsWith('+') && !line.startsWith('+++')) cls = 'bg-green-50 text-green-800';
                        else if (line.startsWith('-') && !line.startsWith('---')) cls = 'bg-red-50 text-red-800';
                        else if (line.startsWith('@@')) cls = 'bg-blue-50 text-blue-700';
                        return (
                          <span key={i} className={`block ${cls}`}>
                            {line || ' '}
                          </span>
                        );
                      })}
                    </pre>
                  </div>
                ) : (
                  <Empty className="py-8" description="暂无 diff 内容" />
                )}
              </Spin>
            )}
          </Card>
        </div>
      )}

      {/* Create branch modal */}
      <Modal
        title="新建分支"
        open={createBranchModal}
        onOk={handleCreateBranch}
        onCancel={() => { setCreateBranchModal(false); setNewBranchName(''); }}
        okText="创建并切换"
        confirmLoading={createBranchMutation.isPending}
      >
        <Input
          placeholder="分支名称（如 feature/my-feature）"
          value={newBranchName}
          onChange={e => setNewBranchName(e.target.value)}
          onPressEnter={handleCreateBranch}
        />
      </Modal>

      {/* Pull modal */}
      <Modal
        title="Git Pull"
        open={pullModal}
        onOk={handlePull}
        onCancel={() => setPullModal(false)}
        okText="拉取"
        confirmLoading={pullMutation.isPending}
      >
        <div className="flex flex-col gap-3">
          <div>
            <div className="text-sm text-gray-500 mb-1">Remote</div>
            <Input value={remoteBranch} onChange={e => setRemoteBranch(e.target.value)} placeholder="origin" />
          </div>
          <div>
            <div className="text-sm text-gray-500 mb-1">分支</div>
            <Input value={currentBranch} disabled />
          </div>
        </div>
      </Modal>

      {/* Push modal */}
      <Modal
        title="Git Push"
        open={pushModal}
        onOk={handlePush}
        onCancel={() => setPushModal(false)}
        okText="推送"
        confirmLoading={pushMutation.isPending}
      >
        <div className="flex flex-col gap-3">
          <div>
            <div className="text-sm text-gray-500 mb-1">Remote</div>
            <Input value={remoteBranch} onChange={e => setRemoteBranch(e.target.value)} placeholder="origin" />
          </div>
          <div>
            <div className="text-sm text-gray-500 mb-1">分支</div>
            <Input value={currentBranch} disabled />
          </div>
        </div>
      </Modal>
    </div>
  );
}
