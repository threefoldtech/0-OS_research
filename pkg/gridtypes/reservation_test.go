package gridtypes

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkloadData(t *testing.T) {
	require := require.New(t)

	wl := Workload{
		Type: VolumeReservation,
		Data: json.RawMessage(`{"size": 10, "type": "ssd"}`),
	}

	data, err := wl.WorkloadData()
	require.NoError(err)

	require.IsType(&Volume{}, data)
	volume := data.(*Volume)

	require.Equal(uint64(10), volume.Size)
	require.Equal(SSDDevice, volume.Type)
}

func TestWorkloadValidation(t *testing.T) {
	require := require.New(t)

	wl := Workload{
		ID:   ID("my-id"),
		User: ID("my-user"),
		Type: VolumeReservation,
		Data: json.RawMessage(`{"size": 10, "type": "ssd"}`),
	}

	err := wl.Valid()
	require.NoError(err)

	wl = Workload{
		ID:   ID("my-id"),
		User: ID("my-user"),
		Type: VolumeReservation,
		Data: json.RawMessage(`{"size": 10, "type": "abc"}`),
	}

	err = wl.Valid()
	require.EqualError(err, "invalid device type")

}
