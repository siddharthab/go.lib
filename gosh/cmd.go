// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gosh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	errAlreadyCalledStart = errors.New("gosh: already called Cmd.Start")
	errAlreadyCalledWait  = errors.New("gosh: already called Cmd.Wait")
	errCloseStdout        = errors.New("gosh: use NopWriteCloser(os.Stdout) to prevent stdout from being closed")
	errCloseStderr        = errors.New("gosh: use NopWriteCloser(os.Stderr) to prevent stderr from being closed")
	errDidNotCallStart    = errors.New("gosh: did not call Cmd.Start")
	errProcessExited      = errors.New("gosh: process exited")
)

// Cmd represents a command. Not thread-safe.
// Public fields should not be modified after calling Start.
type Cmd struct {
	// Err is the most recent error from this Cmd (may be nil).
	Err error
	// Path is the path of the command to run.
	Path string
	// Vars is the map of env vars for this Cmd.
	Vars map[string]string
	// Args is the list of args for this Cmd, starting with the resolved path.
	// Note, we set Args[0] to the resolved path (rather than the user-specified
	// name) so that a command started by Shell can reliably determine the path to
	// its executable.
	Args []string
	// IgnoreParentExit, if true, makes it so the child process does not exit when
	// its parent exits. Only takes effect if the child process was spawned via
	// Shell.FuncCmd or explicitly calls InitChildMain.
	IgnoreParentExit bool
	// ExitAfter, if non-zero, specifies that the child process should exit after
	// the given duration has elapsed. Only takes effect if the child process was
	// spawned via Shell.FuncCmd or explicitly calls InitChildMain.
	ExitAfter time.Duration
	// PropagateOutput is inherited from Shell.Opts.PropagateChildOutput.
	PropagateOutput bool
	// OutputDir is inherited from Shell.Opts.ChildOutputDir.
	OutputDir string
	// ExitErrorIsOk specifies whether an *exec.ExitError should be reported via
	// Shell.HandleError.
	ExitErrorIsOk bool
	// Stdin is a string to write to the child's stdin.
	Stdin string
	// Internal state.
	sh               *Shell
	c                *exec.Cmd
	stdinWriteCloser io.WriteCloser // from exec.Cmd.StdinPipe
	calledStart      bool
	calledWait       bool
	cond             *sync.Cond
	waitChan         chan error
	started          bool // protected by sh.cleanupMu
	exited           bool // protected by cond.L
	stdoutWriters    []io.Writer
	stderrWriters    []io.Writer
	closers          []io.Closer
	recvReady        bool              // protected by cond.L
	recvVars         map[string]string // protected by cond.L
}

// Clone returns a new Cmd with a copy of this Cmd's configuration.
func (c *Cmd) Clone() *Cmd {
	c.sh.Ok()
	res, err := c.clone()
	c.handleError(err)
	return res
}

// StdinPipe returns a thread-safe WriteCloser backed by a buffered pipe for the
// command's stdin. The returned pipe will be closed when the process exits, but
// may also be closed earlier by the caller, e.g. if the command does not exit
// until its stdin is closed. Must be called before Start. It is safe to call
// StdinPipe multiple times; calls after the first return the pipe created by
// the first call.
func (c *Cmd) StdinPipe() io.WriteCloser {
	c.sh.Ok()
	res, err := c.stdinPipe()
	c.handleError(err)
	return res
}

// StdoutPipe returns a Reader backed by a buffered pipe for the command's
// stdout. Must be called before Start. May be called more than once; each
// invocation creates a new pipe.
func (c *Cmd) StdoutPipe() io.Reader {
	c.sh.Ok()
	res, err := c.stdoutPipe()
	c.handleError(err)
	return res
}

// StderrPipe returns a Reader backed by a buffered pipe for the command's
// stderr. Must be called before Start. May be called more than once; each
// invocation creates a new pipe.
func (c *Cmd) StderrPipe() io.Reader {
	c.sh.Ok()
	res, err := c.stderrPipe()
	c.handleError(err)
	return res
}

// AddStdoutWriter configures this Cmd to tee the child's stdout to the given
// WriteCloser, which will be closed when the process exits.
//
// If the same WriteCloser is passed to both AddStdoutWriter and
// AddStderrWriter, Cmd will ensure that its methods are never called
// concurrently and that Close is only called once.
//
// Use NopWriteCloser to extend a Writer to a WriteCloser, or to prevent an
// existing WriteCloser from being closed. It is an error to pass in os.Stdout
// or os.Stderr, since they shouldn't be closed.
func (c *Cmd) AddStdoutWriter(wc io.WriteCloser) {
	c.sh.Ok()
	c.handleError(c.addStdoutWriter(wc))
}

// AddStderrWriter configures this Cmd to tee the child's stderr to the given
// WriteCloser, which will be closed when the process exits.
//
// If the same WriteCloser is passed to both AddStdoutWriter and
// AddStderrWriter, Cmd will ensure that its methods are never called
// concurrently and that Close is only called once.
//
// Use NopWriteCloser to extend a Writer to a WriteCloser, or to prevent an
// existing WriteCloser from being closed. It is an error to pass in os.Stdout
// or os.Stderr, since they shouldn't be closed.
func (c *Cmd) AddStderrWriter(wc io.WriteCloser) {
	c.sh.Ok()
	c.handleError(c.addStderrWriter(wc))
}

// Start starts the command.
func (c *Cmd) Start() {
	c.sh.Ok()
	c.handleError(c.start())
}

// AwaitReady waits for the child process to call SendReady. Must not be called
// before Start or after Wait.
func (c *Cmd) AwaitReady() {
	c.sh.Ok()
	c.handleError(c.awaitReady())
}

// AwaitVars waits for the child process to send values for the given vars
// (using SendVars). Must not be called before Start or after Wait.
func (c *Cmd) AwaitVars(keys ...string) map[string]string {
	c.sh.Ok()
	res, err := c.awaitVars(keys...)
	c.handleError(err)
	return res
}

// Wait waits for the command to exit.
func (c *Cmd) Wait() {
	c.sh.Ok()
	c.handleError(c.wait())
}

// Signal sends a signal to the process.
func (c *Cmd) Signal(sig os.Signal) {
	c.sh.Ok()
	c.handleError(c.signal(sig))
}

// Terminate sends a signal to the process, then waits for it to exit. Terminate
// is different from Signal followed by Wait: Terminate succeeds as long as the
// process exits, whereas Wait fails if the exit code isn't 0.
func (c *Cmd) Terminate(sig os.Signal) {
	c.sh.Ok()
	c.handleError(c.terminate(sig))
}

// Run calls Start followed by Wait.
func (c *Cmd) Run() {
	c.sh.Ok()
	c.handleError(c.run())
}

// Stdout calls Start followed by Wait, then returns the command's stdout.
func (c *Cmd) Stdout() string {
	c.sh.Ok()
	res, err := c.stdout()
	c.handleError(err)
	return res
}

// StdoutStderr calls Start followed by Wait, then returns the command's stdout
// and stderr.
func (c *Cmd) StdoutStderr() (string, string) {
	c.sh.Ok()
	stdout, stderr, err := c.stdoutStderr()
	c.handleError(err)
	return stdout, stderr
}

// CombinedOutput calls Start followed by Wait, then returns the command's
// combined stdout and stderr.
func (c *Cmd) CombinedOutput() string {
	c.sh.Ok()
	res, err := c.combinedOutput()
	c.handleError(err)
	return res
}

// Pid returns the command's PID, or -1 if the command has not been started.
func (c *Cmd) Pid() int {
	if !c.started {
		return -1
	}
	return c.c.Process.Pid
}

////////////////////////////////////////
// Internals

func newCmdInternal(sh *Shell, vars map[string]string, path string, args []string) (*Cmd, error) {
	c := &Cmd{
		Path:     path,
		Vars:     vars,
		Args:     append([]string{path}, args...),
		sh:       sh,
		c:        &exec.Cmd{},
		cond:     sync.NewCond(&sync.Mutex{}),
		waitChan: make(chan error, 1),
		recvVars: map[string]string{},
	}
	// Protect against concurrent signal-triggered Shell.cleanup().
	sh.cleanupMu.Lock()
	defer sh.cleanupMu.Unlock()
	if sh.calledCleanup {
		return nil, errAlreadyCalledCleanup
	}
	sh.cmds = append(sh.cmds, c)
	return c, nil
}

func newCmd(sh *Shell, vars map[string]string, name string, args ...string) (*Cmd, error) {
	// Mimics https://golang.org/src/os/exec/exec.go Command.
	if filepath.Base(name) == name {
		if lp, err := exec.LookPath(name); err != nil {
			return nil, err
		} else {
			name = lp
		}
	}
	return newCmdInternal(sh, vars, name, args)
}

func (c *Cmd) errorIsOk(err error) bool {
	if c.ExitErrorIsOk {
		if _, ok := err.(*exec.ExitError); ok {
			return true
		}
	}
	return err == nil
}

func (c *Cmd) handleError(err error) {
	c.Err = err
	if !c.errorIsOk(err) {
		c.sh.HandleError(err)
	}
}

func (c *Cmd) closeClosers() {
	// If the same WriteCloser was passed to both AddStdoutWriter and
	// AddStderrWriter, we should only close it once.
	cm := map[io.Closer]bool{}
	for _, c := range c.closers {
		if !cm[c] {
			cm[c] = true
			c.Close() // best-effort; ignore returned error
		}
	}
}

func (c *Cmd) isRunning() bool {
	if !c.started {
		return false
	}
	c.cond.L.Lock()
	defer c.cond.L.Unlock()
	return !c.exited
}

// recvWriter listens for gosh messages from a child process.
type recvWriter struct {
	c          *Cmd
	buf        bytes.Buffer
	readPrefix bool // if true, we've read len(msgPrefix) for the current line
	skipLine   bool // if true, ignore bytes until next '\n'
}

func (w *recvWriter) Write(p []byte) (n int, err error) {
	for _, b := range p {
		if b == '\n' {
			if w.readPrefix && !w.skipLine {
				m := msg{}
				if err := json.Unmarshal(w.buf.Bytes(), &m); err != nil {
					return 0, err
				}
				switch m.Type {
				case typeReady:
					w.c.cond.L.Lock()
					w.c.recvReady = true
					w.c.cond.Signal()
					w.c.cond.L.Unlock()
				case typeVars:
					w.c.cond.L.Lock()
					w.c.recvVars = mergeMaps(w.c.recvVars, m.Vars)
					w.c.cond.Signal()
					w.c.cond.L.Unlock()
				default:
					return 0, fmt.Errorf("unknown message type: %q", m.Type)
				}
			}
			// Reset state for next line.
			w.readPrefix, w.skipLine = false, false
			w.buf.Reset()
		} else if !w.skipLine {
			w.buf.WriteByte(b)
			if !w.readPrefix && w.buf.Len() == len(msgPrefix) {
				w.readPrefix = true
				prefix := string(w.buf.Next(len(msgPrefix)))
				if prefix != msgPrefix {
					w.skipLine = true
				}
			}
		}
	}
	return len(p), nil
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	n, err := w.w.Write(p)
	w.mu.Unlock()
	return n, err
}

func (c *Cmd) makeStdoutStderr() (io.Writer, io.Writer, error) {
	c.stdoutWriters = append(c.stdoutWriters, &recvWriter{c: c})
	if c.PropagateOutput {
		c.stdoutWriters = append(c.stdoutWriters, os.Stdout)
		c.stderrWriters = append(c.stderrWriters, os.Stderr)
	}
	if c.OutputDir != "" {
		t := time.Now().Format("20060102.150405.000000")
		name := filepath.Join(c.OutputDir, filepath.Base(c.Path)+"."+t)
		const flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
		switch file, err := os.OpenFile(name+".stdout", flags, 0600); {
		case err != nil:
			return nil, nil, err
		default:
			c.stdoutWriters = append(c.stdoutWriters, file)
			c.closers = append(c.closers, file)
		}
		switch file, err := os.OpenFile(name+".stderr", flags, 0600); {
		case err != nil:
			return nil, nil, err
		default:
			c.stderrWriters = append(c.stderrWriters, file)
			c.closers = append(c.closers, file)
		}
	}
	switch hasOut, hasErr := len(c.stdoutWriters) > 0, len(c.stderrWriters) > 0; {
	case hasOut && hasErr:
		// Make writes synchronous between stdout and stderr. This ensures all
		// writers that capture both will see the same ordering, and don't need to
		// worry about concurrent writes.
		sharedMu := &sync.Mutex{}
		stdout := lockedWriter{sharedMu, io.MultiWriter(c.stdoutWriters...)}
		stderr := lockedWriter{sharedMu, io.MultiWriter(c.stderrWriters...)}
		return stdout, stderr, nil
	case hasOut:
		return io.MultiWriter(c.stdoutWriters...), nil, nil
	case hasErr:
		return nil, io.MultiWriter(c.stderrWriters...), nil
	}
	return nil, nil, nil
}

func (c *Cmd) clone() (*Cmd, error) {
	args := make([]string, len(c.Args))
	copy(args, c.Args)
	res, err := newCmdInternal(c.sh, copyMap(c.Vars), c.Path, args[1:])
	if err != nil {
		return nil, err
	}
	res.IgnoreParentExit = c.IgnoreParentExit
	res.ExitAfter = c.ExitAfter
	res.PropagateOutput = c.PropagateOutput
	res.OutputDir = c.OutputDir
	res.ExitErrorIsOk = c.ExitErrorIsOk
	res.Stdin = c.Stdin
	return res, nil
}

func (c *Cmd) stdinPipe() (io.WriteCloser, error) {
	if c.calledStart {
		return nil, errAlreadyCalledStart
	}
	if c.stdinWriteCloser != nil {
		return c.stdinWriteCloser, nil
	}
	var err error
	c.stdinWriteCloser, err = c.c.StdinPipe()
	return c.stdinWriteCloser, err
}

func (c *Cmd) stdoutPipe() (io.Reader, error) {
	if c.calledStart {
		return nil, errAlreadyCalledStart
	}
	p := NewBufferedPipe()
	c.stdoutWriters = append(c.stdoutWriters, p)
	c.closers = append(c.closers, p)
	return p, nil
}

func (c *Cmd) stderrPipe() (io.Reader, error) {
	if c.calledStart {
		return nil, errAlreadyCalledStart
	}
	p := NewBufferedPipe()
	c.stderrWriters = append(c.stderrWriters, p)
	c.closers = append(c.closers, p)
	return p, nil
}

func (c *Cmd) addStdoutWriter(wc io.WriteCloser) error {
	switch {
	case c.calledStart:
		return errAlreadyCalledStart
	case wc == os.Stdout:
		return errCloseStdout
	case wc == os.Stderr:
		return errCloseStderr
	}
	c.stdoutWriters = append(c.stdoutWriters, wc)
	c.closers = append(c.closers, wc)
	return nil
}

func (c *Cmd) addStderrWriter(wc io.WriteCloser) error {
	switch {
	case c.calledStart:
		return errAlreadyCalledStart
	case wc == os.Stdout:
		return errCloseStdout
	case wc == os.Stderr:
		return errCloseStderr
	}
	c.stderrWriters = append(c.stderrWriters, wc)
	c.closers = append(c.closers, wc)
	return nil
}

// TODO(sadovsky): Maybe wrap every child process with a "supervisor" process
// that calls InitChildMain.

func (c *Cmd) start() error {
	defer func() {
		if !c.started {
			c.closeClosers()
		}
	}()
	if c.calledStart {
		return errAlreadyCalledStart
	}
	c.calledStart = true
	// Protect against Cmd.start() writing to c.c.Process concurrently with
	// signal-triggered Shell.cleanup() reading from it.
	c.sh.cleanupMu.Lock()
	defer c.sh.cleanupMu.Unlock()
	if c.sh.calledCleanup {
		return errAlreadyCalledCleanup
	}
	// Configure the command.
	c.c.Path = c.Path
	vars := copyMap(c.Vars)
	if c.IgnoreParentExit {
		delete(vars, envWatchParent)
	} else {
		vars[envWatchParent] = "1"
	}
	if c.ExitAfter == 0 {
		delete(vars, envExitAfter)
	} else {
		vars[envExitAfter] = c.ExitAfter.String()
	}
	c.c.Env = mapToSlice(vars)
	c.c.Args = c.Args
	if c.Stdin != "" {
		if c.stdinWriteCloser != nil {
			return errors.New("gosh: cannot both set Stdin and call StdinPipe")
		}
		c.c.Stdin = strings.NewReader(c.Stdin)
	}
	var err error
	if c.c.Stdout, c.c.Stderr, err = c.makeStdoutStderr(); err != nil {
		return err
	}
	// Start the command.
	if err = c.c.Start(); err != nil {
		return err
	}
	c.started = true
	c.startExitWaiter()
	return nil
}

// startExitWaiter spawns a goroutine that calls exec.Cmd.Wait, waiting for the
// process to exit. Calling exec.Cmd.Wait here rather than in gosh.Cmd.Wait
// ensures that the child process is reaped once it exits. Note, gosh.Cmd.wait
// blocks on waitChan.
func (c *Cmd) startExitWaiter() {
	go func() {
		waitErr := c.c.Wait()
		c.cond.L.Lock()
		c.exited = true
		c.cond.Signal()
		c.cond.L.Unlock()
		c.closeClosers()
		c.waitChan <- waitErr
	}()
}

// TODO(sadovsky): Maybe add optional timeouts for
// Cmd.{awaitReady,awaitVars,wait}.

func (c *Cmd) awaitReady() error {
	if !c.started {
		return errDidNotCallStart
	} else if c.calledWait {
		return errAlreadyCalledWait
	}
	c.cond.L.Lock()
	defer c.cond.L.Unlock()
	for !c.exited && !c.recvReady {
		c.cond.Wait()
	}
	// Return nil error if both conditions triggered simultaneously.
	if !c.recvReady {
		return errProcessExited
	}
	return nil
}

func (c *Cmd) awaitVars(keys ...string) (map[string]string, error) {
	if !c.started {
		return nil, errDidNotCallStart
	} else if c.calledWait {
		return nil, errAlreadyCalledWait
	}
	wantKeys := map[string]bool{}
	for _, key := range keys {
		wantKeys[key] = true
	}
	res := map[string]string{}
	updateRes := func() {
		for k, v := range c.recvVars {
			if _, ok := wantKeys[k]; ok {
				res[k] = v
			}
		}
	}
	c.cond.L.Lock()
	defer c.cond.L.Unlock()
	updateRes()
	for !c.exited && len(res) < len(wantKeys) {
		c.cond.Wait()
		updateRes()
	}
	// Return nil error if both conditions triggered simultaneously.
	if len(res) < len(wantKeys) {
		return nil, errProcessExited
	}
	return res, nil
}

func (c *Cmd) wait() error {
	if !c.started {
		return errDidNotCallStart
	} else if c.calledWait {
		return errAlreadyCalledWait
	}
	c.calledWait = true
	return <-c.waitChan
}

// Note: We check for this particular error message to handle the unavoidable
// race between sending a signal to a process and the process exiting.
// https://golang.org/src/os/exec_unix.go
// https://golang.org/src/os/exec_windows.go
const errFinished = "os: process already finished"

// NOTE(sadovsky): Technically speaking, Process.Signal(os.Kill) is different
// from Process.Kill. Currently, gosh.Cmd does not provide a way to trigger
// Process.Kill. If it proves necessary, we'll add a "gosh.Kill" implementation
// of the os.Signal interface, and have the signal and terminate methods map
// that to Process.Kill.
func (c *Cmd) signal(sig os.Signal) error {
	if !c.started {
		return errDidNotCallStart
	} else if c.calledWait {
		return errAlreadyCalledWait
	}
	if !c.isRunning() {
		return nil
	}
	if err := c.c.Process.Signal(sig); err != nil && err.Error() != errFinished {
		return err
	}
	return nil
}

func (c *Cmd) terminate(sig os.Signal) error {
	if err := c.signal(sig); err != nil {
		return err
	}
	if err := c.wait(); err != nil {
		// Succeed as long as the process exited, regardless of the exit code.
		if _, ok := err.(*exec.ExitError); !ok {
			return err
		}
	}
	return nil
}

func (c *Cmd) run() error {
	if err := c.start(); err != nil {
		return err
	}
	return c.wait()
}

func (c *Cmd) stdout() (string, error) {
	if c.calledStart {
		return "", errAlreadyCalledStart
	}
	var stdout bytes.Buffer
	c.stdoutWriters = append(c.stdoutWriters, &stdout)
	err := c.run()
	return stdout.String(), err
}

func (c *Cmd) stdoutStderr() (string, string, error) {
	if c.calledStart {
		return "", "", errAlreadyCalledStart
	}
	var stdout, stderr bytes.Buffer
	c.stdoutWriters = append(c.stdoutWriters, &stdout)
	c.stderrWriters = append(c.stderrWriters, &stderr)
	err := c.run()
	return stdout.String(), stderr.String(), err
}

func (c *Cmd) combinedOutput() (string, error) {
	if c.calledStart {
		return "", errAlreadyCalledStart
	}
	var output bytes.Buffer
	c.stdoutWriters = append(c.stdoutWriters, &output)
	c.stderrWriters = append(c.stderrWriters, &output)
	err := c.run()
	return output.String(), err
}