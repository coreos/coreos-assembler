PREFIX ?= "./"

.PHONY: clean check

clean:
	rm -rf target/
	rm -f coreos-assembler

build:
	cargo build --release
	mv target/release/coreos-assembler ${PREFIX}

check:
	find . -name "*.rs" | xargs rustfmt --write-mode diff
