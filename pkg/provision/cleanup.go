package provision

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/tfexplorer/client"
	"github.com/threefoldtech/zbus"
	"github.com/threefoldtech/zos/pkg"
	"github.com/threefoldtech/zos/pkg/provision/common"
	"github.com/threefoldtech/zos/pkg/storage"
	"github.com/threefoldtech/zos/pkg/stubs"
	"github.com/threefoldtech/zos/pkg/zdb"
	"github.com/vishvananda/netlink"
	"golang.org/x/net/context"
)

var (
	vdiskIDMatch = regexp.MustCompile(`^(\d+-\d+)`)
	pubIPIDMatch = regexp.MustCompile(`^p-(\d+-1)$`)
)

// Janitor structure
type Janitor struct {
	zbus zbus.Client

	getter ReservationGetter
}

// NewJanitor creates a new Janitor instance
func NewJanitor(zbus zbus.Client, getter ReservationGetter) *Janitor {
	return &Janitor{
		zbus:   zbus,
		getter: getter,
	}
}

// CleanupResources cleans up unused resources
func (j *Janitor) CleanupResources(ctx context.Context) error {
	// - First remove all lingering zdb namespaces that has NO valid
	// reservation. This will also decomission zdb containers that
	// serves no namespaces anymore
	if err := j.cleanupZdbContainers(ctx); err != nil {
		log.Error().Err(err).Msg("zdb cleaner failed")
		// we don't stop here. if we failed to clean zdb containers
		// any lingering zdb container will end up in the protected
		// volumes so there is no harm of continuing the process
		// to clean what we can
	}

	if err := j.cleanupPublicIPs(ctx); err != nil {
		log.Error().Err(err).Msg("ip cleaner failed")
	}

	// -2nd we clean up all lingering vms on the node
	if err := j.cleanupVms(ctx); err != nil {
		log.Error().Err(err).Msg("vm cleaner failed")
	}

	// - 3rd, we clean up all lingering volumes on the node
	if err := j.cleanupVolumes(ctx); err != nil {
		log.Error().Err(err).Msg("volume cleaner failed")
	}

	// - 4th, we clean up any lingering vdisks that are not being
	// used.
	if err := j.cleanupVdisks(ctx); err != nil {
		log.Error().Err(err).Msg("virtual disks cleaner failed")
	}

	return nil
}

func (j *Janitor) cleanupPublicIPs(ctx context.Context) error {
	//todo: use networkd to list public taps
	links, err := netlink.LinkList()
	if err != nil {
		return err
	}

	netd := stubs.NewNetworkerStub(j.zbus)
	for _, link := range links {
		m := pubIPIDMatch.FindStringSubmatch(link.Attrs().Name)
		if m == nil {
			continue
		}
		id := m[1]
		toDelete, err := j.checkToDelete(id)
		if err != nil {
			log.Error().Err(err).Str("id", id).Msg("failed to check ip for delete")
		}
		log.Debug().Bool("to-delete", toDelete).Str("id", id).Msg("vm reservation status")
		if !toDelete {
			continue
		}
		log.Debug().Str("id", id).Msg("deleting stall ip reservation")
		if err := netd.DisconnectPubTap(id); err != nil {
			log.Error().Err(err).Str("id", id).Msg("failed to delete public ip")
		}
	}

	return nil
}

func (j *Janitor) cleanupVms(ctx context.Context) error {
	vmd := stubs.NewVMModuleStub(j.zbus)
	vms, err := vmd.List()
	if err != nil {
		return err
	}
	for _, vm := range vms {
		toDelete, err := j.checkToDelete(vm)
		if err != nil {
			log.Error().Err(err).Str("id", vm).Msg("failed to check vm for delete")
		}
		log.Debug().Bool("to-delete", toDelete).Str("id", vm).Msg("vm reservation status")
		if !toDelete {
			continue
		}
		log.Debug().Str("id", vm).Msg("deleting stall vm reservation")
		if err := vmd.Delete(vm); err != nil {
			log.Error().Err(err).Str("id", vm).Msg("failed to delete vm")
		}
	}

	return nil
}

func (j *Janitor) checkToDelete(id string) (bool, error) {
	log.Debug().Str("id", id).Msg("checking explorer for reservation")

	reservation, err := j.getter.Get(id)
	if err != nil {
		var hErr client.HTTPError
		if ok := errors.As(err, &hErr); ok {
			resp := hErr.Response()
			// If reservation is not found it should be deleted
			if resp.StatusCode == 404 {
				return true, nil
			}
		}

		return false, err
	}

	return reservation.ToDelete, nil
}

func (j *Janitor) cleanupVdisks(ctx context.Context) error {
	stub := stubs.NewVDiskModuleStub(j.zbus)

	vdisks, err := stub.List()
	if err != nil {
		return errors.Wrap(err, "failed to list virtual disks")
	}
	for _, vdisk := range vdisks {
		//fmt.Sscanf(str string, format string, a ...interface{})
		gwid := vdiskIDMatch.FindString(vdisk.Name())
		clog := log.With().Str("vdisk", vdisk.Name()).Str("id", gwid).Logger()
		if len(gwid) == 0 {
			clog.Warn().Msg("vdisk has invalid id, skipping")
			continue
		}

		delete, err := j.checkToDelete(gwid)
		if err != nil {
			clog.Error().Err(err).Msg("failed to check vdisk reservation")
			continue
		}

		if delete {
			clog.Info().Str("reason", "no-associated-reservation").Msg("delete vdisk")
			if err := stub.Deallocate(vdisk.Name()); err != nil {
				clog.Error().Err(err).Msg("failed to deallocate vdisk")
			}
		} else {
			clog.Info().Msg("skipping vdisk")
		}
	}

	return nil
}

func (j *Janitor) cleanupVolumes(ctx context.Context) error {
	storaged := stubs.NewStorageModuleStub(j.zbus)
	// We get a list with ALL volumes, that are being
	// used by active containers. Note we don't check if
	// containers are valid or not. This code is only for
	// storage cleanup (so far)
	protected, err := j.protectedVolumesFromContainers(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to list retrieve protected volumes")
	}

	// - The we list all volumes from storage.
	// we need to go all each one and do the following checks
	//  - Are they protected ?
	//  - Do they belong to active reservation ?
	//  - If not, delete!
	volumes, err := storaged.ListFilesystems()
	if err != nil {
		return err
	}

	for _, volume := range volumes {
		clog := log.With().Str("volume", volume.Path).Logger()

		clog.Debug().Msg("checking volume for clean up")

		// - Is the volume protected
		if _, ok := protected[volume.Path]; ok {
			clog.Debug().Msg("volume is protected, skipping")
			continue
		}

		if len(volume.Name) == 64 {
			// if the fs is not used by any container and its name is 64 character long
			// they are left over of old containers when flistd used to generate random names
			// for the container root flist subvolumes
			clog.Info().Str("reason", "legacy-root-fs").Msg("delete subvolume")
			if err := storaged.ReleaseFilesystem(volume.Name); err != nil {
				clog.Error().Err(err).Msg("failed to delete subvol")
			}

			continue
		}

		if strings.HasPrefix(volume.Name, storage.ZDBPoolPrefix) {
			clog.Info().Str("reason", "unused-zdb").Msg("delete subvolume")
			if err := storaged.ReleaseFilesystem(volume.Name); err != nil {
				clog.Error().Err(err).Msg("failed to delete subvol")
			}

			continue
		}

		if volume.Name == "fcvms" {
			// left over from testing during vm module development
			clog.Info().Str("reason", "legacy-vm-fs").Msg("delete subvolume")
			if err := storaged.ReleaseFilesystem(volume.Name); err != nil {
				clog.Error().Err(err).Msg("failed to delete subvol")
			}

			continue
		}

		// So this is NOT protected, and obviously
		// not matching any of the above criteria
		// so we need to check if we can delete this reservation
		// Check the explorer if it needs to be deleted
		delete, err := j.checkToDelete(volume.Name)
		if err != nil {
			//TODO: handle error here
			clog.Error().Err(err).Msg("failed to check volume reservation")
			continue
		}

		if delete {
			clog.Info().Str("reason", "no-associated-reservation").Msg("delete subvolume")
			if err := storaged.ReleaseFilesystem(volume.Name); err != nil {
				clog.Error().Err(err).Msg("failed to delete subvolume")
			}
		} else {
			clog.Info().Msg("skipping subvolume")
		}
	}

	return nil
}

func (j *Janitor) cleanupZdbContainer(ctx context.Context, id string) error {
	con, err := newZdbConnection(id)
	if err != nil {
		return err
	}

	defer con.Close()
	namespaces, err := con.Namespaces()
	if err != nil {
		// we need to skip this zdb container for now we are not sure
		// if it has any used values.
		return errors.Wrap(err, "failed to list zdb namespace")
	}

	mapped := make(map[string]struct{})
	for _, namespace := range namespaces {
		if namespace == "default" {
			continue
		}

		mapped[namespace] = struct{}{}

		toDelete, err := j.checkToDelete(namespace)
		if err != nil {
			log.Error().Err(err).Str("zdb-namespace", namespace).Msg("failed to check if we should keep namespace")
			continue
		}

		if !toDelete {
			continue
		}

		if err := con.DeleteNamespace(namespace); err != nil {
			log.Error().Err(err).Str("zdb-namespace", namespace).Msg("failed to delete lingering zdb namespace")
		}

		delete(mapped, namespace)
	}

	if len(mapped) > 0 {
		// not all namespaces are deleted so we need to keep this
		// container instance
		return nil
	}

	// no more namespace to keep, so container can also go
	return common.DeleteZdbContainer(pkg.ContainerID(id), j.zbus)
}

func (j *Janitor) cleanupZdbContainers(ctx context.Context) error {
	containerd := stubs.NewContainerModuleStub(j.zbus)

	containers, err := containerd.List("zdb")
	if err != nil {
		return errors.Wrap(err, "failed to list zdb containers")
	}

	for _, containerID := range containers {
		if err := j.cleanupZdbContainer(ctx, string(containerID)); err != nil {
			log.Error().Err(err).Msg("failed to cleanup zdb container")
		}
	}

	return nil
}

// checks running containers for subvolumes that might need to be saved because they are used
// and subvolumes that might need to be deleted because they have no attached container anymore
func (j *Janitor) protectedVolumesFromContainers(ctx context.Context) (map[string]struct{}, error) {
	toSave := make(map[string]struct{})

	contd := stubs.NewContainerModuleStub(j.zbus)

	cNamespaces, err := contd.ListNS()
	if err != nil {
		log.Err(err).Msgf("failed to list namespaces")
		return nil, err
	}

	for _, ns := range cNamespaces {
		containerIDs, err := contd.List(ns)
		if err != nil {
			log.Error().Err(err).Msg("failed to list container IDs")
			return nil, err
		}

		for _, id := range containerIDs {
			info, err := contd.Inspect(ns, id)
			if err != nil {
				log.Error().Err(err).Msgf("failed to inspect container %s", id)
				continue
			}

			// avoid to remove any used subvolume used by flistd for root container fs
			toSave[info.RootFS] = struct{}{}

			for _, mnt := range info.Mounts {
				// the container has many other things in info.Mounts
				// that are not volumes so we are only interested
				// to volumes from zos
				if !strings.HasPrefix(mnt.Source, "/mnt/") {
					continue
				}

				toSave[mnt.Source] = struct{}{}
			}
		}
	}

	return toSave, nil
}

func socketDir(containerID string) string {
	return fmt.Sprintf("/var/run/zdb_%s", containerID)
}

func newZdbConnection(id string) (zdb.Client, error) {
	socket := fmt.Sprintf("unix://%s/zdb.sock", socketDir(id))
	cl := zdb.New(socket)
	return cl, cl.Connect()
}
