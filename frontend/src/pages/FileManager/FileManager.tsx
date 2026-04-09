import { useState, useEffect } from 'react';
import {
  Card,
  Select,
  Breadcrumb,
  Table,
  Button,
  Tag,
  Modal,
  Input,
  Popconfirm,
  message,
  Dropdown,
  Spin,
  Empty,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { MenuProps } from 'antd';
import {
  FolderOutlined,
  FileOutlined,
  FolderAddOutlined,
  FileAddOutlined,
  ReloadOutlined,
  MoreOutlined,
  DeleteOutlined,
  EditOutlined,
  EyeOutlined,
  SaveOutlined,
  ArrowLeftOutlined,
} from '@ant-design/icons';
import { useIsMobile } from '@/hooks/useMediaQuery';
import {
  useProjects,
  useProjectFiles,
  useProjectFileContent,
  useSaveProjectFile,
  useCreateProjectFile,
  useCreateProjectDir,
  useRenameProjectFile,
  useDeleteProjectFile,
} from '@/hooks';
import type { FileNode } from '@/types';
import dayjs from 'dayjs';

const { TextArea } = Input;

const gitStatusColor: Record<string, string> = {
  modified: 'orange',
  added: 'green',
  deleted: 'red',
  untracked: 'default',
  staged: 'blue',
  conflict: 'red',
};

export function FileManager() {
  const [selectedProjectId, setSelectedProjectId] = useState<string>('');
  const [currentPath, setCurrentPath] = useState('');
  const isMobile = useIsMobile();

  // File editor state
  const [editingFile, setEditingFile] = useState<{ path: string; content: string; revision: string } | null>(null);
  const [editContent, setEditContent] = useState('');
  const [viewingFile, setViewingFile] = useState<string>('');

  // Modals state
  const [createFileModal, setCreateFileModal] = useState(false);
  const [createDirModal, setCreateDirModal] = useState(false);
  const [renameModal, setRenameModal] = useState<{ path: string; name: string } | null>(null);
  const [newFileName, setNewFileName] = useState('');
  const [newDirName, setNewDirName] = useState('');
  const [renameTo, setRenameTo] = useState('');

  const { data: projectsData } = useProjects({ pageSize: 100 });
  const projects = projectsData?.data ?? [];

  const { data: filesData, isLoading: filesLoading, refetch: refetchFiles } = useProjectFiles(selectedProjectId, currentPath);
  const nodes = filesData?.data?.nodes ?? [];

  const { data: fileContentData, isLoading: contentLoading } = useProjectFileContent(
    selectedProjectId,
    viewingFile
  );

  const saveMutation = useSaveProjectFile();
  const createFileMutation = useCreateProjectFile();
  const createDirMutation = useCreateProjectDir();
  const renameMutation = useRenameProjectFile();
  const deleteMutation = useDeleteProjectFile();

  // Open file content for viewing/editing
  useEffect(() => {
    if (fileContentData?.data && viewingFile) {
      setEditingFile({
        path: fileContentData.data.path,
        content: fileContentData.data.content,
        revision: fileContentData.data.revision,
      });
      setEditContent(fileContentData.data.content);
    }
  }, [fileContentData, viewingFile]);

  const handleNodeClick = (node: FileNode) => {
    if (node.type === 'directory') {
      setCurrentPath(node.path);
    } else {
      setViewingFile(node.path);
    }
  };

  const handleBack = () => {
    if (!currentPath) return;
    const parts = currentPath.split('/').filter(Boolean);
    parts.pop();
    setCurrentPath(parts.length ? parts.join('/') : '');
  };

  const handleSaveFile = async () => {
    if (!editingFile) return;
    try {
      const result = await saveMutation.mutateAsync({
        projectId: selectedProjectId,
        path: editingFile.path,
        content: editContent,
        baseRevision: editingFile.revision,
      });
      setEditingFile(prev => prev ? { ...prev, revision: result.data.revision } : null);
      message.success('文件已保存');
    } catch (e: any) {
      message.error(e?.message || '保存失败');
    }
  };

  const handleCreateFile = async () => {
    if (!newFileName.trim()) return;
    const path = currentPath ? `${currentPath}/${newFileName.trim()}` : newFileName.trim();
    try {
      await createFileMutation.mutateAsync({ projectId: selectedProjectId, path });
      message.success('文件已创建');
      setCreateFileModal(false);
      setNewFileName('');
      refetchFiles();
    } catch (e: any) {
      message.error(e?.message || '创建失败');
    }
  };

  const handleCreateDir = async () => {
    if (!newDirName.trim()) return;
    const path = currentPath ? `${currentPath}/${newDirName.trim()}` : newDirName.trim();
    try {
      await createDirMutation.mutateAsync({ projectId: selectedProjectId, path });
      message.success('目录已创建');
      setCreateDirModal(false);
      setNewDirName('');
      refetchFiles();
    } catch (e: any) {
      message.error(e?.message || '创建失败');
    }
  };

  const handleRename = async () => {
    if (!renameModal || !renameTo.trim()) return;
    const parts = renameModal.path.split('/');
    parts[parts.length - 1] = renameTo.trim();
    const newPath = parts.join('/');
    try {
      await renameMutation.mutateAsync({ projectId: selectedProjectId, oldPath: renameModal.path, newPath });
      message.success('重命名成功');
      setRenameModal(null);
      setRenameTo('');
      refetchFiles();
    } catch (e: any) {
      message.error(e?.message || '重命名失败');
    }
  };

  const handleDelete = async (path: string) => {
    try {
      await deleteMutation.mutateAsync({ projectId: selectedProjectId, path });
      message.success('已删除');
      refetchFiles();
    } catch (e: any) {
      message.error(e?.message || '删除失败');
    }
  };

  const breadcrumbItems = [
    { title: <a onClick={() => setCurrentPath('')}>根目录</a> },
    ...currentPath.split('/').filter(Boolean).map((seg, i, arr) => ({
      title: (
        <a onClick={() => setCurrentPath(arr.slice(0, i + 1).join('/'))}>
          {seg}
        </a>
      ),
    })),
  ];

  const columns: ColumnsType<FileNode> = [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      ellipsis: true,
      render: (name, record) => (
        <div className="flex items-center gap-2">
          {record.type === 'directory' ? (
            <FolderOutlined className="text-yellow-500" />
          ) : (
            <FileOutlined className="text-gray-400" />
          )}
          <a onClick={() => handleNodeClick(record)} className="text-blue-500 hover:text-blue-700">
            {name}
          </a>
          {record.gitStatus && (
            <Tag color={gitStatusColor[record.gitStatus] ?? 'default'} className="text-xs">
              {record.gitStatus}
            </Tag>
          )}
        </div>
      ),
    },
    {
      title: '大小',
      dataIndex: 'size',
      key: 'size',
      width: 90,
      responsive: ['md'],
      render: (size, record) => {
        if (record.type === 'directory') return '-';
        if (size < 1024) return `${size} B`;
        if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
        return `${(size / 1024 / 1024).toFixed(1)} MB`;
      },
    },
    {
      title: '修改时间',
      dataIndex: 'modTime',
      key: 'modTime',
      width: 140,
      responsive: ['sm'],
      render: (t) => (t ? dayjs(t).format('MM-DD HH:mm') : '-'),
    },
    {
      title: '操作',
      key: 'actions',
      width: 60,
      fixed: 'right',
      render: (_, record) => {
        const items: MenuProps['items'] = [
          record.type === 'file' && {
            key: 'view',
            icon: <EyeOutlined />,
            label: '查看/编辑',
            onClick: () => setViewingFile(record.path),
          },
          {
            key: 'rename',
            icon: <EditOutlined />,
            label: '重命名',
            onClick: () => {
              setRenameModal({ path: record.path, name: record.name });
              setRenameTo(record.name);
            },
          },
          { type: 'divider' },
          {
            key: 'delete',
            icon: <DeleteOutlined />,
            label: (
              <Popconfirm
                title={`确定删除 ${record.name}？`}
                onConfirm={() => handleDelete(record.path)}
                okText="删除"
                cancelText="取消"
              >
                <span className="text-red-500">删除</span>
              </Popconfirm>
            ),
          },
        ].filter(Boolean) as MenuProps['items'];

        return (
          <Dropdown menu={{ items }} trigger={['click']}>
            <Button type="text" size="small" icon={<MoreOutlined />} />
          </Dropdown>
        );
      },
    },
  ];

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-xl sm:text-2xl font-bold text-gray-800 dark:text-white">文件管理</h1>
        <p className="text-gray-500 dark:text-gray-400 mt-1">浏览和编辑项目文件</p>
      </div>

      {/* Project selector */}
      <Card styles={{ body: { padding: isMobile ? 12 : 16 } }}>
        <div className="flex flex-col sm:flex-row gap-3 items-start sm:items-center">
          <span className="text-gray-600 whitespace-nowrap">选择项目：</span>
          <Select
            placeholder="请选择项目"
            value={selectedProjectId || undefined}
            onChange={(v) => { setSelectedProjectId(v); setCurrentPath(''); setViewingFile(''); setEditingFile(null); }}
            style={{ minWidth: 240 }}
            options={projects.map(p => ({ value: p.id, label: `${p.name} (${p.branch})` }))}
            showSearch
            optionFilterProp="label"
          />
          {selectedProjectId && (
            <Button icon={<ReloadOutlined />} onClick={() => refetchFiles()}>
              刷新
            </Button>
          )}
        </div>
      </Card>

      {selectedProjectId && (
        <>
          {/* Toolbar */}
          <Card styles={{ body: { padding: isMobile ? 12 : 16 } }}>
            <div className="flex flex-col gap-3">
              <div className="flex items-center gap-2">
                {currentPath && (
                  <Button size="small" icon={<ArrowLeftOutlined />} onClick={handleBack} />
                )}
                <Breadcrumb items={breadcrumbItems} />
              </div>
              <div className="flex gap-2">
                <Button icon={<FolderAddOutlined />} onClick={() => setCreateDirModal(true)}>
                  新建目录
                </Button>
                <Button icon={<FileAddOutlined />} onClick={() => setCreateFileModal(true)}>
                  新建文件
                </Button>
              </div>
            </div>
          </Card>

          {/* File list */}
          <Card styles={{ body: { padding: 0 } }}>
            <Spin spinning={filesLoading}>
              {nodes.length === 0 && !filesLoading ? (
                <Empty className="py-12" description="目录为空" />
              ) : (
                <Table
                  columns={columns}
                  dataSource={nodes}
                  rowKey="path"
                  pagination={false}
                  size={isMobile ? 'small' : 'middle'}
                  scroll={{ x: 500 }}
                />
              )}
            </Spin>
          </Card>
        </>
      )}

      {/* File editor modal */}
      <Modal
        title={
          <div className="flex items-center gap-2">
            <FileOutlined />
            <span className="font-mono text-sm">{editingFile?.path}</span>
          </div>
        }
        open={!!editingFile}
        onCancel={() => { setEditingFile(null); setViewingFile(''); }}
        width={isMobile ? '100%' : '80vw'}
        footer={[
          <Button key="close" onClick={() => { setEditingFile(null); setViewingFile(''); }}>
            关闭
          </Button>,
          <Button
            key="save"
            type="primary"
            icon={<SaveOutlined />}
            loading={saveMutation.isPending}
            onClick={handleSaveFile}
          >
            保存
          </Button>,
        ]}
      >
        {contentLoading ? (
          <div className="flex justify-center py-8"><Spin /></div>
        ) : (
          <TextArea
            value={editContent}
            onChange={e => setEditContent(e.target.value)}
            autoSize={{ minRows: 20, maxRows: 40 }}
            className="font-mono text-xs"
          />
        )}
      </Modal>

      {/* Create file modal */}
      <Modal
        title="新建文件"
        open={createFileModal}
        onOk={handleCreateFile}
        onCancel={() => { setCreateFileModal(false); setNewFileName(''); }}
        okText="创建"
        confirmLoading={createFileMutation.isPending}
      >
        <Input
          placeholder="文件名（如 config.json）"
          value={newFileName}
          onChange={e => setNewFileName(e.target.value)}
          onPressEnter={handleCreateFile}
        />
      </Modal>

      {/* Create dir modal */}
      <Modal
        title="新建目录"
        open={createDirModal}
        onOk={handleCreateDir}
        onCancel={() => { setCreateDirModal(false); setNewDirName(''); }}
        okText="创建"
        confirmLoading={createDirMutation.isPending}
      >
        <Input
          placeholder="目录名"
          value={newDirName}
          onChange={e => setNewDirName(e.target.value)}
          onPressEnter={handleCreateDir}
        />
      </Modal>

      {/* Rename modal */}
      <Modal
        title="重命名"
        open={!!renameModal}
        onOk={handleRename}
        onCancel={() => { setRenameModal(null); setRenameTo(''); }}
        okText="确认"
        confirmLoading={renameMutation.isPending}
      >
        <Input
          placeholder="新名称"
          value={renameTo}
          onChange={e => setRenameTo(e.target.value)}
          onPressEnter={handleRename}
        />
      </Modal>
    </div>
  );
}
