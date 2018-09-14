PREFIX ?= /usr
DESTDIR ?=

.PHONY: all rust mantle install

all: mantle rust

rust:
	cargo build --release

mantle:
	cd mantle && ./build ore kola kolet

install:
	install -D -t $(DESTDIR)$(PREFIX)/bin coreos-assembler $$(find src/compat -maxdepth 1 -type f)
	install -D -t $(DESTDIR)$(PREFIX)/libexec target/release/coreos-assembler-rs
	install -D -t $(DESTDIR)$(PREFIX)/libexec/coreos-assembler $$(find src/ -maxdepth 1 -type f)
	install -D -t $(DESTDIR)$(PREFIX)/bin mantle/bin/{ore,kola}
	install -D -m 0755 -t $(DESTDIR)$(PREFIX)/lib/kola/amd64 mantle/bin/amd64/kolet
