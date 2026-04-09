import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Alert, Button, Drawer, Input, Popconfirm, Space, Switch, Tag, Typography, message } from 'antd';
import {
  AimOutlined,
  ClearOutlined,
  DeleteOutlined,
  EditOutlined,
  ReloadOutlined,
  StopOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import type { CLISession } from '@/types';
import {
  destroyCLISession,
  interruptCLISession,
  reconnectCLISession,
  sendCLISessionInput,
  terminateCLISession,
} from '@/hooks';
import {
  loadLegacyTerminalController,
  type LegacyCLITerminalController,
  type LegacyTerminalStateChange,
} from './terminalUtils';

const { Text } = Typography;
const { TextArea } = Input;

interface RuntimeViewState {
  state: string;
  disconnected: boolean;
  error: string;
  lastError: string;
  exitCode?: number;
  done: boolean;
  mode: string;
  sideErrors: Array<{
    id: string;
    code: string;
    message: string;
    timestamp: string;
  }>;
}

interface CLITerminalWorkbenchProps {
  session: CLISession;
  toolbarContainer?: HTMLElement | null;
  onRefresh?: () => Promise<unknown> | void;
  onDeleted?: (sessionId: string) => void;
}

function isDisconnectedError(error: unknown) {
  const apiCode = Number((error as { apiCode?: number } | undefined)?.apiCode ?? 0);
  const text = String((error as { message?: string } | undefined)?.message ?? '').toLowerCase();
  return apiCode === 409 || apiCode === 410 || text.includes('session disconnected') || text.includes('会话实例已断开');
}

function getRuntimeTone(state: RuntimeViewState) {
  if (state.disconnected) {
    return { color: 'default', label: '会话已断开' };
  }
  if (state.state === 'running') {
    return { color: 'processing', label: '实时终端' };
  }
  if (state.error) {
    return { color: 'error', label: '终端异常' };
  }
  if (state.done || state.state === 'exited' || state.state === 'terminated') {
    return { color: 'default', label: '进程已结束' };
  }
  return { color: 'default', label: '终端空闲' };
}

function buildRuntimeMode(event?: LegacyTerminalStateChange, terminalAvailable = false) {
  const base = terminalAvailable ? 'shared-core+xterm' : 'shared-core+fallback';
  if (!event) {
    return `${base}+loading`;
  }
  if (event.disconnected) {
    return `${base}+disconnected`;
  }
  if (event.state === 'error') {
    return `${base}+error`;
  }
  if (event.done) {
    return `${base}+done`;
  }
  return `${base}+stream`;
}

function buildRuntimeIssue(runtime: RuntimeViewState) {
  const parts: string[] = [];
  if (Number.isFinite(runtime.exitCode)) {
    parts.push(`exit code ${runtime.exitCode}`);
  }
  if (runtime.lastError) {
    parts.push(runtime.lastError);
  }
  if (runtime.error && runtime.error !== runtime.lastError) {
    parts.push(runtime.error);
  }
  return parts.join(' | ').trim();
}

export function CLITerminalWorkbench({
  session,
  toolbarContainer,
  onRefresh,
  onDeleted,
}: CLITerminalWorkbenchProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const controllerRef = useRef<LegacyCLITerminalController | null>(null);

  const [composer, setComposer] = useState('');
  const [appendNewline, setAppendNewline] = useState(true);
  const [sending, setSending] = useState(false);
  const [interrupting, setInterrupting] = useState(false);
  const [reconnecting, setReconnecting] = useState(false);
  const [terminating, setTerminating] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [composerOpen, setComposerOpen] = useState(false);
  const [terminalEpoch, setTerminalEpoch] = useState(0);
  const [terminalAvailable, setTerminalAvailable] = useState(false);
  const [runtime, setRuntime] = useState<RuntimeViewState>({
    state: session.sessionState,
    disconnected: false,
    error: '',
    lastError: session.lastError || '',
    exitCode: session.exitCode,
    done: false,
    mode: 'shared-core+loading',
    sideErrors: [],
  });

  useEffect(() => {
    let disposed = false;

    setTerminalAvailable(false);
    setRuntime({
      state: session.sessionState,
      disconnected: false,
      error: '',
      lastError: session.lastError || '',
      exitCode: session.exitCode,
      done: false,
      mode: 'shared-core+loading',
      sideErrors: [],
    });

    const mountController = async () => {
      const ControllerCtor = await loadLegacyTerminalController();
      if (disposed || !hostRef.current || !ControllerCtor) {
        setTerminalAvailable(false);
        setRuntime(prev => ({
          ...prev,
          mode: buildRuntimeMode(undefined, false),
          error: ControllerCtor ? prev.error : '共享终端控制器加载失败',
        }));
        return;
      }

      const handleStateChange = (event: LegacyTerminalStateChange) => {
        if (disposed) {
          return;
        }
        setTerminalAvailable(true);
        setRuntime(prev => ({
          state: event.state || prev.state,
          disconnected: Boolean(event.disconnected),
          error: event.error || '',
          lastError: typeof event.lastError === 'string' ? event.lastError : prev.lastError,
          exitCode: Number.isFinite(event.exitCode) ? event.exitCode : (event.state === 'running' && !event.done ? undefined : prev.exitCode),
          done: Boolean(event.done),
          mode: buildRuntimeMode(event, true),
          sideErrors: Array.isArray(event.sideErrors) ? event.sideErrors : prev.sideErrors,
        }));
      };

      const controller = new ControllerCtor({
        host: hostRef.current,
        sessionID: session.id,
        cliType: session.cliType,
        onStateChange: handleStateChange,
      });
      controllerRef.current = controller;
      setTerminalAvailable(true);
      controller.activate();
      setRuntime(prev => ({
        ...prev,
        mode: buildRuntimeMode(undefined, true),
      }));
    };

    void mountController();

    return () => {
      disposed = true;
      controllerRef.current?.dispose();
      controllerRef.current = null;
    };
  }, [session.id, session.cliType, session.sessionState, terminalEpoch]);

  const runtimeTone = getRuntimeTone(runtime);
  const runtimeIssue = buildRuntimeIssue(runtime);
  const canWrite = !runtime.disconnected && runtime.state === 'running' && !runtime.done;
  const toolbarOutside = toolbarContainer !== undefined;
  const toolbarButtonSize = toolbarOutside ? 'small' : 'middle';

  const handleSend = async () => {
    const text = composer.trim();
    if (!text || !canWrite) {
      return;
    }
    setSending(true);
    try {
      await sendCLISessionInput(session.id, {
        text,
        append_newline: appendNewline,
      });
      setComposer('');
      controllerRef.current?.focus();
      setComposerOpen(false);
    } catch (error) {
      if (isDisconnectedError(error)) {
        setRuntime(prev => ({
          ...prev,
          disconnected: true,
          error: String((error as Error)?.message || 'session disconnected'),
          mode: buildRuntimeMode({ sessionID: session.id, state: prev.state, error: String((error as Error)?.message || ''), disconnected: true, done: prev.done }, terminalAvailable),
        }));
      }
      message.error(String((error as Error)?.message || '发送失败'));
    } finally {
      setSending(false);
    }
  };

  const handleInterrupt = async () => {
    setInterrupting(true);
    try {
      await interruptCLISession(session.id);
      message.success('已发送 Ctrl+C');
    } catch (error) {
      if (isDisconnectedError(error)) {
        setRuntime(prev => ({
          ...prev,
          disconnected: true,
          error: String((error as Error)?.message || 'session disconnected'),
          mode: buildRuntimeMode({ sessionID: session.id, state: prev.state, error: String((error as Error)?.message || ''), disconnected: true, done: prev.done }, terminalAvailable),
        }));
      }
      message.error(String((error as Error)?.message || '发送 Ctrl+C 失败'));
    } finally {
      setInterrupting(false);
    }
  };

  const handleReconnect = async () => {
    setReconnecting(true);
    try {
      const result = await reconnectCLISession(session.id);
      await Promise.resolve(onRefresh?.());
      setTerminalEpoch(prev => prev + 1);
      message.success(result.reused ? '会话已连接' : '会话已重建');
    } catch (error) {
      message.error(String((error as Error)?.message || '重建失败'));
    } finally {
      setReconnecting(false);
    }
  };

  const handleTerminate = async () => {
    setTerminating(true);
    try {
      await terminateCLISession(session.id);
      await Promise.resolve(onRefresh?.());
      message.success('会话已终止');
    } catch (error) {
      message.error(String((error as Error)?.message || '终止失败'));
    } finally {
      setTerminating(false);
    }
  };

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await destroyCLISession(session.id);
      await Promise.resolve(onRefresh?.());
      message.success('会话已删除');
      onDeleted?.(session.id);
    } catch (error) {
      message.error(String((error as Error)?.message || '删除失败'));
    } finally {
      setDeleting(false);
    }
  };

  const toolbar = (
    <div className={`cli-workbench-toolbar${toolbarOutside ? ' is-outside' : ''}`}>
      <Space size={toolbarOutside ? 6 : 8} wrap>
        <Tag color={runtimeTone.color}>{runtimeTone.label}</Tag>
        <Tag>{runtime.mode}</Tag>
        {runtime.done && <Tag>输出已结束</Tag>}
      </Space>
      <Space size={toolbarOutside ? 6 : 8} wrap>
        <Button
          size={toolbarButtonSize}
          icon={<AimOutlined />}
          onClick={() => {
            controllerRef.current?.focus();
          }}
        >
          聚焦
        </Button>
        <Button size={toolbarButtonSize} icon={<ClearOutlined />} onClick={() => controllerRef.current?.clear()}>
          清屏
        </Button>
        {canWrite && (
          <Button size={toolbarButtonSize} icon={<EditOutlined />} onClick={() => setComposerOpen(true)}>
            对话
          </Button>
        )}
        {canWrite && (
          <Button size={toolbarButtonSize} icon={<ThunderboltOutlined />} loading={interrupting} onClick={handleInterrupt}>
            Ctrl+C
          </Button>
        )}
        {runtime.disconnected && (
          <Button size={toolbarButtonSize} icon={<ReloadOutlined />} loading={reconnecting} onClick={handleReconnect}>
            重建
          </Button>
        )}
        {canWrite && (
          <Popconfirm title="终止当前 CLI 会话？" okText="终止" cancelText="取消" onConfirm={handleTerminate}>
            <Button size={toolbarButtonSize} danger icon={<StopOutlined />} loading={terminating}>
              终止
            </Button>
          </Popconfirm>
        )}
        <Popconfirm
          title="删除当前会话？"
          description="会清理运行态和会话记录。"
          okText="删除"
          cancelText="取消"
          onConfirm={handleDelete}
        >
          <Button size={toolbarButtonSize} danger icon={<DeleteOutlined />} loading={deleting}>
            删除
          </Button>
        </Popconfirm>
      </Space>
    </div>
  );

  return (
    <>
      {toolbarOutside ? (toolbarContainer ? createPortal(toolbar, toolbarContainer) : null) : toolbar}

      <div className="cli-workbench">
        {runtime.error && (
          <Alert
            className="cli-workbench-alert"
            showIcon
            type={runtime.disconnected ? 'warning' : 'info'}
            message={runtime.error}
            description={runtimeIssue && runtimeIssue !== runtime.error ? runtimeIssue : undefined}
          />
        )}
        {!runtime.error && runtimeIssue && (
          <Alert
            className="cli-workbench-alert"
            showIcon
            type={runtime.disconnected || runtime.done ? 'warning' : 'info'}
            message={runtimeIssue}
          />
        )}
        {runtime.sideErrors.slice(-3).map(item => (
          <Alert
            key={item.id}
            className="cli-workbench-alert"
            showIcon
            type="warning"
            message={item.code || 'runtime_side_error'}
            description={item.message || item.timestamp}
          />
        ))}

        <div className="cli-terminal-frame">
          <div className="cli-terminal-shell">
            <div ref={hostRef} className="cli-terminal-host legacy-terminal-host is-active" />
          </div>
        </div>

        <Drawer
          title="发送到当前 CLI"
          placement="bottom"
          height={220}
          open={composerOpen}
          onClose={() => setComposerOpen(false)}
          destroyOnClose={false}
        >
          <div className="cli-composer-drawer">
            <div className="cli-composer-header">
              <div>
                <Text strong>对话输入</Text>
                <div className="cli-composer-hint">`Enter` 发送，`Shift + Enter` 换行。默认把空间优先留给 terminal。</div>
              </div>
              <div className="cli-composer-switch">
                <span>自动回车</span>
                <Switch size="small" checked={appendNewline} onChange={setAppendNewline} />
              </div>
            </div>
            <div className="cli-composer-body">
              <TextArea
                value={composer}
                rows={4}
                disabled={!canWrite}
                placeholder={canWrite ? '输入内容后直接发送到当前 CLI 会话' : '当前会话不可写'}
                onChange={event => setComposer(event.target.value)}
                onPressEnter={event => {
                  if (event.shiftKey) {
                    return;
                  }
                  event.preventDefault();
                  void handleSend();
                }}
              />
              <Button type="primary" loading={sending} disabled={!canWrite || !composer.trim()} onClick={() => void handleSend()}>
                发送
              </Button>
            </div>
          </div>
        </Drawer>
      </div>
    </>
  );
}
