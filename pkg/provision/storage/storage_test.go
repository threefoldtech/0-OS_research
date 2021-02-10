package storage

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/threefoldtech/zos/pkg/gridtypes"
	"github.com/threefoldtech/zos/pkg/provision"
)

func TestStorageAdd(t *testing.T) {
	require := require.New(t)
	root, err := ioutil.TempDir("", "storage-")
	require.NoError(err)
	defer os.RemoveAll(root)

	store, err := NewFSStore(root)
	require.NoError(err)

	err = store.Add(gridtypes.Workload{
		ID:   "my-id",
		User: "my-user",
		Type: gridtypes.VolumeType,
	})

	require.NoError(err)
	stat, err := os.Lstat(filepath.Join(root, pathByID, "my-id"))
	require.NoError(err)
	require.True(stat.Mode().IsRegular())

	stat, err = os.Lstat(filepath.Join(root, pathByType, "volume", "my-id"))
	require.NoError(err)
	require.Equal(os.ModeSymlink, stat.Mode()&os.ModeSymlink)

	stat, err = os.Lstat(filepath.Join(root, pathByUser, "my-user", pathByType, "volume", "my-id"))
	require.NoError(err)
	require.Equal(os.ModeSymlink, stat.Mode()&os.ModeSymlink)

	stat, err = os.Lstat(filepath.Join(root, pathByUser, "my-user", pathByID, "my-id"))
	require.NoError(err)
	require.Equal(os.ModeSymlink, stat.Mode()&os.ModeSymlink)

}

func TestStorageAddNetwork(t *testing.T) {
	require := require.New(t)
	root, err := ioutil.TempDir("", "storage-")
	require.NoError(err)
	defer os.RemoveAll(root)

	store, err := NewFSStore(root)
	require.NoError(err)

	err = store.Add(gridtypes.Workload{
		ID:   "my-id",
		User: "my-user",
		Type: gridtypes.NetworkType,
		Data: json.RawMessage(`{"name": "my-network"}`),
	})

	require.NoError(err)
	stat, err := os.Lstat(filepath.Join(root, pathByID, "my-id"))
	require.NoError(err)
	require.True(stat.Mode().IsRegular())

	nid := gridtypes.NetworkID("my-user", "my-network")
	stat, err = os.Lstat(filepath.Join(root, pathByType, "network", string(nid)))
	require.NoError(err)
	require.Equal(os.ModeSymlink, stat.Mode()&os.ModeSymlink)

	stat, err = os.Lstat(filepath.Join(root, pathByUser, "my-user", pathByType, "network", string(nid)))
	require.NoError(err)
	require.Equal(os.ModeSymlink, stat.Mode()&os.ModeSymlink)

	stat, err = os.Lstat(filepath.Join(root, pathByUser, "my-user", pathByID, "my-id"))
	require.NoError(err)
	require.Equal(os.ModeSymlink, stat.Mode()&os.ModeSymlink)

}

func TestStorageGetNetwork(t *testing.T) {
	require := require.New(t)
	root, err := ioutil.TempDir("", "storage-")
	require.NoError(err)
	defer os.RemoveAll(root)

	store, err := NewFSStore(root)
	require.NoError(err)

	err = store.Add(gridtypes.Workload{
		ID:   "my-id",
		User: "my-user",
		Type: gridtypes.NetworkType,
		Data: json.RawMessage(`{"name": "my-network"}`),
	})

	require.NoError(err)

	nid := gridtypes.NetworkID("my-user", "my-network")

	loaded, err := store.GetNetwork(nid)
	require.NoError(err)
	require.Equal(gridtypes.ID("my-id"), loaded.ID)
}

func TestStorageSet(t *testing.T) {
	require := require.New(t)
	root, err := ioutil.TempDir("", "storage-")
	require.NoError(err)
	defer os.RemoveAll(root)

	store, err := NewFSStore(root)
	require.NoError(err)

	err = store.Set(gridtypes.Workload{
		ID:   "my-id",
		User: "my-user",
		Type: gridtypes.VolumeType,
	})

	require.Error(err)
	require.True(errors.Is(err, provision.ErrWorkloadNotExists))

	err = store.Add(gridtypes.Workload{
		ID:   "my-id",
		User: "my-user",
		Type: gridtypes.VolumeType,
	})

	require.NoError(err)

	err = store.Set(gridtypes.Workload{
		ID:   "my-id",
		User: "my-user",
		Type: gridtypes.VolumeType,
	})

	require.NoError(err)
}

func TestStorageGet(t *testing.T) {
	require := require.New(t)
	root, err := ioutil.TempDir("", "storage-")
	require.NoError(err)
	defer os.RemoveAll(root)

	store, err := NewFSStore(root)
	require.NoError(err)

	wl := gridtypes.Workload{
		ID:   "my-id",
		User: "my-user",
		Type: gridtypes.VolumeType,
		Data: json.RawMessage(`"hello world"`),
	}

	err = store.Add(wl)
	require.NoError(err)

	loaded, err := store.Get(wl.ID)
	require.NoError(err)
	require.Equal(wl.Data, loaded.Data)
	require.Equal(wl.ID, loaded.ID)
	require.Equal(wl.User, loaded.User)
	require.Equal(wl.Type, loaded.Type)
}

func TestStorageByType(t *testing.T) {
	require := require.New(t)
	root, err := ioutil.TempDir("", "storage-")
	require.NoError(err)
	defer os.RemoveAll(root)

	store, err := NewFSStore(root)
	require.NoError(err)

	err = store.Add(gridtypes.Workload{
		ID:   "my-volume-id",
		User: "my-user",
		Type: gridtypes.VolumeType,
		Data: json.RawMessage(`"hello volume"`),
	})

	require.NoError(err)

	err = store.Add(gridtypes.Workload{
		ID:   "my-container-id",
		User: "my-user",
		Type: gridtypes.ContainerType,
		Data: json.RawMessage(`"hello container"`),
	})

	require.NoError(err)

	ids, err := store.ByType(gridtypes.VolumeType)
	require.NoError(err)
	require.Len(ids, 1)
	require.Equal("my-volume-id", ids[0].String())

	ids, err = store.ByType(gridtypes.ContainerType)
	require.NoError(err)
	require.Len(ids, 1)
	require.Equal("my-container-id", ids[0].String())

	ids, err = store.ByType(gridtypes.ZDBType)
	require.NoError(err)
	require.Len(ids, 0)

}

func TestStorageByUser(t *testing.T) {
	require := require.New(t)
	root, err := ioutil.TempDir("", "storage-")
	require.NoError(err)
	defer os.RemoveAll(root)

	store, err := NewFSStore(root)
	require.NoError(err)

	err = store.Add(gridtypes.Workload{
		ID:   "my-volume-1",
		User: "my-user-1",
		Type: gridtypes.VolumeType,
		Data: json.RawMessage(`"hello volume"`),
	})

	require.NoError(err)

	err = store.Add(gridtypes.Workload{
		ID:   "my-volume-2",
		User: "my-user-2",
		Type: gridtypes.VolumeType,
		Data: json.RawMessage(`"hello container"`),
	})

	require.NoError(err)

	ids, err := store.ByType(gridtypes.VolumeType)
	require.NoError(err)
	require.Len(ids, 2)

	ids, err = store.ByUser("my-user-1", gridtypes.VolumeType)
	require.NoError(err)
	require.Len(ids, 1)
	require.Equal("my-volume-1", ids[0].String())

	ids, err = store.ByUser("my-user-2", gridtypes.VolumeType)
	require.NoError(err)
	require.Len(ids, 1)
	require.Equal("my-volume-2", ids[0].String())

	ids, err = store.ByType(gridtypes.ZDBType)
	require.NoError(err)
	require.Len(ids, 0)

}