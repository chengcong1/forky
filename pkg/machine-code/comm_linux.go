//go:build linux
// +build linux

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
	machineCode.CpuId, err = GetCPUID()
	if err != nil {
		errs = append(errs, err)
	}
	machineCode.DiskSerial, err = GetDiskSerialNumber()
	if err != nil {
		errs = append(errs, err)
	}
	machineCode.MacAddress, err = GetOneMacAddress()
	if err != nil {
		errs = append(errs, err)
	}
	machineCode.MachineId, err = GetMachineId()
	if err != nil {
		errs = append(errs, err)
	}
	return machineCode, errs
}
