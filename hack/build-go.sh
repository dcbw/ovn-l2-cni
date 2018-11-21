#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/init.sh"

OVN_L2_BINARIES=("$@")

# Check for `go` binary and set ${GOPATH}.
setup_env

go_pkg_dir="${GOPATH}/src/${OVN_L2_GO_PACKAGE}"
cd ${go_pkg_dir}
mkdir -p "${OVN_L2_OUTPUT_BINPATH}"
export GOBIN="${OVN_L2_OUTPUT_BINPATH}"

# Add a buildid to the executable - needed by rpmbuild
go install -ldflags "-B 0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \n')" "${OVN_L2_BINARIES[@]}";
