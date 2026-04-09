import type { ExecutionHealthState } from '@/types';

const executionStateConfig: Record<ExecutionHealthState, { color: string; text: string }> = {
  running: { color: 'processing', text: '执行中' },
  awaiting_confirmation: { color: 'gold', text: '待确认' },
  stalled_no_output: { color: 'warning', text: '无输出' },
  stalled_network: { color: 'orange', text: '网络异常' },
  stalled_quota: { color: 'magenta', text: '额度受限' },
  stalled_interrupted: { color: 'red', text: '已中断' },
  completed: { color: 'success', text: '已完成' },
};

const executionReasonConfig: Record<string, string> = {
  no_output_timeout: '无输出超时',
  quota_or_rate_limit: '额度不足或被限流',
  network_timeout: '网络超时',
  interrupted: '执行中断',
  session_disconnected: '会话断开',
  watchdog_idle_timeout: '长时间无输出',
  watchdog_cli_error: 'CLI 异常',
  'confirmation:codex_trust_directory': '目录信任确认',
  'confirmation:enter_to_continue': '等待回车确认',
  'confirmation:yes_no_confirmation': '等待是/否确认',
};

export function getExecutionHealthMeta(state?: string, reason?: string) {
  if (!state) {
    return null;
  }

  const stateMeta = executionStateConfig[state as ExecutionHealthState];
  const normalizedReason = (reason || '').trim();

  return {
    color: stateMeta?.color || 'default',
    text: stateMeta?.text || state,
    reasonText: executionReasonConfig[normalizedReason] || normalizedReason || '',
  };
}
