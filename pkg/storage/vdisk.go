package storage

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/g0rbe/go-chattr"
	"github.com/pkg/errors"
	log "github.com/rs/zerolog/log"
	"github.com/threefoldtech/zos/pkg"
	"github.com/threefoldtech/zos/pkg/gridtypes"
	"github.com/threefoldtech/zos/pkg/gridtypes/zos"
)

const (
	// vdiskVolumeName is the name of the volume used to store vdisks
	vdiskVolumeName = "vdisks"
)

// VDiskAllocate with given size and an optional source disk, return path to virtual disk (size in MB)
func (d *Module) VDiskAllocate(id string, size gridtypes.Unit) (string, error) {
	path, err := d.findDisk(id)
	if err == nil {
		return path, errors.Wrapf(os.ErrExist, "disk with id '%s' already exists", id)
	}

	base, err := d.vDiskFindCandidate(size)
	if err != nil {
		return "", errors.Wrapf(err, "failed to find a candidate to host vdisk of size '%d'", size)
	}

	path, err = d.safePath(base, id)
	if err != nil {
		return "", err
	}

	defer func() {
		// clean up disk file if error
		if err != nil {
			os.RemoveAll(path)
		}
	}()

	var file *os.File
	file, err = os.Create(path)
	if err != nil {
		return "", err
	}

	defer file.Close()
	if err = chattr.SetAttr(file, chattr.FS_NOCOW_FL); err != nil {
		return "", err
	}

	err = syscall.Fallocate(int(file.Fd()), 0, 0, int64(size))
	return path, err
}

// VDiskEnsureFilesystem ensures that the virtual disk has a valid filesystem
// currently the fs is btrfs
func (d *Module) VDiskEnsureFilesystem(id string) error {
	path, err := d.findDisk(id)
	if err != nil {
		return errors.Wrapf(err, "couldn't find disk with id: %s", id)
	}

	return d.ensureFS(path)
}

// VDiskWriteImage writes an image to a vdisk
func (d *Module) VDiskWriteImage(id string, image string) error {
	path, err := d.findDisk(id)
	if err != nil {
		return errors.Wrapf(err, "couldn't find disk with id: %s", id)
	}
	source, err := os.Open(image)
	if err != nil {
		return errors.Wrap(err, "failed to open image")
	}
	defer source.Close()
	file, err := os.OpenFile(path, os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	imgStat, err := source.Stat()
	if err != nil {
		return errors.Wrap(err, "failed to stat image")
	}
	fileStat, err := file.Stat()
	if err != nil {
		return errors.Wrap(err, "failed to state disk")
	}

	if imgStat.Size() > fileStat.Size() {
		return fmt.Errorf("image size is bigger than disk")
	}

	_, err = io.Copy(file, source)
	if err != nil {
		return errors.Wrap(err, "failed to write disk image")
	}

	return d.expandFs(path)
}

// VDiskDeallocate removes a virtual disk
func (d *Module) VDiskDeallocate(id string) error {
	path, err := d.findDisk(id)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// VDiskExists checks if a vdisk exists
func (d *Module) VDiskExists(id string) bool {
	_, err := d.findDisk(id)

	return err == nil
}

// VDiskInspect return info about the vdisk
func (d *Module) VDiskInspect(id string) (disk pkg.VDisk, err error) {
	path, err := d.findDisk(id)

	if err != nil {
		return disk, err
	}

	disk.Path = path
	stat, err := os.Stat(path)
	if err != nil {
		return disk, err
	}

	disk.Size = stat.Size()
	return
}

// VDiskList lists all vdisks
func (d *Module) VDiskList() ([]pkg.VDisk, error) {
	pools, err := d.VDiskPools()
	if err != nil {
		return nil, err
	}
	var disks []pkg.VDisk
	for _, pool := range pools {

		items, err := ioutil.ReadDir(pool)
		if err != nil {
			return nil, errors.Wrap(err, "failed to list virtual disks")
		}

		for _, item := range items {
			if item.IsDir() {
				continue
			}

			disks = append(disks, pkg.VDisk{
				Path: filepath.Join(pool, item.Name()),
				Size: item.Size(),
			})
		}

		return disks, nil
	}

	return disks, nil
}

func (d *Module) findDisk(id string) (string, error) {
	pools, err := d.VDiskPools()
	if err != nil {
		return "", errors.Wrapf(err, "failed to find disk with id '%s'", id)
	}

	for _, pool := range pools {
		path, err := d.safePath(pool, id)
		if err != nil {
			return "", err
		}

		if _, err := os.Stat(path); err == nil {
			// file exists
			return path, nil
		}
	}

	return "", os.ErrNotExist
}

// vDiskFindCandidate find a suitbale location for creating a vdisk of the given size
func (d *Module) vDiskFindCandidate(size gridtypes.Unit) (path string, err error) {
	candidates, err := d.findCandidates(size, zos.SSDDevice)
	if err != nil {
		return path, err
	}
	// does anyone have a vdisk subvol
	for _, candidate := range candidates {
		volumes, err := candidate.Pool.Volumes()
		if err != nil {
			log.Error().Str("pool", candidate.Pool.Path()).Err(err).Msg("failed to list pool volumes")
			continue
		}
		for _, volume := range volumes {
			if volume.Name() != vdiskVolumeName {
				continue
			}

			return volume.Path(), nil
		}
	}
	// none has a vdiks subvolume, we need to
	// create one.
	candidate := candidates[0]
	volume, err := candidate.Pool.AddVolume(vdiskVolumeName)
	if err != nil {
		return path, errors.Wrap(err, "failed to create vdisk pool")
	}

	return volume.Path(), nil
}

// VDiskPools return a list of all vdisk pools
func (d *Module) VDiskPools() ([]string, error) {
	var paths []string
	for _, pool := range d.pools {
		if pool.Type() != zos.SSDDevice {
			continue
		}

		if _, mounted := pool.Mounted(); !mounted {
			continue
		}

		volumes, err := pool.Volumes()
		if err != nil {
			return nil, errors.Wrapf(err, "failed to list pool '%s' volumes", pool.Path())
		}

		for _, volume := range volumes {
			if volume.Name() == vdiskVolumeName {
				paths = append(paths, volume.Path())
			}
		}
	}

	return paths, nil
}

func (d *Module) ensureFS(disk string) error {
	output, err := exec.Command("mkfs.btrfs", disk).CombinedOutput()
	if err == nil {
		return nil
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return errors.Wrapf(err, "failed to format disk '%s'", string(output))
	}

	if exitErr.ProcessState.ExitCode() == 1 &&
		strings.Contains(string(output), "ERROR: use the -f option to force overwrite") {
		// disk already have filesystem
		return nil
	}

	return errors.Wrapf(err, "unknown btrfs error '%s'", string(output))
}

func (d *Module) expandFs(disk string) error {
	dname, err := ioutil.TempDir("", "btrfs-resize")
	if err != nil {
		return errors.Wrap(err, "couldn't create a temp dir to mount the btrfs fs to resize it")
	}
	defer os.RemoveAll(dname)

	cmd := exec.Command("mount", disk, dname)

	if err := cmd.Run(); err != nil {
		return errors.Wrap(err, "couldn't mount the btrfs fs to resize it")
	}

	defer syscall.Unmount(dname, 0)
	cmd = exec.Command("btrfs", "filesystem", "resize", "max", dname)

	if err := cmd.Run(); err != nil {
		return errors.Wrap(err, "failed to resize file system to disk size")
	}
	return nil
}

func (d *Module) safePath(base, id string) (string, error) {
	path := filepath.Join(base, id)
	// this to avoid passing an `injection` id like '../name'
	// and end up deleting a file on the system. so only delete
	// allocated disks
	location := filepath.Dir(path)
	if filepath.Clean(location) != base {
		return "", fmt.Errorf("invalid disk id: '%s'", id)
	}

	return path, nil
}
