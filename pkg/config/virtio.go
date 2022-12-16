package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// The VirtioDevice interface is an interface which is implemented by all virtio devices.
type VirtioDevice VMComponent

// VirtioVsock configures of a virtio-vsock device allowing 2-way communication
// between the host and the virtual machine type
type VirtioVsock struct {
	// Port is the virtio-vsock port used for this device, see `man vsock` for more
	// details.
	Port uint
	// SocketURL is the path to a unix socket on the host to use for the virtio-vsock communication with the guest.
	SocketURL string
	// If true, vsock connections will have to be done from guest to host. If false, vsock connections will only be possible
	// from host to guest
	Listen bool
}

// VirtioBlk configures a disk device.
type VirtioBlk struct {
	StorageConfig
	DeviceIdentifier string
}

// VirtioFs configures directory sharing between the guest and the host.
type VirtioFs struct {
	SharedDir string
	MountTag  string
}

// virtioRng configures a random number generator (RNG) device.
type VirtioRng struct {
}

// TODO: Add BridgedNetwork support
// https://github.com/Code-Hex/vz/blob/d70a0533bf8ed0fa9ab22fa4d4ca554b7c3f3ce5/network.go#L81-L82

// VirtioNet configures the virtual machine networking.
type VirtioNet struct {
	Nat        bool
	MacAddress net.HardwareAddr
	// file parameter is holding a connected datagram socket.
	// see https://github.com/Code-Hex/vz/blob/7f648b6fb9205d6f11792263d79876e3042c33ec/network.go#L113-L155
	Socket *os.File
}

// VirtioSerial configures the virtual machine serial ports.
type VirtioSerial struct {
	LogFile string
}

// TODO: Add VirtioBalloon
// https://github.com/Code-Hex/vz/blob/master/memory_balloon.go

type option struct {
	key   string
	value string
}

func strToOption(str string) option {
	splitStr := strings.SplitN(str, "=", 2)

	opt := option{
		key: splitStr[0],
	}
	if len(splitStr) > 1 {
		opt.value = splitStr[1]
	}

	return opt
}

func strvToOptions(opts []string) []option {
	parsedOpts := []option{}
	for _, opt := range opts {
		if len(opt) == 0 {
			continue
		}
		parsedOpts = append(parsedOpts, strToOption(opt))
	}

	return parsedOpts
}

func deviceFromCmdLine(deviceOpts string) (VirtioDevice, error) {
	opts := strings.Split(deviceOpts, ",")
	if len(opts) == 0 {
		return nil, fmt.Errorf("empty option list in command line argument")
	}
	var dev VirtioDevice
	switch opts[0] {
	case "virtio-blk":
		dev = virtioBlkNewEmpty()
	case "virtio-fs":
		dev = &VirtioFs{}
	case "virtio-net":
		dev = &VirtioNet{}
	case "virtio-rng":
		dev = &VirtioRng{}
	case "virtio-serial":
		dev = &VirtioSerial{}
	case "virtio-vsock":
		dev = &VirtioVsock{}
	case "usb-mass-storage":
		dev = usbMassStorageNewEmpty()
	default:
		return nil, fmt.Errorf("unknown device type: %s", opts[0])
	}

	parsedOpts := strvToOptions(opts[1:])
	if err := dev.FromOptions(parsedOpts); err != nil {
		return nil, err
	}

	return dev, nil
}

// VirtioSerialNew creates a new serial device for the virtual machine. The
// output the virtual machine sent to the serial port will be written to the
// file at logFilePath.
func VirtioSerialNew(logFilePath string) (VirtioDevice, error) {
	return &VirtioSerial{
		LogFile: logFilePath,
	}, nil
}

func (dev *VirtioSerial) ToCmdLine() ([]string, error) {
	if dev.LogFile == "" {
		return nil, fmt.Errorf("virtio-serial needs the path to the log file")
	}
	return []string{"--device", fmt.Sprintf("virtio-serial,logFilePath=%s", dev.LogFile)}, nil
}

func (dev *VirtioSerial) FromOptions(options []option) error {
	for _, option := range options {
		switch option.key {
		case "logFilePath":
			dev.LogFile = option.value
		default:
			return fmt.Errorf("Unknown option for virtio-serial devices: %s", option.key)
		}
	}
	return nil
}

// VirtioNetNew creates a new network device for the virtual machine. It will
// use macAddress as its MAC address.
func VirtioNetNew(macAddress string) (*VirtioNet, error) {
	var hwAddr net.HardwareAddr

	if macAddress != "" {
		var err error
		if hwAddr, err = net.ParseMAC(macAddress); err != nil {
			return nil, err
		}
	}
	return &VirtioNet{
		Nat:        true,
		MacAddress: hwAddr,
	}, nil
}

// Set the socket to use for the network communication
//
// This maps the virtual machine network interface to a connected datagram
// socket. This means all network traffic on this interface will go through
// file.
// file must be a connected datagram (SOCK_DGRAM) socket.
func (dev *VirtioNet) SetSocket(file *os.File) {
	dev.Socket = file
	dev.Nat = false
}

func (dev *VirtioNet) validate() error {
	if dev.Nat && dev.Socket != nil {
		return fmt.Errorf("'nat' and 'fd' cannot be set at the same time")
	}
	if !dev.Nat && dev.Socket == nil {
		return fmt.Errorf("One of 'nat' or 'fd' must be set")
	}

	return nil
}

func (dev *VirtioNet) ToCmdLine() ([]string, error) {
	if err := dev.validate(); err != nil {
		return nil, err
	}

	builder := strings.Builder{}
	builder.WriteString("virtio-net")
	if dev.Nat {
		builder.WriteString(",nat")
	} else {
		fmt.Fprintf(&builder, ",fd=%d", dev.Socket.Fd())
	}

	if len(dev.MacAddress) != 0 {
		builder.WriteString(fmt.Sprintf(",mac=%s", dev.MacAddress))
	}

	return []string{"--device", builder.String()}, nil
}

func (dev *VirtioNet) FromOptions(options []option) error {
	for _, option := range options {
		switch option.key {
		case "nat":
			if option.value != "" {
				return fmt.Errorf("Unexpected value for virtio-net 'nat' option: %s", option.value)
			}
			dev.Nat = true
		case "mac":
			macAddress, err := net.ParseMAC(option.value)
			if err != nil {
				return err
			}
			dev.MacAddress = macAddress
		case "fd":
			fd, err := strconv.Atoi(option.value)
			if err != nil {
				return err
			}
			dev.Socket = os.NewFile(uintptr(fd), "vfkit virtio-net socket")
		default:
			return fmt.Errorf("Unknown option for virtio-net devices: %s", option.key)
		}
	}

	return dev.validate()
}

// VirtioRngNew creates a new random number generator device to feed entropy
// into the virtual machine.
func VirtioRngNew() (VirtioDevice, error) {
	return &VirtioRng{}, nil
}

func (dev *VirtioRng) ToCmdLine() ([]string, error) {
	return []string{"--device", "virtio-rng"}, nil
}

func (dev *VirtioRng) FromOptions(options []option) error {
	if len(options) != 0 {
		return fmt.Errorf("Unknown options for virtio-rng devices: %s", options)
	}
	return nil
}

func virtioBlkNewEmpty() *VirtioBlk {
	return &VirtioBlk{
		StorageConfig: StorageConfig{
			DevName: "virtio-blk",
		},
		DeviceIdentifier: "",
	}
}

// VirtioBlkNew creates a new disk to use in the virtual machine. It will use
// the file at imagePath as the disk image. This image must be in raw format.
func VirtioBlkNew(imagePath string) (*VirtioBlk, error) {
	virtioBlk := virtioBlkNewEmpty()
	virtioBlk.ImagePath = imagePath

	return virtioBlk, nil
}

func (dev *VirtioBlk) SetDeviceIdentifier(devId string) {
	dev.DeviceIdentifier = devId
}

func (dev *VirtioBlk) FromOptions(options []option) error {
	unhandledOpts := []option{}
	for _, option := range options {
		switch option.key {
		case "deviceId":
			dev.DeviceIdentifier = option.value
		default:
			unhandledOpts = append(unhandledOpts, option)
		}
	}

	if err := dev.StorageConfig.FromOptions(unhandledOpts); err != nil {
		return err
	}

	return nil
}

func (dev *VirtioBlk) ToCmdLine() ([]string, error) {
	cmdLine, err := dev.StorageConfig.ToCmdLine()
	if err != nil {
		return []string{}, err
	}
	if len(cmdLine) != 2 {
		return []string{}, fmt.Errorf("Unexpected storage config commandline")
	}
	if dev.DeviceIdentifier != "" {
		cmdLine[1] = fmt.Sprintf("%s,deviceId=%s", cmdLine[1], dev.DeviceIdentifier)
	}
	return cmdLine, nil
}

// VirtioVsockNew creates a new virtio-vsock device for 2-way communication
// between the host and the virtual machine. The communication will happen on
// vsock port, and on the host it will use the unix socket at socketURL.
// When listen is true, the host will be listening for connections over vsock.
// When listen  is false, the guest will be listening for connections over vsock.
func VirtioVsockNew(port uint, socketURL string, listen bool) (VirtioDevice, error) {
	return &VirtioVsock{
		Port:      port,
		SocketURL: socketURL,
		Listen:    listen,
	}, nil
}

func (dev *VirtioVsock) ToCmdLine() ([]string, error) {
	if dev.Port == 0 || dev.SocketURL == "" {
		return nil, fmt.Errorf("virtio-vsock needs both a port and a socket URL")
	}
	var listenStr string
	if dev.Listen {
		listenStr = "listen"
	} else {
		listenStr = "connect"
	}
	return []string{"--device", fmt.Sprintf("virtio-vsock,port=%d,socketURL=%s,%s", dev.Port, dev.SocketURL, listenStr)}, nil
}

func (dev *VirtioVsock) FromOptions(options []option) error {
	// default to listen for backwards compatibliity
	dev.Listen = true
	for _, option := range options {
		switch option.key {
		case "socketURL":
			dev.SocketURL = option.value
		case "port":
			port, err := strconv.Atoi(option.value)
			if err != nil {
				return err
			}
			dev.Port = uint(port)
		case "listen":
			dev.Listen = true
		case "connect":
			dev.Listen = false
		default:
			return fmt.Errorf("Unknown option for virtio-vsock devices: %s", option.key)
		}
	}
	return nil
}

// VirtioFsNew creates a new virtio-fs device for file sharing. It will share
// the directory at sharedDir with the virtual machine. This directory can be
// mounted in the VM using `mount -t virtiofs mountTag /some/dir`
func VirtioFsNew(sharedDir string, mountTag string) (VirtioDevice, error) {
	return &VirtioFs{
		SharedDir: sharedDir,
		MountTag:  mountTag,
	}, nil
}

func (dev *VirtioFs) ToCmdLine() ([]string, error) {
	if dev.SharedDir == "" {
		return nil, fmt.Errorf("virtio-fs needs the path to the directory to share")
	}
	if dev.MountTag != "" {
		return []string{"--device", fmt.Sprintf("virtio-fs,sharedDir=%s,mountTag=%s", dev.SharedDir, dev.MountTag)}, nil
	} else {
		return []string{"--device", fmt.Sprintf("virtio-fs,sharedDir=%s", dev.SharedDir)}, nil
	}
}

func (dev *VirtioFs) FromOptions(options []option) error {
	for _, option := range options {
		switch option.key {
		case "sharedDir":
			dev.SharedDir = option.value
		case "mountTag":
			dev.MountTag = option.value
		default:
			return fmt.Errorf("Unknown option for virtio-fs devices: %s", option.key)
		}
	}
	return nil
}

type USBMassStorage struct {
	StorageConfig
}

func usbMassStorageNewEmpty() *USBMassStorage {
	return &USBMassStorage{
		StorageConfig{
			DevName: "usb-mass-storage",
		},
	}
}

// USBMassStorageNew creates a new USB disk to use in the virtual machine. It will use
// the file at imagePath as the disk image. This image must be in raw or ISO format.
func USBMassStorageNew(imagePath string) (VMComponent, error) {
	usbMassStorage := usbMassStorageNewEmpty()
	usbMassStorage.ImagePath = imagePath

	return usbMassStorage, nil
}

// StorageConfig configures a disk device.
type StorageConfig struct {
	DevName   string
	ImagePath string
	ReadOnly  bool
}

func (config *StorageConfig) ToCmdLine() ([]string, error) {
	if config.ImagePath == "" {
		return nil, fmt.Errorf("%s devices need the path to a disk image", config.DevName)
	}
	return []string{"--device", fmt.Sprintf("%s,path=%s", config.DevName, config.ImagePath)}, nil
}

func (config *StorageConfig) FromOptions(options []option) error {
	for _, option := range options {
		switch option.key {
		case "path":
			config.ImagePath = option.value
		default:
			return fmt.Errorf("Unknown option for %s devices: %s", config.DevName, option.key)
		}
	}
	return nil
}
