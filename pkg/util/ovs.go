package util

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	kexec "k8s.io/utils/exec"

	"github.com/dcbw/ovn-l2-cni/pkg/config"
)

const (
	ovsCommandTimeout = 15
	ovsVsctlCommand   = "ovs-vsctl"
	ovnNbctlCommand   = "ovn-nbctl"
)

// Exec runs various OVN and OVS utilities
type execHelper struct {
	exec           kexec.Interface
	vsctlPath      string
	nbctlPath      string
}

var runner *execHelper

// SetExec validates executable paths and saves the given exec interface
// to be used for running various OVS and OVN utilites
func SetExec(exec kexec.Interface) error {
	var err error

	runner = &execHelper{exec: exec}
	runner.vsctlPath, err = exec.LookPath(ovsVsctlCommand)
	if err != nil {
		return err
	}
	runner.nbctlPath, err = exec.LookPath(ovnNbctlCommand)
	if err != nil {
		return err
	}
	return nil
}

// GetExec returns the exec interface which can be used for running commands directly.
// Only use for passing an exec interface into pkg/config which cannot call this
// function directly because this module imports pkg/config already.
func GetExec() kexec.Interface {
	return runner.exec
}

func run(cmdPath string, args ...string) (*bytes.Buffer, *bytes.Buffer, error) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := runner.exec.Command(cmdPath, args...)
	cmd.SetStdout(stdout)
	cmd.SetStderr(stderr)
	logrus.Debugf("exec: %s %s", cmdPath, strings.Join(args, " "))
	err := cmd.Run()
	if err != nil {
		logrus.Debugf("exec: %s %s => %v", cmdPath, strings.Join(args, " "), err)
	}
	return stdout, stderr, err
}

// RunOVSVsctl runs a command via ovs-vsctl.
func RunOVSVsctl(args ...string) (string, string, error) {
	cmdArgs := []string{fmt.Sprintf("--timeout=%d", ovsCommandTimeout)}
	cmdArgs = append(cmdArgs, args...)
	stdout, stderr, err := run(runner.vsctlPath, cmdArgs...)
	return strings.Trim(strings.TrimSpace(stdout.String()), "\""), stderr.String(), err
}

// Run the ovn-ctl command and retry if "Connection refused"
// poll waitng for service to become available
func runOVNretry(cmdPath string, args ...string) (*bytes.Buffer, *bytes.Buffer, error) {
	retriesLeft := 200
	for {
		stdout, stderr, err := run(cmdPath, args...)
		if err == nil {
			return stdout, stderr, err
		}

		// Connection refused
		// Master may not be up so keep trying
		if strings.Contains(stderr.String(), "Connection refused") {
			if retriesLeft == 0 {
				return stdout, stderr, err
			}
			retriesLeft--
			time.Sleep(2 * time.Second)
		} else {
			// Some other problem for caller to handle
			return stdout, stderr, err
		}
	}
}

// RunOVNNbctl runs a command via ovn-nbctl.
func RunOVNNbctl(args ...string) (string, string,
	error) {
	var cmdArgs []string
	if config.SchemeIsSSL() {
		cmdArgs = []string{
			fmt.Sprintf("--private-key=%s", config.Ovn.PrivKey),
			fmt.Sprintf("--certificate=%s", config.Ovn.Cert),
			fmt.Sprintf("--bootstrap-ca-cert=%s", config.Ovn.CACert),
			fmt.Sprintf("--db=%s", config.Ovn.Address),
		}
	} else if config.SchemeIsTCP() {
		cmdArgs = []string{
			fmt.Sprintf("--db=%s", config.Ovn.Address),
		}
	}

	cmdArgs = append(cmdArgs, fmt.Sprintf("--timeout=%d", ovsCommandTimeout))
	cmdArgs = append(cmdArgs, args...)
	stdout, stderr, err := runOVNretry(runner.nbctlPath, cmdArgs...)
	return strings.Trim(strings.TrimSpace(stdout.String()), "\""), stderr.String(), err
}
