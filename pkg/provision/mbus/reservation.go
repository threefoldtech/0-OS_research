package mbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/threefoldtech/zos/pkg/gridtypes"
	"github.com/threefoldtech/zos/pkg/provision"
	"github.com/threefoldtech/zos/pkg/provision/mw"
	"github.com/threefoldtech/zos/pkg/rmb"
)

// CreateOrUpdate creates or updates a workload based on a message from the message bus
func (a *WorkloadsMessagebus) CreateOrUpdate(ctx context.Context, message rmb.Message, create bool) (interface{}, mw.Response) {
	bytes, err := message.GetPayload()
	if err != nil {
		return nil, mw.Error(err, 400)
	}

	var deployment gridtypes.Deployment
	if err := json.Unmarshal(bytes, &deployment); err != nil {
		return nil, mw.BadRequest(err)
	}

	if err := deployment.Valid(); err != nil {
		return nil, mw.BadRequest(err)
	}

	authorized := false
	for _, twinID := range message.TwinSrc {
		if twinID == int(deployment.TwinID) {
			authorized = true
		}
	}
	if !authorized {
		return nil, mw.UnAuthorized(fmt.Errorf("invalid user id in request message"))
	}

	if err := deployment.Verify(a.engine.Twins()); err != nil {
		return nil, mw.UnAuthorized(err)
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	action := a.engine.Provision
	if !create {
		action = a.engine.Update
	}

	err = action(ctx, deployment)

	if err == context.DeadlineExceeded {
		return nil, mw.Unavailable(ctx.Err())
	} else if errors.Is(err, provision.ErrDeploymentExists) {
		return nil, mw.Conflict(err)
	} else if errors.Is(err, provision.ErrDeploymentNotExists) {
		return nil, mw.NotFound(err)
	} else if errors.Is(err, provision.ErrDeploymentUpgradeValidationError) {
		return nil, mw.BadRequest(err)
	} else if err != nil {
		return nil, mw.Error(err)
	}

	return nil, mw.Accepted()
}