package utils

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

/*

 */

// TranSimToFullIPv6 converts a compressed IPv6 address to its expanded form.
// TranSimToFullIPv6 将压缩的IPv6地址转换为全写，如果是IPV4的地址就直接返回IPV4地址了
func TranSimToFullIPv6(simIP string) (string, error) {
	ip := net.ParseIP(simIP)
	// 判断 IP 是 IPv4 还是 IPv6
	if ip.To4() != nil {
		// 这是一个 IPv4 地址
		return ip.String(), errors.New("ip is ipv4")
	}
	if ip == nil {
		return "ip地址错误", errors.New("parse ip error")
	}
	// Case 1: The IP is "::" (empty IPv6 address).
	if simIP == "::" {
		return "0000:0000:0000:0000:0000:0000:0000:0000", nil
	}

	// Initialize the IPv6 address with all zeroes.
	ipList := []string{"0000", "0000", "0000", "0000", "0000", "0000", "0000", "0000"}

	// Case 2: The IP starts with "::"
	if strings.HasPrefix(simIP, "::") {
		tmplist := strings.Split(simIP, ":")
		for i := range tmplist {
			ipList[i+8-len(tmplist)] = fmt.Sprintf("%04s", tmplist[i]) // Pad each segment to 4 digits
		}
	} else if strings.HasSuffix(simIP, "::") { // Case 3: The IP ends with "::"
		tmplist := strings.Split(simIP, ":")
		for i := range tmplist {
			ipList[i] = fmt.Sprintf("%04s", tmplist[i]) // Pad each segment to 4 digits
		}
	} else if !strings.Contains(simIP, "::") { // Case 4: No "::" in the address
		tmplist := strings.Split(simIP, ":")
		for i := range tmplist {
			ipList[i] = fmt.Sprintf("%04s", tmplist[i]) // Pad each segment to 4 digits
		}
	} else { // Case 5: The IP contains "::" somewhere in the middle
		tmplist := strings.Split(simIP, "::")
		tmplist0 := strings.Split(tmplist[0], ":") // Split the part before "::"
		for i := range tmplist0 {
			ipList[i] = fmt.Sprintf("%04s", tmplist0[i]) // Pad each segment to 4 digits
		}

		tmplist1 := strings.Split(tmplist[1], ":") // Split the part after "::"
		for i := range tmplist1 {
			ipList[i+8-len(tmplist1)] = fmt.Sprintf("%04s", tmplist1[i]) // Pad each segment to 4 digits
		}
	}
	// Join the address segments with ":" and return the result.
	return strings.Join(ipList, ":"), nil
}
