PREFIX ?= /usr
DESTDIR ?=

.PHONY: all install

all:

check:
	./tests/check.sh

install:
	install -d $(DESTDIR)$(PREFIX)/lib/coreos-assembler
	install -D -t $(DESTDIR)$(PREFIX)/lib/coreos-assembler $$(find src/ -maxdepth 1 -type f)
	install -d $(DESTDIR)$(PREFIX)/bin
	ln -sf ../lib/coreos-assembler/coreos-assembler $(DESTDIR)$(PREFIX)/bin/
