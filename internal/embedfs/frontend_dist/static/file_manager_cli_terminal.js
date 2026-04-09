(() => {
  "use strict";

  function normalizeCLIType(raw) {
    return String(raw || "").trim().toLowerCase();
  }

  class FileManagerCLITerminalController {
    constructor(options = {}) {
      this.host = options.host || null;
      this.toast = options.toast || null;
      this.onStateChange = typeof options.onStateChange === "function"
        ? options.onStateChange
        : () => {};
      this.cliType = normalizeCLIType(options.cliType);
      this.collapseAnimatedFrames = this.cliType === "cursor-agent" || this.cliType === "cursor";

      this.sessionID = String(options.sessionID || "").trim();
      this.pollOffset = 0;
      this.lastSeq = 0;

      this.disposed = false;
      this.active = false;
      this.polling = false;
      this.snapshotLoading = false;
      this.usePollingFallback = false;
      this.streamRetryCount = 0;
      this.transportVersion = 0;

      this.pollTimer = 0;
      this.keyTimer = 0;
      this.resizeTimer = 0;
      this.reconnectTimer = 0;
      this.terminalFitFrame = 0;
      this.terminalFitRetryTimer = 0;
      this.terminalFitRetryCount = 0;

      this.keyBuffer = "";
      this.lastSize = { cols: 0, rows: 0 };
      this.sideErrors = [];
      this.sideErrorIDs = new Set();
      this.runtimeState = "running";
      this.runtimeDisconnected = false;
      this.runtimeDone = false;
      this.runtimeError = "";

      this.textEncoder = new window.TextEncoder();
      this.textDecoder = new window.TextDecoder("utf-8", { fatal: false });
      this.cursorOnboardingSimplified = false;

      this.terminal = null;
      this.terminalRoot = null;
      this.fallbackOutput = null;
      this.resizeObserver = null;
      this.eventSource = null;

      this.boundResizeHandler = () => {
        this.requestTerminalFit(true);
        this.scheduleResize();
      };

      this.mount();
    }

    mount() {
      if (!this.host) {
        return;
      }
      this.host.innerHTML = "";

      const shell = document.createElement("div");
      shell.className = "vscode-cli-terminal-shell";

      const live = document.createElement("div");
      live.className = "vscode-cli-terminal-live";
      live.setAttribute("aria-label", "CLI terminal");
      shell.appendChild(live);
      this.host.appendChild(shell);

      this.terminalRoot = live;

      if (typeof window.Terminal === "function") {
        this.initXTerm();
      } else {
        this.initFallback();
      }
      this.attachResizeSync();
    }

    initXTerm() {
      if (!this.terminalRoot || typeof window.Terminal !== "function") {
        return;
      }
      this.terminal = new window.Terminal({
        cursorBlink: true,
        convertEol: true,
        fontFamily: "\"SFMono-Regular\", Menlo, Consolas, monospace",
        fontSize: 13,
        scrollback: 8000,
        theme: {
          background: "#1f1f1f",
          foreground: "#d4d4d4",
          cursor: "#9cdcfe",
        },
      });
      this.terminal.open(this.terminalRoot);
      this.terminal.onData((data) => {
        this.queueKeys(data);
      });
      if (typeof this.terminal.onResize === "function") {
        this.terminal.onResize(() => {
          this.scheduleResize();
        });
      }
      this.terminalRoot.addEventListener("click", () => {
        this.focus();
      });
      window.setTimeout(() => {
        this.requestTerminalFit(true);
        this.scheduleResize();
      }, 0);
    }

    initFallback() {
      if (!this.terminalRoot) {
        return;
      }
      this.fallbackOutput = document.createElement("pre");
      this.fallbackOutput.className = "vscode-cli-terminal-fallback";
      this.fallbackOutput.textContent = "xterm is unavailable in current environment.\n";
      this.terminalRoot.appendChild(this.fallbackOutput);
    }

    setSessionID(sessionID) {
      this.stopTransport();
      this.sessionID = String(sessionID || "").trim();
      this.pollOffset = 0;
      this.lastSeq = 0;
      this.keyBuffer = "";
      this.cursorOnboardingSimplified = false;
      this.usePollingFallback = false;
      this.streamRetryCount = 0;
      this.resetDecoder();
      this.sideErrors = [];
      this.sideErrorIDs = new Set();
      this.clear();
      this.emitState("running", "", { disconnected: false, done: false });
      if (this.active) {
        this.requestTerminalFit(true);
        this.scheduleResize();
        this.startTransport(140);
      }
    }

    activate() {
      if (this.disposed || !this.sessionID) {
        return;
      }
      this.active = true;
      this.focus();
      this.requestTerminalFit(true);
      this.scheduleResize();
      this.startTransport(140);
    }

    deactivate() {
      this.active = false;
      this.stopTransport();
      this.clearKeyTimer();
      this.clearResizeTimer();
    }

    dispose() {
      if (this.disposed) {
        return;
      }
      this.disposed = true;
      this.deactivate();
      this.detachResizeSync();
      this.clearTerminalFitState();
      if (this.terminal && typeof this.terminal.dispose === "function") {
        this.terminal.dispose();
      }
      this.terminal = null;
      this.terminalRoot = null;
      this.fallbackOutput = null;
    }

    clear() {
      if (this.terminal) {
        this.terminal.clear();
      }
      if (this.fallbackOutput) {
        this.fallbackOutput.textContent = "";
      }
    }

    focus() {
      if (!this.active) {
        return;
      }
      if (this.terminal && typeof this.terminal.focus === "function") {
        this.terminal.focus();
      }
    }

    stopTransport() {
      this.transportVersion += 1;
      this.snapshotLoading = false;
      this.polling = false;
      this.clearPollTimer();
      this.clearReconnectTimer();
      this.closeEventStream();
    }

    isTransportCurrent(version) {
      return this.active
        && !this.disposed
        && !!this.sessionID
        && this.transportVersion === version;
    }

    startTransport(delay) {
      if (!this.active || this.disposed || !this.sessionID) {
        return;
      }
      this.stopTransport();
      const version = this.transportVersion;
      const kickoff = () => {
        if (!this.isTransportCurrent(version)) {
          return;
        }
        void this.bootstrapSnapshotAndTransport(version, this.sessionID);
      };
      if (delay > 0) {
        this.reconnectTimer = window.setTimeout(kickoff, Math.max(0, Number(delay) || 0));
        return;
      }
      kickoff();
    }

    async bootstrapSnapshotAndTransport(version, expectedSessionID) {
      if (!this.isTransportCurrent(version) || this.sessionID !== expectedSessionID) {
        return;
      }
      this.snapshotLoading = true;
      try {
        const payload = await this.request("GET", `/cli/sessions/${encodeURIComponent(expectedSessionID)}/snapshot?limit=500`);
        if (!this.isTransportCurrent(version) || this.sessionID !== expectedSessionID) {
          return;
        }

        this.applySnapshot(payload || {});
        this.snapshotLoading = false;

        if (payload && payload.connected === false) {
          const reason = String(payload.disconnect_reason || "session disconnected").trim();
          this.emitState("disconnected", reason, { disconnected: true, done: false });
          this.deactivate();
          return;
        }

        if (this.usePollingFallback || typeof window.EventSource !== "function") {
          this.usePollingFallback = true;
          this.schedulePoll(120);
          return;
        }

        this.openEventStream(version, expectedSessionID);
      } catch (error) {
        if (!this.isTransportCurrent(version) || this.sessionID !== expectedSessionID) {
          return;
        }
        this.snapshotLoading = false;
        if (this.isDisconnectedError(error)) {
          this.emitState("disconnected", String(error.message || "session disconnected"), { disconnected: true, done: false });
          this.deactivate();
          return;
        }
        this.enablePollingFallback(200);
      }
    }

    applySnapshot(data) {
      this.clear();
      this.resetDecoder();
      this.cursorOnboardingSimplified = false;
      this.pollOffset = 0;
      this.lastSeq = 0;
      this.sideErrors = [];
      this.sideErrorIDs = new Set();

      const connected = data && data.connected !== false;
      const hasEntries = Array.isArray(data.entries) && data.entries.length > 0;
      const hasWindowOutput = typeof data.current_output_b64 === "string" && data.current_output_b64 !== "";
      const currentOffset = Number(data.poll_resume_offset);
      const shouldApplyCurrentWindow = connected && hasWindowOutput;
      const shouldReplayEntries = !shouldApplyCurrentWindow && hasEntries;

      if (shouldReplayEntries) {
        for (const entry of data.entries) {
          if (!entry || typeof entry !== "object") {
            continue;
          }
          if (typeof entry.raw_b64 === "string" && entry.raw_b64 !== "") {
            this.appendRawBase64(entry.raw_b64, false);
          }
          if (Number.isFinite(entry.seq)) {
            this.lastSeq = Math.max(this.lastSeq, Number(entry.seq));
          }
        }
      }

      if (shouldApplyCurrentWindow) {
        this.appendRawBase64(data.current_output_b64, false);
        if (Number.isFinite(currentOffset) && currentOffset >= 0) {
          this.pollOffset = Number(currentOffset);
        }
      } else if (shouldReplayEntries && Number.isFinite(currentOffset) && currentOffset >= 0) {
        this.pollOffset = Number(currentOffset);
      }

      if (Number.isFinite(data.last_seq)) {
        this.lastSeq = Math.max(this.lastSeq, Number(data.last_seq));
      }
      this.applySideErrors(data.side_errors);

      this.emitState(connected ? "running" : "disconnected", "", {
        disconnected: !connected,
        done: false,
      });
    }

    openEventStream(version, expectedSessionID) {
      if (!this.isTransportCurrent(version) || this.sessionID !== expectedSessionID) {
        return;
      }

      let source;
      try {
        const search = new window.URLSearchParams();
        search.set("session_id", expectedSessionID);
        if (Number.isFinite(this.lastSeq) && this.lastSeq > 0) {
          search.set("last_seq", String(this.lastSeq));
        }
        source = new window.EventSource(`/cli/events?${search.toString()}`);
      } catch (_error) {
        this.enablePollingFallback(200);
        return;
      }

      this.closeEventStream();
      this.eventSource = source;

      const isStale = () => {
        return this.eventSource !== source
          || !this.isTransportCurrent(version)
          || this.sessionID !== expectedSessionID;
      };

      source.onopen = () => {
        if (isStale()) {
          source.close();
          return;
        }
        this.streamRetryCount = 0;
      };

      source.addEventListener("output", (event) => {
        if (isStale()) {
          source.close();
          return;
        }
        this.streamRetryCount = 0;
        this.handleStreamEvent("output", event.data);
      });

      source.addEventListener("state", (event) => {
        if (isStale()) {
          source.close();
          return;
        }
        this.streamRetryCount = 0;
        this.handleStreamEvent("state", event.data);
      });

      source.addEventListener("side_error", (event) => {
        if (isStale()) {
          source.close();
          return;
        }
        this.streamRetryCount = 0;
        this.handleStreamEvent("side_error", event.data);
      });

      source.addEventListener("replay_reset", () => {
        if (isStale()) {
          source.close();
          return;
        }
        this.startTransport(0);
      });

      source.onerror = () => {
        if (this.eventSource === source) {
          this.eventSource.close();
          this.eventSource = null;
        } else {
          source.close();
        }
        if (isStale()) {
          return;
        }
        this.scheduleStreamReconnect();
      };
    }

    closeEventStream() {
      if (!this.eventSource) {
        return;
      }
      this.eventSource.close();
      this.eventSource = null;
    }

    handleStreamEvent(eventType, rawData) {
      let data = null;
      try {
        data = rawData ? JSON.parse(rawData) : null;
      } catch (_error) {
        data = null;
      }
      if (!data || typeof data !== "object") {
        return;
      }
      if (eventType === "replay_reset") {
        this.startTransport(0);
        return;
      }
      if (!this.advanceSequence(data.seq)) {
        return;
      }
      if (eventType === "state") {
        this.handleStateEvent(data);
        return;
      }
      if (eventType === "side_error") {
        this.handleSideErrorEvent(data);
        return;
      }
      this.handleOutputEvent(data);
    }

    advanceSequence(seq) {
      if (!Number.isFinite(seq)) {
        return true;
      }
      const nextSeq = Number(seq);
      if (this.lastSeq && nextSeq <= this.lastSeq) {
        return false;
      }
      if (this.lastSeq && nextSeq > this.lastSeq + 1) {
        this.requestStreamReplay();
        return false;
      }
      this.lastSeq = nextSeq;
      return true;
    }

    handleOutputEvent(data) {
      if (!data || typeof data !== "object") {
        return;
      }
      if (typeof data.raw_b64 === "string" && data.raw_b64 !== "") {
        this.appendRawBase64(data.raw_b64, true);
        return;
      }
      if (typeof data.output === "string" && data.output !== "") {
        this.appendOutputString(data.output);
      }
    }

    handleSideErrorEvent(data) {
      if (!data || typeof data !== "object") {
        return;
      }
      this.applySideErrors(data.side_error ? [data.side_error] : data.side_errors);
    }

    handleStateEvent(data) {
      const state = String(data && data.state ? data.state : "running").trim() || "running";
      const done = Boolean(data && data.done) || state === "exited" || state === "terminated";
      const errorMessage = String(data && data.last_error ? data.last_error : "").trim();
      this.emitState(state, errorMessage, {
        disconnected: false,
        done,
      });
      if (!done) {
        return;
      }
      this.flushDecoder();
      this.deactivate();
    }

    scheduleStreamReconnect() {
      if (!this.active || this.disposed || !this.sessionID) {
        return;
      }
      this.clearReconnectTimer();
      this.streamRetryCount += 1;
      if (this.streamRetryCount >= 4) {
        this.startTransport(0);
        return;
      }
      const delay = Math.min(4000, 300 * Math.pow(2, this.streamRetryCount - 1));
      this.reconnectTimer = window.setTimeout(() => {
        this.requestStreamReplay();
      }, delay);
    }

    requestStreamReplay() {
      if (!this.active || this.disposed || !this.sessionID) {
        return;
      }
      if (typeof window.EventSource !== "function") {
        this.enablePollingFallback(0);
        return;
      }
      this.closeEventStream();
      this.openEventStream(this.transportVersion, this.sessionID);
    }

    enablePollingFallback(delay) {
      if (!this.active || this.disposed || !this.sessionID) {
        return;
      }
      this.usePollingFallback = true;
      this.clearReconnectTimer();
      this.closeEventStream();
      this.schedulePoll(delay);
    }

    schedulePoll(delay) {
      if (!this.active || this.disposed || !this.sessionID) {
        return;
      }
      this.clearPollTimer();
      this.pollTimer = window.setTimeout(() => {
        void this.pollOnce();
      }, Math.max(0, Number(delay) || 0));
    }

    async pollOnce() {
      if (!this.active || this.disposed || !this.sessionID || this.polling) {
        return;
      }
      this.polling = true;
      try {
        const payload = await this.request("GET", `/cli/sessions/${encodeURIComponent(this.sessionID)}/poll?offset=${encodeURIComponent(String(this.pollOffset))}`);
        const data = payload || {};

        if (Boolean(data.rewind)) {
          this.clear();
          this.cursorOnboardingSimplified = false;
          this.resetDecoder();
        }

        if (typeof data.raw_b64 === "string" && data.raw_b64 !== "") {
          this.appendRawBase64(data.raw_b64, false);
        } else if (typeof data.output === "string" && data.output !== "") {
          this.appendOutputString(data.output);
        }
        this.applySideErrors(data.side_errors);

        if (Number.isFinite(data.next_offset)) {
          this.pollOffset = Number(data.next_offset);
        }

        const state = String(data.state || "running").trim() || "running";
        this.emitState(state, "", {
          disconnected: false,
          done: Boolean(data.done),
        });

        if (data.done) {
          this.flushDecoder();
          this.deactivate();
          return;
        }

        this.schedulePoll(Boolean(data.more) ? 90 : 420);
      } catch (error) {
        this.handleTransportError(error, true);
      } finally {
        this.polling = false;
      }
    }

    appendOutputString(text) {
      let normalizedText = String(text || "");
      if (!normalizedText) {
        return;
      }
      if (this.collapseAnimatedFrames) {
        normalizedText = this.collapseRepeatedClearFrames(normalizedText);
      }
      if (this.collapseAnimatedFrames) {
        normalizedText = this.simplifyCursorOnboarding(normalizedText);
      }
      if (!normalizedText) {
        return;
      }
      if (this.terminal) {
        this.terminal.write(normalizedText);
        if (this.lastSize.cols <= 0 || this.lastSize.rows <= 0) {
          this.requestTerminalFit(true);
          this.scheduleResize();
        }
        return;
      }
      if (!this.fallbackOutput) {
        return;
      }
      const fallbackText = this.normalizeFallbackText(normalizedText);
      if (!fallbackText) {
        return;
      }
      this.fallbackOutput.textContent += fallbackText;
      if (this.fallbackOutput.textContent.length > 300000) {
        this.fallbackOutput.textContent = this.fallbackOutput.textContent.slice(-220000);
      }
      this.fallbackOutput.scrollTop = this.fallbackOutput.scrollHeight;
    }

    appendRawBase64(rawB64, updateOffset) {
      const encoded = String(rawB64 || "").trim();
      if (!encoded) {
        return;
      }
      try {
        const bytes = this.decodeBase64Bytes(encoded);
        if (updateOffset) {
          this.pollOffset += bytes.length;
        }
        if (this.terminal && !this.collapseAnimatedFrames) {
          this.terminal.write(bytes);
          if (this.lastSize.cols <= 0 || this.lastSize.rows <= 0) {
            this.requestTerminalFit(true);
            this.scheduleResize();
          }
          return;
        }
        const text = this.textDecoder.decode(bytes, { stream: true });
        if (text) {
          this.appendOutputString(text);
        }
      } catch (_error) {
      }
    }

    applySideErrors(items) {
      if (!Array.isArray(items) || items.length === 0) {
        return;
      }
      let changed = false;
      for (const item of items) {
        if (!item || typeof item !== "object") {
          continue;
        }
        const id = String(item.id || "").trim();
        if (!id || this.sideErrorIDs.has(id)) {
          continue;
        }
        this.sideErrorIDs.add(id);
        this.sideErrors.push({
          id,
          code: String(item.code || "").trim(),
          message: String(item.message || "").trim(),
          timestamp: String(item.timestamp || "").trim(),
        });
        changed = true;
      }
      if (!changed) {
        return;
      }
      if (this.sideErrors.length > 12) {
        const trimmed = this.sideErrors.slice(-12);
        this.sideErrorIDs = new Set(trimmed.map((item) => item.id));
        this.sideErrors = trimmed;
      }
      this.emitState(this.runtimeState, this.runtimeError, {
        disconnected: this.runtimeDisconnected,
        done: this.runtimeDone,
      });
    }

    queueKeys(data) {
      if (!this.active || this.disposed || !this.sessionID || !data) {
        return;
      }
      this.keyBuffer += data;
      if (this.keyTimer) {
        return;
      }
      this.keyTimer = window.setTimeout(() => {
        void this.flushKeys();
      }, 25);
    }

    async flushKeys() {
      this.clearKeyTimer();
      const payload = this.keyBuffer;
      this.keyBuffer = "";
      if (!payload || !this.active || !this.sessionID || this.disposed) {
        return;
      }
      try {
        await this.request("POST", `/cli/sessions/${encodeURIComponent(this.sessionID)}/keys`, {
          b64: this.encodeBase64(payload),
        });
      } catch (error) {
        this.handleTransportError(error, false);
      }
    }

    scheduleResize() {
      if (!this.active || this.disposed || !this.sessionID) {
        return;
      }
      this.clearResizeTimer();
      this.resizeTimer = window.setTimeout(() => {
        void this.flushResize();
      }, 70);
    }

    async flushResize() {
      this.clearResizeTimer();
      if (!this.active || this.disposed || !this.sessionID) {
        return;
      }
      this.fitTerminalToContainer();
      const size = this.computeTerminalSize();
      if (!size) {
        return;
      }

      if (size.cols === this.lastSize.cols && size.rows === this.lastSize.rows) {
        return;
      }

      this.lastSize = size;
      if (this.host) {
        this.host.dataset.terminalCols = String(size.cols);
        this.host.dataset.terminalRows = String(size.rows);
      }
      if (this.terminal) {
        this.terminal.resize(size.cols, size.rows);
      }

      try {
        await this.request("POST", `/cli/sessions/${encodeURIComponent(this.sessionID)}/resize`, {
          cols: size.cols,
          rows: size.rows,
        });
      } catch (error) {
        this.handleTransportError(error, false);
      }
    }

    parsePixelValue(value) {
      const parsed = Number.parseFloat(value);
      if (!Number.isFinite(parsed)) {
        return 0;
      }
      return parsed;
    }

    readRenderedCellDimensions() {
      if (!this.terminal || !this.terminal.element || this.terminal.cols <= 0 || this.terminal.rows <= 0) {
        return null;
      }
      const screen = this.terminal.element.querySelector(".xterm-screen");
      if (!screen) {
        return null;
      }
      const rect = screen.getBoundingClientRect();
      if (!Number.isFinite(rect.width) || rect.width <= 0 || !Number.isFinite(rect.height) || rect.height <= 0) {
        return null;
      }
      const width = rect.width / this.terminal.cols;
      const height = rect.height / this.terminal.rows;
      if (!Number.isFinite(width) || width <= 0 || !Number.isFinite(height) || height <= 0) {
        return null;
      }
      return { width, height };
    }

    readTerminalCellDimensions() {
      if (!this.terminal || !this.terminalRoot) {
        return null;
      }
      const rendered = this.readRenderedCellDimensions();
      if (rendered) {
        return rendered;
      }
      const core = this.terminal._core;
      const renderService = core && core._renderService;
      const dimensions = renderService && renderService.dimensions;
      const css = dimensions && dimensions.css;
      const cell = css && css.cell;
      const cellWidth = Number(cell && cell.width);
      const cellHeight = Number(cell && cell.height);
      if (Number.isFinite(cellWidth) && cellWidth > 0 && Number.isFinite(cellHeight) && cellHeight > 0) {
        return { width: cellWidth, height: cellHeight };
      }
      const measureEl = this.terminalRoot.querySelector(".xterm-char-measure-element");
      if (!measureEl) {
        return null;
      }
      const rect = measureEl.getBoundingClientRect();
      if (!Number.isFinite(rect.width) || rect.width <= 0 || !Number.isFinite(rect.height) || rect.height <= 0) {
        return null;
      }
      return { width: rect.width, height: rect.height };
    }

    fitTerminalToContainer() {
      if (!this.terminal || !this.terminalRoot) {
        return false;
      }
      const cell = this.readTerminalCellDimensions();
      if (!cell) {
        return false;
      }
      const hostStyle = window.getComputedStyle(this.terminalRoot);
      const horizontalPadding = this.parsePixelValue(hostStyle.paddingLeft) + this.parsePixelValue(hostStyle.paddingRight);
      const verticalPadding = this.parsePixelValue(hostStyle.paddingTop) + this.parsePixelValue(hostStyle.paddingBottom);
      let scrollbarWidth = 0;
      if (this.terminal.element) {
        const viewport = this.terminal.element.querySelector(".xterm-viewport");
        if (viewport) {
          scrollbarWidth = Math.max(0, viewport.offsetWidth - viewport.clientWidth);
        }
      }
      const availableWidth = Math.max(0, this.terminalRoot.clientWidth - horizontalPadding - scrollbarWidth);
      const availableHeight = Math.max(0, this.terminalRoot.clientHeight - verticalPadding);
      if (availableWidth < 240 || availableHeight < 120) {
        return false;
      }
      const nextCols = Math.max(24, Math.floor(availableWidth / Math.max(cell.width, 1)));
      const nextRows = Math.max(6, Math.floor(availableHeight / Math.max(cell.height, 1)));
      if (nextCols === this.terminal.cols && nextRows === this.terminal.rows) {
        return true;
      }
      this.terminal.resize(nextCols, nextRows);
      return true;
    }

    requestTerminalFit(withRetry) {
      if (!this.terminal) {
        return;
      }
      if (this.terminalFitFrame) {
        return;
      }
      this.terminalFitFrame = window.requestAnimationFrame(() => {
        this.terminalFitFrame = 0;
        const fitted = this.fitTerminalToContainer();
        if (fitted) {
          this.terminalFitRetryCount = 0;
          return;
        }
        if (!withRetry) {
          this.terminalFitRetryCount = 0;
          return;
        }
        if (this.terminalFitRetryCount >= 10) {
          this.terminalFitRetryCount = 0;
          return;
        }
        this.terminalFitRetryCount += 1;
        if (this.terminalFitRetryTimer) {
          window.clearTimeout(this.terminalFitRetryTimer);
        }
        this.terminalFitRetryTimer = window.setTimeout(() => {
          this.terminalFitRetryTimer = 0;
          this.requestTerminalFit(true);
        }, 80);
      });
    }

    clearTerminalFitState() {
      if (this.terminalFitFrame) {
        window.cancelAnimationFrame(this.terminalFitFrame);
        this.terminalFitFrame = 0;
      }
      if (this.terminalFitRetryTimer) {
        window.clearTimeout(this.terminalFitRetryTimer);
        this.terminalFitRetryTimer = 0;
      }
      this.terminalFitRetryCount = 0;
    }

    computeTerminalSize() {
      if (!this.terminalRoot) {
        return null;
      }
      const cell = this.readTerminalCellDimensions();
      if (cell) {
        const hostStyle = window.getComputedStyle(this.terminalRoot);
        const horizontalPadding = this.parsePixelValue(hostStyle.paddingLeft) + this.parsePixelValue(hostStyle.paddingRight);
        const verticalPadding = this.parsePixelValue(hostStyle.paddingTop) + this.parsePixelValue(hostStyle.paddingBottom);
        let scrollbarWidth = 0;
        if (this.terminal && this.terminal.element) {
          const viewport = this.terminal.element.querySelector(".xterm-viewport");
          if (viewport) {
            scrollbarWidth = Math.max(0, viewport.offsetWidth - viewport.clientWidth);
          }
        }
        const availableWidth = Math.max(0, this.terminalRoot.clientWidth - horizontalPadding - scrollbarWidth);
        const availableHeight = Math.max(0, this.terminalRoot.clientHeight - verticalPadding);
        if (availableWidth >= 240 && availableHeight >= 120) {
          return {
            cols: Math.max(24, Math.floor(availableWidth / Math.max(cell.width, 1))),
            rows: Math.max(6, Math.floor(availableHeight / Math.max(cell.height, 1))),
          };
        }
      }
      if (this.terminal && this.terminal.cols > 0 && this.terminal.rows > 0) {
        return {
          cols: this.terminal.cols,
          rows: this.terminal.rows,
        };
      }
      const width = this.terminalRoot.clientWidth;
      const height = this.terminalRoot.clientHeight;
      if (!Number.isFinite(width) || !Number.isFinite(height) || width < 20 || height < 20) {
        return null;
      }

      const cols = Math.max(24, Math.floor(width / 8.2));
      const rows = Math.max(6, Math.floor(height / 18));
      return { cols, rows };
    }

    attachResizeSync() {
      window.addEventListener("resize", this.boundResizeHandler);
      if (typeof window.ResizeObserver !== "function" || !this.host) {
        return;
      }
      this.resizeObserver = new window.ResizeObserver(() => {
        this.requestTerminalFit(true);
        this.scheduleResize();
      });
      this.resizeObserver.observe(this.host);
      if (this.host.parentElement) {
        this.resizeObserver.observe(this.host.parentElement);
      }
    }

    detachResizeSync() {
      window.removeEventListener("resize", this.boundResizeHandler);
      if (this.resizeObserver) {
        this.resizeObserver.disconnect();
        this.resizeObserver = null;
      }
      this.clearTerminalFitState();
    }

    clearPollTimer() {
      if (!this.pollTimer) {
        return;
      }
      window.clearTimeout(this.pollTimer);
      this.pollTimer = 0;
    }

    clearKeyTimer() {
      if (!this.keyTimer) {
        return;
      }
      window.clearTimeout(this.keyTimer);
      this.keyTimer = 0;
    }

    clearResizeTimer() {
      if (!this.resizeTimer) {
        return;
      }
      window.clearTimeout(this.resizeTimer);
      this.resizeTimer = 0;
    }

    clearReconnectTimer() {
      if (!this.reconnectTimer) {
        return;
      }
      window.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = 0;
    }

    emitState(state, errorMessage, extra = {}) {
      this.runtimeState = String(state || "").trim();
      this.runtimeError = String(errorMessage || "").trim();
      this.runtimeDisconnected = Boolean(extra.disconnected);
      this.runtimeDone = Boolean(extra.done);
      this.onStateChange({
        sessionID: this.sessionID,
        state: this.runtimeState,
        error: this.runtimeError,
        disconnected: this.runtimeDisconnected,
        done: this.runtimeDone,
        sideErrors: this.sideErrors.slice(),
      });
    }

    handleTransportError(error, retryOnError) {
      const message = String(error && error.message ? error.message : "request failed").trim();
      if (this.isDisconnectedError(error)) {
        this.emitState("disconnected", message || "session disconnected", { disconnected: true, done: false });
        this.deactivate();
        return;
      }
      this.emitState("error", message, { disconnected: false, done: false });
      if (retryOnError && this.active) {
        this.schedulePoll(1100);
      }
    }

    isDisconnectedError(error) {
      const apiCode = Number(error && error.apiCode ? error.apiCode : 0);
      const text = String(error && error.message ? error.message : "").toLowerCase();
      return apiCode === 409
        || apiCode === 410
        || text.includes("session disconnected")
        || text.includes("会话实例已断开");
    }

    encodeBase64(text) {
      const bytes = this.textEncoder.encode(text);
      let binary = "";
      for (let i = 0; i < bytes.length; i += 1) {
        binary += String.fromCharCode(bytes[i]);
      }
      return window.btoa(binary);
    }

    decodeBase64Bytes(rawB64) {
      const binary = window.atob(String(rawB64 || ""));
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i += 1) {
        bytes[i] = binary.charCodeAt(i);
      }
      return bytes;
    }

    resetDecoder() {
      this.textDecoder = new window.TextDecoder("utf-8", { fatal: false });
    }

    collapseRepeatedClearFrames(text) {
      const clearMarker = "\x1b[2J";
      let index = text.indexOf(clearMarker);
      if (index < 0) {
        return text;
      }
      let clearCount = 0;
      let lastIndex = -1;
      while (index >= 0) {
        clearCount += 1;
        lastIndex = index;
        index = text.indexOf(clearMarker, index + clearMarker.length);
      }
      if (clearCount < 2 || lastIndex <= 0) {
        return text;
      }
      this.resetDecoder();
      return text.slice(lastIndex);
    }

    simplifyCursorOnboarding(text) {
      const signInMarker = "Press any key to sign in...";
      const plain = this.normalizeFallbackText(text);
      if (plain.includes(signInMarker)) {
        if (this.cursorOnboardingSimplified) {
          return "";
        }
        this.cursorOnboardingSimplified = true;
        return "\x1b[2J\x1b[3J\x1b[HCursor Agent\n\nPress any key to sign in...\n";
      }
      if (this.cursorOnboardingSimplified) {
        if (!plain.trim()) {
          return "";
        }
        this.cursorOnboardingSimplified = false;
      }
      return text;
    }

    normalizeFallbackText(text) {
      let plain = String(text || "");
      if (!plain) {
        return "";
      }
      // Strip common ANSI CSI/OSC control sequences for non-xterm fallback rendering.
      plain = plain.replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, "");
      plain = plain.replace(/\x1b\][^\x07]*(?:\x07|\x1b\\)/g, "");
      plain = plain.replace(/\x1b[@-_]/g, "");
      return plain;
    }

    flushDecoder() {
      if (!this.textDecoder) {
        return;
      }
      try {
        const tail = this.textDecoder.decode();
        if (tail) {
          this.appendOutputString(tail);
        }
      } catch (_error) {
      }
    }

    async request(method, url, payload) {
      const options = {
        method,
      };
      if (method !== "GET") {
        options.headers = {
          "Content-Type": "application/json",
        };
        options.body = JSON.stringify(payload || {});
      }
      const response = await window.fetch(url, options);
      const text = await response.text();
      let envelope = {};
      try {
        envelope = text ? JSON.parse(text) : {};
      } catch (_error) {
        envelope = {};
      }
      if (!response.ok || Number(envelope.code) !== 0) {
        const error = new Error(envelope.message || `request failed (${response.status})`);
        error.apiCode = Number(envelope.code) || response.status;
        error.httpStatus = response.status;
        throw error;
      }
      return envelope.data || {};
    }
  }

  window.FileManagerCLITerminalController = FileManagerCLITerminalController;
})();
