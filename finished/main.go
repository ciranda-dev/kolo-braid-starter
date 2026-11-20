// Command doc is a document that several people write together in the terminal. This file reads the flags and what you
// type; doc.go is the Kolo part.
package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	koloid "github.com/ciranda-dev/kolo-id"
)

func main() {
	dir := flag.String("dir", "", "folder for your key and your copy of the document (required)")
	listen := flag.String("listen", "", "IP:PORT to start the document on and let others in")
	peer := flag.String("peer", "", "IP:PORT of the person who started the document")
	key := flag.String("key", "", "the key of the person who started the document")
	flag.Parse()
	owner, err := hex.DecodeString(*key)
	if *dir == "" || (*listen == "") == (*peer == "") || err != nil || (*peer != "" && len(owner) != 32) {
		log.Fatal("give -dir and either -listen, or -peer and -key")
	}
	for _, addr := range []string{*listen, *peer} {
		if err := checkAddr(addr); err != nil {
			log.Fatal(err)
		}
	}
	log.SetFlags(log.Ltime)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	id, err := loadIdentity(filepath.Join(*dir, "identity.key"))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("me %x", id.PublicKey())
	d, err := openDoc(ctx, id, *dir, *listen, *peer, owner)
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()
	log.Print("type: say <text>, show, add <key>, remove <key>")

	lines := make(chan string)
	go func() {
		defer stop()
		for sc := bufio.NewScanner(os.Stdin); sc.Scan(); {
			select {
			case lines <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case line := <-lines:
			cmd, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
			if err := d.do(ctx, cmd, arg); err != nil {
				log.Print(err)
			}
		}
	}
}

// checkAddr accepts an empty address or an IPv4 IP:PORT that is not 0.0.0.0.
func checkAddr(addr string) error {
	if addr == "" {
		return nil
	}
	ap, err := netip.ParseAddrPort(addr)
	if err != nil || !ap.Addr().Is4() || ap.Addr().IsUnspecified() || ap.Port() == 0 {
		return fmt.Errorf("%q is not an IPv4 IP:PORT such as 127.0.0.1:7450", addr)
	}
	return nil
}

// loadIdentity reads the key at path, or makes one and saves it there with mode 0600.
func loadIdentity(path string) (*koloid.Identity, error) {
	id, err := koloid.Load(path)
	if !errors.Is(err, fs.ErrNotExist) {
		return id, err
	}
	if id, err = koloid.Generate(); err != nil {
		return nil, err
	}
	return id, id.SaveNew(path)
}
