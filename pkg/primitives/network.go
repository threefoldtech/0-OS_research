package primitives

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/threefoldtech/zos/pkg"
	"github.com/threefoldtech/zos/pkg/gridtypes"
	"github.com/threefoldtech/zos/pkg/gridtypes/zos"
	"github.com/threefoldtech/zos/pkg/provision"
	"github.com/threefoldtech/zos/pkg/stubs"
)

// networkProvision is entry point to provision a network
func (p *Primitives) networkProvisionImpl(ctx context.Context, wl *gridtypes.WorkloadWithID) error {
	var network zos.Network
	if err := json.Unmarshal(wl.Data, &network); err != nil {
		return fmt.Errorf("failed to unmarshal network from reservation: %w", err)
	}

	mgr := stubs.NewNetworkerStub(p.zbus)
	log.Debug().Str("network", fmt.Sprintf("%+v", network)).Msg("provision network")

	wgKey, err := p.decryptSecret(ctx, wl.User, network.WGPrivateKeyEncrypted, wl.Version)
	if err != nil {
		return errors.Wrap(err, "failed to decrypt wireguard private key")
	}

	deployment := provision.GetDeployment(ctx)

	_, err = mgr.CreateNR(pkg.Network{
		Network:           network,
		NetID:             zos.NetworkID(deployment.TwinID, wl.Name),
		WGPrivateKeyPlain: wgKey,
	})

	if err != nil {
		return errors.Wrapf(err, "failed to create network resource for network %s", wl.ID)
	}

	return nil
}

func (p *Primitives) networkProvision(ctx context.Context, wl *gridtypes.WorkloadWithID) (interface{}, error) {
	return nil, p.networkProvisionImpl(ctx, wl)
}

func (p *Primitives) networkDecommission(ctx context.Context, wl *gridtypes.WorkloadWithID) error {
	mgr := stubs.NewNetworkerStub(p.zbus)

	var network zos.Network
	if err := json.Unmarshal(wl.Data, &network); err != nil {
		return fmt.Errorf("failed to unmarshal network from reservation: %w", err)
	}

	deployment := provision.GetDeployment(ctx)
	if err := mgr.DeleteNR(pkg.Network{
		Network: network,
		NetID:   zos.NetworkID(deployment.TwinID, wl.Name),
	}); err != nil {
		return fmt.Errorf("failed to delete network resource: %w", err)
	}

	return nil
}