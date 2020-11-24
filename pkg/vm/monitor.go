package vm

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/zos/pkg/stubs"
)

const (
	failuresBeforeDestroy = 4
	monitorEvery          = 10 * time.Second
)

var (
	permanent = struct{}{}
)

// Monitor start vms  monitoring
func (m *Module) Monitor(ctx context.Context) {
	go func() {
		for {
			select {
			case <-time.After(monitorEvery):
				if err := m.monitor(ctx); err != nil {
					log.Error().Err(err).Msg("failed to run monitoring")
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (m *Module) monitor(ctx context.Context) error {
	// this lock works with Run call to avoid
	// monitoring trying to restart a machine that is not running yet.
	m.lock.Lock()
	defer m.lock.Unlock()

	// list all machines available under `{root}/firecracker`
	running, err := m.findAll()
	if err != nil {
		return err
	}

	log.Debug().Int("machines", len(running)).Msg("machines running")
	// list all configurations
	root := filepath.Join(m.root, "firecracker")
	items, err := ioutil.ReadDir(root)
	if err != nil {
		return err
	}

	for _, item := range items {
		if !item.IsDir() {
			continue
		}

		id := item.Name()

		if err := m.monitorID(ctx, running, id); err != nil {
			log.Err(err).Str("id", id).Msg("failed to monitor machine")
		}

	}
	return nil
}

func (m *Module) monitorID(ctx context.Context, running map[string]int, id string) error {
	log := log.With().Str("id", id).Logger()

	if _, ok := running[id]; ok {
		return nil
	}

	// otherwise machine is not running. we need to check if we need to restart
	// it

	marker, ok := m.failures.Get(id)
	if !ok {
		// no previous value. so this is the first failure
		m.failures.Set(id, int(0), cache.DefaultExpiration)
	}

	if marker == permanent {
		// if the marker is permanent. it means that this vm
		// is being deleted. we don't need to take any more action here
		// (don't try to restart or delete)
		log.Debug().Msg("permanent delete marker is set")
		return nil
	}

	count, err := m.failures.IncrementInt(id, 1)
	if err != nil {
		// this should never happen because we make sure value
		// is set
		return errors.Wrap(err, "failed to check number of failure for the vm")
	}

	var reason error
	if count < failuresBeforeDestroy {
		log.Debug().Msg("trying to restart the container")
		jailed, err := JailedFromPath(filepath.Join(m.root, "firecracker", id, "root"))
		if err != nil {
			return err
		}

		reason = jailed.Start(ctx)
	} else {
		reason = fmt.Errorf("deleting vm due to so many crashes")
	}

	if reason != nil {
		log.Debug().Err(reason).Msg("deleting container due to restart error")

		stub := stubs.NewProvisionStub(m.client)
		if err := stub.DecommissionCached(id, reason.Error()); err != nil {
			return errors.Wrapf(err, "failed to decommission reservation '%s'", id)
		}
	}

	return nil
}