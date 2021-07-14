package stubs

import (
	"context"
	zbus "github.com/threefoldtech/zbus"
	pkg "github.com/threefoldtech/zos/pkg"
	gridtypes "github.com/threefoldtech/zos/pkg/gridtypes"
	zos "github.com/threefoldtech/zos/pkg/gridtypes/zos"
)

type StorageModuleStub struct {
	client zbus.Client
	module string
	object zbus.ObjectID
}

func NewStorageModuleStub(client zbus.Client) *StorageModuleStub {
	return &StorageModuleStub{
		client: client,
		module: "storage",
		object: zbus.ObjectID{
			Name:    "storage",
			Version: "0.0.1",
		},
	}
}

func (s *StorageModuleStub) BrokenDevices(ctx context.Context) (ret0 []pkg.BrokenDevice) {
	args := []interface{}{}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "BrokenDevices", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) BrokenPools(ctx context.Context) (ret0 []pkg.BrokenPool) {
	args := []interface{}{}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "BrokenPools", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) GetCacheFS(ctx context.Context) (ret0 pkg.Filesystem, ret1 error) {
	args := []interface{}{}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "GetCacheFS", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	ret1 = new(zbus.RemoteError)
	if err := result.Unmarshal(1, &ret1); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) Monitor(ctx context.Context) (<-chan pkg.PoolsStats, error) {
	ch := make(chan pkg.PoolsStats)
	recv, err := s.client.Stream(ctx, s.module, s.object, "Monitor")
	if err != nil {
		return nil, err
	}
	go func() {
		defer close(ch)
		for event := range recv {
			var obj pkg.PoolsStats
			if err := event.Unmarshal(&obj); err != nil {
				panic(err)
			}
			ch <- obj
		}
	}()
	return ch, nil
}

func (s *StorageModuleStub) Total(ctx context.Context, arg0 zos.DeviceType) (ret0 uint64, ret1 error) {
	args := []interface{}{arg0}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "Total", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	ret1 = new(zbus.RemoteError)
	if err := result.Unmarshal(1, &ret1); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) VDiskAllocate(ctx context.Context, arg0 string, arg1 gridtypes.Unit) (ret0 string, ret1 error) {
	args := []interface{}{arg0, arg1}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "VDiskAllocate", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	ret1 = new(zbus.RemoteError)
	if err := result.Unmarshal(1, &ret1); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) VDiskDeallocate(ctx context.Context, arg0 string) (ret0 error) {
	args := []interface{}{arg0}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "VDiskDeallocate", args...)
	if err != nil {
		panic(err)
	}
	ret0 = new(zbus.RemoteError)
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) VDiskEnsureFilesystem(ctx context.Context, arg0 string) (ret0 error) {
	args := []interface{}{arg0}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "VDiskEnsureFilesystem", args...)
	if err != nil {
		panic(err)
	}
	ret0 = new(zbus.RemoteError)
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) VDiskExists(ctx context.Context, arg0 string) (ret0 bool) {
	args := []interface{}{arg0}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "VDiskExists", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) VDiskInspect(ctx context.Context, arg0 string) (ret0 pkg.VDisk, ret1 error) {
	args := []interface{}{arg0}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "VDiskInspect", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	ret1 = new(zbus.RemoteError)
	if err := result.Unmarshal(1, &ret1); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) VDiskList(ctx context.Context) (ret0 []pkg.VDisk, ret1 error) {
	args := []interface{}{}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "VDiskList", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	ret1 = new(zbus.RemoteError)
	if err := result.Unmarshal(1, &ret1); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) VDiskWriteImage(ctx context.Context, arg0 string, arg1 string) (ret0 error) {
	args := []interface{}{arg0, arg1}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "VDiskWriteImage", args...)
	if err != nil {
		panic(err)
	}
	ret0 = new(zbus.RemoteError)
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) VolumeAllocate(ctx context.Context, arg0 string, arg1 gridtypes.Unit) (ret0 pkg.Filesystem, ret1 error) {
	args := []interface{}{arg0, arg1}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "VolumeAllocate", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	ret1 = new(zbus.RemoteError)
	if err := result.Unmarshal(1, &ret1); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) VolumePath(ctx context.Context, arg0 string) (ret0 pkg.Filesystem, ret1 error) {
	args := []interface{}{arg0}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "VolumePath", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	ret1 = new(zbus.RemoteError)
	if err := result.Unmarshal(1, &ret1); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) VolumeRelease(ctx context.Context, arg0 string) (ret0 error) {
	args := []interface{}{arg0}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "VolumeRelease", args...)
	if err != nil {
		panic(err)
	}
	ret0 = new(zbus.RemoteError)
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) VolumeUpdate(ctx context.Context, arg0 string, arg1 gridtypes.Unit) (ret0 pkg.Filesystem, ret1 error) {
	args := []interface{}{arg0, arg1}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "VolumeUpdate", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	ret1 = new(zbus.RemoteError)
	if err := result.Unmarshal(1, &ret1); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) VolumesList(ctx context.Context) (ret0 []pkg.Filesystem, ret1 error) {
	args := []interface{}{}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "VolumesList", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	ret1 = new(zbus.RemoteError)
	if err := result.Unmarshal(1, &ret1); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) ZDBAllocate(ctx context.Context, arg0 string, arg1 gridtypes.Unit, arg2 zos.ZDBMode) (ret0 pkg.Allocation, ret1 error) {
	args := []interface{}{arg0, arg1, arg2}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "ZDBAllocate", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	ret1 = new(zbus.RemoteError)
	if err := result.Unmarshal(1, &ret1); err != nil {
		panic(err)
	}
	return
}

func (s *StorageModuleStub) ZDBFind(ctx context.Context, arg0 string) (ret0 pkg.Allocation, ret1 error) {
	args := []interface{}{arg0}
	result, err := s.client.RequestContext(ctx, s.module, s.object, "ZDBFind", args...)
	if err != nil {
		panic(err)
	}
	if err := result.Unmarshal(0, &ret0); err != nil {
		panic(err)
	}
	ret1 = new(zbus.RemoteError)
	if err := result.Unmarshal(1, &ret1); err != nil {
		panic(err)
	}
	return
}
