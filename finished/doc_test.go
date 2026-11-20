package main

import (
	"bytes"
	"encoding/hex"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	koloid "github.com/ciranda-dev/kolo-id"
	kolosecure "github.com/ciranda-dev/kolo-secure"
)

// TestDoc runs an owner and a writer on 127.0.0.1: the same lines in the same order on both, a removed writer whose
// later line the owner never shows and who gets no new line, and an owner that finds its document again after a restart.
func TestDoc(t *testing.T) {
	ctx := t.Context()
	addr := freeAddr(t)
	ana, bruno := identity(t), identity(t)
	anaDir, brunoDir := t.TempDir(), t.TempDir()

	a, err := openDoc(ctx, ana, anaDir, addr, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := make(chan *Doc, 1)
	go func() {
		b, err := openDoc(ctx, bruno, brunoDir, "", addr, ana.PublicKey())
		if err != nil {
			t.Error(err)
		}
		joined <- b
	}()
	do(t, a, "add", hex.EncodeToString(bruno.PublicKey()))
	b := <-joined
	if b == nil {
		t.FailNow()
	}
	defer b.Close()

	do(t, a, "say", "Hello from Ana")
	do(t, b, "say", "Hello from Bruno")
	waitFor(t, func() bool {
		sa, sb := show(a), show(b)
		return strings.Count(sa, "(settled)") == 2 && sa == sb
	})

	do(t, a, "remove", hex.EncodeToString(bruno.PublicKey()))
	waitFor(t, func() bool {
		s := a.braid.Settled()
		defer s.Close()
		return !slices.ContainsFunc(s.Members(), func(m kolobraid.Member) bool { return bytes.Equal(m.Key, bruno.PublicKey()) })
	})
	do(t, a, "say", "Ana after the removal")
	do(t, b, "say", "Bruno after the removal")
	do(t, a, "say", "Ana last")
	waitFor(t, func() bool {
		return strings.Contains(show(a), "4. "+hex.EncodeToString(ana.PublicKey()[:4])+": Ana last (settled)")
	})
	if strings.Contains(show(a), "Bruno after the removal") {
		t.Fatalf("the owner shows a line the removed writer wrote later:\n%s", show(a))
	}
	if strings.Contains(show(b), "Ana after the removal") {
		t.Fatalf("the removed writer got a new line:\n%s", show(b))
	}

	before := show(a)
	a.Close()
	a, err = openDoc(ctx, ana, anaDir, addr, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if after := show(a); after != before {
		t.Fatalf("after a restart:\n%s\nwant:\n%s", after, before)
	}
}

// TestApplyRejectsLongLine appends straight to the braid, as a remote writer could, and checks that every peer drops the line.
func TestApplyRejectsLongLine(t *testing.T) {
	id := identity(t)
	a, err := openDoc(t.Context(), id, t.TempDir(), freeAddr(t), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.braid.Append(t.Context(), []byte(strings.Repeat("x", maxLine+1))); err != nil {
		t.Fatal(err)
	}
	do(t, a, "say", "Hello")
	want := "1. " + hex.EncodeToString(id.PublicKey()[:4]) + ": Hello (settled)\n"
	waitFor(t, func() bool { return show(a) == want })
}

func TestDoRejectsBadInput(t *testing.T) {
	a, err := openDoc(t.Context(), identity(t), t.TempDir(), freeAddr(t), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, c := range []struct{ cmd, arg string }{
		{"say", ""},
		{"say", strings.Repeat("x", maxLine+1)},
		{"add", "zz"},
		{"add", strings.Repeat("ab", 31)},
		{"add", strings.Repeat("ab", 33)},
		{"remove", "zz"},
		{"remove", strings.Repeat("ab", 31)},
		{"nothing", ""},
	} {
		if err := a.do(t.Context(), c.cmd, c.arg); err == nil {
			t.Errorf("%s %.8q: no error", c.cmd, c.arg)
		}
	}
	do(t, a, "say", strings.Repeat("x", maxLine))
	if got := show(a); strings.Count(got, "\n") != 1 {
		t.Fatalf("want only the 500 byte line, got:\n%s", got)
	}
}

// TestIdleConnectionKeepsPermission holds a connection that sends no join request, then checks the key can still join.
func TestIdleConnectionKeepsPermission(t *testing.T) {
	ctx := t.Context()
	addr := freeAddr(t)
	ana, bruno := identity(t), identity(t)
	a, err := openDoc(ctx, ana, t.TempDir(), addr, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	do(t, a, "add", hex.EncodeToString(bruno.PublicKey()))
	c, err := kolosecure.Dial(ctx, addr, bruno, ana.PublicKey(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	time.Sleep(200 * time.Millisecond)
	b, err := openDoc(ctx, bruno, t.TempDir(), "", addr, ana.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	b.Close()
}

func identity(t *testing.T) *koloid.Identity {
	id, err := koloid.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// freeAddr returns a UDP address on 127.0.0.1 that was free a moment ago.
func freeAddr(t *testing.T) string {
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().String()
}

func do(t *testing.T, d *Doc, cmd, arg string) {
	if err := d.do(t.Context(), cmd, arg); err != nil {
		t.Fatal(err)
	}
}

func show(d *Doc) string {
	var w strings.Builder
	d.show(&w)
	return w.String()
}

func waitFor(t *testing.T, ok func() bool) {
	for end := time.Now().Add(30 * time.Second); !ok(); time.Sleep(100 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("timed out")
		}
	}
}
