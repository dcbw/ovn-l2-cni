package util

import (
	"fmt"
	"math/big"
	"math/rand"
	"net"
	"strings"
	"time"
)

// GenerateMac generates mac address.
func GenerateMac() string {
	prefix := "00:00:00"
	newRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	mac := fmt.Sprintf("%s:%02X:%02X:%02X", prefix, newRand.Intn(255), newRand.Intn(255), newRand.Intn(255))
	return mac
}

// NextIP returns IP incremented by 1
func NextIP(ip net.IP) net.IP {
	i := ipToInt(ip)
	return intToIP(i.Add(i, big.NewInt(1)))
}

func ipToInt(ip net.IP) *big.Int {
	if v := ip.To4(); v != nil {
		return big.NewInt(0).SetBytes(v)
	}
	return big.NewInt(0).SetBytes(ip.To16())
}

func intToIP(i *big.Int) net.IP {
	return net.IP(i.Bytes())
}

// GetPortAddresses returns the MAC and IP of the given logical switch port
func GetPortAddresses(portName string, isStaticIP bool) (string, net.IP, error) {
	addrType := "dynamic_addresses"
	if isStaticIP {
		addrType = "addresses"
	}
	out, _, err := RunOVNNbctl("get", "logical_switch_port", portName, addrType)
	if err != nil {
		return "", nil, fmt.Errorf("Error while obtaining addresses for %s: %v", portName, err)
	}
	if out == "[]" {
		// No addresses
		return "", nil, nil
	}

	// static addresses have format ["0a:00:00:00:00:01 192.168.1.3"], while
	// dynamic addresses have format "0a:00:00:00:00:01 192.168.1.3".
	outStr := strings.Trim(out, `[]`)
	outStr = strings.Trim(outStr, `"`)
	addresses := strings.Split(outStr, " ")
	if len(addresses) < 1 || len(addresses) > 2 {
		return "", nil, fmt.Errorf("failed to parse addresess %q for port %s", outStr, portName)
	}
	if _, err := net.ParseMAC(addresses[0]); err != nil {
		return "", nil, fmt.Errorf("failed to parse port %q MAC %q: %v", portName, addresses[0], err)
	}
	var ip net.IP
	if len(addresses) > 1 {
		ip = net.ParseIP(addresses[1])
		if ip == nil {
			return "", nil, fmt.Errorf("failed to parse port %q IP %q", portName, addresses[1])
		}
	}
	return addresses[0], ip, nil
}
