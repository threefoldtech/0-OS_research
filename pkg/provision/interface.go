package provision

import (
	"context"
	"fmt"

	"github.com/threefoldtech/zos/pkg/gridtypes"
)

// Engine is engine interface
type Engine interface {
	// Provision pushes a workload to engine queue. on success
	// means that workload has been committed to storage (accepts)
	// and will be processes later
	Provision(ctx context.Context, wl gridtypes.Workload) error
	Deprovision(ctx context.Context, id gridtypes.ID) error
	Get(gridtypes.ID) (gridtypes.Workload, error)
}

// Provisioner interface
type Provisioner interface {
	Provision(ctx context.Context, wl *gridtypes.Workload) (*gridtypes.Result, error)
	Decommission(ctx context.Context, wl *gridtypes.Workload) error
}

// Filter is filtering function for Purge method

var (
	//ErrWorkloadExists returned if object exist
	ErrWorkloadExists = fmt.Errorf("exists")
	//ErrWorkloadNotExists returned if object not exists
	ErrWorkloadNotExists = fmt.Errorf("not exists")
)

// Storage interface
type Storage interface {
	Add(wl gridtypes.Workload) error
	Set(wl gridtypes.Workload) error
	Get(id gridtypes.ID) (gridtypes.Workload, error)
	GetNetwork(id gridtypes.NetID) (gridtypes.Workload, error)

	ByType(t gridtypes.ReservationType) ([]gridtypes.ID, error)
	ByUser(user gridtypes.ID, t gridtypes.ReservationType) ([]gridtypes.ID, error)
}

// Janitor interface
type Janitor interface {
	Cleanup(ctx context.Context) error
}
