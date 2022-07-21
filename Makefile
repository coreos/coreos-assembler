PREFIX ?= /usr
DESTDIR ?=
# E128 continuation line under-indented for visual indent
# E241 multiple spaces after ','
# E402 module level import not at top of file
# E501 line too long
# E722 do not use bare 'except'
# W503 line break before binary operator
# W504 line break after binary operator
PYIGNORE ?= E128,E241,E402,E501,E722,W503,W504

.PHONY: all check shellcheck flake8 pycheck unittest clean mantle mantle-check install tools

MANTLE_BINARIES := ore kola plume

all: bin/coreos-assembler tools mantle

src:=$(shell find src -maxdepth 1 -type f -executable -print)
pysources=$(shell find src -type f -name '*.py') $(shell for x in $(src); do if head -1 $$x | grep -q python; then echo $$x; fi; done)
src_checked:=$(patsubst src/%,src/.%.shellchecked,${src})
tests:=$(shell find tests -maxdepth 1 -type f -executable -print)
tests_checked:=$(patsubst tests/%,tests/.%.shellchecked,${tests})
cwd:=$(shell find . -maxdepth 1 -type f -executable -print)
cwd_checked:=$(patsubst ./%,.%.shellchecked,${cwd})
GOARCH:=$(shell uname -m)
export COSA_META_SCHEMA:=$(shell pwd)/src/v1.json
ifeq ($(GOARCH),x86_64)
        GOARCH="amd64"
else ifeq ($(GOARCH),aarch64)
        GOARCH="arm64"
endif

bin/coreos-assembler:
	cd cmd && go build -mod vendor -o ../$@
.PHONY: bin/coreos-assembler

.%.shellchecked: %
	./tests/check_one.sh $< $@

shellcheck: ${src_checked} ${tests_checked} ${cwd_checked}

check: flake8 pycheck schema-check mantle-check cosa-go-check
	echo OK

pycheck:
	python3 -m py_compile $(pysources)
	pylint -E $(pysources)

flake8:
	python3 -m flake8 --ignore=$(PYIGNORE) $(pysources)
	# The following lines will verify python files that are not modules
	# but are commented out as the files are not ready for checking yet
	# grep -r "^\#\!/usr/bin/py" src/ | cut -d : -f 1 | xargs flake8 --ignore=$(PYIGNORE)
	# find src -maxdepth 1 -name "*.py" | xargs flake8 --ignore=$(PYIGNORE)

unittest:
	COSA_TEST_META_PATH=`pwd`/fixtures \
		PYTHONPATH=`pwd`/src python3 -m pytest tests/

cosa-go-check:
	(cd cmd && go test -mod=vendor)
	go test -mod=vendor github.com/coreos/coreos-assembler/internal/pkg/bashexec
	go test -mod=vendor github.com/coreos/coreos-assembler/internal/pkg/cosash

clean:
	rm -f ${src_checked} ${tests_checked} ${cwd_checked}
	rm -rf tools/bin
	find . -name "*.py[co]" -type f | xargs rm -f
	find . -name "__pycache__" -type d | xargs rm -rf

mantle:
	cd mantle && $(MAKE)

.PHONY: $(MANTLE_BINARIES) kolet
$(MANTLE_BINARIES) kolet:
	cd mantle && $(MAKE) $@

mantle-check:
	cd mantle && $(MAKE) test

tools:
	cd tools && $(MAKE)

.PHONY: schema
schema:
	$(MAKE) -C schema

# To update the coreos-assembler schema:
# Edit src/v1.json
# $ cp src/v1.json schema/
# $ make schema
# $ for d in mantle gangplank; do (cd $d && go mod vendor); done
.PHONY: schema-check
schema-check: DIGEST = $(shell sha256sum src/v1.json | awk '{print $$1}')
schema-check:
	# Are the JSON Schema copies synced with each other?
	diff -u src/v1.json schema/v1.json
	# Is the generated Go code synced with the schema?
	grep -q "$(DIGEST)" schema/cosa/cosa_v1.go
	grep -q "$(DIGEST)" schema/cosa/schema_doc.go
	# Are the vendored copies of the generated code synced with the
	# canonical ones?
	diff -u mantle/vendor/github.com/coreos/coreos-assembler-schema/cosa/cosa_v1.go schema/cosa/cosa_v1.go
	diff -u mantle/vendor/github.com/coreos/coreos-assembler-schema/cosa/schema_doc.go schema/cosa/schema_doc.go

install:
	install -d $(DESTDIR)$(PREFIX)/lib/coreos-assembler
	install -D -t $(DESTDIR)$(PREFIX)/lib/coreos-assembler $$(find src/ -maxdepth 1 -type f)
	cp -df -t $(DESTDIR)$(PREFIX)/lib/coreos-assembler $$(find src/ -maxdepth 1 -type l)
	install -d $(DESTDIR)$(PREFIX)/lib/coreos-assembler/ci
	cp -df -t $(DESTDIR)$(PREFIX)/lib/coreos-assembler/ci $$(find ci/ -maxdepth 1 -type f)
	install -d $(DESTDIR)$(PREFIX)/lib/coreos-assembler/cosalib
	install -D -t $(DESTDIR)$(PREFIX)/lib/coreos-assembler/cosalib $$(find src/cosalib/ -maxdepth 1 -type f)
	install -d $(DESTDIR)$(PREFIX)/bin
	install bin/coreos-assembler $(DESTDIR)$(PREFIX)/bin/
	ln -sf ../lib/coreos-assembler/cp-reflink $(DESTDIR)$(PREFIX)/bin/
	ln -sf coreos-assembler $(DESTDIR)$(PREFIX)/bin/cosa
	install -d $(DESTDIR)$(PREFIX)/lib/coreos-assembler/tests/kola
	cd tools && $(MAKE) install DESTDIR=$(DESTDIR)
	cd mantle && $(MAKE) install DESTDIR=$(DESTDIR)
