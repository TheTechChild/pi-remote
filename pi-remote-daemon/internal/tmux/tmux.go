// SPDX-License-Identifier: MIT
package tmux

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TheTechChild/pi-remote-daemon/internal/session"
)

// Client wraps the long-lived tmux control-mode process.
type Client struct {
	binary        string
	sessionPrefix string
	reg           *session.Registry
	multiplex     *session.Multiplex
	log           *slog.Logger

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr *bytes.Buffer

	mu           sync.Mutex
	pendingCmds  map[string]chan cmdResult
	pendingQueue []chan cmdResult

	// paneToSession maps a tmux pane ID (like "%0") to active SessionID.
	paneToSession map[string]string

	// pendingSpawns maps a spawn token to its pending registration channel.
	pendingSpawns map[string]chan *session.Session
}

type cmdResult struct {
	output []string
	err    error
}

// NewClient constructs a tmux control-mode client.
func NewClient(binary, sessionPrefix string, reg *session.Registry, multiplex *session.Multiplex, log *slog.Logger) *Client {
	if binary == "" {
		binary = "tmux"
	}
	if sessionPrefix == "" {
		sessionPrefix = "pi-remote-"
	}
	if log == nil {
		log = slog.Default()
	}
	return &Client{
		binary:        binary,
		sessionPrefix: sessionPrefix,
		reg:           reg,
		multiplex:     multiplex,
		log:           log,
		pendingCmds:   make(map[string]chan cmdResult),
		paneToSession: make(map[string]string),
		pendingSpawns: make(map[string]chan *session.Session),
	}
}

// SetMultiplex late-binds the session multiplexer to the tmux client.
func (c *Client) SetMultiplex(mux *session.Multiplex) {
	c.multiplex = mux
}

// Start launches or attaches to the tmux control-mode session and runs the read/write loops.
func (c *Client) Start(ctx context.Context) error {
	// Always kill any existing control session to start with a fresh, clean slate
	_ = exec.Command(c.binary, "kill-session", "-t", "pi-remote-control").Run()

	c.log.Info("creating new tmux control session", slog.String("session", "pi-remote-control"))
	// Create a detached session first to avoid requiring a controlling terminal (TTY/PTY)
	if createErr := exec.Command(c.binary, "new-session", "-d", "-s", "pi-remote-control").Run(); createErr != nil {
		return fmt.Errorf("failed to create new tmux session: %w", createErr)
	}
	cmd := exec.Command(c.binary, "-C", "attach", "-t", "pi-remote-control")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start tmux CC: %w", err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.stderr = &stderrBuf

	// Start read/parse loop
	go c.readLoop(ctx)

	return nil
}

// Close terminates the tmux control process.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil {
		_ = c.cmd.Process.Kill()
	}
	return nil
}

// RunCommand issues a command to tmux CC and blocks waiting for its response.
func (c *Client) RunCommand(cmdStr string) ([]string, error) {
	ch := make(chan cmdResult, 1)

	c.mu.Lock()
	if c.stdin == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("tmux client not started or closed")
	}
	c.pendingQueue = append(c.pendingQueue, ch)
	c.mu.Unlock()

	c.log.Debug("issuing tmux command", slog.String("cmd", cmdStr))
	if _, err := fmt.Fprintf(c.stdin, "%s\n", cmdStr); err != nil {
		c.mu.Lock()
		for i, q := range c.pendingQueue {
			if q == ch {
				c.pendingQueue = append(c.pendingQueue[:i], c.pendingQueue[i+1:]...)
				break
			}
		}
		c.mu.Unlock()
		return nil, fmt.Errorf("write command: %w", err)
	}

	select {
	case res := <-ch:
		return res.output, res.err
	case <-time.After(10 * time.Second):
		c.mu.Lock()
		for i, q := range c.pendingQueue {
			if q == ch {
				c.pendingQueue = append(c.pendingQueue[:i], c.pendingQueue[i+1:]...)
				break
			}
		}
		for k, v := range c.pendingCmds {
			if v == ch {
				delete(c.pendingCmds, k)
				break
			}
		}
		c.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for command response")
	}
}

// Spawn handles the spawn flow triggered by a spawn_request from the coordinator.
func (c *Client) Spawn(ctx context.Context, cwd string, requestID string) {
	sessionUUID, err := generateUUID()
	if err != nil {
		c.log.Error("failed to generate session UUID", slog.String("err", err.Error()))
		c.sendSpawnResponse(requestID, false, "", "", "internal error generating UUID")
		return
	}

	spawnToken, err := generateSpawnToken()
	if err != nil {
		c.log.Error("failed to generate spawn token", slog.String("err", err.Error()))
		c.sendSpawnResponse(requestID, false, "", "", "internal error generating spawn token")
		return
	}

	// Register the pending spawn before launching the process
	regCh := make(chan *session.Session, 1)
	c.mu.Lock()
	c.pendingSpawns[spawnToken] = regCh
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pendingSpawns, spawnToken)
		c.mu.Unlock()
	}()

	sessionName := fmt.Sprintf("%s%s", c.sessionPrefix, sessionUUID)
	// Spawn the tmux session with env variable and command
	cmdStr := fmt.Sprintf("new-session -d -s %s -c %s \"env PI_REMOTE_SPAWN_TOKEN=%s pi\"", sessionName, cwd, spawnToken)
	_, err = c.RunCommand(cmdStr)
	if err != nil {
		c.log.Error("tmux new-session command failed", slog.String("err", err.Error()))
		c.sendSpawnResponse(requestID, false, "", "", fmt.Sprintf("tmux failed: %s", err.Error()))
		return
	}

	// Wait for the extension registration or timeout
	select {
	case s := <-regCh:
		// Map the target to get the pane ID
		resolvedTarget := fmt.Sprintf("%s:0.0", sessionName)
		paneID, mapErr := c.MapPane(resolvedTarget, s.SessionID)
		if mapErr != nil {
			c.log.Error("failed to map pane ID on spawn", slog.String("target", resolvedTarget), slog.String("err", mapErr.Error()))
			c.sendSpawnResponse(requestID, false, "", "", fmt.Sprintf("failed mapping pane: %s", mapErr.Error()))
			return
		}

		c.log.Info("spawned session successfully registered", slog.String("session_id", s.SessionID), slog.String("pane_id", paneID))
		c.sendSpawnResponse(requestID, true, s.SessionID, resolvedTarget, "")

	case <-time.After(10 * time.Second):
		c.log.Warn("spawn timed out waiting for extension registration", slog.String("token", spawnToken))
		// Kill the empty session
		_, _ = c.RunCommand(fmt.Sprintf("kill-session -t %s", sessionName))
		c.sendSpawnResponse(requestID, false, "", "", "pi did not register within 10s; check daemon logs")
	case <-ctx.Done():
		return
	}
}

func (c *Client) sendSpawnResponse(requestID string, success bool, sessionID, tmuxTarget, errMsg string) {
	resp := map[string]any{
		"type":       "spawn_response",
		"v":          1,
		"request_id": requestID,
		"success":    success,
	}
	if success {
		resp["session_id"] = sessionID
		resp["tmux_target"] = tmuxTarget
	} else {
		resp["error"] = errMsg
	}
	if err := c.multiplex.SendPtyOrFrame(resp); err != nil {
		c.log.Error("failed to send spawn_response", slog.String("err", err.Error()))
	}
}

// ResolveAndMap correlates PID/TTY or spawn token to a real tmux target and pane ID.
func (c *Client) ResolveAndMap(pid int, inputTarget, sessionID, spawnToken string) (string, string, error) {
	resolvedTarget := inputTarget

	// Fallback/Local registration: resolve "untmuxed:0.0"
	if resolvedTarget == "untmuxed:0.0" {
		target, err := c.findTmuxTargetByPIDOrTTY(pid)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve untmuxed target: %w", err)
		}
		resolvedTarget = target
	}

	// Query pane ID for this resolved target
	paneID, err := c.MapPane(resolvedTarget, sessionID)
	if err != nil {
		return "", "", fmt.Errorf("failed to map pane for target %q: %w", resolvedTarget, err)
	}

	// Notify the spawn listener if any
	if spawnToken != "" {
		c.mu.Lock()
		regCh, ok := c.pendingSpawns[spawnToken]
		c.mu.Unlock()
		if ok {
			// Get the session from registry (which will be added right after this returns)
			// So we can notify the Spawn goroutine with a dummy session so it unblocks
			regCh <- &session.Session{SessionID: sessionID}
		}
	}

	return resolvedTarget, paneID, nil
}

// MapPane queries tmux for the pane_id of a target, links its window to the control session, and saves it in the mapping.
func (c *Client) MapPane(target, sessionID string) (string, error) {
	// 1. Lazy cleanup of dead sessions in paneToSession
	c.mu.Lock()
	var deadPanes []struct {
		pID string
		sID string
	}
	for pID, sID := range c.paneToSession {
		if _, ok := c.reg.Get(sID); !ok {
			deadPanes = append(deadPanes, struct{ pID, sID string }{pID, sID})
		}
	}
	c.mu.Unlock()

	for _, dp := range deadPanes {
		c.log.Info("cleaning up dead tmux pane mapping", slog.String("pane_id", dp.pID), slog.String("session_id", dp.sID))
		// Query the window_id for the dead pane
		wOut, wErr := c.RunCommand(fmt.Sprintf("display-message -p -t %s \"#{window_id}\"", dp.pID))
		if wErr == nil && len(wOut) > 0 && strings.TrimSpace(wOut[0]) != "" {
			wID := strings.TrimSpace(wOut[0])
			// First try to unlink (if it is still linked to other sessions)
			_, uErr := c.RunCommand(fmt.Sprintf("unlink-window -t pi-remote-control:%s", wID))
			if uErr != nil {
				// If it is only linked to pi-remote-control, unlink fails, so kill it
				_, _ = c.RunCommand(fmt.Sprintf("kill-window -t pi-remote-control:%s", wID))
			}
		}
		c.mu.Lock()
		delete(c.paneToSession, dp.pID)
		c.mu.Unlock()
	}

	// 2. Resolve the paneID for the new target
	out, err := c.RunCommand(fmt.Sprintf("display-message -p -t %q \"#{pane_id}\"", target))
	if err != nil {
		return "", err
	}
	if len(out) == 0 || strings.TrimSpace(out[0]) == "" {
		return "", fmt.Errorf("empty pane_id returned for target %q", target)
	}
	paneID := strings.TrimSpace(out[0])

	c.mu.Lock()
	c.paneToSession[paneID] = sessionID
	c.mu.Unlock()

	c.log.Debug("mapped tmux target to pane ID", slog.String("target", target), slog.String("pane_id", paneID), slog.String("session_id", sessionID))

	// 3. Link the window containing this pane into the control session if not already linked
	wOut, wErr := c.RunCommand(fmt.Sprintf("display-message -p -t %s \"#{window_id}\"", paneID))
	if wErr != nil || len(wOut) == 0 || strings.TrimSpace(wOut[0]) == "" {
		return paneID, nil
	}
	windowID := strings.TrimSpace(wOut[0])

	// Check if already linked
	linkedWindows, lwErr := c.RunCommand("list-windows -t pi-remote-control -F \"#{window_id}\"")
	alreadyLinked := false
	if lwErr == nil {
		for _, lw := range linkedWindows {
			if strings.TrimSpace(lw) == windowID {
				alreadyLinked = true
				break
			}
		}
	}

	if !alreadyLinked {
		c.log.Info("linking window into tmux control session to receive %output stream", slog.String("window_id", windowID), slog.String("pane_id", paneID), slog.String("session_id", sessionID))
		_, linkErr := c.RunCommand(fmt.Sprintf("link-window -d -s %s -t pi-remote-control", paneID))
		if linkErr != nil {
			c.log.Warn("failed to link window into control session", slog.String("pane_id", paneID), slog.String("err", linkErr.Error()))
		}
	}

	// 4. Capture current pane contents and cursor position to initialize the client's screen state
	if c.multiplex != nil {
		go func() {
			// Wait a small moment to let tmux stabilize the link/creation
			time.Sleep(50 * time.Millisecond)

			c.log.Info("capturing initial screen state for pane", slog.String("pane_id", paneID), slog.String("session_id", sessionID))
			capOut, capErr := c.RunCommand(fmt.Sprintf("capture-pane -e -p -t %s", paneID))
			if capErr != nil {
				c.log.Warn("failed to capture initial pane state", slog.String("pane_id", paneID), slog.String("err", capErr.Error()))
				return
			}

			// Query cursor position
			curOut, curErr := c.RunCommand(fmt.Sprintf("display-message -p -t %s \"#{cursor_x} #{cursor_y}\"", paneID))
			var cursorX, cursorY int
			if curErr == nil && len(curOut) > 0 {
				parts := strings.Fields(curOut[0])
				if len(parts) == 2 {
					_, _ = fmt.Sscanf(parts[0], "%d", &cursorX)
					_, _ = fmt.Sscanf(parts[1], "%d", &cursorY)
				}
			}

			// Construct initial payload: clear screen + home cursor + captured text (joined by \r\n) + move cursor to active position
			var buf bytes.Buffer
			buf.WriteString("\x1b[2J\x1b[H") // Clear screen and move to home

			// Join lines with \r\n to prevent staircase rendering
			for i, line := range capOut {
				buf.WriteString(line)
				if i < len(capOut)-1 {
					buf.WriteString("\r\n")
				}
			}

			// Move cursor to active position (ANSI is 1-based, tmux cursor coordinates are 0-based)
			fmt.Fprintf(&buf, "\x1b[%d;%dH", cursorY+1, cursorX+1)

			if err := c.multiplex.SendPty(sessionID, buf.Bytes()); err != nil {
				c.log.Error("failed to send initial captured pty frame", slog.String("session_id", sessionID), slog.String("err", err.Error()))
			} else {
				c.log.Info("successfully sent initial captured screen state", slog.String("session_id", sessionID), slog.Int("bytes_len", buf.Len()))
			}
		}()
	}

	return paneID, nil
}

func (c *Client) findTmuxTargetByPIDOrTTY(pid int) (string, error) {
	// List all tmux panes: #{pane_id} #{pane_tty} #{pane_pid} #{session_name}:#{window_index}.#{pane_index}
	out, err := c.RunCommand("list-panes -a -F \"#{pane_id} #{pane_tty} #{pane_pid} #{session_name}:#{window_index}.#{pane_index}\"")
	if err != nil {
		return "", fmt.Errorf("list-panes: %w", err)
	}

	// 1. TTY matching
	tty, _ := getProcessTTY(pid)
	if tty != "" && tty != "?" && tty != "??" {
		for _, line := range out {
			parts := strings.Split(line, " ")
			if len(parts) >= 4 {
				paneTTY := parts[1]
				target := parts[3]
				if strings.HasSuffix(paneTTY, tty) {
					return target, nil
				}
			}
		}
	}

	// 2. Process Tree matching
	pids := make(map[int]bool)
	curr := pid
	for i := 0; i < 10; i++ {
		pids[curr] = true
		ppid, err := getParentPID(curr)
		if err != nil || ppid <= 0 {
			break
		}
		curr = ppid
	}

	for _, line := range out {
		parts := strings.Split(line, " ")
		if len(parts) >= 4 {
			panePIDStr := parts[2]
			target := parts[3]
			panePID, err := strconv.Atoi(panePIDStr)
			if err == nil && pids[panePID] {
				return target, nil
			}
		}
	}

	return "", fmt.Errorf("could not resolve tmux target for PID %d", pid)
}

func (c *Client) readLoop(ctx context.Context) {
	defer func() {
		c.log.Info("tmux CC read loop stopped")
		c.handleExitNotification()

		c.mu.Lock()
		if c.stdin != nil {
			_ = c.stdin.Close()
		}
		for _, ch := range c.pendingCmds {
			select {
			case ch <- cmdResult{err: fmt.Errorf("tmux control process exited")}:
			default:
			}
		}
		c.pendingCmds = make(map[string]chan cmdResult)
		c.mu.Unlock()

		if c.cmd != nil {
			_ = c.cmd.Wait()
			if c.stderr != nil && c.stderr.Len() > 0 {
				c.log.Error("tmux stderr output on exit", slog.String("stderr", strings.TrimSpace(c.stderr.String())))
			}
		}
	}()

	scanner := bufio.NewScanner(c.stdout)

	var activeCmdID string
	var activeCmdOutput []string
	var activeCmdIsError bool

	for scanner.Scan() {
		line := scanner.Text()

		// Interleaved notification parsing
		if strings.HasPrefix(line, "%") {
			parts := strings.SplitN(line, " ", 4)
			prefix := parts[0]

			switch prefix {
			case "%output":
				if len(parts) >= 3 {
					paneID := parts[1]
					dataEsc := parts[2]
					c.handleOutputNotification(paneID, dataEsc)
				}
				continue

			case "%exit":
				return

			case "%begin":
				if len(parts) >= 3 {
					activeCmdID = parts[2]
					activeCmdOutput = nil
					activeCmdIsError = false

					c.mu.Lock()
					if len(c.pendingQueue) > 0 {
						ch := c.pendingQueue[0]
						c.pendingQueue = c.pendingQueue[1:]
						c.pendingCmds[activeCmdID] = ch
					}
					c.mu.Unlock()
				}
				continue

			case "%error":
				if len(parts) >= 3 {
					cmdID := parts[2]
					if cmdID == activeCmdID {
						activeCmdIsError = true
						c.mu.Lock()
						ch, ok := c.pendingCmds[cmdID]
						c.mu.Unlock()

						if ok {
							err := fmt.Errorf("%s", strings.Join(activeCmdOutput, "\n"))
							ch <- cmdResult{output: activeCmdOutput, err: err}
						}
						activeCmdID = ""
						activeCmdOutput = nil
					}
				}
				continue

			case "%end":
				if len(parts) >= 3 {
					cmdID := parts[2]
					if cmdID == activeCmdID {
						c.mu.Lock()
						ch, ok := c.pendingCmds[cmdID]
						c.mu.Unlock()

						if ok {
							var err error
							if activeCmdIsError {
								err = fmt.Errorf("%s", strings.Join(activeCmdOutput, "\n"))
							}
							ch <- cmdResult{output: activeCmdOutput, err: err}
						}
						activeCmdID = ""
						activeCmdOutput = nil
					}
				}
				continue
			}
		}

		if activeCmdID != "" {
			activeCmdOutput = append(activeCmdOutput, line)
		}
	}
}

func (c *Client) handleOutputNotification(paneID, dataEsc string) {
	c.mu.Lock()
	sessionID, ok := c.paneToSession[paneID]
	c.mu.Unlock()

	if !ok {
		// Not a registered pane we are tracking
		return
	}

	rawBytes, err := DeescapeTmux(dataEsc)
	if err != nil {
		c.log.Warn("failed to de-escape tmux output", slog.String("pane", paneID), slog.String("err", err.Error()))
		return
	}

	if len(rawBytes) == 0 {
		return
	}

	c.log.Info("handleOutputNotification forwarding PTY", slog.String("pane", paneID), slog.String("session_id", sessionID), slog.Int("bytes_len", len(rawBytes)))

	// Forward raw terminal bytes to multiplexer
	if err := c.multiplex.SendPty(sessionID, rawBytes); err != nil {
		c.log.Error("failed to forward pty bytes to multiplexer", slog.String("session_id", sessionID), slog.String("err", err.Error()))
	}
}

func (c *Client) handleExitNotification() {
	c.log.Warn("tmux control session exited, marking all sessions as lost")

	// Get all live sessions from registry
	sessions := c.reg.Snapshot()

	// Terminate each session with reason tmux_server_lost
	for _, s := range sessions {
		c.reg.RemoveWithReason(s.SessionID, string(session.ReasonTmuxServerLost))
	}
}

// DeescapeTmux decodes backslash-escaped and octal-escaped characters from tmux control mode output.
func DeescapeTmux(s string) ([]byte, error) {
	res := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] == '\\' {
			if i+1 >= len(s) {
				return nil, fmt.Errorf("trailing backslash")
			}
			if s[i+1] == '\\' {
				res = append(res, '\\')
				i += 2
				continue
			}
			if i+3 < len(s) {
				isOctal := true
				for j := 1; j <= 3; j++ {
					if s[i+j] < '0' || s[i+j] > '7' {
						isOctal = false
						break
					}
				}
				if isOctal {
					val := (s[i+1]-'0')*64 + (s[i+2]-'0')*8 + (s[i+3] - '0')
					res = append(res, byte(val))
					i += 4
					continue
				}
			}
			return nil, fmt.Errorf("invalid escape sequence at index %d", i)
		} else {
			res = append(res, s[i])
			i++
		}
	}
	return res, nil
}

func getParentPID(pid int) (int, error) {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	val := strings.TrimSpace(string(out))
	if val == "" {
		return 0, fmt.Errorf("no parent pid found")
	}
	return strconv.Atoi(val)
}

func getProcessTTY(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "tty=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func generateUUID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func generateSpawnToken() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// WritePty sends keystrokes/data to the specified session's tmux pane.
func (c *Client) WritePty(sessionID string, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	c.mu.Lock()
	if c.stdin == nil {
		c.mu.Unlock()
		return fmt.Errorf("tmux client not started or closed")
	}
	var target string
	for p, sID := range c.paneToSession {
		if sID == sessionID {
			target = p
			break
		}
	}
	c.mu.Unlock()

	if target == "" {
		sess, ok := c.reg.Get(sessionID)
		if !ok {
			return fmt.Errorf("session not found: %s", sessionID)
		}
		target = sess.TmuxTarget
	}

	hexStrings := make([]string, len(data))
	for i, b := range data {
		hexStrings[i] = fmt.Sprintf("%02x", b)
	}

	cmdStr := fmt.Sprintf("send-keys -t %q -H %s", target, strings.Join(hexStrings, " "))
	_, err := c.RunCommand(cmdStr)
	return err
}

// ResizePty resizes the tmux pane for the specified session.
func (c *Client) ResizePty(sessionID string, cols, rows int) error {
	if cols < 1 || rows < 1 {
		return fmt.Errorf("invalid dimensions: cols=%d, rows=%d", cols, rows)
	}

	c.mu.Lock()
	if c.stdin == nil {
		c.mu.Unlock()
		return fmt.Errorf("tmux client not started or closed")
	}
	var target string
	for p, sID := range c.paneToSession {
		if sID == sessionID {
			target = p
			break
		}
	}
	c.mu.Unlock()

	if target == "" {
		sess, ok := c.reg.Get(sessionID)
		if !ok {
			return fmt.Errorf("session not found: %s", sessionID)
		}
		target = sess.TmuxTarget
	}

	cmdStr := fmt.Sprintf("resize-window -t %q -x %d -y %d", target, cols, rows)
	_, err := c.RunCommand(cmdStr)
	return err
}
