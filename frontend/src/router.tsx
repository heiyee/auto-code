import { createBrowserRouter } from 'react-router-dom';
import { MainLayout } from '@/components/layout';
import { Dashboard } from '@/pages/Dashboard';
import { Projects } from '@/pages/Projects';
import { Requirements } from '@/pages/Requirements';
import { FileManager } from '@/pages/FileManager';
import { CLISessions } from '@/pages/CLISessions';
import { Workflows } from '@/pages/Workflows';
import { Reviews } from '@/pages/Reviews';
import { Decisions } from '@/pages/Decisions';
import { CodeChanges } from '@/pages/CodeChanges';
import { Git } from '@/pages/Git';
import { Login } from '@/pages/Login';

const routerBase = import.meta.env.PROD ? '/app' : '/';

export const router = createBrowserRouter([
  {
    path: '/login',
    element: <Login />,
  },
  {
    path: '/',
    element: <MainLayout />,
    children: [
      {
        index: true,
        element: <Dashboard />,
      },
      {
        path: 'projects',
        element: <Projects />,
      },
      {
        path: 'requirements',
        element: <Requirements />,
      },
      {
        path: 'workflows',
        element: <Workflows />,
      },
      {
        path: 'reviews',
        element: <Reviews />,
      },
      {
        path: 'decisions',
        element: <Decisions />,
      },
      {
        path: 'changes',
        element: <CodeChanges />,
      },
      {
        path: 'files',
        element: <FileManager />,
      },
      {
        path: 'git',
        element: <Git />,
      },
      {
        path: 'sessions',
        element: <CLISessions />,
      },
      {
        path: 'sessions/:sessionId',
        element: <CLISessions />,
      },
    ],
  },
], {
  basename: routerBase,
});
