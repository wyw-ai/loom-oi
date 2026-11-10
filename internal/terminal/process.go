package terminal

import (
	"context"
	"io"
	"os"
	"os/exec"

	"github.com/wyw-ai/loom-oi/internal/procgroup"
)

type process struct {
	output     *os.File
	input      *os.File
	wait       func() (int, int, error)
	terminate  func() error
	closeExtra func()
}

func (p *process) closeOutput() {
	_ = p.output.Close()
	if p.closeExtra != nil {
		p.closeExtra()
	}
}
func (p *process) write(ctx context.Context, b []byte) (int, error) {
	if p.input == nil {
		return 0, io.ErrClosedPipe
	}
	return writeTerminal(ctx, p.input, b)
}
func launchPipe(cmd *exec.Cmd) (*process, error) {
	r, w, e := os.Pipe()
	if e != nil {
		return nil, e
	}
	cmd.Stdout = w
	cmd.Stderr = w // The same OS pipe preserves observed write order.
	group, e := procgroup.New()
	if e != nil {
		r.Close()
		w.Close()
		return nil, e
	}
	if e = group.Start(cmd); e != nil {
		group.Close()
		r.Close()
		w.Close()
		return nil, e
	}
	w.Close()
	p := &process{output: r, terminate: group.Close}
	p.wait = func() (int, int, error) {
		err := cmd.Wait()
		code, sig := exitStatus(cmd, err)
		_ = group.Close()
		return code, sig, err
	}
	return p, nil
}
