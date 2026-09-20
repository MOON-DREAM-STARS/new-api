// Command workspace-clipd exposes the X11 clipboard to one Web Workspace
// runtime over a private Unix socket. It accepts a small JSON request followed
// by an optional bounded binary body and never logs clipboard contents.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxPayloadBytes int64 = 8 << 20

	errMIMEUnsupported = "ERR_CLIPD_MIME_UNSUPPORTED"
	errPayloadTooLarge = "ERR_CLIPD_PAYLOAD_TOO_LARGE"
	errX11Failed       = "ERR_CLIPD_X11_FAILED"
	errXclipFailed     = "ERR_CLIPD_XCLIP_FAILED"
	errProtocol        = "ERR_CLIPD_PROTOCOL"
)

var (
	imageMIMEs = []string{"image/png", "image/jpeg", "image/webp"}
	copyMIMEs  = []string{"image/png", "image/jpeg", "image/webp", "text/plain"}
	pasteMIMEs = map[string]string{
		"text/plain": "UTF8_STRING",
		"image/png":  "image/png",
		"image/jpeg": "image/jpeg",
		"image/webp": "image/webp",
	}

	errClipMIMEUnsupported = &clipError{code: errMIMEUnsupported}
	errClipPayloadTooLarge = &clipError{code: errPayloadTooLarge}
	errClipX11Failed       = &clipError{code: errX11Failed}
	errClipXclipFailed     = &clipError{code: errXclipFailed}
	errClipProtocol        = &clipError{code: errProtocol}
)

type clipError struct {
	code string
}

func (e *clipError) Error() string {
	return e.code
}

type clipboardRequest struct {
	Op     string `json:"op"`
	MIME   string `json:"mime"`
	Length *int64 `json:"length"`
}

type copyResponse struct {
	OK     bool   `json:"ok"`
	MIME   string `json:"mime"`
	Length int    `json:"length"`
}

type pasteResponse struct {
	OK bool `json:"ok"`
}

type errorResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type clipboardOps interface {
	Copy(context.Context) (string, []byte, *clipError)
	Paste(context.Context, string, []byte) *clipError
}

type x11Clipboard struct {
	display string
}

type clipServer struct {
	mu  sync.Mutex
	ops clipboardOps
}

func main() {
	logger := log.New(os.Stderr, "workspace-clipd: ", 0)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ops := &x11Clipboard{display: os.Getenv("DISPLAY")}
	if err := serve(ctx, defaultSocketPath(), ops, logger); err != nil {
		logger.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func defaultSocketPath() string {
	workspaceDir := strings.TrimSpace(os.Getenv("WW_WORKSPACE_DIR"))
	if workspaceDir == "" {
		workspaceDir = "/workspace"
	}
	return filepath.Join(workspaceDir, "tmp", "clipd.sock")
}

func serve(ctx context.Context, socketPath string, ops clipboardOps, logger *log.Logger) error {
	listener, err := listenUnix(socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socketPath)

	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-stopped:
		}
	}()
	defer close(stopped)

	logger.Printf("listening on %s", socketPath)
	server := &clipServer{ops: ops}
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		server.handleConnection(ctx, conn)
	}
}

func listenUnix(socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		return nil, err
	}

	info, err := os.Lstat(socketPath)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing to remove non-socket path %s", socketPath)
		}
		if err := os.Remove(socketPath); err != nil {
			return nil, err
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return nil, err
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		return nil, err
	}
	return listener, nil
}

func (s *clipServer) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	stopClose := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stopClose:
		}
	}()
	defer close(stopClose)

	reader := bufio.NewReader(conn)
	for {
		line, err := readRequestLine(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			_ = writeClipError(conn, err)
			return
		}

		req, clipErr := parseRequestLine(line)
		if clipErr != nil {
			_ = writeClipError(conn, clipErr)
			return
		}

		switch req.Op {
		case "copy":
			s.mu.Lock()
			mime, payload, clipErr := s.ops.Copy(ctx)
			s.mu.Unlock()
			if clipErr != nil {
				_ = writeClipError(conn, clipErr)
				return
			}
			if int64(len(payload)) > maxPayloadBytes {
				_ = writeClipError(conn, errClipPayloadTooLarge)
				return
			}
			if err := writeJSONLine(conn, copyResponse{OK: true, MIME: mime, Length: len(payload)}); err != nil {
				return
			}
			if err := writeAll(conn, payload); err != nil {
				return
			}

		case "paste":
			payload, err := readPayload(reader, *req.Length)
			if err != nil {
				_ = writeClipError(conn, err)
				return
			}
			s.mu.Lock()
			clipErr := s.ops.Paste(ctx, req.MIME, payload)
			s.mu.Unlock()
			if clipErr != nil {
				_ = writeClipError(conn, clipErr)
				return
			}
			if err := writeJSONLine(conn, pasteResponse{OK: true}); err != nil {
				return
			}
		}
	}
}

func readRequestLine(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, 128)
	for {
		part, err := reader.ReadSlice('\n')
		if int64(len(line)+len(part)) > maxPayloadBytes {
			return nil, errClipPayloadTooLarge
		}
		line = append(line, part...)

		switch {
		case err == nil:
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if len(line) == 0 {
				return nil, io.EOF
			}
			return nil, errClipProtocol
		default:
			return nil, errClipProtocol
		}
	}
}

func parseRequestLine(line []byte) (clipboardRequest, *clipError) {
	if int64(len(line)) > maxPayloadBytes {
		return clipboardRequest{}, errClipPayloadTooLarge
	}
	line = bytes.TrimRight(line, "\r\n")

	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()

	var req clipboardRequest
	if err := decoder.Decode(&req); err != nil {
		return clipboardRequest{}, errClipProtocol
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return clipboardRequest{}, errClipProtocol
	}

	switch req.Op {
	case "copy":
		if req.MIME != "" || req.Length != nil {
			return clipboardRequest{}, errClipProtocol
		}
		return req, nil
	case "paste":
		if req.MIME == "" || req.Length == nil {
			return clipboardRequest{}, errClipProtocol
		}
		if _, ok := pasteMIMEs[req.MIME]; !ok {
			return clipboardRequest{}, errClipMIMEUnsupported
		}
		if *req.Length < 0 {
			return clipboardRequest{}, errClipProtocol
		}
		if *req.Length > maxPayloadBytes {
			return clipboardRequest{}, errClipPayloadTooLarge
		}
		return req, nil
	default:
		return clipboardRequest{}, errClipProtocol
	}
}

func readPayload(reader io.Reader, length int64) ([]byte, error) {
	if length < 0 || length > maxPayloadBytes {
		return nil, errClipPayloadTooLarge
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, errClipProtocol
	}
	return payload, nil
}

func (c *x11Clipboard) Copy(ctx context.Context) (string, []byte, *clipError) {
	if err := runXDoTool(ctx, c.display, "ctrl+c"); err != nil {
		return "", nil, errClipX11Failed
	}
	targets, owned := clipboardTargets(ctx, c.display)
	return copyFromClipboard(targets, owned, func(mime string) ([]byte, error) {
		readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return runCommandOutputLimited(readCtx, c.display, "xclip", "-selection", "clipboard", "-o", "-t", mime)
	})
}

// copyFromClipboard reads the owned CLIPBOARD selection. An unowned selection is
// a normal empty clipboard rather than a failure: the caller must leave the
// local clipboard untouched instead of reporting a gateway error, which is what
// pressing Ctrl+C without a selection does.
func copyFromClipboard(targets []string, owned bool, read func(string) ([]byte, error)) (string, []byte, *clipError) {
	if !owned {
		return "text/plain", []byte{}, nil
	}
	return selectCopyMIMEWithCandidates(copyCandidatesForTargets(targets), read)
}

// clipboardTargets reports the advertised ICCCM targets and whether the
// CLIPBOARD selection has an owner at all. A failed TARGETS probe means nothing
// currently owns the selection.
func clipboardTargets(ctx context.Context, display string) ([]string, bool) {
	readCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	payload, err := runCommandOutputLimited(readCtx, display, "xclip", "-selection", "clipboard", "-o", "-t", "TARGETS")
	if err != nil {
		return nil, false
	}
	targets := strings.Fields(string(payload))
	return targets, len(targets) > 0
}

func copyCandidatesForTargets(targets []string) []string {
	if len(targets) == 0 {
		return copyMIMEs
	}

	available := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		available[target] = struct{}{}
	}

	candidates := make([]string, 0, len(copyMIMEs))
	for _, mime := range imageMIMEs {
		if _, ok := available[mime]; ok {
			candidates = append(candidates, mime)
		}
	}
	for _, textTarget := range []string{"text/plain", "UTF8_STRING", "TEXT", "STRING"} {
		if _, ok := available[textTarget]; ok {
			candidates = append(candidates, "text/plain")
			break
		}
	}
	if len(candidates) == 0 {
		return copyMIMEs
	}
	return candidates
}

func selectCopyMIME(read func(string) ([]byte, error)) (string, []byte, *clipError) {
	return selectCopyMIMEWithCandidates(copyMIMEs, read)
}

func selectCopyMIMEWithCandidates(candidates []string, read func(string) ([]byte, error)) (string, []byte, *clipError) {
	for _, mime := range candidates {
		payload, err := read(mime)
		if err != nil {
			var clipErr *clipError
			if errors.As(err, &clipErr) && clipErr.code == errPayloadTooLarge {
				return "", nil, clipErr
			}
			continue
		}
		if len(payload) == 0 {
			continue
		}
		if int64(len(payload)) > maxPayloadBytes {
			return "", nil, errClipPayloadTooLarge
		}
		return mime, payload, nil
	}
	return "", nil, errClipXclipFailed
}

func (c *x11Clipboard) Paste(ctx context.Context, mime string, payload []byte) *clipError {
	target, ok := pasteMIMEs[mime]
	if !ok {
		return errClipMIMEUnsupported
	}
	if int64(len(payload)) > maxPayloadBytes {
		return errClipPayloadTooLarge
	}
	cmd, err := startClipboard(ctx, c.display, target, payload)
	if err != nil {
		return errClipXclipFailed
	}
	if err := runXDoTool(ctx, c.display, "ctrl+v"); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return errClipX11Failed
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

func pasteTarget(mime string) (string, bool) {
	target, ok := pasteMIMEs[mime]
	return target, ok
}

func startClipboard(ctx context.Context, display, target string, payload []byte) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-i", "-t", target)
	cmd.Env = displayEnv(display)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	for i := 0; i < 20; i++ {
		probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		output, err := runCommandOutputLimited(probeCtx, display, "xclip", "-selection", "clipboard", "-o", "-t", target)
		cancel()
		if err == nil && (len(payload) == 0 || bytes.Equal(output, payload)) {
			return cmd, nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return nil, errors.New("clipboard did not become readable")
}

func runXDoTool(ctx context.Context, display, key string) error {
	cmd := exec.CommandContext(ctx, "xdotool", "key", "--clearmodifiers", key)
	cmd.Env = displayEnv(display)
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func runCommandOutputLimited(ctx context.Context, display, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = displayEnv(display)
	cmd.Stderr = io.Discard

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errClipXclipFailed
	}
	if err := cmd.Start(); err != nil {
		return nil, errClipXclipFailed
	}

	payload, readErr := io.ReadAll(io.LimitReader(stdout, maxPayloadBytes+1))
	if int64(len(payload)) > maxPayloadBytes {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, errClipPayloadTooLarge
	}
	waitErr := cmd.Wait()
	if readErr != nil || waitErr != nil {
		return nil, errClipXclipFailed
	}
	return payload, nil
}

func displayEnv(display string) []string {
	env := os.Environ()
	if display != "" {
		env = append(env, "DISPLAY="+display)
	}
	return env
}

func writeClipError(writer io.Writer, err error) error {
	code := errProtocol
	var clipErr *clipError
	if errors.As(err, &clipErr) {
		code = clipErr.code
	}
	return writeJSONLine(writer, errorResponse{OK: false, Error: code})
}

func writeJSONLine(writer io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := writeAll(writer, payload); err != nil {
		return err
	}
	return writeAll(writer, []byte{'\n'})
}

func writeAll(writer io.Writer, payload []byte) error {
	for len(payload) > 0 {
		n, err := writer.Write(payload)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		payload = payload[n:]
	}
	return nil
}
