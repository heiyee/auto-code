import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import type {
  Project,
  Requirement,
  RequirementMutationInput,
  RequirementUpdateInput,
  CLISession,
  DashboardStats,
  Activity,
  WorkflowRun,
  ReviewGate,
  ChangeSet,
  TaskItem,
  DecisionRequest,
  Artifact,
  StageRun,
  FileNode,
  FileContent,
  GitStatus,
  GitBranchList,
  GitLocalRepoInspection,
  GitFileDiff,
  GitActionResult,
  CLIProfileGroup,
  CLISessionCreateResult,
  CLIPollResult,
  CLISnapshot,
  CLIReconnectResult,
  SolutionTemplate,
  SolutionBootstrapInput,
  SolutionBootstrapData,
} from '@/types';

const API_BASE = '/api/v1';
const LOGIN_PATH = import.meta.env.PROD ? '/app/login' : '/login';

interface CLIEnvelope<T> {
  code: number;
  message: string;
  data: T;
}

interface AuthStatusResponse {
  auth_enabled: boolean;
}

// Fetch helper
async function fetchApi<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });

  if (response.status === 401) {
    if (typeof window !== 'undefined' && window.location.pathname !== LOGIN_PATH) {
      window.location.assign(LOGIN_PATH);
    }
    throw new Error('Unauthorized');
  }

  if (!response.ok) {
    throw new Error(`API Error: ${response.status}`);
  }

  return response.json();
}

async function fetchCLI<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });

  if (response.status === 401) {
    if (typeof window !== 'undefined' && window.location.pathname !== LOGIN_PATH) {
      window.location.assign(LOGIN_PATH);
    }
    throw new Error('Unauthorized');
  }

  const payload = await response.json() as CLIEnvelope<T>;
  if (!response.ok || payload.code !== 0) {
    const error = new Error(payload.message || `API Error: ${response.status}`) as Error & {
      apiCode?: number;
      httpStatus?: number;
    };
    error.apiCode = payload.code || response.status;
    error.httpStatus = response.status;
    throw error;
  }

  return payload.data;
}

function invalidateRequirementDerivedQueries(queryClient: ReturnType<typeof useQueryClient>) {
  const dependentKeys = new Set([
    'dashboard',
    'requirements',
    'requirement',
    'workflows',
    'workflow',
    'reviews',
    'review',
    'decisions',
    'changesets',
    'artifacts',
    'snapshots',
  ]);

  return queryClient.invalidateQueries({
    predicate: query => dependentKeys.has(String(query.queryKey[0] ?? '')),
  });
}

// Dashboard API
export function useDashboardStats() {
  return useQuery({
    queryKey: ['dashboard', 'stats'],
    queryFn: () => fetchApi<{ data: DashboardStats; success: boolean }>(`${API_BASE}/dashboard/stats`),
  });
}

export function useDashboardActivities() {
  return useQuery({
    queryKey: ['dashboard', 'activities'],
    queryFn: () => fetchApi<{ data: Activity[]; success: boolean }>(`${API_BASE}/dashboard/activities`),
  });
}

// Projects API
export function useProjects(params?: { page?: number; pageSize?: number; search?: string }) {
  const searchParams = new URLSearchParams();
  if (params?.page) searchParams.set('page', String(params.page));
  if (params?.pageSize) searchParams.set('pageSize', String(params.pageSize));
  if (params?.search) searchParams.set('search', params.search);

  const queryString = searchParams.toString();
  const url = `${API_BASE}/projects${queryString ? `?${queryString}` : ''}`;

  return useQuery({
    queryKey: ['projects', params],
    queryFn: () => fetchApi<{ data: Project[]; total: number; page: number; pageSize: number; success: boolean }>(url),
  });
}

export function useProject(id: string) {
  return useQuery({
    queryKey: ['project', id],
    queryFn: () => fetchApi<{ data: Project; success: boolean }>(`${API_BASE}/projects/${id}`),
    enabled: !!id,
  });
}

export function useCreateProject() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (project: Omit<Project, 'id' | 'createdAt' | 'updatedAt'>) =>
      fetchApi<{ data: Project; success: boolean }>(`${API_BASE}/projects`, {
        method: 'POST',
        body: JSON.stringify(project),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
    },
  });
}

export function useUpdateProject() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<Project> }) =>
      fetchApi<{ data: Project; success: boolean }>(`${API_BASE}/projects/${id}`, {
        method: 'PUT',
        body: JSON.stringify(data),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
    },
  });
}

export function useDeleteProject() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) =>
      fetchApi<{ success: boolean }>(`${API_BASE}/projects/${id}`, {
        method: 'DELETE',
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
    },
  });
}

export function useInspectLocalRepo() {
  return useMutation({
    mutationFn: async (workDir: string) => {
      const payload = await fetchCLI<{
        operation: string;
        repository: string;
        values?: string[];
        current_branch?: string;
        remote_url?: string;
      }>('/api/git/query', {
        method: 'POST',
        body: JSON.stringify({
          operation: 'inspect-local-repo',
          repository: workDir,
          limit: 100,
        }),
      });

      return {
        operation: payload.operation,
        repository: payload.repository,
        branches: Array.isArray(payload.values) ? payload.values : [],
        currentBranch: String(payload.current_branch || '').trim(),
        remoteURL: String(payload.remote_url || '').trim(),
      } satisfies GitLocalRepoInspection;
    },
  });
}

export function useSolutionTemplates() {
  return useQuery({
    queryKey: ['solution-templates'],
    queryFn: () => fetchApi<{ data: SolutionTemplate[]; success: boolean }>(`${API_BASE}/solution/templates`),
  });
}

export function useBootstrapSolution() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: SolutionBootstrapInput) =>
      fetchApi<{ data: SolutionBootstrapData; success: boolean }>(`${API_BASE}/solution/bootstrap`, {
        method: 'POST',
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      invalidateRequirementDerivedQueries(queryClient);
      queryClient.invalidateQueries({ queryKey: ['solution-templates'] });
      queryClient.invalidateQueries({ queryKey: ['sessions'] });
    },
  });
}

// Requirements API
export function useRequirements(params?: { page?: number; pageSize?: number; status?: string; projectId?: string }) {
  const filters = new URLSearchParams();
  if (params?.status) filters.set('status', params.status);
  if (params?.projectId) filters.set('projectId', params.projectId);

  const fetchRequirementsPage = (page: number, pageSize: number) => {
    const searchParams = new URLSearchParams(filters);
    searchParams.set('page', String(page));
    searchParams.set('pageSize', String(pageSize));
    const queryString = searchParams.toString();
    const url = `${API_BASE}/requirements${queryString ? `?${queryString}` : ''}`;
    return fetchApi<{ data: Requirement[]; total: number; page: number; pageSize: number; success: boolean }>(url);
  };

  return useQuery({
    queryKey: ['requirements', params],
    queryFn: async () => {
      if (params?.page || params?.pageSize) {
        return fetchRequirementsPage(params.page ?? 1, params.pageSize ?? 200);
      }

      const aggregated: Requirement[] = [];
      let page = 1;
      const pageSize = 200;
      let latestResponse = await fetchRequirementsPage(page, pageSize);
      aggregated.push(...latestResponse.data);

      while (aggregated.length < latestResponse.total && latestResponse.data.length === pageSize) {
        page += 1;
        latestResponse = await fetchRequirementsPage(page, pageSize);
        aggregated.push(...latestResponse.data);
      }

      return {
        ...latestResponse,
        data: aggregated,
        page: 1,
      };
    },
  });
}

export function useRequirement(id: string) {
  return useQuery({
    queryKey: ['requirement', id],
    queryFn: () => fetchApi<{ data: Requirement; success: boolean }>(`${API_BASE}/requirements/${id}`),
    enabled: !!id,
  });
}

export function useCreateRequirement() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (requirement: RequirementMutationInput) =>
      fetchApi<{ data: Requirement; success: boolean }>(`${API_BASE}/requirements`, {
        method: 'POST',
        body: JSON.stringify(requirement),
      }),
    onSuccess: () => {
      invalidateRequirementDerivedQueries(queryClient);
    },
  });
}

export function useUpdateRequirement() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: RequirementUpdateInput }) =>
      fetchApi<{ data: Requirement; success: boolean }>(`${API_BASE}/requirements/${id}`, {
        method: 'PUT',
        body: JSON.stringify(data),
      }),
    onSuccess: () => {
      invalidateRequirementDerivedQueries(queryClient);
    },
  });
}

export function useDeleteRequirement() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) =>
      fetchApi<{ success: boolean }>(`${API_BASE}/requirements/${id}`, {
        method: 'DELETE',
      }),
    onSuccess: () => {
      invalidateRequirementDerivedQueries(queryClient);
    },
  });
}

// CLI Sessions API
export function useCLISessions(params?: { page?: number; pageSize?: number; state?: string }) {
  const searchParams = new URLSearchParams();
  if (params?.page) searchParams.set('page', String(params.page));
  if (params?.pageSize) searchParams.set('pageSize', String(params.pageSize));
  if (params?.state) searchParams.set('state', params.state);

  const queryString = searchParams.toString();
  const url = `${API_BASE}/sessions${queryString ? `?${queryString}` : ''}`;

  return useQuery({
    queryKey: ['sessions', params],
    queryFn: () => fetchApi<{ data: CLISession[]; total: number; page: number; pageSize: number; success: boolean }>(url),
  });
}

export function useCLISession(id: string) {
  return useQuery({
    queryKey: ['session', id],
    queryFn: () => fetchApi<{ data: CLISession; success: boolean }>(`${API_BASE}/sessions/${id}`),
    enabled: !!id,
  });
}

export function useDeleteSession() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) =>
      fetchApi<{ success: boolean }>(`${API_BASE}/sessions/${id}`, {
        method: 'DELETE',
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sessions'] });
    },
  });
}

// Workflow Runs API
export function useWorkflows(params?: { page?: number; pageSize?: number; status?: string; projectId?: string; requirementId?: string; refetchInterval?: number }) {
  const searchParams = new URLSearchParams();
  if (params?.page) searchParams.set('page', String(params.page));
  if (params?.pageSize) searchParams.set('pageSize', String(params.pageSize));
  if (params?.status) searchParams.set('status', params.status);
  if (params?.projectId) searchParams.set('projectId', params.projectId);
  if (params?.requirementId) searchParams.set('requirementId', params.requirementId);

  const queryString = searchParams.toString();
  const url = `${API_BASE}/workflows${queryString ? `?${queryString}` : ''}`;

  return useQuery({
    queryKey: ['workflows', params],
    queryFn: () => fetchApi<{ data: WorkflowRun[]; total: number; page: number; pageSize: number; success: boolean }>(url),
    refetchInterval: params?.refetchInterval,
  });
}

export function useWorkflow(id: string, options?: { refetchInterval?: number }) {
  return useQuery({
    queryKey: ['workflow', id],
    queryFn: () => fetchApi<{ data: WorkflowRun & { stages: StageRun[] }; success: boolean }>(`${API_BASE}/workflows/${id}`),
    enabled: !!id,
    refetchInterval: options?.refetchInterval,
  });
}

export function useWorkflowStages(workflowId: string) {
  return useQuery({
    queryKey: ['workflow', workflowId, 'stages'],
    queryFn: () => fetchApi<{ data: StageRun[]; success: boolean }>(`${API_BASE}/workflows/${workflowId}/stages`),
    enabled: !!workflowId,
  });
}

export function useWorkflowTasks(workflowId: string) {
  return useQuery({
    queryKey: ['workflow', workflowId, 'tasks'],
    queryFn: () => fetchApi<{ data: TaskItem[]; success: boolean }>(`${API_BASE}/workflows/${workflowId}/tasks`),
    enabled: !!workflowId,
  });
}

// Review Gates API
export function useReviews(params?: { status?: string; workflowRunId?: string; refetchInterval?: number }) {
  const searchParams = new URLSearchParams();
  if (params?.status) searchParams.set('status', params.status);
  if (params?.workflowRunId) searchParams.set('workflowRunId', params.workflowRunId);

  const queryString = searchParams.toString();
  const url = `${API_BASE}/reviews${queryString ? `?${queryString}` : ''}`;

  return useQuery({
    queryKey: ['reviews', params],
    queryFn: () => fetchApi<{ data: ReviewGate[]; success: boolean }>(url),
    refetchInterval: params?.refetchInterval,
  });
}

export function useUpdateReview() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<ReviewGate> }) =>
      fetchApi<{ data: ReviewGate; success: boolean }>(`${API_BASE}/reviews/${id}`, {
        method: 'PUT',
        body: JSON.stringify(data),
      }),
    onSuccess: () => {
      invalidateRequirementDerivedQueries(queryClient);
    },
  });
}

// Change Sets API
export function useChangeSets(params?: { workflowRunId?: string }) {
  const searchParams = new URLSearchParams();
  if (params?.workflowRunId) searchParams.set('workflowRunId', params.workflowRunId);

  const queryString = searchParams.toString();
  const url = `${API_BASE}/changesets${queryString ? `?${queryString}` : ''}`;

  return useQuery({
    queryKey: ['changesets', params],
    queryFn: () => fetchApi<{ data: ChangeSet[]; success: boolean }>(url),
  });
}

export function useChangeSet(id: string) {
  return useQuery({
    queryKey: ['changeset', id],
    queryFn: () => fetchApi<{ data: ChangeSet; success: boolean }>(`${API_BASE}/changesets/${id}`),
    enabled: !!id,
  });
}

// Artifacts API
export function useArtifacts(params?: { workflowRunId?: string }) {
  const searchParams = new URLSearchParams();
  if (params?.workflowRunId) searchParams.set('workflowRunId', params.workflowRunId);

  const queryString = searchParams.toString();
  const url = `${API_BASE}/artifacts${queryString ? `?${queryString}` : ''}`;

  return useQuery({
    queryKey: ['artifacts', params],
    queryFn: () => fetchApi<{ data: Artifact[]; success: boolean }>(url),
  });
}

// Decision Requests API
export function useDecisions(params?: { workflowRunId?: string; refetchInterval?: number }) {
  const searchParams = new URLSearchParams();
  if (params?.workflowRunId) searchParams.set('workflowRunId', params.workflowRunId);

  const queryString = searchParams.toString();
  const url = `${API_BASE}/decisions${queryString ? `?${queryString}` : ''}`;

  return useQuery({
    queryKey: ['decisions', params],
    queryFn: () => fetchApi<{ data: DecisionRequest[]; success: boolean }>(url),
    refetchInterval: params?.refetchInterval,
  });
}

export function useResolveDecision() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, decision, decider }: { id: string; decision: string; decider?: string }) =>
      fetchApi<{ data: DecisionRequest; success: boolean }>(`${API_BASE}/decisions/${id}`, {
        method: 'PUT',
        body: JSON.stringify({ decision, decider }),
      }),
    onSuccess: () => {
      invalidateRequirementDerivedQueries(queryClient);
    },
  });
}

export function useArtifactContent(id: string) {
  return useQuery({
    queryKey: ['artifact-content', id],
    queryFn: () =>
      fetch(`${API_BASE}/artifacts/${id}/content`).then(r => {
        if (!r.ok) throw new Error(`API Error: ${r.status}`);
        return r.text();
      }),
    enabled: !!id,
  });
}

export function useAuthStatus() {
  return useQuery({
    queryKey: ['auth-status'],
    queryFn: () => fetchApi<AuthStatusResponse>('/api/auth/status'),
    staleTime: 60_000,
  });
}

export async function logout(): Promise<void> {
  const response = await fetch('/api/auth/logout', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
  });
  if (!response.ok) {
    throw new Error(`Logout Error: ${response.status}`);
  }
}

export async function login(username: string, password: string): Promise<void> {
  const response = await fetch('/api/auth/login', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ username, password }),
  });
  if (!response.ok) {
    let message = `Login Error: ${response.status}`;
    try {
      const payload = await response.json() as { error?: string };
      if (payload?.error) {
        message = payload.error;
      }
    } catch {
      // keep fallback status message
    }
    throw new Error(message);
  }
}

// ─── File Manager API ────────────────────────────────────────────────────────

const PROJECT_API = '/api/projects';

export function useProjectFiles(projectId: string, path?: string) {
  const url = `${PROJECT_API}/${projectId}/files${path ? `?path=${encodeURIComponent(path)}` : ''}`;
  return useQuery({
    queryKey: ['project-files', projectId, path ?? ''],
    queryFn: () => fetchApi<{ data: { path: string; nodes: FileNode[] }; success: boolean }>(url),
    enabled: !!projectId,
  });
}

export function useProjectFileContent(projectId: string, filePath: string) {
  return useQuery({
    queryKey: ['project-file-content', projectId, filePath],
    queryFn: () =>
      fetchApi<{ data: FileContent; success: boolean }>(
        `${PROJECT_API}/${projectId}/files/content?path=${encodeURIComponent(filePath)}`
      ),
    enabled: !!projectId && !!filePath,
  });
}

export function useSaveProjectFile() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectId, path, content, baseRevision }: { projectId: string; path: string; content: string; baseRevision: string }) =>
      fetchApi<{ data: { path: string; revision: string }; success: boolean }>(
        `${PROJECT_API}/${projectId}/files/save`,
        { method: 'POST', body: JSON.stringify({ path, content, baseRevision }) }
      ),
    onSuccess: (_, { projectId }) => {
      queryClient.invalidateQueries({ queryKey: ['project-files', projectId] });
      queryClient.invalidateQueries({ queryKey: ['project-file-content', projectId] });
    },
  });
}

export function useCreateProjectFile() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectId, path, content }: { projectId: string; path: string; content?: string }) =>
      fetchApi<{ data: { path: string; revision: string }; success: boolean }>(
        `${PROJECT_API}/${projectId}/files/create`,
        { method: 'POST', body: JSON.stringify({ path, content: content ?? '' }) }
      ),
    onSuccess: (_, { projectId }) => {
      queryClient.invalidateQueries({ queryKey: ['project-files', projectId] });
    },
  });
}

export function useCreateProjectDir() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectId, path }: { projectId: string; path: string }) =>
      fetchApi<{ success: boolean }>(
        `${PROJECT_API}/${projectId}/files/mkdir`,
        { method: 'POST', body: JSON.stringify({ path }) }
      ),
    onSuccess: (_, { projectId }) => {
      queryClient.invalidateQueries({ queryKey: ['project-files', projectId] });
    },
  });
}

export function useRenameProjectFile() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectId, oldPath, newPath }: { projectId: string; oldPath: string; newPath: string }) =>
      fetchApi<{ success: boolean }>(
        `${PROJECT_API}/${projectId}/files/rename`,
        { method: 'POST', body: JSON.stringify({ oldPath, newPath }) }
      ),
    onSuccess: (_, { projectId }) => {
      queryClient.invalidateQueries({ queryKey: ['project-files', projectId] });
    },
  });
}

export function useDeleteProjectFile() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectId, path }: { projectId: string; path: string }) =>
      fetchApi<{ success: boolean }>(
        `${PROJECT_API}/${projectId}/files?path=${encodeURIComponent(path)}`,
        { method: 'DELETE' }
      ),
    onSuccess: (_, { projectId }) => {
      queryClient.invalidateQueries({ queryKey: ['project-files', projectId] });
    },
  });
}

// ─── Git API ─────────────────────────────────────────────────────────────────

export function useGitStatus(projectId: string, options?: { refetchInterval?: number }) {
  return useQuery({
    queryKey: ['git-status', projectId],
    queryFn: () => fetchApi<{ data: GitStatus; success: boolean }>(`${PROJECT_API}/${projectId}/git/status`),
    enabled: !!projectId,
    refetchInterval: options?.refetchInterval,
  });
}

export function useGitBranches(projectId: string) {
  return useQuery({
    queryKey: ['git-branches', projectId],
    queryFn: () => fetchApi<{ data: GitBranchList; success: boolean }>(`${PROJECT_API}/${projectId}/git/branches`),
    enabled: !!projectId,
  });
}

export function useGitDiff(projectId: string, path: string) {
  return useQuery({
    queryKey: ['git-diff', projectId, path],
    queryFn: () =>
      fetchApi<{ data: GitFileDiff; success: boolean }>(
        `${PROJECT_API}/${projectId}/git/diff?path=${encodeURIComponent(path)}`
      ),
    enabled: !!projectId && !!path,
  });
}

export function useGitStage() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectId, path }: { projectId: string; path: string }) =>
      fetchApi<{ success: boolean }>(`${PROJECT_API}/${projectId}/git/stage`, {
        method: 'POST',
        body: JSON.stringify({ path }),
      }),
    onSuccess: (_, { projectId }) => {
      queryClient.invalidateQueries({ queryKey: ['git-status', projectId] });
    },
  });
}

export function useGitUnstage() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectId, path }: { projectId: string; path: string }) =>
      fetchApi<{ success: boolean }>(`${PROJECT_API}/${projectId}/git/unstage`, {
        method: 'POST',
        body: JSON.stringify({ path }),
      }),
    onSuccess: (_, { projectId }) => {
      queryClient.invalidateQueries({ queryKey: ['git-status', projectId] });
    },
  });
}

export function useGitCommit() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectId, message }: { projectId: string; message: string }) =>
      fetchApi<{ data: GitActionResult; success: boolean }>(`${PROJECT_API}/${projectId}/git/commit`, {
        method: 'POST',
        body: JSON.stringify({ message }),
      }),
    onSuccess: (_, { projectId }) => {
      queryClient.invalidateQueries({ queryKey: ['git-status', projectId] });
      queryClient.invalidateQueries({ queryKey: ['git-branches', projectId] });
    },
  });
}

export function useGitCheckout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectId, branch }: { projectId: string; branch: string }) =>
      fetchApi<{ data: GitActionResult; success: boolean }>(`${PROJECT_API}/${projectId}/git/checkout`, {
        method: 'POST',
        body: JSON.stringify({ branch }),
      }),
    onSuccess: (_, { projectId }) => {
      queryClient.invalidateQueries({ queryKey: ['git-status', projectId] });
      queryClient.invalidateQueries({ queryKey: ['git-branches', projectId] });
    },
  });
}

export function useGitCreateBranch() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectId, name, checkout }: { projectId: string; name: string; checkout: boolean }) =>
      fetchApi<{ data: GitActionResult; success: boolean }>(`${PROJECT_API}/${projectId}/git/branch/create`, {
        method: 'POST',
        body: JSON.stringify({ name, checkout }),
      }),
    onSuccess: (_, { projectId }) => {
      queryClient.invalidateQueries({ queryKey: ['git-status', projectId] });
      queryClient.invalidateQueries({ queryKey: ['git-branches', projectId] });
    },
  });
}

export function useGitPull() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectId, remote, branch }: { projectId: string; remote: string; branch: string }) =>
      fetchApi<{ data: GitActionResult; success: boolean }>(`${PROJECT_API}/${projectId}/git/pull`, {
        method: 'POST',
        body: JSON.stringify({ remote, branch }),
      }),
    onSuccess: (_, { projectId }) => {
      queryClient.invalidateQueries({ queryKey: ['git-status', projectId] });
    },
  });
}

export function useGitPush() {
  return useMutation({
    mutationFn: ({ projectId, remote, branch }: { projectId: string; remote: string; branch: string }) =>
      fetchApi<{ data: GitActionResult; success: boolean }>(`${PROJECT_API}/${projectId}/git/push`, {
        method: 'POST',
        body: JSON.stringify({ remote, branch }),
      }),
  });
}

// ─── CLI Profiles & Session Creation ─────────────────────────────────────────

export function useCLIProfiles() {
  return useQuery({
    queryKey: ['cli-profiles'],
    queryFn: () => fetchApi<{ data: CLIProfileGroup[]; success: boolean }>('/api/cli/profiles'),
  });
}

export function useCreateCLISession() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      cli_type,
      profile,
      command,
      cols,
      rows,
    }: {
      cli_type?: string;
      profile?: string;
      command?: string;
      cols?: number;
      rows?: number;
    }) =>
      fetchApi<{ data: CLISessionCreateResult; success: boolean }>('/cli/sessions', {
        method: 'POST',
        body: JSON.stringify({ cli_type, profile, command, cols, rows }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sessions'] });
    },
  });
}

export function useCLISessionSnapshot(sessionId: string) {
  return useQuery({
    queryKey: ['cli-snapshot', sessionId],
    queryFn: () => fetchCLI<CLISnapshot>(`/cli/sessions/${sessionId}/snapshot`),
    enabled: !!sessionId,
  });
}

// Plain async helpers for CLI session runtime actions (used outside React Query)
export async function pollCLISession(sessionId: string, offset: number): Promise<CLIPollResult> {
  return fetchCLI<CLIPollResult>(`/cli/sessions/${sessionId}/poll?offset=${offset}`);
}

export async function getCLISessionSnapshot(sessionId: string, limit = 500): Promise<CLISnapshot> {
  const search = new URLSearchParams();
  if (limit > 0) {
    search.set('limit', String(limit));
  }
  return fetchCLI<CLISnapshot>(`/cli/sessions/${sessionId}/snapshot?${search.toString()}`);
}

export async function sendCLISessionInput(
  sessionId: string,
  payload: { text: string; append_newline?: boolean }
): Promise<void> {
  await fetchCLI(`/cli/sessions/${sessionId}/input`, {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export async function sendCLISessionKeys(sessionId: string, b64: string): Promise<void> {
  await fetchCLI(`/cli/sessions/${sessionId}/keys`, {
    method: 'POST',
    body: JSON.stringify({ b64 }),
  });
}

export async function resizeCLISession(sessionId: string, cols: number, rows: number): Promise<void> {
  await fetchCLI(`/cli/sessions/${sessionId}/resize`, {
    method: 'POST',
    body: JSON.stringify({ cols, rows }),
  });
}

export async function interruptCLISession(sessionId: string): Promise<void> {
  await fetchCLI(`/cli/sessions/${sessionId}/interrupt`, { method: 'POST' });
}

export async function terminateCLISession(sessionId: string): Promise<void> {
  await fetchCLI(`/cli/sessions/${sessionId}/terminate`, { method: 'POST' });
}

export async function reconnectCLISession(sessionId: string): Promise<CLIReconnectResult> {
  return fetchCLI<CLIReconnectResult>(`/cli/sessions/${sessionId}/reconnect`, {
    method: 'POST',
  });
}

export async function destroyCLISession(sessionId: string): Promise<void> {
  await fetchCLI(`/cli/sessions/${sessionId}/destroy`, {
    method: 'POST',
  });
}
