PREFIX ?= /usr
DESTDIR ?=

.PHONY: all install

all:

install:
	install -D -t $(DESTDIR)$(PREFIX)/bin coreos-assembler $$(find src/compat -maxdepth 1 -type f)
	install -D -t $(DESTDIR)$(PREFIX)/libexec/coreos-assembler $$(find src/ -maxdepth 1 -type f)
