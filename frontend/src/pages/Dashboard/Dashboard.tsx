import { Card, Row, Col, Statistic, List, Tag, Button, Skeleton, Alert, Progress } from 'antd';
import {
  ProjectOutlined,
  FileTextOutlined,
  PlusOutlined,
  ClockCircleOutlined,
  ThunderboltOutlined,
  AuditOutlined,
  SyncOutlined,
  ExclamationCircleOutlined,
  CheckCircleOutlined,
  QuestionCircleOutlined,
} from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { useDashboardStats, useDashboardActivities, useWorkflows } from '@/hooks';
import { useIsMobile } from '@/hooks/useMediaQuery';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import 'dayjs/locale/zh-cn';
import type { Activity } from '@/types';

dayjs.extend(relativeTime);
dayjs.locale('zh-cn');

const activityColors: Record<string, string> = {
  project: 'blue',
  requirement: 'green',
  session: 'orange',
  workflow: 'purple',
  review: 'magenta',
  artifact: 'cyan',
};

const actionText: Record<string, string> = {
  created: '创建',
  updated: '更新',
  completed: '完成',
  started: '开始',
  approved: '通过',
  rejected: '驳回',
};

export function Dashboard() {
  const navigate = useNavigate();
  const isMobile = useIsMobile();

  const { data: statsData, isLoading: statsLoading } = useDashboardStats();
  const { data: activitiesData, isLoading: activitiesLoading } = useDashboardActivities();
  const { data: workflowsData } = useWorkflows({ status: 'running', refetchInterval: 8000 });

  const stats = statsData?.data;
  const activities = activitiesData?.data || [];
  const activeWorkflows = workflowsData?.data || [];

  const statCards = [
    {
      title: '项目总数',
      value: stats?.totalProjects || 0,
      icon: <ProjectOutlined className="text-2xl" />,
      color: 'text-blue-500',
      bgColor: 'bg-blue-50',
    },
    {
      title: '需求数',
      value: stats?.totalRequirements || 0,
      icon: <FileTextOutlined className="text-2xl" />,
      color: 'text-green-500',
      bgColor: 'bg-green-50',
    },
    {
      title: '运行中',
      value: stats?.runningTasks || 0,
      icon: <SyncOutlined spin className="text-2xl" />,
      color: 'text-orange-500',
      bgColor: 'bg-orange-50',
    },
    {
      title: '已完成',
      value: stats?.completedTasks || 0,
      icon: <CheckCircleOutlined className="text-2xl" />,
      color: 'text-purple-500',
      bgColor: 'bg-purple-50',
    },
    {
      title: '活跃工作流',
      value: stats?.activeWorkflows || 0,
      icon: <ThunderboltOutlined className="text-2xl" />,
      color: 'text-cyan-500',
      bgColor: 'bg-cyan-50',
    },
    {
      title: '待审核',
      value: stats?.pendingReviews || 0,
      icon: <AuditOutlined className="text-2xl" />,
      color: 'text-magenta-500',
      bgColor: 'bg-magenta-50',
    },
    {
      title: '待决策',
      value: stats?.pendingDecisions || 0,
      icon: <QuestionCircleOutlined className="text-2xl" />,
      color: 'text-orange-500',
      bgColor: 'bg-orange-50',
    },
  ];

  const renderActivityItem = (item: Activity) => (
    <List.Item>
      <div className="flex items-start gap-3 w-full">
        <div className="flex-shrink-0 mt-1">
          <ClockCircleOutlined className="text-gray-400" />
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="font-medium text-gray-800 dark:text-gray-200">
              {item.title}
            </span>
            <Tag color={activityColors[item.type]}>
              {actionText[item.action]}
            </Tag>
          </div>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1 truncate">
            {item.description}
          </p>
          <span className="text-xs text-gray-400">
            {dayjs(item.timestamp).fromNow()}
          </span>
        </div>
      </div>
    </List.Item>
  );

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-xl sm:text-2xl font-bold text-gray-800 dark:text-white">
            仪表盘
          </h1>
          <p className="text-gray-500 dark:text-gray-400 mt-1">
            欢迎回来，这是您的自动化研发系统概览
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => navigate('/requirements')}
            className="w-full sm:w-auto"
          >
            创建需求
          </Button>
        </div>
      </div>

      {/* Pending Blockers Alerts */}
      {stats && (stats.pendingReviews > 0 || stats.pendingDecisions > 0) && (
        <div className="space-y-2">
          {stats.pendingDecisions > 0 && (
            <Alert
              message={`您有 ${stats.pendingDecisions} 个待处理决策`}
              description="待处理的决策会阻塞工作流继续执行，请优先处理"
              type="error"
              showIcon
              icon={<QuestionCircleOutlined />}
              action={
                <Button size="small" type="primary" danger onClick={() => navigate('/decisions')}>
                  立即处理
                </Button>
              }
            />
          )}
          {stats.pendingReviews > 0 && (
            <Alert
              message={`您有 ${stats.pendingReviews} 个待审核项`}
              description="请及时处理待审核项，避免阻塞工作流执行"
              type="warning"
              showIcon
              icon={<ExclamationCircleOutlined />}
              action={
                <Button size="small" type="primary" onClick={() => navigate('/reviews')}>
                  立即处理
                </Button>
              }
            />
          )}
        </div>
      )}

      {/* Stats Cards */}
      <Row gutter={[16, 16]}>
        {statCards.map((card, index) => (
          <Col key={index} xs={12} sm={12} md={8} lg={4}>
            <Card
              className="h-full hover:shadow-md transition-shadow cursor-pointer"
              styles={{ body: { padding: isMobile ? 16 : 20 } }}
              onClick={() => {
                if (index === 0) navigate('/projects');
                else if (index === 1) navigate('/requirements');
                else if (index === 2 || index === 4) navigate('/workflows');
                else if (index === 5) navigate('/reviews');
                else if (index === 6) navigate('/decisions');
              }}
            >
              {statsLoading ? (
                <Skeleton active paragraph={{ rows: 1 }} />
              ) : (
                <div className="flex items-center gap-3">
                  <div className={`p-2 sm:p-3 rounded-lg ${card.bgColor}`}>
                    <span className={card.color}>{card.icon}</span>
                  </div>
                  <Statistic
                    title={<span className="text-xs sm:text-sm">{card.title}</span>}
                    value={card.value}
                    className="flex-1"
                  />
                </div>
              )}
            </Card>
          </Col>
        ))}
      </Row>

      {/* Workflow Progress Overview */}
      <Card title="工作流执行状态" styles={{ body: { padding: isMobile ? 12 : 24 } }}>
        <Row gutter={[24, 24]}>
          <Col xs={24} md={12}>
            <div className="space-y-4">
              <div className="flex justify-between items-center">
                <span className="text-gray-600">运行中工作流</span>
                <span className="text-primary-500 font-medium">{stats?.activeWorkflows || 0} 个</span>
              </div>
              <Progress
                percent={stats && (stats.completedTasks + stats.runningTasks) > 0
                  ? Math.round(stats.completedTasks / (stats.completedTasks + stats.runningTasks) * 100)
                  : 0}
                status="active"
              />
              <div className="grid grid-cols-2 sm:grid-cols-3 gap-4 text-center text-sm">
                <div>
                  <div className="text-lg font-bold text-green-500">{stats?.completedTasks || 0}</div>
                  <div className="text-gray-400">已完成</div>
                </div>
                <div>
                  <div className="text-lg font-bold text-blue-500">{stats?.runningTasks || 0}</div>
                  <div className="text-gray-400">进行中</div>
                </div>
                <div>
                  <div className="text-lg font-bold text-gray-400">{stats?.pendingReviews || 0}</div>
                  <div className="text-gray-400">待审核</div>
                </div>
              </div>
            </div>
          </Col>
          <Col xs={24} md={12}>
            <div className="space-y-3">
              <h4 className="font-medium text-gray-700">活跃工作流</h4>
              <List
                size="small"
                dataSource={activeWorkflows.slice(0, 3)}
                locale={{ emptyText: '暂无活跃工作流' }}
                renderItem={item => (
                  <List.Item>
                    <div className="flex-1">
                      <div className="flex justify-between mb-1">
                        <span className="text-sm">{item.requirementTitle || '未命名需求'}</span>
                        <span className="text-xs text-gray-400">{item.projectName}</span>
                      </div>
                      <Progress percent={item.progress} size="small" showInfo={false} />
                    </div>
                  </List.Item>
                )}
              />
            </div>
          </Col>
        </Row>
      </Card>

      {/* Quick Actions & Recent Activity */}
      <Row gutter={[16, 16]}>
        {/* Quick Actions */}
        <Col xs={24} lg={8}>
          <Card
            title="快速操作"
            className="h-full"
            styles={{ body: { padding: isMobile ? 16 : 24 } }}
          >
            <div className="space-y-3">
              <Button
                block
                icon={<PlusOutlined />}
                onClick={() => navigate('/requirements')}
              >
                创建需求
              </Button>
              <Button
                block
                icon={<ThunderboltOutlined />}
                onClick={() => navigate('/workflows')}
              >
                查看工作流
              </Button>
              <Button
                block
                icon={<AuditOutlined />}
                onClick={() => navigate('/reviews')}
              >
                处理审核
              </Button>
              <Button
                block
                icon={<QuestionCircleOutlined />}
                onClick={() => navigate('/decisions')}
              >
                处理决策
              </Button>
              <Button
                block
                icon={<ProjectOutlined />}
                onClick={() => navigate('/projects')}
              >
                项目管理
              </Button>
            </div>
          </Card>
        </Col>

        {/* Recent Activity */}
        <Col xs={24} lg={16}>
          <Card
            title="最近活动"
            className="h-full"
            styles={{ body: { padding: 0 } }}
          >
            {activitiesLoading ? (
              <div className="p-4">
                <Skeleton active paragraph={{ rows: 4 }} />
              </div>
            ) : (
              <List
                dataSource={activities}
                renderItem={renderActivityItem}
                className="px-4 py-2"
                locale={{ emptyText: '暂无活动记录' }}
              />
            )}
          </Card>
        </Col>
      </Row>
    </div>
  );
}
