package host

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func echo(msg []byte) []byte {
	if m := strings.TrimRight(string(msg), "\r"); strings.HasSuffix(m, "?") {
		return []byte("R:" + m + "\n")
	}
	return nil
}

// Raw sessions get their own replies; telnet greets and keeps one session.
func TestTransports(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Serve(ctx, echo, Config{Raw: "127.0.0.1:0", Telnet: "127.0.0.1:0", Banner: "Welcome to the SCPI instrument 'M X'"}, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	dial := func(a net.Addr) (net.Conn, *bufio.Reader) {
		c, err := net.Dial("tcp", a.String())
		if err != nil {
			t.Fatal(err)
		}
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		return c, bufio.NewReader(c)
	}
	a, ar := dial(s.Raw)
	b, br := dial(s.Raw)
	_, _ = a.Write([]byte("SET\nA?\n"))
	_, _ = b.Write([]byte("B?\r\n"))
	if l, _ := ar.ReadString('\n'); l != "R:A?\n" {
		t.Fatalf("a: %q", l)
	}
	if l, _ := br.ReadString('\n'); l != "R:B?\n" {
		t.Fatalf("b: %q", l)
	}
	tc, tr := dial(s.Telnet)
	banner := make([]byte, len("Welcome to the SCPI instrument 'M X'\r\n>>"))
	if _, err := io(tr, banner); err != nil || string(banner) != "Welcome to the SCPI instrument 'M X'\r\n>>" {
		t.Fatalf("banner %q %v", banner, err)
	}
	_, _ = tc.Write([]byte("T?\n"))
	if l, _ := tr.ReadString('\n'); l != "R:T?\n" {
		t.Fatalf("telnet: %q", l)
	}
	_, tr2 := dial(s.Telnet)
	if _, err := tr2.ReadByte(); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.ReadByte(); err == nil {
		t.Fatal("the first telnet session stayed open")
	}
	s.Close()
}

func io(r *bufio.Reader, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := r.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
