OUT_DIR = _output
export OUT_DIR
PREFIX ?= ${DESTDIR}/usr
BINDIR ?= ${PREFIX}/bin
CNIBINDIR ?= ${DESTDIR}/opt/cni/bin

.PHONY: all build check test

# Example:
#   make
#   make all

all build:
	hack/build-go.sh ovn-l2/ovn-l2.go

check test:
	hack/test-go.sh ${PKGS}

install:
	install -D -m 755 ${OUT_DIR}/go/bin/ovn-l2 ${BINDIR}/

clean:
	rm -rf ${OUT_DIR}

.PHONY: check-gopath install.tools lint gofmt

check-gopath:
ifndef GOPATH
	$(error GOPATH is not set)
endif

install.tools: check-gopath
	go get -u gopkg.in/alecthomas/gometalinter.v1; \
	$(GOPATH)/bin/gometalinter.v1 --install;

lint:
	./hack/lint.sh

gofmt:
	@./hack/verify-gofmt.sh
