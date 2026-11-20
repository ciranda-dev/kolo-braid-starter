package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	koloid "github.com/ciranda-dev/kolo-id"
	kolosecure "github.com/ciranda-dev/kolo-secure"
	kolostore "github.com/ciranda-dev/kolo-store"
)

const (
	app     = "doc/v1"
	maxLine = 500
)

var errKey = errors.New("a key is 64 hex digits")

// apply turns each data entry into one line of the document, kept under its position in the braid's order with the
// first 4 bytes of its writer's key.
func apply(tx *kolobraid.Tx, e *kolobraid.Entry) error {
	if e.Kind != kolobraid.KindData {
		return nil
	}
	if len(e.Body) == 0 || len(e.Body) > maxLine {
		return kolobraid.Reject("a line is 1 to 500 bytes")
	}
	key := binary.BigEndian.AppendUint64([]byte("line/"), e.Position)
	return tx.Put(key, append(e.Writer[:4:4], e.Body...))
}

// Doc is one person's copy of the shared document.
type Doc struct {
	braid   *kolobraid.Braid
	db      kolostore.Backend
	l       *kolosecure.Listener
	stop    context.CancelFunc
	wg      sync.WaitGroup
	allowed sync.Map // keys the owner typed add for, each let in once
}

// openDoc opens the document stored in dir, or starts one (listen) or joins one (peer), and keeps it in sync.
func openDoc(ctx context.Context, id *koloid.Identity, dir, listen, peer string, owner ed25519.PublicKey) (*Doc, error) {
	db, err := kolostore.OpenPebble(filepath.Join(dir, "doc"))
	if err != nil {
		return nil, err
	}
	opts := kolobraid.Options{Backend: db, App: app, Apply: apply}
	br, err := kolobraid.Open(ctx, id, opts)
	switch {
	case errors.Is(err, kolobraid.ErrNotFound) && listen != "":
		log.Print("started a new document")
		br, err = kolobraid.Create(ctx, id, kolobraid.GroupConfig{App: app}, opts)
	case errors.Is(err, kolobraid.ErrNotFound):
		br, err = join(ctx, id, opts, peer, owner)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	ctx, stop := context.WithCancel(ctx)
	d := &Doc{braid: br, db: db, stop: stop}
	if listen == "" {
		d.wg.Go(func() { d.follow(ctx, id, peer, owner) })
		return d, nil
	}
	if d.l, err = kolosecure.Listen(ctx, listen, id, nil); err != nil {
		d.Close()
		return nil, err
	}
	log.Print("listening on ", listen)
	d.wg.Go(func() { d.accept(ctx) })
	return d, nil
}

// Close stops the document.
func (d *Doc) Close() {
	d.stop()
	if d.l != nil {
		d.l.Close()
	}
	d.wg.Wait()
	d.braid.Close()
	d.db.Close()
}

// do runs one command typed in the terminal.
func (d *Doc) do(ctx context.Context, cmd, arg string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	key, _ := hex.DecodeString(arg)
	switch cmd {
	case "say":
		if len(arg) == 0 || len(arg) > maxLine {
			return errors.New("a line is 1 to 500 bytes")
		}
		_, err := d.braid.Append(ctx, []byte(arg))
		return err
	case "show":
		d.show(os.Stdout)
		return nil
	case "add":
		if len(key) != ed25519.PublicKeySize {
			return errKey
		}
		d.allowed.Store(string(key), true)
		log.Printf("%.8s can join now", arg)
		return nil
	case "remove":
		if len(key) != ed25519.PublicKeySize {
			return errKey
		}
		_, err := d.braid.RemoveMember(ctx, key)
		return err
	}
	return errors.New("commands: say <text>, show, add <key>, remove <key>")
}

// show prints every line in the braid's order with its writer, and whether its place is settled or still tentative.
func (d *Doc) show(w io.Writer) {
	view, settled := d.braid.View(), d.braid.Settled()
	defer view.Close()
	defer settled.Close()
	n := 0
	for k, v := range view.Scan([]byte("line/"), []byte("line0")) {
		state := "tentative"
		if s, err := settled.Get(k); err == nil && bytes.Equal(s, v) {
			state = "settled"
		}
		n++
		fmt.Fprintf(w, "%d. %x: %s (%s)\n", n, v[:4], v[4:], state)
	}
	if n == 0 {
		fmt.Fprintln(w, "the document is empty")
	}
}

// accept syncs with members, lets in a key the owner typed add for, and closes every other connection.
func (d *Doc) accept(ctx context.Context) {
	for {
		c, err := d.l.Accept(ctx)
		if err != nil {
			return
		}
		d.wg.Go(func() {
			defer c.Close()
			key := c.RemotePublicKey()
			v := d.braid.View()
			member := slices.ContainsFunc(v.Members(), func(m kolobraid.Member) bool { return bytes.Equal(m.Key, key) })
			v.Close()
			if member {
				d.braid.ServeConn(ctx, c)
			} else if _, ok := d.allowed.Load(string(key)); ok {
				if err := d.admit(ctx, c); err != nil {
					log.Print("add: ", err)
				} else {
					d.allowed.Delete(string(key))
				}
			}
		})
	}
}

// admit reads a join request on c, adds its sender as a writer and sends the welcome back.
func (d *Doc) admit(ctx context.Context, c *kolosecure.Conn) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s, err := c.AcceptStream(ctx)
	if err != nil {
		return err
	}
	req, err := io.ReadAll(io.LimitReader(s, kolobraid.MaxJoinRequest))
	if err != nil {
		return err
	}
	writer := kolobraid.Role(0) // not an admin and not a settler
	welcome, err := d.braid.Admit(ctx, c.RemotePublicKey(), req, &kolobraid.AdmitOptions{Roles: &writer})
	if err != nil {
		return err
	}
	if _, err := s.Write(welcome); err != nil {
		d.braid.RemoveMember(ctx, c.RemotePublicKey())
		return err
	}
	s.Close()
	select { // the joiner hangs up once it has the welcome
	case <-c.Context().Done():
	case <-ctx.Done():
	}
	log.Printf("added %x", c.RemotePublicKey()[:4])
	return nil
}

// join asks the owner to let this key in, once a second until the owner typed add for it, and opens the document.
func join(ctx context.Context, id *koloid.Identity, opts kolobraid.Options, peer string, owner ed25519.PublicKey) (*kolobraid.Braid, error) {
	j, err := kolobraid.NewJoin(ctx, id, opts)
	if err != nil {
		return nil, err
	}
	req, err := j.Request(nil)
	if err != nil {
		return nil, err
	}
	log.Print("waiting for the owner to type add and my key")
	for {
		if welcome, err := ask(ctx, id, peer, owner, req); err == nil {
			log.Print("joined the document")
			return j.Complete(ctx, welcome)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// ask sends the join request to the owner and returns the welcome.
func ask(ctx context.Context, id *koloid.Identity, peer string, owner ed25519.PublicKey, req []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, err := kolosecure.Dial(ctx, peer, id, owner, nil)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	s, err := c.OpenStream(ctx)
	if err != nil {
		return nil, err
	}
	s.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := s.Write(req); err != nil {
		return nil, err
	}
	s.Close()
	welcome, err := io.ReadAll(io.LimitReader(s, kolobraid.MaxWelcome))
	if err == nil && len(welcome) == 0 {
		err = errors.New("not added yet")
	}
	return welcome, err
}

// follow keeps a connection to the owner and syncs the document over it, again after the owner restarts.
func (d *Doc) follow(ctx context.Context, id *koloid.Identity, peer string, owner ed25519.PublicKey) {
	for ctx.Err() == nil {
		if c, err := kolosecure.Dial(ctx, peer, id, owner, nil); err == nil {
			d.braid.ServeConn(ctx, c)
			c.Close()
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}
