package ndmz

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v3"
	"github.com/threefoldtech/tfexplorer/schema"
	"github.com/threefoldtech/zos/pkg/network/bridge"
	"github.com/threefoldtech/zos/pkg/network/dhcp"
	"github.com/threefoldtech/zos/pkg/network/ifaceutil"
	"github.com/threefoldtech/zos/pkg/network/nft"
	"github.com/threefoldtech/zos/pkg/network/options"
	"github.com/threefoldtech/zos/pkg/network/types"
	"github.com/threefoldtech/zos/pkg/network/yggdrasil"
	"github.com/threefoldtech/zos/pkg/zinit"

	"github.com/threefoldtech/zos/pkg/network/nr"

	"github.com/threefoldtech/zos/pkg/network/macvlan"

	"github.com/rs/zerolog/log"
	"github.com/vishvananda/netlink"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/pkg/errors"
	"github.com/threefoldtech/zos/pkg/network/namespace"
)

const (

	//ndmzBridge is the name of the ipv4 routing bridge in the ndmz namespace
	ndmzBridge = "br-ndmz"
	//dmzNamespace name of the dmz namespace
	dmzNamespace = "ndmz"

	ndmzNsMACDerivationSuffix6 = "-ndmz6"
	ndmzNsMACDerivationSuffix4 = "-ndmz4"

	// dmzPub4 ipv4 public interface
	dmzPub4 = "npub4"
	// dmzPub6 ipv6 public interface
	dmzPub6 = "npub6"

	//nrPubIface is the name of the public interface in a network resource
	nrPubIface = "public"

	tonrsIface = "tonrs"
)

// dmzImpl implement DMZ interface using dual stack ipv4/ipv6
type dmzImpl struct {
	nodeID string
	public *netlink.Bridge
}

// New creates a new DMZ DualStack
func New(nodeID string, public *netlink.Bridge) DMZ {
	return &dmzImpl{
		nodeID: nodeID,
		public: public,
	}
}

// Create create the NDMZ network namespace and configure its default routes and addresses
func (d *dmzImpl) Create(ctx context.Context) error {
	// There are 2 options for the master:
	// - use the interface directly
	// - create a bridge and plug the interface into that one
	// The second option is used by default, and the first one is now legacy.
	// However to not break existing containers, we keep the old one if networkd
	// is restarted but the node is not. In this case, ndmz will already be present.
	//
	// Now, it is possible that we are a 1 nic dualstack node, in which case
	// master will actually be `zos`. In that case, we can't plug the physical
	// iface, but need to create a veth pair between br-pub and zos.

	netNS, err := namespace.GetByName(dmzNamespace)
	if err != nil {
		netNS, err = namespace.Create(dmzNamespace)
		if err != nil {
			return errors.Wrap(err, "failed to create ndmz namespace")
		}
	}

	defer netNS.Close()

	if err := createRoutingBridge(ndmzBridge, netNS); err != nil {
		return errors.Wrapf(err, "ndmz: createRoutingBride error")
	}

	if err := createPubIface6(dmzPub6, d.public, d.nodeID, netNS); err != nil {
		return errors.Wrapf(err, "ndmz: could not node create pub iface 6")
	}

	if err := createPubIface4(dmzPub4, d.nodeID, netNS); err != nil {
		return errors.Wrapf(err, "ndmz: could not create pub iface 4")
	}

	if err = applyFirewall(); err != nil {
		return err
	}

	err = netNS.Do(func(_ ns.NetNS) error {
		if err := options.SetIPv6Forwarding(true); err != nil {
			return errors.Wrapf(err, "failed to enable forwarding in ndmz")
		}

		if err := waitIP4(); err != nil {
			return err
		}

		if err := waitIP6(); err != nil {
			log.Error().Err(err).Msg("ndmz: no ipv6 found")
		}
		return nil
	})
	if err != nil {
		return err
	}

	z, err := zinit.New("")
	if err != nil {
		return err
	}
	dhcpMon := NewDHCPMon(dmzPub4, dmzNamespace, z)
	go dhcpMon.Start(ctx)

	return nil
}

// Delete deletes the NDMZ network namespace
func (d *dmzImpl) Delete() error {
	netNS, err := namespace.GetByName(dmzNamespace)
	if err == nil {
		if err := namespace.Delete(netNS); err != nil {
			return errors.Wrap(err, "failed to delete ndmz network namespace")
		}
	}

	return nil
}

// AttachNR links a network resource to the NDMZ
func (d *dmzImpl) AttachNR(networkID string, nr *nr.NetResource, ipamLeaseDir string) error {
	nrNSName, err := nr.Namespace()
	if err != nil {
		return err
	}

	nrNS, err := namespace.GetByName(nrNSName)
	if err != nil {
		return err
	}

	if !ifaceutil.Exists(nrPubIface, nrNS) {
		if _, err = macvlan.Create(nrPubIface, ndmzBridge, nrNS); err != nil {
			return err
		}
	}

	return nrNS.Do(func(_ ns.NetNS) error {
		addr, err := allocateIPv4(networkID, ipamLeaseDir)
		if err != nil {
			return errors.Wrap(err, "ip allocation for network resource")
		}
		pubIface, err := netlink.LinkByName(nrPubIface)
		if err != nil {
			return err
		}

		if err := netlink.AddrAdd(pubIface, &netlink.Addr{IPNet: addr}); err != nil && !os.IsExist(err) {
			return err
		}

		ipv6 := convertIpv4ToIpv6(addr.IP)
		log.Debug().Msgf("ndmz: setting public NR ip to: %s from %s", ipv6.String(), addr.IP.String())

		if err := netlink.AddrAdd(pubIface, &netlink.Addr{IPNet: &net.IPNet{
			IP:   ipv6,
			Mask: net.CIDRMask(64, 128),
		}}); err != nil && !os.IsExist(err) {
			return err
		}

		if err = netlink.LinkSetUp(pubIface); err != nil {
			return err
		}

		err = netlink.RouteAdd(&netlink.Route{
			Dst: &net.IPNet{
				IP:   net.ParseIP("0.0.0.0"),
				Mask: net.CIDRMask(0, 32),
			},
			Gw:        net.ParseIP("100.127.0.1"),
			LinkIndex: pubIface.Attrs().Index,
		})
		if err != nil && !os.IsExist(err) {
			return err
		}

		err = netlink.RouteAdd(&netlink.Route{
			Dst: &net.IPNet{
				IP:   net.ParseIP("::"),
				Mask: net.CIDRMask(0, 128),
			},
			Gw:        net.ParseIP("fe80::1"),
			LinkIndex: pubIface.Attrs().Index,
		})
		if err != nil && !os.IsExist(err) {
			return err
		}

		return nil
	})
}

// IsIPv4Only means dmz only supports ipv4 addresses
func (d *dmzImpl) IsIPv4Only() (bool, error) {
	// this is true if DMZPub6 only has local not routable ipv6 addresses
	//DMZPub6
	netNS, err := namespace.GetByName(dmzNamespace)
	if err != nil {
		return false, errors.Wrap(err, "failed to get ndmz namespace")
	}
	defer netNS.Close()

	var ipv4Only bool
	err = netNS.Do(func(_ ns.NetNS) error {
		link, err := netlink.LinkByName(dmzPub6)
		if err != nil {
			return errors.Wrapf(err, "failed to get interface '%s'", dmzPub6)
		}
		ips, err := netlink.AddrList(link, netlink.FAMILY_V6)
		if err != nil {
			return errors.Wrapf(err, "failed to list '%s' ips", dmzPub6)
		}

		for _, ip := range ips {
			if ip.IP.IsGlobalUnicast() && !ifaceutil.IsULA(ip.IP) {
				return nil
			}
		}

		ipv4Only = true
		return nil
	})

	return ipv4Only, err
}

func (d *dmzImpl) GetIPFor(inf string) ([]net.IPNet, error) {

	netns, err := namespace.GetByName(dmzNamespace)
	if err != nil {
		return nil, err
	}

	defer netns.Close()

	var results []net.IPNet
	err = netns.Do(func(_ ns.NetNS) error {
		ln, err := netlink.LinkByName(inf)
		if err != nil {
			return err
		}

		ips, err := netlink.AddrList(ln, netlink.FAMILY_ALL)
		if err != nil {
			return err
		}

		for _, ip := range ips {
			results = append(results, *ip.IPNet)
		}

		return nil
	})

	return results, err
}

func (d *dmzImpl) GetIP(family int) ([]net.IPNet, error) {
	var links []string
	if family == netlink.FAMILY_V4 || family == netlink.FAMILY_ALL {
		links = append(links, dmzPub4)
	}
	if family == netlink.FAMILY_V6 || family == netlink.FAMILY_ALL {
		links = append(links, dmzPub6)
	}

	if len(links) == 0 {
		return nil, nil
	}

	netns, err := namespace.GetByName(dmzNamespace)
	if err != nil {
		return nil, err
	}

	defer netns.Close()

	var results []net.IPNet
	err = netns.Do(func(_ ns.NetNS) error {
		for _, name := range links {
			ln, err := netlink.LinkByName(name)
			if err != nil {
				return err
			}

			ips, err := netlink.AddrList(ln, family)
			if err != nil {
				return err
			}

			for _, ip := range ips {
				results = append(results, *ip.IPNet)
			}
		}

		return nil
	})

	return results, err
}

// SetIP sets an ip inside dmz
func (d *dmzImpl) SetIP(subnet net.IPNet) error {
	netns, err := namespace.GetByName(dmzNamespace)
	if err != nil {
		return err
	}
	defer netns.Close()

	err = netns.Do(func(_ ns.NetNS) error {
		inf := dmzPub4
		if subnet.IP.To16() != nil {
			inf = dmzPub6
		}

		link, err := netlink.LinkByName(inf)
		if err != nil {
			return err
		}

		if err := netlink.AddrAdd(link, &netlink.Addr{
			IPNet: &subnet,
		}); err != nil && !os.IsExist(err) {
			return err
		}
		return nil
	})
	return err
}

// SupportsPubIPv4 implements DMZ interface
func (d *dmzImpl) SupportsPubIPv4() bool {
	return true
}

//Interfaces return information about dmz interfaces
func (d *dmzImpl) Interfaces() ([]types.IfaceInfo, error) {
	var output []types.IfaceInfo

	f := func(_ ns.NetNS) error {
		links, err := netlink.LinkList()
		if err != nil {
			log.Error().Err(err).Msgf("failed to list interfaces")
			return err
		}
		for _, link := range links {
			name := link.Attrs().Name
			if name == tonrsIface {
				continue
			}

			addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
			if err != nil {
				return err
			}

			info := types.IfaceInfo{
				Name:       name,
				Addrs:      make([]types.IPNet, len(addrs)),
				MacAddress: schema.MacAddress{link.Attrs().HardwareAddr},
			}
			for i, addr := range addrs {
				info.Addrs[i] = types.NewIPNet(addr.IPNet)
			}

			output = append(output, info)

		}
		return nil
	}

	// get the ndmz network namespace
	ndmz, err := namespace.GetByName(dmzNamespace)
	if err != nil {
		return nil, err
	}
	defer ndmz.Close()

	err = ndmz.Do(f)
	if err != nil {
		return nil, err
	}

	return output, nil
}

// waitIP4 waits to receives some IPv4 from DHCP
// it returns the pid of the dhcp process or an error
func waitIP4() error {
	// run DHCP to interface public in ndmz
	probe := dhcp.NewProbe()

	if err := probe.Start(dmzPub4); err != nil {
		return err
	}
	defer probe.Stop()

	link, err := netlink.LinkByName(dmzPub4)
	if err != nil {
		return err
	}

	cTimeout := time.After(time.Second * 30)
	for {
		select {
		case <-cTimeout:
			return errors.Errorf("public interface in ndmz did not received an IP. make sure DHCP is working")
		default:
			hasGW, _, err := ifaceutil.HasDefaultGW(link, netlink.FAMILY_V4)
			if err != nil {
				return err
			}
			if !hasGW {
				time.Sleep(time.Second)
				continue
			}
			return nil
		}
	}
}

func waitIP6() error {
	if err := ifaceutil.SetLoUp(); err != nil {
		return errors.Wrapf(err, "ndmz: couldn't bring lo up in ndmz namespace")
	}
	// also, set kernel parameter that public always accepts an ra even when forwarding
	if err := options.Set(dmzPub6,
		options.AcceptRA(options.RAAcceptIfForwardingIsEnabled),
		options.LearnDefaultRouteInRA(true),
		options.ProxyArp(false)); err != nil {
		return errors.Wrapf(err, "ndmz: failed to set ndmz pub6 flags")
	}

	getRoutes := func() (err error) {
		log.Info().Msg("wait for slaac to give ipv6")
		// check if in the mean time SLAAC gave us an IPv6 deft gw, save it, and reapply after enabling forwarding
		checkipv6 := net.ParseIP("2606:4700:4700::1111")
		if _, err = netlink.RouteGet(checkipv6); err != nil {
			return errors.Wrapf(err, "ndmz: failed to get the IPv6 routes in ndmz")
		}
		return nil
	}

	bo := backoff.NewExponentialBackOff()
	bo.MaxElapsedTime = 2 * time.Minute // default RA from router is every 60 secs
	return backoff.Retry(getRoutes, bo)
}

func createPubIface6(name string, master *netlink.Bridge, nodeID string, netNS ns.NetNS) error {
	if !ifaceutil.Exists(name, netNS) {
		if _, err := macvlan.Create(name, master.Name, netNS); err != nil {
			return err
		}
	}

	return netNS.Do(func(_ ns.NetNS) error {
		// set mac address to something static to make sure we receive the same IP from a DHCP server
		mac := ifaceutil.HardwareAddrFromInputBytes([]byte(nodeID + ndmzNsMACDerivationSuffix6))
		log.Debug().
			Str("mac", mac.String()).
			Str("interface", name).
			Msg("set mac on ipv6 ndmz public iface")

		return ifaceutil.SetMAC(name, mac, nil)
	})
}

func createPubIface4(name, nodeID string, netNS ns.NetNS) error {
	if !ifaceutil.Exists(name, netNS) {
		if _, err := macvlan.Create(name, types.DefaultBridge, netNS); err != nil {
			return err
		}
	}

	return netNS.Do(func(_ ns.NetNS) error {
		if err := options.Set(name, options.IPv6Disable(true)); err != nil {
			return errors.Wrapf(err, "failed to disable ip6 on %s", name)
		}
		// set mac address to something static to make sure we receive the same IP from a DHCP server
		mac := ifaceutil.HardwareAddrFromInputBytes([]byte(nodeID + ndmzNsMACDerivationSuffix4))
		log.Debug().
			Str("mac", mac.String()).
			Str("interface", name).
			Msg("set mac on ipv4 ndmz public iface")

		return ifaceutil.SetMAC(name, mac, nil)
	})
}

func createRoutingBridge(name string, netNS ns.NetNS) error {
	if !bridge.Exists(name) {
		if _, err := bridge.New(name); err != nil {
			return errors.Wrapf(err, "couldn't create bridge %s", name)
		}
	}

	if !ifaceutil.Exists(tonrsIface, netNS) {
		if _, err := macvlan.Create(tonrsIface, name, netNS); err != nil {
			return errors.Wrapf(err, "ndmz: couldn't create %s", tonrsIface)
		}
	}

	if err := options.Set(name, options.IPv6Disable(true)); err != nil {
		return errors.Wrapf(err, "failed to disable ip6 on bridge %s", name)
	}

	return netNS.Do(func(_ ns.NetNS) error {

		link, err := netlink.LinkByName(tonrsIface)
		if err != nil {
			return err
		}
		if err := options.Set(tonrsIface, options.IPv6Disable(false)); err != nil {
			return errors.Wrapf(err, "failed to enable ip6 on interface %s", tonrsIface)
		}

		addrs := []*netlink.Addr{
			{
				IPNet: &net.IPNet{
					IP:   net.ParseIP("100.127.0.1"),
					Mask: net.CIDRMask(16, 32),
				},
			},
			{
				IPNet: &net.IPNet{
					IP:   net.ParseIP("fe80::1"),
					Mask: net.CIDRMask(64, 128),
				},
			},
			{
				IPNet: &net.IPNet{
					IP:   net.ParseIP("fd00::1"),
					Mask: net.CIDRMask(64, 128),
				},
			},
		}

		for _, addr := range addrs {
			err = netlink.AddrAdd(link, addr)
			if err != nil && !os.IsExist(err) {
				return err
			}
		}

		return netlink.LinkSetUp(link)
	})
}

func applyFirewall() error {
	buf := bytes.Buffer{}

	data := struct {
		YggPorts string
	}{
		YggPorts: strings.Join([]string{
			strconv.Itoa(yggdrasil.YggListenTCP),
			strconv.Itoa(yggdrasil.YggListenTLS),
			strconv.Itoa(yggdrasil.YggListenLinkLocal),
		}, ","),
	}

	if err := fwTmpl.Execute(&buf, data); err != nil {
		return errors.Wrap(err, "failed to build nft rule set")
	}

	if err := nft.Apply(&buf, dmzNamespace); err != nil {
		return errors.Wrap(err, "failed to apply nft rule set")
	}

	return nil
}

func convertIpv4ToIpv6(ip net.IP) net.IP {
	var ipv6 string
	if len(ip) == net.IPv4len {
		ipv6 = fmt.Sprintf("fd00::%02x%02x", ip[2], ip[3])
	} else {
		ipv6 = fmt.Sprintf("fd00::%02x%02x", ip[14], ip[15])
	}

	return net.ParseIP(ipv6)
}
