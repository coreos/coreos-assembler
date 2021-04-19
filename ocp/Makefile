.PHONY: build-bc-image build-dev
build-bc-image:
	cd ../ && buildah bud -f ocp/Dockerfile.buildconfig -t quay.io/coreos-assembler/coreos-assembler:ocp

build-dev:
	cd ../ && buildah bud -f ocp/Dockerfile.dev -t quay.io/coreos-assembler/coreos-assembler:ocp-dev
