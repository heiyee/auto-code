export interface XTermInstance {
  open(host: HTMLElement): void;
  write(data: string | Uint8Array): void;
  clear(): void;
  focus(): void;
  resize(cols: number, rows: number): void;
  dispose(): void;
  onData(listener: (data: string) => void): void;
}

export interface XTermConstructor {
  new (options?: Record<string, unknown>): XTermInstance;
}

export interface LegacyTerminalStateChange {
  sessionID: string;
  state: string;
  error: string;
  lastError?: string;
  exitCode?: number;
  disconnected: boolean;
  done: boolean;
  sideErrors?: Array<{
    id: string;
    code: string;
    message: string;
    timestamp: string;
  }>;
}

export interface LegacyCLITerminalController {
  setSessionID(sessionID: string): void;
  activate(): void;
  deactivate(): void;
  dispose(): void;
  clear(): void;
  focus(): void;
}

export interface LegacyCLITerminalControllerConstructor {
  new (options: {
    host: HTMLElement | null;
    cliType?: string;
    onStateChange?: (event: LegacyTerminalStateChange) => void;
    sessionID?: string;
  }): LegacyCLITerminalController;
}

declare global {
  interface Window {
    Terminal?: XTermConstructor;
    FileManagerCLITerminalController?: LegacyCLITerminalControllerConstructor;
  }
}

const XTERM_SCRIPT_ID = 'auto-code-xterm-script';
const XTERM_STYLE_ID = 'auto-code-xterm-style';
const LEGACY_CLI_SCRIPT_ID = 'auto-code-legacy-cli-terminal-script';
const LEGACY_CLI_ASSET_VERSION = '20260406-cli-binary-replay-1';
const PUBLIC_BASE = import.meta.env.BASE_URL || '/';
const TERMINAL_MIN_COLS = 24;
const TERMINAL_MIN_ROWS = 6;
const TERMINAL_DEFAULT_COLS = 120;
const TERMINAL_DEFAULT_ROWS = 40;
const TERMINAL_SHELL_HORIZONTAL_PADDING = 16;
const TERMINAL_SHELL_VERTICAL_PADDING = 22;
const TERMINAL_FALLBACK_CELL_WIDTH = 8.45;
const TERMINAL_FALLBACK_CELL_HEIGHT = 17;

let xtermLoader: Promise<XTermConstructor | null> | null = null;
let legacyControllerLoader: Promise<LegacyCLITerminalControllerConstructor | null> | null = null;

function publicAssetPath(relativePath: string): string {
  const base = PUBLIC_BASE.endsWith('/') ? PUBLIC_BASE : `${PUBLIC_BASE}/`;
  const clean = String(relativePath || '').replace(/^\/+/, '');
  return `${base}${clean}`;
}

function readLiveTerminalSize() {
  if (typeof document === 'undefined') {
    return null;
  }
  const host = document.querySelector<HTMLElement>('.cli-terminal-host[data-terminal-cols][data-terminal-rows]');
  if (!host) {
    return null;
  }
  const cols = Number(host.dataset.terminalCols || 0);
  const rows = Number(host.dataset.terminalRows || 0);
  if (!Number.isFinite(cols) || cols < TERMINAL_MIN_COLS || !Number.isFinite(rows) || rows < TERMINAL_MIN_ROWS) {
    return null;
  }
  return { cols: Math.floor(cols), rows: Math.floor(rows) };
}

function readTerminalViewportRect() {
  if (typeof document === 'undefined' || typeof window === 'undefined') {
    return null;
  }

  const directHost = document.querySelector<HTMLElement>('.cli-terminal-host');
  if (directHost && directHost.clientWidth > 0 && directHost.clientHeight > 0) {
    return { width: directHost.clientWidth, height: directHost.clientHeight };
  }

  const focusPanel = document.querySelector<HTMLElement>('.cli-session-focus-panel');
  const workbench = document.querySelector<HTMLElement>('.cli-workbench');
  const toolbar = document.querySelector<HTMLElement>('.cli-focus-compact');
  if (focusPanel && focusPanel.clientWidth > 0) {
    const width = Math.max(0, focusPanel.clientWidth - 24);
    const heightBase = workbench?.clientHeight || window.innerHeight;
    const height = Math.max(0, heightBase - (toolbar?.offsetHeight || 112) - 24);
    if (width > 0 && height > 0) {
      return { width, height };
    }
  }

  const fallbackWidth = Math.max(0, window.innerWidth - 420);
  const fallbackHeight = Math.max(0, window.innerHeight - 180);
  if (fallbackWidth <= 0 || fallbackHeight <= 0) {
    return null;
  }
  return { width: fallbackWidth, height: fallbackHeight };
}

export function estimateTerminalCreateSize(): { cols: number; rows: number } {
  const liveSize = readLiveTerminalSize();
  if (liveSize) {
    return liveSize;
  }

  const viewport = readTerminalViewportRect();
  if (!viewport) {
    return { cols: TERMINAL_DEFAULT_COLS, rows: TERMINAL_DEFAULT_ROWS };
  }

  const availableWidth = Math.max(0, viewport.width - TERMINAL_SHELL_HORIZONTAL_PADDING);
  const availableHeight = Math.max(0, viewport.height - TERMINAL_SHELL_VERTICAL_PADDING);
  const cols = Math.max(TERMINAL_MIN_COLS, Math.floor(availableWidth / TERMINAL_FALLBACK_CELL_WIDTH));
  const rows = Math.max(TERMINAL_MIN_ROWS, Math.floor(availableHeight / TERMINAL_FALLBACK_CELL_HEIGHT));
  return { cols, rows };
}

export function stripTerminalSequences(text: string): string {
  let plain = String(text || '');
  if (!plain) {
    return '';
  }

  plain = plain.replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, '');
  plain = plain.replace(/\x1b\][^\x07]*(?:\x07|\x1b\\)/g, '');
  plain = plain.replace(/\x1b[@-_]/g, '');
  plain = plain.replace(/\r\n/g, '\n');
  plain = plain.replace(/\r/g, '\n');
  plain = plain.replace(/[^\x09\x0a\x20-\x7e\u00a0-\uffff]/g, '');
  return plain;
}

export function appendFallbackText(target: HTMLElement | null, text: string): void {
  if (!target) {
    return;
  }
  const next = stripTerminalSequences(text);
  if (!next) {
    return;
  }
  target.textContent = `${target.textContent || ''}${next}`;
  if ((target.textContent || '').length > 300_000) {
    target.textContent = (target.textContent || '').slice(-220_000);
  }
  target.scrollTop = target.scrollHeight;
}

export function clearFallbackText(target: HTMLElement | null): void {
  if (!target) {
    return;
  }
  target.textContent = '';
}

export function encodeBase64Bytes(text: string, encoder: TextEncoder): string {
  const bytes = encoder.encode(text);
  let binary = '';
  for (let index = 0; index < bytes.length; index += 1) {
    binary += String.fromCharCode(bytes[index]);
  }
  return window.btoa(binary);
}

export function decodeBase64Bytes(rawB64: string): Uint8Array {
  const binary = window.atob(String(rawB64 || ''));
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

function ensureXTermStyle() {
  if (typeof document === 'undefined' || document.getElementById(XTERM_STYLE_ID)) {
    return;
  }

  const link = document.createElement('link');
  link.id = XTERM_STYLE_ID;
  link.rel = 'stylesheet';
  link.href = publicAssetPath('static/xterm.min.css');
  document.head.appendChild(link);
}

function loadScript(scriptId: string, src: string, onResolve: () => unknown): Promise<unknown> {
  return new Promise((resolve) => {
    const existing = document.getElementById(scriptId) as HTMLScriptElement | null;
    if (existing) {
      existing.addEventListener('load', () => resolve(onResolve()), { once: true });
      existing.addEventListener('error', () => resolve(null), { once: true });
      return;
    }

    const script = document.createElement('script');
    script.id = scriptId;
    script.src = src;
    script.async = true;
    script.onload = () => resolve(onResolve());
    script.onerror = () => resolve(null);
    document.body.appendChild(script);
  });
}

export async function loadXTermAssets(): Promise<XTermConstructor | null> {
  if (typeof window === 'undefined' || typeof document === 'undefined') {
    return null;
  }

  ensureXTermStyle();
  if (window.Terminal) {
    return window.Terminal;
  }
  if (xtermLoader) {
    return xtermLoader;
  }

  xtermLoader = loadScript(
    XTERM_SCRIPT_ID,
    publicAssetPath('static/xterm.min.js'),
    () => window.Terminal || null
  ) as Promise<XTermConstructor | null>;

  return xtermLoader;
}

export async function loadLegacyTerminalController(): Promise<LegacyCLITerminalControllerConstructor | null> {
  if (typeof window === 'undefined' || typeof document === 'undefined') {
    return null;
  }
  await loadXTermAssets();
  if (window.FileManagerCLITerminalController) {
    return window.FileManagerCLITerminalController;
  }
  if (legacyControllerLoader) {
    return legacyControllerLoader;
  }

  legacyControllerLoader = loadScript(
    LEGACY_CLI_SCRIPT_ID,
    `${publicAssetPath('static/file_manager_cli_terminal.js')}?v=${LEGACY_CLI_ASSET_VERSION}`,
    () => window.FileManagerCLITerminalController || null
  ) as Promise<LegacyCLITerminalControllerConstructor | null>;

  return legacyControllerLoader;
}
