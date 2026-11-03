package integration

import (
	"io"
	"net"
	"sync"
)

// Proxy modes.
const (
	pass   = iota // forward new connections
	hold          // accept new connections but forward only after release
	refuse        // close new connections at once
)

// proxy is a TCP relay that can cut chosen connections, hold or refuse
// new ones, and retarget (OIS restarts, PostgreSQL outages, control-only
// cuts: TV-B1-05/11/14, TV-B2-10, TV-B3-04/05/06).
type proxy struct {
	ln      net.Listener
	mu      sync.Mutex
	target  string
	mode    int
	conns   []*pconn
	pending []net.Conn
}

type pconn struct {
	a, b net.Conn
}

func (c *pconn) cut() {
	_ = c.a.Close()
	if c.b != nil {
		_ = c.b.Close()
	}
}

func newProxy(target string) (*proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &proxy{ln: ln, target: target}
	go p.accept()
	return p, nil
}

func (p *proxy) addr() string { return p.ln.Addr().String() }

func (p *proxy) close() {
	_ = p.ln.Close()
	p.cutAll()
}

func (p *proxy) accept() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		switch p.mode {
		case refuse:
			_ = c.Close()
		case hold:
			p.pending = append(p.pending, c)
		default:
			p.forward(c)
		}
		p.mu.Unlock()
	}
}

// forward relays c to the target. Caller holds p.mu.
func (p *proxy) forward(c net.Conn) {
	pc := &pconn{a: c}
	p.conns = append(p.conns, pc)
	target := p.target
	go func() {
		s, err := net.Dial("tcp", target)
		if err != nil {
			_ = c.Close()
			return
		}
		p.mu.Lock()
		pc.b = s
		p.mu.Unlock()
		go func() {
			_, _ = io.Copy(s, c)
			_ = s.Close()
		}()
		_, _ = io.Copy(c, s)
		_ = c.Close()
	}()
}

func (p *proxy) setMode(m int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mode = m
	if m == pass {
		for _, c := range p.pending {
			p.forward(c)
		}
		p.pending = nil
	}
}

func (p *proxy) setTarget(t string) {
	p.mu.Lock()
	p.target = t
	p.mu.Unlock()
}

// count is the number of forwarded connections so far.
func (p *proxy) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.conns)
}

func (p *proxy) heldCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pending)
}

// cut closes forwarded connection i (0 = the first one).
func (p *proxy) cut(i int) {
	p.mu.Lock()
	c := p.conns[i]
	p.mu.Unlock()
	c.cut()
}

func (p *proxy) cutAll() {
	p.mu.Lock()
	conns := append([]*pconn(nil), p.conns...)
	pending := p.pending
	p.pending = nil
	p.mu.Unlock()
	for _, c := range conns {
		c.cut()
	}
	for _, c := range pending {
		_ = c.Close()
	}
}
