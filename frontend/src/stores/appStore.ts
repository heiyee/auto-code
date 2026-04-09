import { create } from 'zustand';
import type { Project, Requirement, CLISession } from '@/types';

interface AppState {
  // Sidebar state
  sidebarCollapsed: boolean;
  setSidebarCollapsed: (collapsed: boolean) => void;
  toggleSidebar: () => void;

  // Mobile drawer state
  mobileDrawerOpen: boolean;
  setMobileDrawerOpen: (open: boolean) => void;

  // Selected items
  selectedProject: Project | null;
  setSelectedProject: (project: Project | null) => void;

  selectedRequirement: Requirement | null;
  setSelectedRequirement: (requirement: Requirement | null) => void;

  selectedSession: CLISession | null;
  setSelectedSession: (session: CLISession | null) => void;
}

export const useAppStore = create<AppState>((set) => ({
  // Sidebar
  sidebarCollapsed: false,
  setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
  toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),

  // Mobile drawer
  mobileDrawerOpen: false,
  setMobileDrawerOpen: (open) => set({ mobileDrawerOpen: open }),

  // Selected items
  selectedProject: null,
  setSelectedProject: (project) => set({ selectedProject: project }),

  selectedRequirement: null,
  setSelectedRequirement: (requirement) => set({ selectedRequirement: requirement }),

  selectedSession: null,
  setSelectedSession: (session) => set({ selectedSession: session }),
}));
