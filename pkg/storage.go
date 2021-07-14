package pkg

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/threefoldtech/zos/pkg/gridtypes"
	"github.com/threefoldtech/zos/pkg/gridtypes/zos"
)

//go:generate mkdir -p stubs
//go:generate zbusc -module storage -version 0.0.1 -name storage -package stubs github.com/threefoldtech/zos/pkg+StorageModule stubs/storage_stub.go

// RaidProfile type
type RaidProfile string

const (
	// Single profile
	Single RaidProfile = "single"
	// Raid0 profile
	Raid0 RaidProfile = "raid0"
	// Raid1 profile
	Raid1 RaidProfile = "raid1"
	// Raid10 profile
	Raid10 RaidProfile = "raid10"
)

// ErrNotEnoughSpace indicates that there is not enough space in a pool
// of the requested type to create the filesystem
type ErrNotEnoughSpace struct {
	DeviceType DeviceType
}

func (e ErrNotEnoughSpace) Error() string {
	return fmt.Sprintf("Not enough space left in pools of this type %s", e.DeviceType)
}

// ErrInvalidDeviceType raised when trying to allocate space on unsupported device type
type ErrInvalidDeviceType struct {
	DeviceType DeviceType
}

func (e ErrInvalidDeviceType) Error() string {
	return fmt.Sprintf("invalid device type '%s'. type unknown", e.DeviceType)
}

// DeviceType is the actual type of hardware that the storage device runs on,
// i.e. SSD or HDD
type DeviceType = zos.DeviceType

type (
	// BrokenDevice is a disk which is somehow not fully functional. Storage keeps
	// track of disks which have failed at some point, so they are not used, and
	// to be able to later report this to other daemons.
	BrokenDevice struct {
		// Path to allow identification of the disk
		Path string
		// Err returned which lead to the disk being marked as faulty
		Err error
	}

	// BrokenPool contains info about a malfunctioning storage pool
	BrokenPool struct {
		// Label of the broken pool
		Label string
		// Err returned by the action which let to the pool being marked as broken
		Err error
	}
)

// Validate make sure profile is correct
func (p RaidProfile) Validate() error {
	if _, ok := raidProfiles[p]; !ok {
		return fmt.Errorf("not supported raid profile '%s'", p)
	}

	return nil
}

var (
	raidProfiles = map[RaidProfile]struct{}{
		Single: {}, Raid1: {}, Raid10: {},
	}
	// DefaultPolicy value
	DefaultPolicy = StoragePolicy{
		Raid: Single,
	}

	// NullPolicy does not create pools
	NullPolicy = StoragePolicy{}
)

// StoragePolicy describes the pool creation policy
type StoragePolicy struct {
	// Raid profile for this policy
	Raid RaidProfile
	// Number of disks to use in a single pool
	// note that, the disks count must be valid for
	// the chosen raid profile.
	Disks uint8

	// Only create this amount of storage pools. Default to 0 -> unlimited.
	// The spared disks can later be used in automatic repair if a physical
	// disk got corrupt or bad.
	// Note that if it's set to 0 (unlimited), some disks might be spared anyway
	// in case the number of disks required in the policy doesn't add up to pools
	// for example, a pool of 2s on a machine with 5 disks.
	MaxPools uint8
}

// Usage struct
type Usage struct {
	Size gridtypes.Unit
	Used gridtypes.Unit
}

// Filesystem represents a storage space that can be used as a filesystem
type Filesystem struct {
	// Filesystem ID
	ID int
	// Path of the Filesystem
	Path string
	// Usage reports the current usage of the Filesystem
	Usage Usage
	// Name of the Filesystem
	Name string
	// FsType of the Filesystem
	FsType string
	// DiskType of the Filesystem
	DiskType DeviceType
}

// VDisk info returned by a call to inspect
type VDisk struct {
	// Path to disk
	Path string
	// Size in bytes
	Size int64
}

// Name returns the Name part of the disk path
func (d *VDisk) Name() string {
	return filepath.Base(d.Path)
}

// StorageAllocater interface
type StorageAllocater interface {
	// ZDBAllocate
	ZDBAllocate(id string, size gridtypes.Unit, mode ZDBMode) (Allocation, error)

	// ZDBFind finds a ZDB by id
	ZDBFind(id string) (Allocation, error)

	// VolumeAllocate
	VolumeAllocate(id string, size gridtypes.Unit) (Filesystem, error)

	// VolumeUpdate changes a filesystem size to given value.
	VolumeUpdate(name string, size gridtypes.Unit) (Filesystem, error)
	// VolumeRelease signals that the named filesystem is no longer needed.
	// The filesystem will be unmounted and subsequently removed.
	// All data contained in the filesystem will be lost, and the
	// space which has been reserved for this filesystem will be reclaimed.
	VolumeRelease(name string) error
	// VolumesList return all the filesystem managed by storeaged present on the nodes
	// this can be an expensive call on server with a lot of disk, don't use it in a
	// intensive loop
	// Special filesystem like internal cache and vdisk are not return by this function
	// to access them use the GetCacheFS or GetVdiskFS
	VolumesList() ([]Filesystem, error)
	// Path return the filesystem named name
	// if no filesystem with this name exists, an error is returned
	VolumePath(name string) (Filesystem, error)

	// VDiskAllocate
	VDiskAllocate(id string, size gridtypes.Unit) (string, error)

	// VDiskWriteImage an image image to disk with id
	VDiskWriteImage(id string, image string) error
	// VDiskEnsureFilesystem ensures disk has a valid filesystem
	// this method is idempotent
	VDiskEnsureFilesystem(id string) error
	// VDiskDeallocate removes a virtual disk
	VDiskDeallocate(id string) error
	// VDiskExists checks if disk with that ID already allocated
	VDiskExists(id string) bool
	// VDiskInspect return info about the disk
	VDiskInspect(id string) (VDisk, error)
	// VDiskList lists all the available vdisks
	VDiskList() ([]VDisk, error)

	// GetCacheFS return the special filesystem used by 0-OS to store internal state and flist cache
	GetCacheFS() (Filesystem, error)
}

// StorageModule defines the api for storage
type StorageModule interface {
	StorageAllocater

	// Total gives the total amount of storage available for a device type
	Total(kind DeviceType) (uint64, error)
	// BrokenPools lists the broken storage pools that have been detected
	BrokenPools() []BrokenPool
	// BrokenDevices lists the broken devices that have been detected
	BrokenDevices() []BrokenDevice

	//Monitor returns stats stream about pools
	Monitor(ctx context.Context) <-chan PoolsStats
}
