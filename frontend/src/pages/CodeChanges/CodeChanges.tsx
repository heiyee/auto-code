import { useState } from 'react';
import {
  Card,
  Table,
  Button,
  Space,
  Tag,
  Drawer,
  Descriptions,
  Tabs,
  Tree,
  Statistic,
  Row,
  Col,
  Segmented,
  Typography,
  Empty,
  Alert,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { DataNode } from 'antd/es/tree';
import {
  FileAddOutlined,
  EditOutlined,
  DeleteOutlined,
  DiffOutlined,
  FolderOutlined,
  FileTextOutlined,
  EyeOutlined,
  SplitCellsOutlined,
  InfoCircleOutlined,
} from '@ant-design/icons';
import { useIsMobile } from '@/hooks/useMediaQuery';
import { useChangeSets } from '@/hooks';
import dayjs from 'dayjs';
import type { ChangeSet, FileChange } from '@/types';

const { Text } = Typography;

const statusColors: Record<string, string> = {
  A: 'green',
  M: 'blue',
  D: 'red',
  R: 'orange',
};

const statusText: Record<string, string> = {
  A: '新增',
  M: '修改',
  D: '删除',
  R: '重命名',
};

export function CodeChanges() {
  const [viewingChangeSet, setViewingChangeSet] = useState<ChangeSet | null>(null);
  const [selectedFile, setSelectedFile] = useState<FileChange | null>(null);
  const [viewMode, setViewMode] = useState<'unified' | 'split'>('unified');
  const isMobile = useIsMobile();

  const { data, isLoading } = useChangeSets();
  const changeSets = data?.data || [];

  const totalAdded = changeSets.reduce((sum, cs) => sum + cs.fileStats.added, 0);
  const totalModified = changeSets.reduce((sum, cs) => sum + cs.fileStats.modified, 0);
  const totalDeleted = changeSets.reduce((sum, cs) => sum + cs.fileStats.deleted, 0);

  const columns: ColumnsType<ChangeSet> = [
    {
      title: '变更集 ID',
      dataIndex: 'id',
      key: 'id',
      width: 100,
      render: (id, record) => (
        <a onClick={() => setViewingChangeSet(record)} className="font-mono text-primary-500">
          {id.slice(0, 8)}
        </a>
      ),
    },
    {
      title: '摘要',
      dataIndex: 'summary',
      key: 'summary',
      ellipsis: true,
    },
    {
      title: '范围',
      dataIndex: 'changeScope',
      key: 'changeScope',
      width: 100,
      render: (scope) => {
        const colors: Record<string, string> = { stage: 'blue', workflow: 'green', review: 'purple', delivery: 'orange' };
        return <Tag color={colors[scope]}>{scope}</Tag>;
      },
    },
    {
      title: '文件变更',
      key: 'files',
      width: 180,
      responsive: ['md'],
      render: (_, record) => (
        <Space>
          {record.fileStats.added > 0 && <Tag color="green">+{record.fileStats.added}</Tag>}
          {record.fileStats.modified > 0 && <Tag color="blue">~{record.fileStats.modified}</Tag>}
          {record.fileStats.deleted > 0 && <Tag color="red">-{record.fileStats.deleted}</Tag>}
        </Space>
      ),
    },
    {
      title: '行数变更',
      key: 'lines',
      width: 120,
      responsive: ['lg'],
      render: (_, record) => (
        <span>
          <Text type="success">+{record.fileStats.totalAdditions}</Text>
          {' / '}
          <Text type="danger">-{record.fileStats.totalDeletions}</Text>
        </span>
      ),
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
      width: 100,
      fixed: 'right',
      render: (_, record) => (
        <Space size="small">
          <Button type="text" size="small" icon={<EyeOutlined />} onClick={() => setViewingChangeSet(record)}>
            查看
          </Button>
        </Space>
      ),
    },
  ];

  const buildFileTree = (files: FileChange[]): DataNode[] => {
    const tree: DataNode[] = [];
    const paths = new Map<string, DataNode>();

    files.forEach(file => {
      const parts = file.path.split('/');
      let currentPath = '';

      parts.forEach((part, index) => {
        const isFile = index === parts.length - 1;
        currentPath += (currentPath ? '/' : '') + part;

        if (!paths.has(currentPath)) {
          const node: DataNode = {
            key: currentPath,
            title: isFile ? (
              <div className="flex items-center gap-2">
                <Tag color={statusColors[file.status]} className="!m-0 !text-xs">
                  {statusText[file.status]}
                </Tag>
                <span>{part}</span>
                <span className="text-xs text-gray-400">
                  +{file.additions}/-{file.deletions}
                </span>
              </div>
            ) : part,
            icon: isFile ? (
              <FileTextOutlined className={file.status === 'D' ? 'text-red-400' : 'text-gray-500'} />
            ) : (
              <FolderOutlined className="text-yellow-500" />
            ),
            isLeaf: isFile,
            children: isFile ? undefined : [],
          };
          paths.set(currentPath, node);

          if (index === 0) {
            tree.push(node);
          } else {
            const parentPath = parts.slice(0, index).join('/');
            const parent = paths.get(parentPath);
            if (parent && parent.children) {
              parent.children.push(node);
            }
          }
        }
      });
    });

    return tree;
  };

  const renderDiffView = () => {
    if (!selectedFile) {
      return <Empty description="请选择一个文件查看差异" />;
    }

    return (
      <div>
        <div className="bg-gray-100 dark:bg-gray-800 p-3 rounded-t-lg border-b">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Tag color={statusColors[selectedFile.status]}>
                {statusText[selectedFile.status]}
              </Tag>
              <Text code>{selectedFile.path}</Text>
              {selectedFile.oldPath && (
                <Text type="secondary">← {selectedFile.oldPath}</Text>
              )}
            </div>
            <div>
              <Text type="success">+{selectedFile.additions}</Text>
              {' / '}
              <Text type="danger">-{selectedFile.deletions}</Text>
            </div>
          </div>
        </div>
        <div className="flex justify-end p-2 bg-gray-50 border-b">
          <Segmented
            value={viewMode}
            onChange={(v) => setViewMode(v as 'unified' | 'split')}
            options={[
              { value: 'unified', label: 'Unified', icon: <DiffOutlined /> },
              { value: 'split', label: 'Split', icon: <SplitCellsOutlined /> },
            ]}
            size="small"
          />
        </div>
        <Alert
          className="m-4"
          type="info"
          icon={<InfoCircleOutlined />}
          showIcon
          message="差异内容"
          description={
            viewingChangeSet?.patchArtifactId
              ? `差异补丁已保存为产物 (ID: ${viewingChangeSet.patchArtifactId})，可通过产物管理查看完整 diff。`
              : '此变更集暂无关联的补丁产物文件。'
          }
        />
      </div>
    );
  };

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-xl sm:text-2xl font-bold text-gray-800 dark:text-white">
            代码变更
          </h1>
          <p className="text-gray-500 dark:text-gray-400 mt-1">
            查看和审阅工作流产生的代码变更
          </p>
        </div>
      </div>

      {/* Stats */}
      <Row gutter={[16, 16]}>
        <Col xs={12} sm={6}>
          <Card>
            <Statistic
              title="变更集"
              value={changeSets.length}
              prefix={<DiffOutlined className="text-blue-500" />}
            />
          </Card>
        </Col>
        <Col xs={12} sm={6}>
          <Card>
            <Statistic
              title="新增文件"
              value={totalAdded}
              prefix={<FileAddOutlined className="text-green-500" />}
            />
          </Card>
        </Col>
        <Col xs={12} sm={6}>
          <Card>
            <Statistic
              title="修改文件"
              value={totalModified}
              prefix={<EditOutlined className="text-blue-500" />}
            />
          </Card>
        </Col>
        <Col xs={12} sm={6}>
          <Card>
            <Statistic
              title="删除文件"
              value={totalDeleted}
              prefix={<DeleteOutlined className="text-red-500" />}
            />
          </Card>
        </Col>
      </Row>

      {/* Table */}
      <Card styles={{ body: { padding: 0 } }}>
        <Table
          columns={columns}
          dataSource={changeSets}
          rowKey="id"
          loading={isLoading}
          scroll={{ x: 700 }}
          pagination={{
            showSizeChanger: false,
            simple: isMobile,
          }}
          size={isMobile ? 'small' : 'middle'}
        />
      </Card>

      {/* Detail Drawer */}
      <Drawer
        title={`变更集详情 - ${viewingChangeSet?.id?.slice(0, 8)}`}
        open={!!viewingChangeSet}
        onClose={() => {
          setViewingChangeSet(null);
          setSelectedFile(null);
        }}
        width={isMobile ? '100%' : 800}
      >
        {viewingChangeSet && (
          <div className="space-y-4">
            {/* Summary */}
            <Card size="small">
              <Descriptions column={isMobile ? 1 : 2} size="small">
                <Descriptions.Item label="摘要" span={2}>{viewingChangeSet.summary}</Descriptions.Item>
                <Descriptions.Item label="范围">
                  <Tag color={{ stage: 'blue', workflow: 'green', review: 'purple', delivery: 'orange' }[viewingChangeSet.changeScope]}>
                    {viewingChangeSet.changeScope}
                  </Tag>
                </Descriptions.Item>
                <Descriptions.Item label="创建时间">
                  {dayjs(viewingChangeSet.createdAt).format('YYYY-MM-DD HH:mm:ss')}
                </Descriptions.Item>
              </Descriptions>
            </Card>

            {/* File Stats */}
            <Row gutter={16}>
              <Col span={6}>
                <Card size="small" className="text-center">
                  <Statistic
                    value={viewingChangeSet.fileStats.added}
                    prefix={<FileAddOutlined className="text-green-500" />}
                    valueStyle={{ color: '#52c41a' }}
                  />
                  <div className="text-xs text-gray-500 mt-1">新增</div>
                </Card>
              </Col>
              <Col span={6}>
                <Card size="small" className="text-center">
                  <Statistic
                    value={viewingChangeSet.fileStats.modified}
                    prefix={<EditOutlined className="text-blue-500" />}
                    valueStyle={{ color: '#1890ff' }}
                  />
                  <div className="text-xs text-gray-500 mt-1">修改</div>
                </Card>
              </Col>
              <Col span={6}>
                <Card size="small" className="text-center">
                  <Statistic
                    value={viewingChangeSet.fileStats.deleted}
                    prefix={<DeleteOutlined className="text-red-500" />}
                    valueStyle={{ color: '#ff4d4f' }}
                  />
                  <div className="text-xs text-gray-500 mt-1">删除</div>
                </Card>
              </Col>
              <Col span={6}>
                <Card size="small" className="text-center">
                  <Statistic
                    value={viewingChangeSet.fileStats.renamed}
                    prefix={<DiffOutlined className="text-orange-500" />}
                    valueStyle={{ color: '#fa8c16' }}
                  />
                  <div className="text-xs text-gray-500 mt-1">重命名</div>
                </Card>
              </Col>
            </Row>

            {/* Tabs */}
            <Tabs
              defaultActiveKey="files"
              items={[
                {
                  key: 'files',
                  label: '文件列表',
                  children: (
                    <Table
                      size="small"
                      dataSource={viewingChangeSet.files}
                      rowKey="path"
                      pagination={false}
                      columns={[
                        {
                          title: '状态',
                          dataIndex: 'status',
                          width: 60,
                          render: (status) => (
                            <Tag color={statusColors[status]}>{status}</Tag>
                          ),
                        },
                        {
                          title: '路径',
                          dataIndex: 'path',
                          ellipsis: true,
                          render: (path, record) => (
                            <a onClick={() => setSelectedFile(record as FileChange)} className="text-primary-500">
                              {path}
                            </a>
                          ),
                        },
                        {
                          title: '变更',
                          key: 'changes',
                          width: 100,
                          render: (_, record) => (
                            <span>
                              <Text type="success">+{record.additions}</Text>
                              {' / '}
                              <Text type="danger">-{record.deletions}</Text>
                            </span>
                          ),
                        },
                      ]}
                    />
                  ),
                },
                {
                  key: 'tree',
                  label: '文件树',
                  children: (
                    <Tree
                      showIcon
                      defaultExpandAll
                      treeData={buildFileTree(viewingChangeSet.files)}
                      onSelect={(keys) => {
                        const file = viewingChangeSet.files.find(f => f.path === keys[0]);
                        if (file) setSelectedFile(file);
                      }}
                    />
                  ),
                },
                {
                  key: 'diff',
                  label: '差异查看',
                  children: renderDiffView(),
                },
              ]}
            />
          </div>
        )}
      </Drawer>
    </div>
  );
}
