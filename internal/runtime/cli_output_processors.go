package runtime

import (
	"bytes"
	"strings"
	"sync"
	"time"
)

type CLIOutputChunk struct {
	SessionID string
	AgentID   string
	Timestamp time.Time
	Payload   []byte
}

type CLIOutputProcessor interface {
	Name() string
	Process(chunk CLIOutputChunk) (CLIOutputChunk, error)
}

type CLISessionScopedProcessor interface {
	ClearSession(sessionID, agentID string)
}

type CLIOutputPipeline struct {
	mu         sync.RWMutex
	processors []CLIOutputProcessor
}

// NewCLIOutputPipeline creates an ordered output processor pipeline.
func NewCLIOutputPipeline(processors ...CLIOutputProcessor) *CLIOutputPipeline {
	list := make([]CLIOutputProcessor, 0, len(processors))
	for _, processor := range processors {
		if processor == nil {
			continue
		}
		list = append(list, processor)
	}
	return &CLIOutputPipeline{processors: list}
}

// Add appends one processor to the end of the pipeline.
func (p *CLIOutputPipeline) Add(processor CLIOutputProcessor) {
	if processor == nil {
		return
	}
	p.mu.Lock()
	p.processors = append(p.processors, processor)
	p.mu.Unlock()
}

// Process executes the pipeline sequentially and returns transformed chunk.
func (p *CLIOutputPipeline) Process(chunk CLIOutputChunk) CLIOutputChunk {
	if p == nil {
		return chunk
	}
	p.mu.RLock()
	processors := append([]CLIOutputProcessor(nil), p.processors...)
	p.mu.RUnlock()

	out := chunk
	for _, processor := range processors {
		next, err := processor.Process(out)
		if err != nil {
			continue
		}
		out = next
	}
	return out
}

// ClearSession clears per-session state in stateful processors.
func (p *CLIOutputPipeline) ClearSession(sessionID, agentID string) {
	if p == nil {
		return
	}
	p.mu.RLock()
	processors := append([]CLIOutputProcessor(nil), p.processors...)
	p.mu.RUnlock()
	for _, processor := range processors {
		scoped, ok := processor.(CLISessionScopedProcessor)
		if !ok {
			continue
		}
		scoped.ClearSession(sessionID, agentID)
	}
}

// DefaultCLIOutputProcessors returns the default runtime output processors.
func DefaultCLIOutputProcessors() []CLIOutputProcessor {
	return []CLIOutputProcessor{
		NULByteStripProcessor{},
		UTF8SanitizerProcessor{},
		&ANSIEscapeStripProcessor{},
		&TerminalLineRewriteProcessor{},
	}
}

type NULByteStripProcessor struct{}

// Name returns processor identifier.
func (NULByteStripProcessor) Name() string { return "nul-byte-strip" }

// Process strips NUL bytes from output payload.
func (NULByteStripProcessor) Process(chunk CLIOutputChunk) (CLIOutputChunk, error) {
	if len(chunk.Payload) == 0 {
		return chunk, nil
	}
	cleaned := bytes.ReplaceAll(chunk.Payload, []byte{0x00}, nil)
	if len(cleaned) == len(chunk.Payload) {
		return chunk, nil
	}
	chunk.Payload = cleaned
	return chunk, nil
}

type UTF8SanitizerProcessor struct{}

// Name returns processor identifier.
func (UTF8SanitizerProcessor) Name() string { return "utf8-sanitizer" }

// Process replaces invalid UTF-8 sequences with replacement rune bytes.
func (UTF8SanitizerProcessor) Process(chunk CLIOutputChunk) (CLIOutputChunk, error) {
	if len(chunk.Payload) == 0 {
		return chunk, nil
	}
	chunk.Payload = bytes.ToValidUTF8(chunk.Payload, []byte("\xef\xbf\xbd"))
	return chunk, nil
}

type CRLFNormalizeProcessor struct{}

// Name returns processor identifier.
func (CRLFNormalizeProcessor) Name() string { return "crlf-normalizer" }

// Process normalizes CRLF into LF.
func (CRLFNormalizeProcessor) Process(chunk CLIOutputChunk) (CLIOutputChunk, error) {
	if len(chunk.Payload) == 0 {
		return chunk, nil
	}
	chunk.Payload = bytes.ReplaceAll(chunk.Payload, []byte("\r\n"), []byte("\n"))
	return chunk, nil
}

// ANSIEscapeStripProcessor removes ANSI CSI/OSC control sequences from a
// byte stream while preserving printable characters.
type ANSIEscapeStripProcessor struct {
	mu     sync.Mutex
	states map[string]*ansiStripState
}

// Name returns processor identifier.
func (*ANSIEscapeStripProcessor) Name() string { return "ansi-escape-strip" }

// Process strips ANSI control sequences while preserving text payload.
func (p *ANSIEscapeStripProcessor) Process(chunk CLIOutputChunk) (CLIOutputChunk, error) {
	if len(chunk.Payload) == 0 {
		return chunk, nil
	}

	p.mu.Lock()
	if p.states == nil {
		p.states = make(map[string]*ansiStripState)
	}
	key := outputSessionKey(chunk.SessionID, chunk.AgentID)
	state, ok := p.states[key]
	if !ok {
		state = &ansiStripState{}
		p.states[key] = state
	}
	defer p.mu.Unlock()

	out := make([]byte, 0, len(chunk.Payload))
	for _, b := range chunk.Payload {
		if state.osc {
			switch {
			case b == 0x07:
				state.osc = false
				state.oscEsc = false
			case state.oscEsc && b == '\\':
				state.osc = false
				state.oscEsc = false
			case state.oscEsc:
				state.oscEsc = false
			case b == 0x1b:
				state.oscEsc = true
			}
			continue
		}
		if state.csi {
			if b >= 0x40 && b <= 0x7e {
				state.csi = false
			}
			continue
		}
		if state.esc {
			state.esc = false
			switch b {
			case '[':
				state.csi = true
			case ']':
				state.osc = true
				state.oscEsc = false
			}
			continue
		}
		if b == 0x1b {
			state.esc = true
			continue
		}
		out = append(out, b)
	}

	chunk.Payload = out
	return chunk, nil
}

// ClearSession drops cached ANSI parse state for one session.
func (p *ANSIEscapeStripProcessor) ClearSession(sessionID, agentID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.states == nil {
		return
	}
	delete(p.states, outputSessionKey(sessionID, agentID))
}

type ansiStripState struct {
	esc    bool
	csi    bool
	osc    bool
	oscEsc bool
}

// TerminalLineRewriteProcessor converts terminal-style carriage-return updates
// into developer-readable line output.
type TerminalLineRewriteProcessor struct {
	mu     sync.Mutex
	states map[string]*terminalLineState
}

// Name returns processor identifier.
func (*TerminalLineRewriteProcessor) Name() string { return "terminal-line-rewrite" }

// Process rewrites carriage-return style terminal updates into final lines.
func (p *TerminalLineRewriteProcessor) Process(chunk CLIOutputChunk) (CLIOutputChunk, error) {
	if len(chunk.Payload) == 0 {
		return chunk, nil
	}

	p.mu.Lock()
	if p.states == nil {
		p.states = make(map[string]*terminalLineState)
	}
	key := outputSessionKey(chunk.SessionID, chunk.AgentID)
	state, ok := p.states[key]
	if !ok {
		state = &terminalLineState{}
		p.states[key] = state
	}
	defer p.mu.Unlock()

	var out strings.Builder
	for _, r := range string(chunk.Payload) {
		switch r {
		case '\r':
			state.cursor = 0
		case '\n':
			line := strings.TrimSpace(string(state.line))
			if line != "" {
				out.WriteString(line)
				out.WriteByte('\n')
			}
			state.line = state.line[:0]
			state.cursor = 0
		case '\b', 0x7f:
			if state.cursor > 0 {
				state.cursor--
				if state.cursor < len(state.line) {
					state.line = append(state.line[:state.cursor], state.line[state.cursor+1:]...)
				}
			}
		case '\t':
			writeRuneState(state, ' ')
			writeRuneState(state, ' ')
			writeRuneState(state, ' ')
			writeRuneState(state, ' ')
		default:
			if r < 0x20 {
				continue
			}
			writeRuneState(state, r)
		}
	}

	chunk.Payload = []byte(out.String())
	return chunk, nil
}

// ClearSession drops cached terminal line state for one session.
func (p *TerminalLineRewriteProcessor) ClearSession(sessionID, agentID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.states == nil {
		return
	}
	delete(p.states, outputSessionKey(sessionID, agentID))
}

type terminalLineState struct {
	line   []rune
	cursor int
}

// writeRuneState writes one rune at cursor position and advances the cursor.
func writeRuneState(state *terminalLineState, r rune) {
	if state == nil {
		return
	}
	if state.cursor < 0 {
		state.cursor = 0
	}
	if state.cursor >= len(state.line) {
		state.line = append(state.line, r)
	} else {
		state.line[state.cursor] = r
	}
	state.cursor++
}

// outputSessionKey builds processor state key from session and agent IDs.
func outputSessionKey(sessionID, agentID string) string {
	sessionID = strings.TrimSpace(sessionID)
	agentID = strings.TrimSpace(agentID)
	if sessionID == "" && agentID == "" {
		return "__default__"
	}
	return sessionID + "|" + agentID
}
