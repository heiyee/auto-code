import { MenuOutlined, UserOutlined, BellOutlined } from '@ant-design/icons';
import { Button, Avatar, Dropdown, Badge, message } from 'antd';
import type { MenuProps } from 'antd';
import { useAppStore } from '@/stores/appStore';
import { logout, useAuthStatus, useIsMobile } from '@/hooks';
import clsx from 'clsx';

const LOGIN_PATH = import.meta.env.PROD ? '/app/login' : '/login';

export function Header() {
  const { toggleSidebar, setMobileDrawerOpen, sidebarCollapsed } = useAppStore();
  const isMobile = useIsMobile();
  const { data: authStatus } = useAuthStatus();
  const authEnabled = authStatus?.auth_enabled ?? false;

  const userMenuItems: MenuProps['items'] = authEnabled ? [
    {
      key: 'logout',
      label: '退出登录',
    },
  ] : [];

  const handleUserMenuClick: MenuProps['onClick'] = async ({ key }) => {
    if (key !== 'logout') {
      return;
    }
    try {
      await logout();
      window.location.assign(LOGIN_PATH);
    } catch {
      message.error('退出登录失败');
    }
  };

  return (
    <header className="h-14 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 flex items-center justify-between px-4">
      {/* Left section */}
      <div className="flex items-center">
        {isMobile ? (
          <Button
            type="text"
            icon={<MenuOutlined />}
            onClick={() => setMobileDrawerOpen(true)}
            className="!p-2"
          />
        ) : (
          <Button
            type="text"
            icon={<MenuOutlined />}
            onClick={toggleSidebar}
            className={clsx(
              '!p-2 transition-transform duration-300',
              sidebarCollapsed && 'rotate-180'
            )}
          />
        )}
        <h1 className="ml-3 text-lg font-medium text-gray-800 dark:text-white hidden sm:block">
          AI 编程助手管理平台
        </h1>
      </div>

      {/* Right section */}
      <div className="flex items-center gap-2 sm:gap-4">
        {/* Notifications */}
        <Badge count={3} size="small">
          <Button
            type="text"
            icon={<BellOutlined className="text-lg" />}
            className="!p-2"
          />
        </Badge>

        {/* User menu */}
        <Dropdown
          menu={{ items: userMenuItems, onClick: handleUserMenuClick }}
          placement="bottomRight"
          trigger={['click']}
          disabled={!authEnabled}
        >
          <div className="flex items-center cursor-pointer hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg px-2 py-1 transition-colors">
            <Avatar
              size="small"
              icon={<UserOutlined />}
              className="bg-primary-500"
            />
            <span className="ml-2 text-sm text-gray-700 dark:text-gray-200 hidden sm:block">
              {authEnabled ? 'Admin' : 'Local'}
            </span>
          </div>
        </Dropdown>
      </div>
    </header>
  );
}
