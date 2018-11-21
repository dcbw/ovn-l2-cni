#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/init.sh"

do_vendor() {
	# Check for `go` binary and set ${GOPATH}.
	setup_env

	local go_pkg_dir="${GOPATH}/src/${OVN_L2_GO_PACKAGE}"
	cd ${go_pkg_dir}
	mkdir -p "${OVN_L2_OUTPUT_BINPATH}"
	export GOBIN="${OVN_L2_OUTPUT_BINPATH}"

	$(dirname "${BASH_SOURCE}")/govendor fetch ${1}
}

do_vendor $@

