package vm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/vishvananda/netlink"
)

// Boot config struct
type Boot struct {
	Kernel string `json:"kernel_image_path"`
	Initrd string `json:"initrd_path,omitempty"`
	Args   string `json:"boot_args"`
}

// Disk struct
type Disk struct {
	ID         string `json:"drive_id"`
	Path       string `json:"path_on_host"`
	RootDevice bool   `json:"is_root_device"`
	ReadOnly   bool   `json:"is_read_only"`
}

func (d Disk) String() string {
	on := "off"
	if d.ReadOnly {
		on = "on"
	}

	return fmt.Sprintf(`path=%s,readonly=%s`, d.Path, on)
}

// Disks is a list of vm disks
type Disks []Disk

type InterfaceType string

const (
	InterfaceTAP     = "tuntap"
	InterfaceMACvTAP = "macvtap"
)

// Interface nic struct
type Interface struct {
	ID  string `json:"iface_id"`
	Tap string `json:"host_dev_name"`
	Mac string `json:"guest_mac,omitempty"`
}

func (i Interface) AsMACvTap(fd int) string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("fd=%d", fd))
	if len(i.Mac) > 0 {
		buf.WriteString(fmt.Sprintf(",mac=%s", i.Mac))
	}

	return buf.String()
}

func (i Interface) AsTap() string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("tap=%s", i.Tap))
	if len(i.Mac) > 0 {
		buf.WriteString(fmt.Sprintf(",mac=%s", i.Mac))
	}

	return buf.String()
}

// Type detects the interface type
func (i *Interface) Type() (InterfaceType, int, error) {
	link, err := netlink.LinkByName(i.Tap)
	if err != nil {
		return "", 0, err
	}
	log.Debug().Str("name", i.Tap).Str("type", link.Type()).Msg("checking device type")

	switch link.Type() {
	case InterfaceMACvTAP:
		return InterfaceMACvTAP, link.Attrs().Index, nil
	case InterfaceTAP:
		return InterfaceTAP, link.Attrs().Index, nil
	default:
		return "", 0, fmt.Errorf("unknown tap type")
	}
}

// Interfaces is a list of node interfaces
type Interfaces []Interface

// MemMib is memory size in mib
type MemMib int64

func (m MemMib) String() string {
	return fmt.Sprintf("size=%dM", int64(m))
}

// CPU type
type CPU uint8

func (c CPU) String() string {
	return fmt.Sprintf("boot=%d", c)
}

// Config struct
type Config struct {
	CPU       CPU    `json:"vcpu_count"`
	Mem       MemMib `json:"mem_size_mib"`
	HTEnabled bool   `json:"ht_enabled"`
}

// Machine struct
type Machine struct {
	ID         string     `json:"id"`
	Boot       Boot       `json:"boot-source"`
	Disks      Disks      `json:"drives"`
	Interfaces Interfaces `json:"network-interfaces"`
	Config     Config     `json:"machine-config"`
	// NoKeepAlive is not used by firecracker, but instead a marker
	// for the vm  mananger to not restart the machine when it stops
	NoKeepAlive bool `json:"no-keep-alive"`
}

// Save saves a machine into a file
func (m *Machine) Save(n string) error {
	f, err := os.Create(n)
	if err != nil {
		return errors.Wrap(err, "failed to create vm config file")
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(m); err != nil {
		return errors.Wrap(err, "failed to serialize machine object")
	}

	return nil
}

// MachineFromFile loads a vm config from file
func MachineFromFile(n string) (*Machine, error) {
	f, err := os.Open(n)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open machine config file")
	}
	defer f.Close()
	var m Machine
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return nil, errors.Wrap(err, "failed to decode machine config file")
	}

	return &m, nil
}
