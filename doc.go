package main

import (
	"context"
	"crypto/ed25519"
	"errors"

	koloid "github.com/ciranda-dev/kolo-id"
)

// Doc is one person's copy of the shared document. The guide replaces this file.
type Doc struct{}

// openDoc opens, starts or joins the document. For now it does nothing.
func openDoc(ctx context.Context, id *koloid.Identity, dir, listen, peer string, owner ed25519.PublicKey) (*Doc, error) {
	return &Doc{}, nil
}

// Close stops the document.
func (d *Doc) Close() {}

// do runs one command typed in the terminal.
func (d *Doc) do(ctx context.Context, cmd, arg string) error {
	return errors.New("not written yet: follow the guide")
}
