package zos

import (
	"fmt"
	"io"
	"net"

	"github.com/threefoldtech/zos/pkg/gridtypes"
)

// Kubernetes reservation data
type Kubernetes struct {
	// Size of the vm, this defines the amount of vCpu, memory, and the disk size
	// Docs: docs/kubernetes/sizes.md
	Size uint8 `json:"size"`
	// NetworkID of the network namepsace in which to run the VM. The network
	// must be provisioned previously.
	NetworkID NetID `json:"network_id"`
	// IP of the VM. The IP must be part of the subnet available in the network
	// resource defined by the networkID on this node
	IP net.IP `json:"ip"`
	// ClusterSecret is the hex encoded encrypted(?) cluster secret.
	ClusterSecret string `json:"cluster_secret"`
	// MasterIPs define the URL's for the kubernetes master nodes. If this
	// list is empty, this node is considered to be a master node.
	MasterIPs []net.IP `json:"master_ips"`
	// SSHKeys is a list of ssh keys to add to the VM. Keys can be either
	// a full ssh key, or in the form of `github:${username}`. In case of
	// the later, the VM will retrieve the github keys for this username
	// when it boots.
	SSHKeys []string `json:"ssh_keys"`
	// PublicIP points to a reservation for a public ip
	PublicIP gridtypes.ID `json:"public_ip"`

	// PlainClusterSecret plaintext secret
	PlainClusterSecret string `json:"-"`
}

func (k Kubernetes) Valid() error {
	return nil
}

// Challenge implementation
func (k Kubernetes) Challenge(b io.Writer) error {
	if _, err := fmt.Fprintf(b, "%d", k.Size); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(b, "%s", k.ClusterSecret); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(b, "%s", k.NetworkID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(b, "%s", k.IP.String()); err != nil {
		return err
	}
	for _, ip := range k.MasterIPs {
		if _, err := fmt.Fprintf(b, "%s", ip.String()); err != nil {
			return err
		}
	}
	for _, key := range k.SSHKeys {
		if _, err := fmt.Fprintf(b, "%s", key); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(b, "%s", k.PublicIP.String()); err != nil {
		return err
	}

	return nil
}

// KubernetesResult result returned by k3s reservation
type KubernetesResult struct {
	ID string `json:"id"`
	IP string `json:"ip"`
}