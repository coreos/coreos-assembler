PREFIX ?= /usr
DESTDIR ?=

.PHONY: all mantle rdgo install

all: mantle rdgo

mantle:
	cd mantle && ./build ore kola kolet

rdgo:
	(cd rpmdistro-gitoverlay && ./autogen.sh --prefix=$(PREFIX) --sysconfdir=/etc && make)

install:
	install -D -t $(DESTDIR)$(PREFIX)/bin coreos-assembler
	install -D -t $(DESTDIR)$(PREFIX)/lib/coreos-assembler $$(find src/ -maxdepth 1 -type f)
	install -D -t $(DESTDIR)$(PREFIX)/bin mantle/bin/{ore,kola}
	install -D -m 0755 -t $(DESTDIR)$(PREFIX)/lib/kola/amd64 mantle/bin/amd64/kolet
	(cd rpmdistro-gitoverlay && make install)
