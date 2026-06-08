//go:build windows

package control

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	maxInterfaceNameLen = 256
	maxPhysAddrLen      = 8
	maxIfDescrLen       = 256
	ifTypeLoopback      = 24
)

var getIfTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetIfTable")

type mibIfRow struct {
	Name            [maxInterfaceNameLen]uint16
	Index           uint32
	Type            uint32
	MTU             uint32
	Speed           uint32
	PhysAddrLen     uint32
	PhysAddr        [maxPhysAddrLen]byte
	AdminStatus     uint32
	OperStatus      uint32
	LastChange      uint32
	InOctets        uint32
	InUcastPkts     uint32
	InNUcastPkts    uint32
	InDiscards      uint32
	InErrors        uint32
	InUnknownProtos uint32
	OutOctets       uint32
	OutUcastPkts    uint32
	OutNUcastPkts   uint32
	OutDiscards     uint32
	OutErrors       uint32
	OutQLen         uint32
	DescrLen        uint32
	Descr           [maxIfDescrLen]byte
}

func readNonLoopbackNetworkBytes() (uint64, error) {
	var size uint32
	_, _, err := getIfTable.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if size == 0 {
		return 0, err
	}
	buf := make([]byte, size)
	ret, _, err := getIfTable.Call(uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)), 0)
	if ret != 0 {
		return 0, err
	}
	count := *(*uint32)(unsafe.Pointer(&buf[0]))
	base := uintptr(unsafe.Pointer(&buf[4]))
	rowSize := unsafe.Sizeof(mibIfRow{})
	var total uint64
	for i := uint32(0); i < count; i++ {
		row := (*mibIfRow)(unsafe.Pointer(base + uintptr(i)*rowSize))
		if row.Type == ifTypeLoopback {
			continue
		}
		total += uint64(row.OutOctets)
	}
	return total, nil
}
