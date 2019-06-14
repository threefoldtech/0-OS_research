package namespace

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	netNSPath = "/var/run/netns"
)

func CreateNetNS(name string) (netns.NsHandle, error) {
	// create a network namespace
	ns, err := netns.New()
	if err != nil {
		return ns, err
	}
	defer ns.Close()

	// set its ID
	// In an attempt to avoid namespace id collisions, set this to something
	// insanely high. When the kernel assigns IDs, it does so starting from 0
	// So, just use our pid shifted up 16 bits
	// wantID := os.Getpid() << 16

	// h, err := netlink.NewHandle()
	// if err != nil {
	// 	return nil, err
	// }
	// h.SetNetNsIdByFd
	// err = h.SetNetNsIdByFd(int(ns), wantID)
	// if err != nil {
	// 	return nil, err
	// }

	return ns, mountBindNetNS(name)
}

func DeleteNetNS(name string) error {
	path := filepath.Join(netNSPath, name)
	ns, err := netns.GetFromPath(path)
	if err != nil {
		return err
	}

	if err := ns.Close(); err != nil {
		return err
	}

	fmt.Println("ns found")
	if err := syscall.Unmount(path, syscall.MNT_FORCE); err != nil {
		return err
	}
	fmt.Println("umounted")

	if err := os.Remove(path); err != nil {
		return err
	}
	fmt.Println("removed")

	return nil
}

func mountBindNetNS(name string) error {
	log.Info().Msg("create netnsPath")
	if err := os.MkdirAll(netNSPath, 0660); err != nil {
		return err
	}

	nsPath := filepath.Join(netNSPath, name)
	log.Info().Msg("create file")
	if err := touch(nsPath); err != nil {
		return err
	}

	log.Info().Msg("bind mount")
	return syscall.Mount("/proc/self/ns/net", nsPath, "bind", syscall.MS_BIND, "")
}

func touch(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL, 0660)
	if err != nil {
		return err
	}
	return f.Close()
}

func SetLinkNS(link netlink.Link, name string) error {
	ns, err := netns.GetFromName(name)
	if err != nil {
		return err
	}
	defer ns.Close()

	handle, err := netlink.NewHandleAt(ns)
	if err != nil {
		return err
	}

	return handle.LinkSetNsFd(link, int(ns))
}

func ExecInNS(ns string, f func() error) error {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	log.Info().Msg("get origin ns")
	origin, err := netns.Get()
	if err != nil {
		return err
	}
	defer origin.Close()

	log.Info().Msg("getfrom name")
	workingNS, err := netns.GetFromName(ns)
	if err != nil {
		return err
	}

	log.Info().Msg("switch to ns")
	if err := netns.Set(workingNS); err != nil {
		return err
	}
	defer workingNS.Close()

	log.Info().Msg("exec f")
	err = f()

	log.Info().Msg("reset to origin ns")
	return netns.Set(origin)
}

type NSContext struct {
	origin  netns.NsHandle
	working netns.NsHandle
}

func (c *NSContext) Enter(nsName string) error {
	// Lock thread to prevent switching of namespaces
	runtime.LockOSThread()

	var err error
	c.origin, err = netns.Get()
	if err != nil {
		return err
	}

	c.working, err = netns.GetFromName(nsName)
	if err != nil {
		return err
	}

	return netns.Set(c.working)
}

func (c *NSContext) Exit() error {
	// always unlock thread
	defer runtime.UnlockOSThread()

	// Switch back to the original namespace
	if err := netns.Set(c.origin); err != nil {
		return err
	}
	// close working namespace
	if err := c.working.Close(); err != nil {
		return err
	}
	// close origin namespace
	// if err := c.origin.Close(); err != nil {
	// 	return err
	// }
	return nil
}
