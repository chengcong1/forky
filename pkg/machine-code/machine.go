package machinecode

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/jaypipes/ghw"
	psucpu "github.com/shirou/gopsutil/v4/cpu"
)

// 获取CPU ID 由 gopsutil实现
func GetCPUID() (string, error) {
	cpuInfo, err := psucpu.Info()
	if err != nil {
		return "", fmt.Errorf("获取CPU ID失败：%v", err)
	}
	if len(cpuInfo) == 0 {
		return "", errors.New("未检测到CPU信息")
	}
	// fmt.Println(cpuInfo)
	cpuID := strings.TrimSpace(cpuInfo[0].PhysicalID)
	if cpuID == "" { // 如果cpuInfo[0].PhysicalID不存在，则尝试获取cpuInfo[0].Model
		switch cpuInfo[0].VendorID {
		case "ARM":
			if cpuInfo[0].Model != "" {
				return strings.TrimSpace(cpuInfo[0].Model), nil
			}
		default:
		}
		cpuID = strings.TrimSpace(cpuInfo[0].VendorID + "_" + cpuInfo[0].Family + "_" + cpuInfo[0].ModelName)
	}
	return cpuID, nil
}

// 获取硬盘序列号 ghw实现，windows下建议使用windows.GetDriveDiskSerial()
func GetDiskSerialNumber() (string, error) {
	block, err := ghw.Block()
	if err != nil {
		return "", err
	}
	for _, disk := range block.Disks {
		if disk.IsRemovable {
			continue
		}
		return disk.SerialNumber, nil
	}
	return "", fmt.Errorf("no disk found")
}

// 得到一个MacAddress，优先获取WLAN网卡
func GetOneMacAddress() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	var newInterface []net.Interface
	for _, iface := range interfaces {
		name := strings.ToLower(iface.Name)
		// if iface.Flags&net.FlagUp == 0 {
		// 	continue // 忽略未启动的接口，这个取消万一WLAN网卡没有启用呢
		// }
		// iface.Flags&net.FlagUp 是位运算，检查指定标志位是否被设置
		// & 是按位与操作符，用于检查特定的标志位
		if iface.HardwareAddr.String() == "" {
			continue // 忽略MAC地址为空的
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // 忽略回环接口
		}
		if strings.HasPrefix(name, "tun") ||
			strings.HasPrefix(name, "tap") ||
			strings.HasPrefix(name, "vethernet") {
			continue // 忽略TUN接口  // 忽略vEthernet接口(windows)
		}
		if name == "wlan" || strings.HasPrefix(name, "wl") {
			return iface.HardwareAddr.String(), nil
		}
		newInterface = append(newInterface, iface)
	}
	for _, iface := range newInterface {
		if strings.HasPrefix(iface.Name, "eth") ||
			strings.HasPrefix(iface.Name, "en") {
			return iface.HardwareAddr.String(), nil
		}
		if iface.Name == "以太网" {
			return iface.HardwareAddr.String(), nil
		}
	}
	return "", fmt.Errorf("no MAC address found")
}
