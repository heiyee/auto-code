import { useNavigate, useLocation } from 'react-router-dom';
import {
  DashboardOutlined,
  ProjectOutlined,
  FileTextOutlined,
  CodeOutlined,
  FolderOutlined,
  ThunderboltOutlined,
  AuditOutlined,
  DiffOutlined,
  QuestionCircleOutlined,
  BranchesOutlined,
} from '@ant-design/icons';
import { Menu } from 'antd';
import type { MenuProps } from 'antd';
import { useAppStore } from '@/stores/appStore';
import { useIsMobile } from '@/hooks';
import clsx from 'clsx';

type MenuItem = Required<MenuProps>['items'][number];

const menuItems: MenuItem[] = [
  {
    key: '/',
    icon: <DashboardOutlined />,
    label: '仪表盘',
  },
  {
    key: '/workflows',
    icon: <ThunderboltOutlined />,
    label: '工作流',
  },
  {
    key: '/projects',
    icon: <ProjectOutlined />,
    label: '项目管理',
  },
  {
    key: '/requirements',
    icon: <FileTextOutlined />,
    label: '需求管理',
  },
  {
    key: '/reviews',
    icon: <AuditOutlined />,
    label: '审核中心',
  },
  {
    key: '/decisions',
    icon: <QuestionCircleOutlined />,
    label: '决策中心',
  },
  {
    key: '/changes',
    icon: <DiffOutlined />,
    label: '代码变更',
  },
  {
    key: '/files',
    icon: <FolderOutlined />,
    label: '文件管理',
  },
  {
    key: '/git',
    icon: <BranchesOutlined />,
    label: 'Git 管理',
  },
  {
    key: '/sessions',
    icon: <CodeOutlined />,
    label: 'CLI 会话',
  },
];

export function Sidebar() {
  const navigate = useNavigate();
  const location = useLocation();
  const { sidebarCollapsed, setMobileDrawerOpen } = useAppStore();
  const isMobile = useIsMobile();
  const effectiveCollapsed = sidebarCollapsed;

  const handleMenuClick: MenuProps['onClick'] = ({ key }) => {
    navigate(key);
    if (isMobile) {
      setMobileDrawerOpen(false);
    }
  };

  const selectedKey = menuItems
    .map(item => String(item?.key || ''))
    .find(key => {
      if (!key) {
        return false;
      }
      if (key === '/') {
        return location.pathname === '/';
      }
      return location.pathname === key || location.pathname.startsWith(`${key}/`);
    }) || '/';

  return (
    <div
      className={clsx(
        'h-full bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700',
        'transition-all duration-300',
        effectiveCollapsed ? 'w-16' : 'w-56'
      )}
    >
      {/* Logo */}
      <div
        className={clsx(
          'h-14 flex items-center border-b border-gray-200 dark:border-gray-700',
          effectiveCollapsed ? 'justify-center px-2' : 'px-4'
        )}
      >
        <CodeOutlined className="text-xl text-primary-500" />
        {!effectiveCollapsed && (
          <span className="ml-2 font-semibold text-gray-800 dark:text-white">
            WorkFlow Deck
          </span>
        )}
      </div>

      {/* Menu */}
      <Menu
        mode="inline"
        selectedKeys={[selectedKey]}
        onClick={handleMenuClick}
        items={menuItems}
        className={clsx(
          'border-none h-[calc(100%-56px)]',
          effectiveCollapsed ? '!w-16' : '!w-56'
        )}
        inlineCollapsed={effectiveCollapsed}
      />
    </div>
  );
}
