// Package host serves an SCPI engine on the LAN transports every scope
// offers: VXI-11 (vxi11srv, registered with the portmapper), the raw socket
// (5025) and the telnet line interface (5024). Each session's replies go
// back to that session; the engine runs one message at a time.
package host

import (
	"bufio"
	"context"
	"errors"
	"net"
	"sync"

	"github.com/labeth/open-dso-core/vxi11srv"
)

// Exec runs one program message and returns its reply bytes.
type Exec func(msg []byte) []byte

// Config selects the transports. An empty address leaves a transport off.
type Config struct {
	Raw    string // raw socket address, e.g. ":5025"
	Telnet string // telnet line interface address, e.g. ":5024"
	Banner string // the telnet greeting, sent on accept
	VXI11  bool   // serve VXI-11
	// Register registers VXI-11 with the system portmapper (off in tests).
	Register bool
}

// Servers are the running transports.
type Servers struct {
	VXI11       *vxi11srv.Server
	VXI11Port   int
	Raw, Telnet net.Addr
	lns         []net.Listener
}

// Close stops every transport.
// TRLC-LINKS: REQ-DSO-024
func (s *Servers) Close() {
	for _, ln := range s.lns {
		_ = ln.Close()
	}
	if s.VXI11 != nil {
		_ = s.VXI11.Close()
	}
}

// Serve starts the transports cfg names; they run until ctx ends. A
// transport that cannot start is logged and the others still run; the
// error joins every failure.
// TRLC-LINKS: REQ-DSO-024
func Serve(ctx context.Context, exec Exec, cfg Config, logf func(string, ...any)) (*Servers, error) {
	s := &Servers{}
	var errs []error
	if cfg.VXI11 {
		srv, port, err := vxi11srv.Start(vxi11srv.Handler(exec), cfg.Register, logf)
		if err != nil {
			errs = append(errs, err)
			logf("scpi: VXI-11: %v", err)
		} else {
			s.VXI11, s.VXI11Port = srv, port
			logf("scpi: VXI-11 on tcp/%d", port)
		}
	}
	listen := func(addr, name string, serve func(net.Listener)) net.Addr {
		if addr == "" {
			return nil
		}
		var lc net.ListenConfig
		ln, err := lc.Listen(ctx, "tcp", addr)
		if err != nil {
			errs = append(errs, err)
			logf("scpi: %s: %v", name, err)
			return nil
		}
		s.lns = append(s.lns, ln)
		logf("scpi: %s on %s", name, ln.Addr())
		go serve(ln)
		return ln.Addr()
	}
	s.Raw = listen(cfg.Raw, "raw socket", func(ln net.Listener) { acceptRaw(ln, exec, logf) })
	tel := &telnet{exec: exec, banner: cfg.Banner}
	s.Telnet = listen(cfg.Telnet, "telnet", func(ln net.Listener) { tel.accept(ln, logf) })
	go func() {
		<-ctx.Done()
		s.Close()
	}()
	return s, errors.Join(errs...)
}

// acceptRaw serves raw-socket sessions, several at once.
// TRLC-LINKS: REQ-DSO-024
func acceptRaw(ln net.Listener, exec Exec, logf func(string, ...any)) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				logf("scpi: raw socket: %v", err)
			}
			return
		}
		go serveLines(conn, exec)
	}
}

// serveLines runs one session: every LF ends a program message, whose
// reply is sent as the engine wrote it (no extra framing, binary blocks
// raw). It returns when the peer closes.
// TRLC-LINKS: REQ-DSO-024
func serveLines(conn net.Conn, exec Exec) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReaderSize(conn, 64<<10)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			msg := line
			if msg[len(msg)-1] == '\n' {
				msg = msg[:len(msg)-1]
			}
			if out := exec(msg); len(out) > 0 {
				if _, werr := conn.Write(out); werr != nil {
					return
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// telnet is the line interface: one session at a time (a new connection
// closes the previous one), a greeting on accept, then lines as on the raw
// socket. It does not echo, sends no NULs and no prompt after a line: a
// client sending whole lines reads each reply as on the raw socket.
type telnet struct {
	exec   Exec
	banner string
	mu     sync.Mutex
	cur    net.Conn
}

// TRLC-LINKS: REQ-DSO-024
func (t *telnet) accept(ln net.Listener, logf func(string, ...any)) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				logf("scpi: telnet: %v", err)
			}
			return
		}
		t.mu.Lock()
		if t.cur != nil {
			_ = t.cur.Close()
		}
		t.cur = conn
		t.mu.Unlock()
		if t.banner != "" {
			if _, err := conn.Write([]byte(t.banner + "\r\n>>")); err != nil {
				_ = conn.Close()
				continue
			}
		}
		go serveLines(conn, t.exec)
	}
}
