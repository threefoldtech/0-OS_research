package storage

import (
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"
	log "github.com/rs/zerolog/log"
	"github.com/threefoldtech/zos/pkg"
	"github.com/threefoldtech/zos/pkg/gridtypes"
	"github.com/threefoldtech/zos/pkg/gridtypes/zos"
	"github.com/threefoldtech/zos/pkg/storage/filesystem"
)

// VolumeAllocate with the given size in a storage pool.
func (s *Module) VolumeAllocate(name string, size gridtypes.Unit) (pkg.Filesystem, error) {
	log.Info().Msgf("Creating new volume with size %d", size)
	if strings.HasPrefix(name, "zdb") {
		return pkg.Filesystem{}, fmt.Errorf("invalid volume name. zdb prefix is reserved")
	}

	fs, err := s.createSubvolWithQuota(size, name, zos.SSDDevice)
	if err != nil {
		return pkg.Filesystem{}, err
	}

	usage, err := fs.Usage()
	if err != nil {
		return pkg.Filesystem{}, err
	}

	return pkg.Filesystem{
		ID:     fs.ID(),
		FsType: fs.FsType(),
		Name:   fs.Name(),
		Path:   fs.Path(),
		Usage: pkg.Usage{
			Size: gridtypes.Unit(usage.Size),
			Used: gridtypes.Unit(usage.Used),
		},
		DiskType: zos.SSDDevice,
	}, nil
}

// VolumeUpdate updates filesystem size
func (s *Module) VolumeUpdate(name string, size gridtypes.Unit) (pkg.Filesystem, error) {
	_, volume, fs, err := s.path(name)
	if err != nil {
		return pkg.Filesystem{}, err
	}

	if err := volume.Limit(uint64(size)); err != nil {
		return fs, err
	}

	fs.Usage.Size = size
	return fs, nil
}

// VolumeRelease with the given name, this will unmount and then delete
// the filesystem. After this call, the caller must not perform any more actions
// on this filesystem
func (s *Module) VolumeRelease(name string) error {
	log.Info().Msgf("Deleting volume %v", name)

	for _, pool := range s.pools {
		if _, mounted := pool.Mounted(); !mounted {
			continue
		}

		volumes, err := pool.Volumes()
		if err != nil {
			return err
		}
		for _, vol := range volumes {
			if vol.Name() != name {
				continue
			}
			log.Debug().Msgf("Removing filesystem %v in volume %v", vol.Name(), pool.Name())
			err = pool.RemoveVolume(vol.Name())
			if err != nil {
				log.Err(err).Msgf("Error removing volume %s", vol.Name())
				return err
			}
			// if there is only 1 volume, unmount and shutdown pool
			if len(volumes) == 1 {
				err = pool.UnMount()
				if err != nil {
					log.Err(err).Msgf("Error unmounting pool %s", pool.Name())
					return err
				}
				err = pool.Shutdown()
				if err != nil {
					log.Err(err).Msgf("Error shutting down pool %s", pool.Name())
					return err
				}
			}

			return nil
		}
	}

	log.Warn().Msgf("Could not find filesystem %v", name)
	return nil
}

// VolumesList return all the filesystem managed by storeaged present on the nodes
func (s *Module) VolumesList() ([]pkg.Filesystem, error) {
	fss := make([]pkg.Filesystem, 0, 10)

	for _, pool := range s.pools {
		if _, mounted := pool.Mounted(); !mounted {
			continue
		}

		volumes, err := pool.Volumes()
		if err != nil {
			return nil, err
		}
		for _, v := range volumes {
			// Do not return "special" volumes here
			// instead the GetCacheFS and GetVdiskFS to access them
			if v.Name() == cacheLabel ||
				v.Name() == vdiskVolumeName {
				continue
			}

			usage, err := v.Usage()
			if err != nil {
				return nil, err
			}

			fss = append(fss, pkg.Filesystem{
				ID:     v.ID(),
				FsType: v.FsType(),
				Name:   v.Name(),
				Path:   v.Path(),
				Usage: pkg.Usage{
					Size: gridtypes.Unit(usage.Size),
					Used: gridtypes.Unit(usage.Used),
				},
				DiskType: pool.Type(),
			})
		}
	}

	return fss, nil
}

// VolumePath return the path of the mountpoint of the named filesystem
// if no volume with name exists, an empty path and an error is returned
func (s *Module) VolumePath(name string) (pkg.Filesystem, error) {
	_, _, fs, err := s.path(name)
	return fs, err
}

// Path return the path of the mountpoint of the named filesystem
// if no volume with name exists, an empty path and an error is returned
func (s *Module) path(name string) (filesystem.Pool, filesystem.Volume, pkg.Filesystem, error) {
	for _, pool := range s.pools {
		if _, mounted := pool.Mounted(); !mounted {
			continue
		}
		filesystems, err := pool.Volumes()
		if err != nil {
			return nil, nil, pkg.Filesystem{}, err
		}
		for _, fs := range filesystems {
			if fs.Name() == name {
				usage, err := fs.Usage()
				if err != nil {
					return nil, nil, pkg.Filesystem{}, err
				}

				return pool, fs, pkg.Filesystem{
					ID:     fs.ID(),
					FsType: fs.FsType(),
					Name:   fs.Name(),
					Path:   fs.Path(),
					Usage: pkg.Usage{
						Size: gridtypes.Unit(usage.Size),
						Used: gridtypes.Unit(usage.Used),
					},
					DiskType: pool.Type(),
				}, nil
			}
		}
	}

	return nil, nil, pkg.Filesystem{}, errors.Wrapf(os.ErrNotExist, "subvolume '%s' not found", name)
}
