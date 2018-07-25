PREFIX ?= /usr
DESTDIR ?=

.PHONY: all install

all:

install:
	install -D -t $(DESTDIR)$(PREFIX)/bin coreos-virt-install coreos-oemid
	install -D coreos-assembler.sh $(DESTDIR)$(PREFIX)/bin/coreos-assembler
