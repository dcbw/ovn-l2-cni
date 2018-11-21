package config

import (
	"fmt"
	"os"
	"strings"

	gcfg "gopkg.in/gcfg.v1"

	kexec "k8s.io/utils/exec"
)

var (
	// Ovn is a global config parameter that other modules may access directly
	Ovn  *ovnConfig
	exec kexec.Interface
)

type ovnConfig struct {
	// e.g: "ssl:192.168.1.2:6641"
	OvsBridge string `gcfg:"ovs-bridge"`
	Address   string `gcfg:"address"`
	PrivKey   string `gcfg:"privkey"`
	Cert      string `gcfg:"cert"`
	CACert    string `gcfg:"cacert"`
}

func SchemeIsUnix() bool {
	return strings.HasPrefix(Ovn.Address, "unix") || len(Ovn.Address) == 0
}

func SchemeIsTCP() bool {
	return strings.HasPrefix(Ovn.Address, "tcp")
}

func SchemeIsSSL() bool {
	return strings.HasPrefix(Ovn.Address, "ssl")
}

func InitConfig(e kexec.Interface) error {
	return InitConfigWithPath(e, "/etc/ovn-l2.conf")
}

func InitConfigWithPath(e kexec.Interface, configFile string) error {
	exec = exec

	f, err := os.Open(configFile)
	if err != nil {
		return fmt.Errorf("failed to open config file %s: %v", configFile, err)
	}
	defer f.Close()

	var cfg ovnConfig
	if err = gcfg.ReadInto(&cfg, f); err != nil {
		return fmt.Errorf("failed to parse config file %s: %v", f.Name(), err)
	}

	*Ovn = cfg
	if Ovn.OvsBridge == "" {
		Ovn.OvsBridge = "br-int"
	}

	switch {
	case SchemeIsUnix() || SchemeIsTCP():
		if Ovn.PrivKey != "" || Ovn.Cert != "" || Ovn.CACert != "" {
			return fmt.Errorf("certificate or key given; perhaps you mean to use the 'ssl' scheme?")
		}
	case SchemeIsSSL():
		if !pathExists(Ovn.PrivKey) {
			return fmt.Errorf("private key file %s not found", Ovn.PrivKey)
		}
		if !pathExists(Ovn.Cert) {
			return fmt.Errorf("certificate file %s not found", Ovn.Cert)
		}
		if !pathExists(Ovn.CACert) {
			return fmt.Errorf("CA certificate file %s not found", Ovn.CACert)
		}
	}

	return nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	if err != nil && os.IsNotExist(err) {
		return false
	}
	return true
}
