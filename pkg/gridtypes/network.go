package gridtypes

import (
	"fmt"
)

// Network is the description of a part of a network local to a specific node
type Network struct {
	Name string
	//unique id inside the reservation is an autoincrement (USE AS NET_ID)
	NetID NetID `json:"net_id"`
	// IP range of the network, must be an IPv4 /16
	NetworkIPRange IPNet `json:"ip_range"`

	NodeID string `json:"node_id"`
	// IPV4 subnet for this network resource
	Subnet IPNet `json:"subnet"`

	WGPrivateKey string `json:"wg_private_key"`
	WGPublicKey  string `json:"wg_public_key"`
	WGListenPort uint16 `json:"wg_listen_port"`

	Peers []Peer `json:"peers"`
}

// Valid checks if the network resource is valid.
func (nr *Network) Valid() error {
	if nr.NetID == "" {
		return fmt.Errorf("network ID cannot be empty")
	}

	if nr.Name == "" {
		return fmt.Errorf("network name cannot be empty")
	}

	if nr.NetworkIPRange.Nil() {
		return fmt.Errorf("network IP range cannot be empty")
	}

	if nr.NodeID == "" {
		return fmt.Errorf("network resource node ID cannot empty")
	}
	if len(nr.Subnet.IP) == 0 {
		return fmt.Errorf("network resource subnet cannot empty")
	}

	if nr.WGPrivateKey == "" {
		return fmt.Errorf("network resource wireguard private key cannot empty")
	}

	if nr.WGPublicKey == "" {
		return fmt.Errorf("network resource wireguard public key cannot empty")
	}

	if nr.WGListenPort == 0 {
		return fmt.Errorf("network resource wireguard listen port cannot empty")
	}

	for _, peer := range nr.Peers {
		if err := peer.Valid(); err != nil {
			return err
		}
	}

	return nil
}

// Peer is the description of a peer of a NetResource
type Peer struct {
	// IPV4 subnet of the network resource of the peer
	Subnet IPNet `json:"subnet"`

	WGPublicKey string  `json:"wg_public_key"`
	AllowedIPs  []IPNet `json:"allowed_ips"`
	Endpoint    string  `json:"endpoint"`
}

// NetID is a type defining the ID of a network
type NetID string

// Valid checks if peer is valid
func (p *Peer) Valid() error {
	if p.WGPublicKey == "" {
		return fmt.Errorf("peer wireguard public key cannot empty")
	}

	if p.Subnet.Nil() {
		return fmt.Errorf("peer wireguard subnet cannot empty")
	}

	if len(p.AllowedIPs) <= 0 {
		return fmt.Errorf("peer wireguard allowedIPs cannot empty")
	}
	return nil
}
