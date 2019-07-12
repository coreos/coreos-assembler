PREFIX ?= /usr
DESTDIR ?=

.PHONY: all mantle install check clean

all: mantle

src:=$(shell find src -maxdepth 1 -type f -executable -print)
src_checked:=$(patsubst src/%,src/.%.shellchecked,${src})
tests:=$(shell find tests -maxdepth 1 -type f -executable -print)
tests_checked:=$(patsubst tests/%,tests/.%.shellchecked,${tests})
cwd:=$(shell find . -maxdepth 1 -type f -executable -print)
cwd_checked:=$(patsubst ./%,.%.shellchecked,${cwd})

.%.shellchecked: %
	./tests/check_one.sh $< $@

check: ${src_checked} ${tests_checked} ${cwd_checked}
	echo OK

clean:
	rm -f ${src_checked} ${tests_checked} ${cwd_checked}
	find . -name "*.py[co]" -type f | xargs rm -f
	find . -name "__pycache__" -type d | xargs rm -rf

mantle:
	cd mantle && ./build ore kola kolet plume

install:
	install -d $(DESTDIR)$(PREFIX)/lib/coreos-assembler
	install -D -t $(DESTDIR)$(PREFIX)/lib/coreos-assembler $$(find src/ -maxdepth 1 -type f)
	install -d $(DESTDIR)$(PREFIX)/lib/coreos-assembler/cosalib
	install -D -t $(DESTDIR)$(PREFIX)/lib/coreos-assembler/cosalib $$(find src/cosalib/ -maxdepth 1 -type f)
	install -d $(DESTDIR)$(PREFIX)/bin
	ln -sf ../lib/coreos-assembler/coreos-assembler $(DESTDIR)$(PREFIX)/bin/
	ln -sf coreos-assembler $(DESTDIR)$(PREFIX)/bin/cosa
	install -D -t $(DESTDIR)$(PREFIX)/bin mantle/bin/{ore,kola,plume}
	install -d $(DESTDIR)$(PREFIX)/lib/kola/amd64
	install -D -m 0755 -t $(DESTDIR)$(PREFIX)/lib/kola/amd64 mantle/bin/amd64/kolet
