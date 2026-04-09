import { Outlet, useLocation } from 'react-router-dom';
import { Drawer } from 'antd';
import { Header } from './Header';
import { Sidebar } from './Sidebar';
import { useAppStore } from '@/stores/appStore';
import { useIsMobile } from '@/hooks';
import clsx from 'clsx';

export function MainLayout() {
  const location = useLocation();
  const { mobileDrawerOpen, setMobileDrawerOpen } = useAppStore();
  const isMobile = useIsMobile();
  const isSessionsRoute = location.pathname === '/sessions' || location.pathname.startsWith('/sessions/');

  return (
    <div className="h-screen flex flex-col bg-gray-50 dark:bg-gray-900">
      <Header />

      <div className="flex-1 flex overflow-hidden">
        {/* Desktop Sidebar */}
        {!isMobile && <Sidebar />}

        {/* Mobile Drawer */}
        {isMobile && (
          <Drawer
            placement="left"
            open={mobileDrawerOpen}
            onClose={() => setMobileDrawerOpen(false)}
            width={224}
            className="!p-0"
            closable={false}
            bodyStyle={{ padding: 0, height: '100%' }}
          >
            <Sidebar />
          </Drawer>
        )}

        {/* Main Content */}
        <main
          className={clsx(
            'flex-1 min-h-0 transition-all duration-300',
            isSessionsRoute ? 'overflow-hidden p-3 sm:p-4' : 'overflow-auto p-4 sm:p-6',
          )}
        >
          <div className={clsx(isSessionsRoute ? 'w-full h-full min-h-0' : 'max-w-7xl mx-auto')}>
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  );
}
