package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/threefoldtech/zbus"
	"github.com/threefoldtech/zos/pkg/gridtypes"
	"github.com/threefoldtech/zos/pkg/provision"
)

type provisionFn func(ctx context.Context, wl *gridtypes.Workload) (interface{}, error)
type decommissionFn func(ctx context.Context, wl *gridtypes.Workload) error

// Primitives hold all the logic responsible to provision and decomission
// the different primitives workloads defined by this package
type Primitives struct {
	zbus zbus.Client

	provisioners    map[gridtypes.ReservationType]provisionFn
	decommissioners map[gridtypes.ReservationType]decommissionFn
}

var _ provision.Provisioner = (*Primitives)(nil)

// NewPrimitivesProvisioner creates a new 0-OS provisioner
func NewPrimitivesProvisioner(zbus zbus.Client) *Primitives {
	p := &Primitives{
		zbus: zbus,
	}
	p.provisioners = map[gridtypes.ReservationType]provisionFn{
		gridtypes.ContainerReservation:  p.containerProvision,
		gridtypes.VolumeReservation:     p.volumeProvision,
		gridtypes.NetworkReservation:    p.networkProvision,
		gridtypes.ZDBReservation:        p.zdbProvision,
		gridtypes.KubernetesReservation: p.kubernetesProvision,
		gridtypes.PublicIPReservation:   p.publicIPProvision,
	}
	p.decommissioners = map[gridtypes.ReservationType]decommissionFn{
		gridtypes.ContainerReservation:  p.containerDecommission,
		gridtypes.VolumeReservation:     p.volumeDecommission,
		gridtypes.NetworkReservation:    p.networkDecommission,
		gridtypes.ZDBReservation:        p.zdbDecommission,
		gridtypes.KubernetesReservation: p.kubernetesDecomission,
		gridtypes.PublicIPReservation:   p.publicIPDecomission,
	}

	return p
}

// RuntimeUpgrade runs upgrade needed when provision daemon starts
func (p *Primitives) RuntimeUpgrade(ctx context.Context) {
	p.upgradeRunningZdb(ctx)
}

// Provision implemenents provision.Provisioner
func (p *Primitives) Provision(ctx context.Context, wl *gridtypes.Workload) (*gridtypes.Result, error) {
	handler, ok := p.provisioners[wl.Type]
	if !ok {
		return nil, fmt.Errorf("unknown reservation type '%s' for reservation id '%s'", wl.Type, wl.ID)
	}

	data, err := handler(ctx, wl)
	return p.buildResult(wl, data, err)
}

// Decommission implementation for provision.Provisioner
func (p *Primitives) Decommission(ctx context.Context, wl *gridtypes.Workload) error {
	handler, ok := p.decommissioners[wl.Type]
	if !ok {
		return fmt.Errorf("unknown reservation type '%s' for reservation id '%s'", wl.Type, wl.ID)
	}

	return handler(ctx, wl)
}

func (p *Primitives) buildResult(wl *gridtypes.Workload, data interface{}, err error) (*gridtypes.Result, error) {
	result := &gridtypes.Result{
		Created: time.Now(),
	}

	if err != nil {
		result.Error = err.Error()
		result.State = gridtypes.StateError
	} else {
		result.State = gridtypes.StateOk
	}

	br, err := json.Marshal(data)
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode result")
	}
	result.Data = br

	return result, nil
}
