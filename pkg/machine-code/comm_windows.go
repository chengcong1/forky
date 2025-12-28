//go:build windows
// +build windows

package machinecode

type MachineCode struct {
	MachineId  string
	DiskSerial string
	MacAddress string
	CpuId      string
}

func GetMachineCode() (MachineCode, []error) {
	// var machineFingerprint string
	// machineFingerprint = machineFingerprint + "|" + diskSerial
	var machineCode MachineCode
	var errs []error
	var err error
	machineCode.CpuId, err = GetCPUId_WIM()
	if err != nil {
		errs = append(errs, err)
	}
	machineCode.DiskSerial, err = GetDriveDiskSerial_WIM()
	if err != nil {
		errs = append(errs, err)
	}
	machineCode.MacAddress, err = GetOneMacAddress_WIM()
	if err != nil {
		errs = append(errs, err)
	}
	machineCode.MachineId, err = GetWindowsMachineGuid()
	if err != nil {
		errs = append(errs, err)
	}
	return machineCode, errs
	// return machineCode, nil
}
