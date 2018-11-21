package main

import (
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"strings"

	"github.com/vishvananda/netlink"

	"github.com/dcbw/ovn-l2-cni/pkg/config"
	"github.com/dcbw/ovn-l2-cni/pkg/util"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/ip"
)

type NetConf struct {
	types.NetConf
	MTU    int  `json:"mtu"`
	Subnet_ string `json:"subnet"`
	Subnet *net.IPNet
}

type EnvArgs struct {
	types.CommonArgs
	IP                types.UnmarshallableString `json:"ip,omitempty"`
	MAC               types.UnmarshallableString `json:"mac,omitempty"`
	K8S_POD_NAMESPACE types.UnmarshallableString `json:"k8s_pod_namespace"`
	K8S_POD_NAME      types.UnmarshallableString `json:"k8s_pod_name"`
}

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

func loadConf(bytes []byte) (*NetConf, string, error) {
	n := &NetConf{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, "", fmt.Errorf("failed to load netconf: %v", err)
	}
	if err := version.ParsePrevResult(&n.NetConf); err != nil {
		return nil, "", err
	}
	if n.Name == "" {
		return nil, "", fmt.Errorf("a network name is required")
	}
	if n.Subnet_ != "" {
		var err error
		_, n.Subnet, err = net.ParseCIDR(n.Subnet_)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse subnet %q: %v", n.Subnet_, err)
		}
	}

	return n, n.CNIVersion, nil
}

func setupVeth(netns ns.NetNS, ifName string, mtu int, macAddr string, ipAddr *net.IPNet) (*current.Interface, *current.Interface, error) {
	hostIface := &current.Interface{}
	contIface := &current.Interface{}

	if err := netns.Do(func(hostNS ns.NetNS) error {
		hostVeth, contVeth, err := ip.SetupVeth(ifName, mtu, hostNS)
		if err != nil {
			return err
		}

		addr, err := net.ParseMAC(macAddr)
		if err != nil {
			return fmt.Errorf("invalid MAC address %q: %v", macAddr, err)
		}
		link, err := netlink.LinkByName(ifName)
		if err != nil {
			return fmt.Errorf("failed to get %q: %v", ifName, err)
		}
		err = netlink.LinkSetDown(link)
		if err != nil {
			return fmt.Errorf("failed to set %q down: %v", ifName, err)
		}
		err = netlink.LinkSetHardwareAddr(link, addr)
		if err != nil {
			return fmt.Errorf("failed to set %q address to %q: %v", ifName, macAddr, err)
		}
		err = netlink.LinkSetUp(link)
		if err != nil {
			return fmt.Errorf("failed to set %q up: %v", ifName, err)
		}

		if ipAddr != nil {
			if err = netlink.AddrAdd(link, &netlink.Addr{IPNet: ipAddr}); err != nil {
				return fmt.Errorf("failed to add IP addr %v to %q: %v", ipAddr, ifName, err)
			}
		}

		hostIface.Name = hostVeth.Name
		hostIface.Mac = hostVeth.HardwareAddr.String()
		contIface.Name = contVeth.Name
		contIface.Mac = macAddr
		contIface.Sandbox = netns.Path()
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return hostIface, contIface, nil
}

func ensurePrefix(name string) string {
	const ovnL2Prefix string = "ovnl2_"

	if strings.HasPrefix(name, ovnL2Prefix) {
		return name
	}
	return ovnL2Prefix + name
}

func getPodDetails(args string) (*EnvArgs, error) {
	e := &EnvArgs{}
	err := types.LoadArgs(args, e)
	if err != nil {
		return nil, err
	}
	if e.K8S_POD_NAMESPACE == "" {
		return nil, fmt.Errorf("missing K8S_POD_NAMESPACE")
	}
	if e.K8S_POD_NAME == "" {
		return nil, fmt.Errorf("missing K8S_POD_NAME")
	}
	return e, nil
}

func cmdAdd(args *skel.CmdArgs) error {
	if err := config.InitConfig(nil); err != nil {
		return err
	}

	n, cniVersion, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	envArgs, err := getPodDetails(args.Args)
	if err != nil {
		return err
	}
	podNamespace := envArgs.K8S_POD_NAMESPACE
	podName := envArgs.K8S_POD_NAME

	// Create the logical switch if it's not already created
	lsName := ensurePrefix(n.Name)
	lsArgs := []string{
		"--may-exist", "ls-add", lsName,
	}
	var portIP, gatewayIP net.IP
	if n.Subnet != nil {
		nid := n.Subnet.IP.Mask(n.Subnet.Mask)
		gatewayIP = ip.NextIP(nid)
		lsArgs = append(lsArgs,
			"--", "set", "logical_switch", lsName, "other-config:subnet=" + n.Subnet.String(),
			"--", "set", "logical_switch", lsName, "other-config:exclude_ips=" + gatewayIP.String(),
		)

		if envArgs.IP != "" {
			portIP = net.ParseIP(string(envArgs.IP))
			if portIP == nil {
				return fmt.Errorf("invalid pod IP %q", string(envArgs.IP))
			}
			if !n.Subnet.Contains(portIP) {
				return fmt.Errorf("switch subnet %q does not contain requested pod IP %q", n.Subnet.String(), string(envArgs.IP)	)
			}
		}
	}
	stdout, stderr, err := util.RunOVNNbctl(lsArgs...)
	if err != nil {
		return fmt.Errorf("Failed to create logical switch, stdout: %q, "+
			"stderr: %q, error: %v", stdout, stderr, err)
	}

	portMac := string(envArgs.MAC)
	if portMac != "" {
		if _, err := net.ParseMAC(portMac); err != nil {
			return fmt.Errorf("failed to parse requested pod MAC %q: %v", portMac, err)
		}
	}

	// Add the container port to the logical switch with any requested MACs or IPs
	portName := ensurePrefix(fmt.Sprintf("%s_%s_%s", podNamespace, podName, n.Name))
	var isStatic bool
	lspArgs := []string{
		"--wait=sb", "--may-exist", "lsp-add", lsName, portName,
	}
	if n.Subnet != nil {
		lspArgs = append(lspArgs,
			"--", "--if-exists", "clear", "logical_switch_port", portName, "dynamic_addresses",
			"--", "lsp-set-addresses", portName,
		)
		if portMac != "" && portIP != nil {
			lspArgs = append(lspArgs, portMac, portIP.String())
			isStatic = true
 		} else if portMac == "" && portIP != nil || portMac != "" && portIP == nil {
			return fmt.Errorf("cannot mix static/dynamic MAC (%s) and IP (%s)", portMac, portIP) 			
 		} else {
			lspArgs = append(lspArgs, "dynamic")
 		}
	} else {
		if portIP != nil {
			return fmt.Errorf("static IPAM requires switch subnet")
		}
		if portMac == "" {
			portMac = util.GenerateMac()
		}
		lspArgs = append(lspArgs,
			"--", "lsp-set-addresses", portName, portMac,
		)
		isStatic = true
	}
	stdout, stderr, err = util.RunOVNNbctl(lspArgs...)
	if err != nil {
		return fmt.Errorf("failed to add logical switch port %s, "+
			"stdout: %q, stderr: %q, error: %v",
			portName, stdout, stderr, err)
	}

	portMac, portIP, err = util.GetPortAddresses(portName, isStatic)
	if err != nil {
		return fmt.Errorf("Error while getting for addresses for %q: %v", portName, err)
	}

	// Create the container veth
	var ipConfig *current.IPConfig
	var portCIDR *net.IPNet
	if portIP != nil {
		portCIDR = &net.IPNet{IP:portIP, Mask:n.Subnet.Mask}
		ipVer := "6"
		if portIP.To4() != nil {
			ipVer = "4"
		}
		ipConfig = &current.IPConfig{
			Version:   ipVer,
			Interface: current.Int(1),
			Address:   *portCIDR,
			Gateway:   gatewayIP,
		}
	}

	hostIface, contIface, err := setupVeth(netns, args.IfName, n.MTU, portMac, portCIDR)
	if err != nil {
		return err
	}

	ovsArgs := []string{
		"add-port", config.Ovn.OvsBridge, hostIface.Name,
		"--", "set", "interface", hostIface.Name,
		fmt.Sprintf("external_ids:attached_mac=%s", portMac),
		fmt.Sprintf("external_ids:iface-id=%s", portName),
	}
	stdout, stderr, err = util.RunOVSVsctl(ovsArgs...)
	if err != nil {
		return fmt.Errorf("failed to add port %q to OVS, "+
			"stdout: %q, stderr: %q, error: %v",
			portName, stdout, stderr, err)
	}

	result := &current.Result{
		Interfaces: []*current.Interface{hostIface, contIface},
	}
	if ipConfig != nil {
		result.IPs = append(result.IPs, ipConfig)
	}
	return types.PrintResult(result, cniVersion)
}

func cmdCheck(args *skel.CmdArgs) error {
	return nil
}

func cmdDel(args *skel.CmdArgs) error {
	_, _, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	if args.Netns == "" {
		return nil
	}

	// There is a netns so try to clean up. Delete can be called multiple times
	// so don't return an error if the device is already removed.
	err = ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		if err := ip.DelLinkByName(args.IfName); err != nil {
			if err != ip.ErrLinkNotFound {
				return err
			}
		}
		return nil
	})

	return err
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, "An OVN L2 switch plugin")
}
