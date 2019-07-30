PREFIX ?= /usr
DESTDIR ?=
# E402 module level import not at top of file
# E722 do not use bare 'except'
PYIGNORE ?= E402,E722

pysources = src/cosalib src/oscontainer.py src/cmd-kola

.PHONY: all check flake8 unittest clean mantle install

all: mantle

src:=$(shell find src -maxdepth 1 -type f -executable -print)
src_checked:=$(patsubst src/%,src/.%.shellchecked,${src})
tests:=$(shell find tests -maxdepth 1 -type f -executable -print)
tests_checked:=$(patsubst tests/%,tests/.%.shellchecked,${tests})
cwd:=$(shell find . -maxdepth 1 -type f -executable -print)
cwd_checked:=$(patsubst ./%,.%.shellchecked,${cwd})

.%.shellchecked: %
	./tests/check_one.sh $< $@

check: ${src_checked} ${tests_checked} ${cwd_checked} flake8
	echo OK

flake8:
	python3 -m flake8 --ignore=$(PYIGNORE) $(pysources)
	# The following lines will verify python files that are not modules
	# but are commented out as the files are not ready for checking yet
	# grep -r "^\#\!/usr/bin/py" src/ | cut -d : -f 1 | xargs flake8 --ignore=$(PYIGNORE)
	# find src -maxdepth 1 -name "*.py" | xargs flake8 --ignore=$(PYIGNORE)

unittest:
	PYTHONPATH=`pwd`/src python3 -m pytest tests/

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
