//go:build windows
// +build windows

package machinecode

import (
	"fmt"
	"net"
	"strings"

	"github.com/yusufpapurcu/wmi" // 也可替换为 "github.com/bi-zone/wmi"
	"golang.org/x/sys/windows/registry"
)

/*
1、GetSystemUUID() (string, error) 获取系统 UUID
2、GetOSSerial() (string, error) 获取系统序列号，并非硬件 UUID，等同于windows.GetWindowsProductId()
3、GetMotherboardSerial() (string, error) 获取主板序列号
4、GetBIOSSerial() (string, error) 获取BIOS序列号
5、GetCPUId() (string, error) 获取 CPU ID，同类型或同一批次的CPU值一致
6、GetAllDiskIds() ([]string, error) 获取所有硬盘 ID（序列号）
7、GetAllMACAddress() ([]string, error) 获取所有网卡 MAC 地址
8、GetDriveDiskSerial() (string, error) 获取一个硬盘序列号，如果只有一个硬盘，则返回该硬盘的序列号，如果多个硬盘，则返回C盘对应的序列号
9、GetCDriveDiskSerial() (string, error) 获取C盘对应硬盘序列号
10、GetWindowsMachineGuid() (string, error) 通过注册表获取 windows MachineGUID
11、GetWindowsProductId() (string, error)  获取 windows 产品ID ProductId，等同于windows.GetOSSerial()
12、GetWindowsMachineGuid() (string, error) 通过注册表获取 windows MachineGUID
13、GetOneMacAddress() (string, error) 获取一个网卡 MAC 地址，优先WLAN/WI-FI网卡
14、GetGraphicsOutput() (uint32, uint32, uint32, error) 查询显卡的输出的长、高、分辨率；慎用且仅返回一个结果
15、
*/

// ############## WMI 获取硬件信息 ###############

// 获取系统 UUID（优先返回有效 UUID，过滤默认占位符） WMI实现
func GetSystemUUID() (string, error) {
	var systemProducts []Win32_ComputerSystemProduct
	// WQL 查询：获取 UUID 及相关产品信息
	query := "SELECT UUID, Name, Vendor, IdentifyingNumber FROM Win32_ComputerSystemProduct"
	err := wmi.Query(query, &systemProducts)
	if err != nil {
		return "", fmt.Errorf("查询 Win32_ComputerSystemProduct 失败：%w", err)
	}

	if len(systemProducts) == 0 {
		return "", fmt.Errorf("未获取到系统产品信息，无法提取 UUID")
	}
	fmt.Println(systemProducts[0])
	// 提取并清理 UUID
	uuid := strings.TrimSpace(systemProducts[0].UUID)
	// 过滤无效占位符（部分未初始化设备/虚拟机可能返回的默认值）
	invalidUUIDs := map[string]bool{
		"":                                     true,
		"00000000-0000-0000-0000-000000000000": true,
		"FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF": true,
		"To be filled by O.E.M.":               true,
		"None":                                 true,
	}

	if invalidUUIDs[uuid] {
		return "", fmt.Errorf("获取到无效系统 UUID（占位符/默认值）")
	}

	return uuid, nil
}

// 系统安装序列号 SerialNumber，WMI实现 并非硬件 UUID，值等同于windows.GetWindowsProductId()
func GetOSSerial() (string, error) {
	var osList []Win32_OperatingSystem
	err := wmi.Query("SELECT SerialNumber FROM Win32_OperatingSystem", &osList)
	if err != nil || len(osList) == 0 {
		return "", fmt.Errorf("查询系统序列号失败：%w", err)
	}
	return strings.TrimSpace(osList[0].SerialNumber), nil
}

// 获取主板序列号 WMI实现
func GetMotherboardSerial() (string, error) {
	var baseBoards []Win32_BaseBoard
	query := "SELECT SerialNumber FROM Win32_BaseBoard"
	err := wmi.Query(query, &baseBoards)
	if err != nil || len(baseBoards) == 0 {
		return "", err
	}
	serial := strings.TrimSpace(baseBoards[0].SerialNumber)
	// 过滤占位符
	if serial == "To be filled by O.E.M." || serial == "None" || serial == "" {
		return "", fmt.Errorf("boardSerial is empty")
	}
	return serial, nil
}

// 获取BIOS序列号 WMI实现
func GetBIOSSerial() (string, error) {
	var biosList []Win32_BIOS
	query := "SELECT SerialNumber FROM Win32_BIOS"
	if err := wmi.Query(query, &biosList); err != nil || len(biosList) == 0 {
		return "", err
	}
	serial := strings.TrimSpace(biosList[0].SerialNumber)
	if serial == "To be filled by O.E.M." || serial == "None" || serial == "" {
		return "", fmt.Errorf("BIOSSerial is empty")
	}
	return serial, nil
}

// 获取 CPU ID，同类型或同一批次的CPU值一致 WMI实现
func GetCPUId_WIM() (string, error) {
	var processors []Win32_Processor
	// 执行 WQL 查询：查询 Win32_Processor 的所有实例，获取 ProcessorId 和 Name
	query := "SELECT ProcessorId, Name FROM Win32_Processor"
	err := wmi.Query(query, &processors)
	if err != nil {
		return "", fmt.Errorf("查询 CPU 信息失败：%w", err)
	}
	if len(processors) == 0 {
		return "", fmt.Errorf("未获取到 CPU 信息")
	}
	return strings.TrimSpace(processors[0].ProcessorId), nil
}

// 获取所有网卡 MAC 地址 WMI实现
func GetAllMACAddress() ([]Win32_NetworkAdapterConfiguration, error) {
	var netAdapters []Win32_NetworkAdapterConfiguration
	// 执行 WQL 查询：查询启用 IP 的网卡，获取 MAC 地址和描述
	query := "SELECT MACAddress, Description, IPEnabled FROM Win32_NetworkAdapterConfiguration"
	err := wmi.Query(query, &netAdapters)
	if err != nil {
		return nil, fmt.Errorf("查询网卡信息失败：%w", err)
	}
	// var macAddresses []string
	// for _, n := range netAdapters {
	// 	if n.MACAddress != "" {
	// 		macAddresses = append(macAddresses, fmt.Sprintf("网卡描述：%s，MAC 地址：%s", n.Description, n.MACAddress))
	// 	}
	// }
	var newNetAdapters []Win32_NetworkAdapterConfiguration
	for _, n := range netAdapters {
		if n.MACAddress != "" {
			newNetAdapters = append(newNetAdapters, n)
		}
	}
	return newNetAdapters, nil
}

// 获取所有硬盘的ID（序列号） WMI实现
func GetAllDiskIds() ([]Win32_DiskDrive, error) {
	var diskDrives []Win32_DiskDrive
	// 执行 WQL 查询：查询 Win32_DiskDrive 的所有实例，获取序列号、设备ID和型号
	query := "SELECT SerialNumber, DeviceID, Model FROM Win32_DiskDrive"
	err := wmi.Query(query, &diskDrives)
	if err != nil {
		return nil, fmt.Errorf("查询硬盘信息失败：%w", err)
	}

	var newDiskDrives []Win32_DiskDrive
	for _, d := range diskDrives {
		if d.SerialNumber != "" {
			newDiskDrives = append(newDiskDrives, d)
		}
	}
	return newDiskDrives, nil
}

// 获取一个硬盘序列号，如果只有一个硬盘，则返回该硬盘的序列号，如果多个硬盘，则返回C盘对应的序列号
func GetDriveDiskSerial_WIM() (string, error) {
	// 方案一获取所有硬盘的序列号，如果只有1个直接返回
	var diskDrives []Win32_DiskDrive
	// query := "SELECT SerialNumber,DeviceID,Model FROM Win32_DiskDrive"
	query := "SELECT SerialNumber,Model FROM Win32_DiskDrive"
	if err := wmi.Query(query, &diskDrives); err != nil || len(diskDrives) == 0 {
		return "", err
	}
	if len(diskDrives) == 1 {
		return strings.TrimSpace(diskDrives[0].SerialNumber), nil
	}
	// 方案二 获取Bootable = TRUE的分区，如果分区所在的磁盘都是同一个物理磁盘，则返回该硬盘的DiskIndex，然后根据DiskIndex获取硬盘的序列号
	var diskPartition []Win32_DiskPartition // 1块硬盘可能存在多个分区，所以不能简单的len(diskPartition)==1
	query = "SELECT DiskIndex,DeviceID FROM Win32_DiskPartition WHERE Bootable = TRUE"
	if err := wmi.Query(query, &diskPartition); err != nil || len(diskPartition) == 0 {
		return "", err
	}
	var index = make(map[uint32]struct{}) // 对磁盘去重
	for _, part := range diskPartition {
		index[part.DiskIndex] = struct{}{}
	}
	if len(index) == 1 { // 如果只有一个硬盘
		var diskDrives []Win32_DiskDrive
		query = fmt.Sprintf("SELECT SerialNumber FROM Win32_DiskDrive WHERE Index = %d", diskPartition[0].DiskIndex)
		if err := wmi.Query(query, &diskDrives); err != nil || len(diskDrives) == 0 {
			return "", err
		}
		return strings.TrimSpace(diskDrives[0].SerialNumber), nil
	}
	// 方案三 这里省略了步骤一，需要查询3次
	return GetCDriveDiskSerial()
}

// 获取C盘对应硬盘序列号
// 步骤1：查询C盘逻辑磁盘；步骤2：查询C盘分区关联，解析分区DeviceID；步骤3：查询分区对应的硬盘索引；步骤4：查询硬盘序列号
func GetCDriveDiskSerial() (string, error) {
	// 步骤1：查询C盘逻辑磁盘

	// var logicalDisks []Win32_LogicalDisk
	// cQuery := "SELECT DeviceID,DriveType FROM Win32_LogicalDisk WHERE DeviceID = 'C:' AND DriveType = 3"
	// if err := wmi.Query(cQuery, &logicalDisks); err != nil || len(logicalDisks) == 0 {
	// 	return "", err
	// }
	// fmt.Println(logicalDisks[0].DeviceID)

	// 步骤2：查询C盘分区关联
	var diskToParts []Win32_LogicalDiskToPartition
	assocQuery := `SELECT Antecedent FROM Win32_LogicalDiskToPartition WHERE Dependent = 'Win32_LogicalDisk.DeviceID="C:"'`
	if err := wmi.Query(assocQuery, &diskToParts); err != nil || len(diskToParts) == 0 {
		return "", err
	}
	// 解析分区DeviceID
	partitionID := ""
	antecedent := diskToParts[0].Antecedent
	if strings.Contains(antecedent, "DeviceID=") {
		parts := strings.Split(antecedent, "DeviceID=")
		if len(parts) > 1 {
			partitionID = strings.Trim(parts[1], "\"")
		}
	}
	if partitionID == "" {
		return "", fmt.Errorf("partitionID is empty")
	}
	// 步骤3：查询分区对应的硬盘索引
	var partitions []Win32_DiskPartition
	partQuery := fmt.Sprintf("SELECT DiskIndex FROM Win32_DiskPartition WHERE DeviceID = '%s'", partitionID)
	if err := wmi.Query(partQuery, &partitions); err != nil || len(partitions) == 0 {
		return "", err
	}
	diskIndex := partitions[0].DiskIndex
	// 步骤4：查询硬盘序列号
	var diskDrives []Win32_DiskDrive
	diskQuery := fmt.Sprintf("SELECT SerialNumber,Model FROM Win32_DiskDrive WHERE Index = %d", diskIndex)
	if err := wmi.Query(diskQuery, &diskDrives); err != nil || len(diskDrives) == 0 {
		return "", err
	}

	serial := strings.TrimSpace(diskDrives[0].SerialNumber)
	if serial == "" || serial == "None" {
		return "", fmt.Errorf("serial is empty")
	}
	return serial, nil
}

// 查询显卡的输出的长、高、分辨率，仅返回一个
func GetGraphicsOutput() (uint32, uint32, uint32, error) {
	var videoController []Win32_VideoController
	query := `SELECT Name, CurrentHorizontalResolution, CurrentVerticalResolution, CurrentRefreshRate, DeviceID FROM Win32_VideoController`
	if err := wmi.Query(query, &videoController); err != nil || len(videoController) == 0 {
		return 0, 0, 0, err
	}
	for _, video := range videoController {
		if video.CurrentHorizontalResolution != 0 && video.CurrentVerticalResolution != 0 && video.CurrentRefreshRate != 0 {
			return video.CurrentHorizontalResolution, video.CurrentVerticalResolution, video.CurrentRefreshRate, nil
		}
	}
	return 0, 0, 0, fmt.Errorf("所有的显示器的信息获取不完整")
}

// ############## 通过注册表获取信息 ##############

// 通过注册表获取 windows MachineGUID
func GetWindowsMachineGuid() (string, error) {
	// 注册表路径：系统设备标识存储位置
	keyPath := `SOFTWARE\Microsoft\Cryptography`
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, keyPath, registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("打开注册表失败：%w", err)
	}
	defer key.Close()

	deviceID, _, err := key.GetStringValue("MachineGuid")
	if err != nil {
		return "", fmt.Errorf("读取设备标识符失败：%w", err)
	}
	return deviceID, nil
}

// 通过注册表获取 Windows的产品ID（ProductId） 值等同于windows.GetOSSerial()
func GetWindowsProductId() (string, error) {
	// 注册表路径：系统设备标识存储位置
	keyPath := `SOFTWARE\Microsoft\Windows NT\CurrentVersion`
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, keyPath, registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("打开注册表失败：%w", err)
	}
	defer key.Close()

	deviceID, _, err := key.GetStringValue("ProductId")
	if err != nil {
		return "", fmt.Errorf("读取设备标识符失败：%w", err)
	}
	return deviceID, nil
}

// ############## 通过内置包获取信息 ################
// func GetMACAddressByName(interfaceName string) (string, error) {
// 	iface, err := net.InterfaceByName(interfaceName)
// 	if err != nil {
// 		return "", err
// 	}
// 	if iface.HardwareAddr == nil {
// 		return "", fmt.Errorf("no MAC address found")
// 	}
// 	return iface.HardwareAddr.String(), nil
// }

// 得到一个MacAddress，优先获取WLAN网卡
func GetOneMacAddress_WIM() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue // 忽略回环接口
		}
		if iface.HardwareAddr.String() == "" {
			continue // 忽略MAC地址为空的
		}
		if iface.Name == "WLAN" { // 优先获取WLAN网卡
			return iface.HardwareAddr.String(), nil
		}
	}
	networkAdapters, err := GetAllMACAddress()
	if err != nil {
		return "", err
	}
	var newNetworkAdapters []Win32_NetworkAdapterConfiguration
	for _, networkAdapter := range networkAdapters {
		name := strings.ToLower(networkAdapter.Description)
		if strings.Contains(name, "tap-win") || // openvpn 网卡
			strings.Contains(name, "virtual") || // 任何带virtual的网卡，比如：Microsoft Wi-Fi Direct Virtual Adapter 是 Windows 系统内置的虚拟无线网卡，核心作用是实现 Wi-Fi Direct（无线直连） 功能，简单来说就是让设备无需路由器，直接与其他支持 Wi-Fi Direct 的设备（如手机、打印机、电脑）进行无线通信。
			strings.Contains(name, "virtualbox") || // virtualbox 网卡
			strings.Contains(name, "bluetooth") || // 蓝牙网卡
			strings.Contains(name, "docker") || // docker 网卡
			strings.Contains(name, "miniport") || // WAN Miniport (IP) 是 Windows 内置的虚拟网卡，核心是为 VPN、拨号等广域网连接提供 IP 协议驱动支持
			strings.Contains(name, "vmware") || // VMware 虚拟网卡
			strings.Contains(name, "loopback") || // 本地环回接口
			strings.Contains(name, "vethernet ") || // WSL2 网络桥接 / 转发
			strings.Contains(name, "remote") || // Remote NDIS based Internet Sharing Device：USB 共享网络、手机热点 USB 连接
			strings.Contains(name, "hyper-v") || // Hyper-V 虚拟网卡
			strings.Contains(name, "vpn") { // 包含vpn相关的网卡
			continue
		}
		newNetworkAdapters = append(newNetworkAdapters, networkAdapter)
	}
	if len(newNetworkAdapters) == 1 {
		return newNetworkAdapters[0].MACAddress, nil
	}
	if len(newNetworkAdapters) == 0 {
		return "", fmt.Errorf("no MAC address found")
	}
	for _, networkAdapter := range newNetworkAdapters {
		name := strings.ToLower(networkAdapter.Description)
		if strings.Contains(name, "wi-fi") {
			return networkAdapter.MACAddress, nil
		}
		if strings.Contains(name, "family") || strings.Contains(name, "2.5gb") {
			return networkAdapter.MACAddress, nil
		}
	}
	return newNetworkAdapters[0].MACAddress, nil
}

// ############## WMI 获取硬件信息 ##############

// 对应 Win32_ComputerSystemProduct 类，存储系统硬件核心标识
type Win32_ComputerSystemProduct struct {
	UUID              string `wmi:"UUID"`              // 系统唯一 UUID（核心属性） 74DB3F80-XXXX-XXXX-XXXX-0F7D09EXXFXX
	Name              string `wmi:"Name"`              // 产品名称 KUANGSHI Series
	Vendor            string `wmi:"Vendor"`            // 设备厂商 MECHREVO
	IdentifyingNumber string `wmi:"IdentifyingNumber"` // 设备序列号（与 UUID 关联） 425052C32551520565
}

// 通过Win32_OperatingSystem 获取系统标识（非标准 UUID）
// SerialNumber 是系统安装序列号，并非硬件 UUID
type Win32_OperatingSystem struct {
	SerialNumber string `wmi:"SerialNumber"` // 系统安装序列号 XXXXX-XXXXX-XXXXX-AAOEM
	// RegisteredUser string `wmi:"RegisteredUser"` // 注册用户 xxxxxxxx@qq.com
	// PSComputerName string `wmi:"PSComputerName"` // 计算机名称 DESKTOP-KXXXXX
	// Name           string `wmi:"Name"`           // 系统名称 Microsoft Windows 11 家庭中文版|C:\Windows|\Device\Harddisk0\Partition3
	// MUILanguages   string `wmi:"MUILanguages"`   // 语言列表 {zh-CN}
	// Caption        string `wmi:"Caption"`        // 系统名称 Microsoft Windows 11 家庭中文版
}

// 主板信息
type Win32_BaseBoard struct {
	SerialNumber string `wmi:"SerialNumber"` // 主板序列号 有的电脑显示为 Standard
}

// BIOS信息
type Win32_BIOS struct {
	SerialNumber string `wmi:"SerialNumber"` // BIOS序列号 425052X3255X520565
}

// 定义对应 Win32_Processor WMI 类的结构体
type Win32_Processor struct {
	ProcessorId string `wmi:"ProcessorId"` // 对应 WMI 的 ProcessorId 属性，字段名需与 WMI 属性一致（大小写不敏感）
	Name        string `wmi:"Name"`        // 可选，CPU 名称，用于辅助识别
}

// 定义对应 Win32_NetworkAdapterConfiguration WMI 类的结构体
type Win32_NetworkAdapterConfiguration struct {
	MACAddress  string // 网卡 MAC 地址
	IPEnabled   bool   // 是否启用 IP，用于过滤有效网卡
	Description string // 网卡描述，可选
}

// Win32_VideoController：显卡/显示控制器信息
type Win32_VideoController struct {
	Name                        string
	CurrentHorizontalResolution uint32
	CurrentVerticalResolution   uint32
	CurrentRefreshRate          uint32
	DeviceID                    string
}

// 1. 逻辑磁盘（对应C:、D:等盘符）
type Win32_LogicalDisk struct {
	DeviceID  string `wmi:"DeviceID"`  // 盘符，如 "C:"
	DriveType uint32 `wmi:"DriveType"` // 驱动器类型：3=本地固定磁盘，2=可移动磁盘，5=光驱
}

// 2. 逻辑磁盘与磁盘分区的关联类
type Win32_LogicalDiskToPartition struct {
	Antecedent string `wmi:"Antecedent"` // 对应 Win32_DiskPartition 的路径
	Dependent  string `wmi:"Dependent"`  // 对应 Win32_LogicalDisk 的路径
}

// 3. 磁盘分区
type Win32_DiskPartition struct {
	DeviceID  string `wmi:"DeviceID"`  // 分区ID，如 "Disk #0, Partition #1"
	DiskIndex uint32 `wmi:"DiskIndex"` // 对应物理硬盘的索引 0
}

// 定义对应 Win32_DiskDrive WMI 类的结构体
type Win32_DiskDrive struct {
	SerialNumber string `wmi:"SerialNumber"` // 硬盘序列号（硬盘 ID）
	DeviceID     string `wmi:"DeviceID"`     // 硬盘设备ID
	Model        string `wmi:"Model"`        // 硬盘型号，可选
}

// ############## WMI 获取硬件信息 ###############
