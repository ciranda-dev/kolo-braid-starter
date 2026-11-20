# kolo-braid starter

[![status: beta](https://img.shields.io/badge/status-beta-orange)](CHANGELOG.md)
[![license: MIT OR Apache-2.0](https://img.shields.io/badge/license-MIT%20OR%20Apache--2.0-blue)](#license)
[![go: 1.26+](https://img.shields.io/badge/go-1.26%2B-00ADD8)](go.mod)

The scaffold for the "Build a shared document in two terminals" guide in the [Kolo docs](https://docs.kolo.ciranda.dev/build/shared-doc/). Two people write one document together, each line signed by its writer, and both see the same lines in the same order, with [kolo-braid](https://github.com/ciranda-dev/kolo-braid).

`main.go` is done: it reads the flags, keeps your key in the folder you give and passes each line you type to `doc.go`. `doc.go` is empty: the guide fills it in. The finished version is in `finished/`; its `main.go` is the same file as the one in this folder.

Requirements: Go 1.26 or newer.

## Run the scaffold

```sh
go run . -dir "$(mktemp -d)" -listen 127.0.0.1:7450
```

It prints your key and answers every command with `not written yet: follow the guide`.

## Run the finished version

In a first terminal, start a document. It prints your key on the `me` line:

```sh
go run ./finished -dir "$(mktemp -d)" -listen 127.0.0.1:7450
```

In a second terminal, join it with that key, then type `add` and the key the second terminal prints in the first one:

```sh
go run ./finished -dir "$(mktemp -d)" -peer 127.0.0.1:7450 -key <key of the first terminal>
```

Then type `say <text>` and `show` in either terminal.

## Between two computers

The two terminals connect directly: `-listen` opens the port and `-peer` dials
it. They do not use the DHT or the swarm, so there is no bootstrap to configure.

- On one computer, use `127.0.0.1` as above.
- On the same local network, the first terminal passes `-listen` with the IPv4
  address of its network card (for example `-listen 192.168.1.20:7450`) and the
  second passes `-peer 192.168.1.20:7450` with the key.
- Between two different networks it works only when the first computer has a
  public IPv4 address the second can reach, with the UDP port open in its
  firewall.
- Two computers that are both behind home routers will not connect: this
  program does not punch through NAT and uses no relay.

For that case use the Kolo apps (chat, share, meet, board) or the
[kolo-swarm](https://github.com/ciranda-dev/kolo-swarm) library, which find
peers through the public entry nodes by default.

## Test

```sh
go test ./...
```

## License

Licensed under either of the [Apache License, Version 2.0](LICENSE) or the [MIT license](LICENSE-MIT), at your option.
