// Project types
export interface Project {
  id: string;
  name: string;
  repository: string;
  branch: string;
  workDir: string;
  automationPaused?: boolean;
  description?: string;
  createdAt: string;
  updatedAt: string;
}

export interface SolutionTemplate {
  id: string;
  name: string;
  summary: string;
  businessGoal: string;
  suggestedStack: string;
  requirementCount: number;
}

export interface SolutionBootstrapInput {
  appName: string;
  templateId?: string;
  repository?: string;
  branch?: string;
  workDir?: string;
  cliType?: string;
  autoClearSession?: boolean;
}

export interface SolutionBootstrapData {
  project: Project;
  template: SolutionTemplate;
  requirements: Requirement[];
  designBrief: string;
  autoProgress: boolean;
}

// Requirement types
export type RequirementStatus = 'planning' | 'running' | 'paused' | 'failed' | 'done';
export type ExecutionMode = 'manual' | 'auto';
export type RequirementNoResponseAction = 'none' | 'resend_requirement' | 'close_and_resend_requirement';
export type ExecutionHealthState =
  | 'running'
  | 'awaiting_confirmation'
  | 'stalled_no_output'
  | 'stalled_network'
  | 'stalled_quota'
  | 'stalled_interrupted'
  | 'completed';

export interface Requirement {
  id: string;
  projectId: string;
  sortOrder?: number;
  title: string;
  description: string;
  status: RequirementStatus;
  executionMode: ExecutionMode;
  cliType: string;
  autoClearSession: boolean;
  noResponseTimeoutMinutes?: number;
  noResponseErrorAction?: RequirementNoResponseAction;
  noResponseIdleAction?: RequirementNoResponseAction;
  requiresDesignReview?: boolean;
  requiresCodeReview?: boolean;
  requiresAcceptanceReview?: boolean;
  requiresReleaseApproval?: boolean;
  createdAt: string;
  startedAt?: string;
  endedAt?: string;
  promptSentAt?: string;
  promptReplayedAt?: string;
  autoRetryAttempts?: number;
  retryBudget?: number;
  retryBudgetExhausted?: boolean;
  executionState?: ExecutionHealthState;
  executionReason?: string;
  lastOutputAt?: string;
  lastWatchdogEvent?: {
    triggerKind: 'cli_error' | 'cli_idle';
    triggerReason: string;
    action: RequirementNoResponseAction;
    status: 'pending' | 'succeeded' | 'failed' | 'skipped';
    detail?: string;
    createdAt: string;
    finishedAt?: string;
  };
  projectName?: string;
  // Extended fields
  priority?: 'low' | 'medium' | 'high' | 'critical';
  sourceType?: 'manual' | 'github' | 'api' | 'webhook';
  riskLevel?: 'low' | 'medium' | 'high' | 'critical';
}

export interface RequirementMutationInput {
  projectId: string;
  title: string;
  description: string;
  executionMode: ExecutionMode;
  cliType: string;
  autoClearSession: boolean;
  noResponseTimeoutMinutes?: number;
  noResponseErrorAction?: RequirementNoResponseAction;
  noResponseIdleAction?: RequirementNoResponseAction;
  requiresDesignReview?: boolean;
  requiresCodeReview?: boolean;
  requiresAcceptanceReview?: boolean;
  requiresReleaseApproval?: boolean;
}

export type RequirementUpdateInput = Partial<RequirementMutationInput> & {
  status?: RequirementStatus;
  sortOrder?: number;
};

// CLI Session types
export interface CLISession {
  id: string;
  cliType: string;
  profile: string;
  profileName?: string;
  agentId: string;
  projectId: string;
  requirementId: string;
  workDir: string;
  sessionState: string;
  processPID: number;
  createdAt: string;
  lastActiveAt: string;
  exitCode?: number;
  lastError?: string;
  executionState?: ExecutionHealthState;
  executionReason?: string;
  lastOutputAt?: string;
  projectName?: string;
  requirementTitle?: string;
}

// Workflow Run types
export type WorkflowStatus = 'pending' | 'running' | 'paused' | 'completed' | 'failed' | 'canceled';
export type TriggerMode = 'manual' | 'auto' | 'scheduled' | 'webhook';

export interface WorkflowRun {
  id: string;
  projectId: string;
  requirementId: string;
  status: WorkflowStatus;
  currentStage: string;
  triggerMode: TriggerMode;
  riskLevel: 'low' | 'medium' | 'high' | 'critical';
  startedAt: string;
  endedAt?: string;
  lastError?: string;
  resumeFromStage?: string;
  projectName?: string;
  requirementTitle?: string;
  progress: number;
}

// Stage Run types
export type StageStatus = 'pending' | 'running' | 'stalled' | 'verifying' | 'awaiting_input' | 'awaiting_review' | 'blocked' | 'partial' | 'completed' | 'failed' | 'interrupted' | 'canceled';

export interface StageRun {
  id: string;
  workflowRunId: string;
  stageName: string;
  displayName: string;
  status: StageStatus;
  attempt: number;
  ownerType: 'system' | 'human';
  agentSessionId?: string;
  startedAt?: string;
  endedAt?: string;
  resultSummary?: string;
  artifacts?: string[];
  ruleReportId?: string;
  order: number;
}

// Artifact types
export type ArtifactType = 'requirement_brief' | 'solution_design' | 'task_breakdown' | 'implementation_plan' | 'test_plan' | 'review_record' | 'rule_report' | 'release_note' | 'change_summary' | 'review_package';
export type ArtifactStatus = 'draft' | 'generated' | 'under_review' | 'approved' | 'rejected' | 'archived';

export interface Artifact {
  id: string;
  projectId: string;
  requirementId: string;
  workflowRunId: string;
  stageRunId?: string;
  artifactType: ArtifactType;
  title: string;
  path: string;
  version: number;
  status: ArtifactStatus;
  source: 'system' | 'agent' | 'human';
  contentHash?: string;
  createdAt: string;
  updatedAt: string;
}

// Review Gate types
export type GateType = 'design_review' | 'code_review' | 'acceptance_review' | 'release_approval';
export type GateStatus = 'pending' | 'approved' | 'rejected' | 'waived';
export type GateDecision = 'pass' | 'reject' | 'return_for_revision';

export interface ReviewGate {
  id: string;
  workflowRunId: string;
  stageName: string;
  gateType: GateType;
  status: GateStatus;
  reviewer?: string;
  decision?: GateDecision;
  comment?: string;
  createdAt: string;
  resolvedAt?: string;
  // Additional UI fields
  title?: string;
  description?: string;
  blockingItems?: string[];
}

// Task Item types
export type TaskStatus = 'planned' | 'running' | 'done' | 'waived' | 'blocked' | 'failed';
export type TaskScope = 'file' | 'module' | 'test' | 'verification';

export interface TaskItem {
  id: string;
  workflowRunId: string;
  stageRunId?: string;
  parentTaskId?: string;
  title: string;
  description: string;
  scope: TaskScope;
  required: boolean;
  status: TaskStatus;
  ownerSessionId?: string;
  dependsOn?: string[];
  evidenceArtifactId?: string;
  createdAt: string;
  updatedAt: string;
}

// Decision Request types
export type RequestType = 'clarification' | 'option_selection' | 'risk_confirmation' | 'scope_confirmation';
export type DecisionStatus = 'pending' | 'resolved' | 'expired';

export interface DecisionRequest {
  id: string;
  workflowRunId: string;
  stageRunId?: string;
  requestType: RequestType;
  title: string;
  question: string;
  context?: string;
  options?: { value: string; label: string }[];
  recommendedOption?: string;
  blocking: boolean;
  status: DecisionStatus;
  decision?: string;
  decider?: string;
  createdAt: string;
  resolvedAt?: string;
}

// Code Snapshot types
export type SnapshotType = 'workflow_start' | 'stage_start' | 'stage_end' | 'pre_review' | 'pre_commit' | 'post_commit';

export interface CodeSnapshot {
  id: string;
  projectId: string;
  workflowRunId: string;
  stageRunId?: string;
  snapshotType: SnapshotType;
  gitCommit?: string;
  gitBranch: string;
  workspaceRevision?: string;
  fileCount: number;
  createdAt: string;
}

// Change Set types
export type ChangeScope = 'stage' | 'workflow' | 'review' | 'delivery';

export interface FileChange {
  path: string;
  status: 'A' | 'M' | 'D' | 'R';
  additions: number;
  deletions: number;
  oldPath?: string;
}

export interface ChangeSet {
  id: string;
  projectId: string;
  workflowRunId: string;
  stageRunId?: string;
  baseSnapshotId: string;
  targetSnapshotId: string;
  changeScope: ChangeScope;
  summary: string;
  fileStats: {
    added: number;
    modified: number;
    deleted: number;
    renamed: number;
    totalAdditions: number;
    totalDeletions: number;
  };
  files: FileChange[];
  patchArtifactId?: string;
  createdAt: string;
}

// Rule Pack types
export interface RulePack {
  id: string;
  name: string;
  scope: 'design' | 'code' | 'system' | 'test' | 'delivery';
  version: string;
  enabled: boolean;
  blocking: boolean;
  sourceType: 'document' | 'config' | 'command' | 'custom';
  sourceRef: string;
  description?: string;
}

// Rule Execution Report types
export type ReportStatus = 'pass' | 'warning' | 'fail' | 'waived';

export interface RuleExecutionReport {
  id: string;
  workflowRunId: string;
  stageRunId?: string;
  rulePackId: string;
  rulePackName: string;
  status: ReportStatus;
  score?: number;
  blockingViolations: number;
  nonBlockingViolations: number;
  outputPath?: string;
  createdAt: string;
}

// Dashboard statistics
export interface DashboardStats {
  totalProjects: number;
  totalRequirements: number;
  runningTasks: number;
  completedTasks: number;
  activeWorkflows: number;
  pendingReviews: number;
  pendingDecisions: number;
}

// Activity types
export interface Activity {
  id: string;
  type: 'project' | 'requirement' | 'session' | 'workflow' | 'review' | 'artifact';
  action: 'created' | 'updated' | 'completed' | 'started' | 'approved' | 'rejected';
  title: string;
  description: string;
  timestamp: string;
}

// API Response types
export interface ApiResponse<T> {
  data: T;
  success: boolean;
  message?: string;
}

export interface PaginatedResponse<T> {
  data: T[];
  total: number;
  page: number;
  pageSize: number;
}

// File Manager types
export interface FileNode {
  path: string;
  name: string;
  type: 'file' | 'directory';
  size: number;
  modTime: string;
  gitStatus?: string;
  hasChildren?: boolean;
  children?: FileNode[];
}

export interface FileContent {
  path: string;
  content: string;
  revision: string;
}

// Git types
export interface GitFileChange {
  path: string;
  status: string;
}

export interface GitStatus {
  currentBranch: string;
  changes: GitFileChange[];
  staged: GitFileChange[];
  untracked: string[];
}

export interface GitBranchInfo {
  name: string;
  current: boolean;
}

export interface GitBranchList {
  currentBranch: string;
  branches: GitBranchInfo[];
}

export interface GitLocalRepoInspection {
  operation: string;
  repository: string;
  branches: string[];
  currentBranch: string;
  remoteURL: string;
}

export interface GitFileDiff {
  path: string;
  status: string;
  diff: string;
}

export interface GitActionResult {
  currentBranch?: string;
  commitHash?: string;
  tag?: string;
  message?: string;
  output?: string;
}

// CLI Profile types
export interface CLIProfileItem {
  id: string;
  name: string;
  description: string;
}

export interface CLIProfileGroup {
  cli_type: string;
  default_profile?: string;
  profiles: CLIProfileItem[];
}

export interface CLISessionCreateResult {
  session_id: string;
  agentid: string;
  cli_type: string;
  profile: string;
}

export interface CLIPollResult {
  session_id: string;
  agentid: string;
  state: string;
  output: string;
  raw_b64?: string;
  side_errors?: CLISideError[];
  next_offset: number;
  rewind: boolean;
  done: boolean;
  more: boolean;
  exit_code?: number;
  last_error?: string;
}

export interface CLISideError {
  id: string;
  code: string;
  message: string;
  timestamp: string;
}

export interface CLISnapshotEntry {
  seq: number;
  timestamp: string;
  raw_b64: string;
}

export interface CLISnapshot {
  session_id: string;
  agentid: string;
  entries: CLISnapshotEntry[];
  last_seq: number;
  current_output_b64?: string;
  poll_resume_offset?: number;
  side_errors?: CLISideError[];
  session_state?: string;
  exit_code?: number;
  last_error?: string;
  connected: boolean;
  reconnectable: boolean;
  disconnect_reason?: string;
}

export interface CLIReconnectResult {
  session_id: string;
  agentid: string;
  reused: boolean;
}

// Stage definitions
export const STAGE_DEFINITIONS = [
  { name: 'requirement_intake', displayName: '需求接入', icon: '📥' },
  { name: 'requirement_analysis', displayName: '需求分析', icon: '🔍' },
  { name: 'solution_design', displayName: '方案设计', icon: '📐' },
  { name: 'design_review', displayName: '设计审核', icon: '👁️' },
  { name: 'task_planning', displayName: '任务拆解', icon: '📋' },
  { name: 'implementation', displayName: '编码实施', icon: '💻' },
  { name: 'code_diff', displayName: '变更归集', icon: '📊' },
  { name: 'code_standards', displayName: '代码规范', icon: '📏' },
  { name: 'system_standards', displayName: '系统规范', icon: '🏗️' },
  { name: 'testing', displayName: '测试验证', icon: '🧪' },
  { name: 'acceptance_review', displayName: '人工验收', icon: '✅' },
  { name: 'git_delivery', displayName: 'Git 交付', icon: '📤' },
  { name: 'release_gate', displayName: '发布门', icon: '🚪' },
  { name: 'release', displayName: '发布归档', icon: '🚀' },
] as const;

export type StageName = typeof STAGE_DEFINITIONS[number]['name'];
