package provision

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	generated "github.com/threefoldtech/zos/pkg/gedis/types/provision"
	"github.com/threefoldtech/zos/pkg/schema"
	"github.com/threefoldtech/zos/pkg/versioned"
	"github.com/threefoldtech/zos/tools/bcdb_mock/models/generated/workloads"
)

// ReservationType type
type ReservationType string

const (
	// ContainerReservation type
	ContainerReservation ReservationType = "container"
	// VolumeReservation type
	VolumeReservation ReservationType = "volume"
	// NetworkReservation type
	NetworkReservation ReservationType = "network"
	// ZDBReservation type
	ZDBReservation ReservationType = "zdb"
	// DebugReservation type
	DebugReservation ReservationType = "debug"
	// KubernetesReservation type
	KubernetesReservation ReservationType = "kubernetes"
)

var (
	// reservationSchemaV1 reservation schema version 1
	reservationSchemaV1 = versioned.MustParse("1.0.0")
	// reservationSchemaLastVersion link to latest version
	reservationSchemaLastVersion = reservationSchemaV1
)

// Reservation struct
type Reservation struct {
	// ID of the reservation
	ID string `json:"id"`
	// NodeID of the node where to deploy this reservation
	NodeID string `json:"node_id"`
	// Identification of the user requesting the reservation
	User string `json:"user_id"`
	// Type of the reservation (container, zdb, vm, etc...)
	Type ReservationType `json:"type"`
	// Data is the reservation type arguments.
	Data json.RawMessage `json:"data,omitempty"`
	// Date of creation
	Created time.Time `json:"created"`
	// Duration of the reservation
	Duration time.Duration `json:"duration"`
	// Signature is the signature to the reservation
	// it contains all the field of this struct except the signature itself and the Result field
	Signature []byte `json:"signature,omitempty"`

	// This flag is set to true when a reservation needs to be deleted
	// before its expiration time
	ToDelete bool `json:"to_delete"`

	// Tag object is mainly used for debugging.
	Tag Tag `json:"-"`
}

// AppendTag appends tags
func AppendTag(t, n Tag) Tag {
	if t == nil {
		t = Tag{}
	}

	for k, v := range n {
		t[k] = v
	}

	return t
}

// Tag is custom tag to mark certain reservations
type Tag map[string]string

func (t Tag) String() string {
	var builder strings.Builder
	for k, v := range t {
		if builder.Len() != 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(k)
		builder.WriteString(": ")
		builder.WriteString(v)
	}

	return builder.String()
}

//SplitID gets the reservation part and the workload part from a full reservation ID
func (r *Reservation) SplitID() (reservation uint64, workload uint64, err error) {
	parts := strings.SplitN(r.ID, "-", 2)
	if len(parts) != 2 {
		return reservation, workload, fmt.Errorf("invalid reservation id format (wront length)")
	}
	reservation, err = strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return reservation, workload, errors.Wrap(err, "invalid reservation id format (reservation part)")
	}
	workload, err = strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return reservation, workload, errors.Wrap(err, "invalid reservation id format (workload part)")
	}

	return
}

// Expired returns a boolean depending if the reservation
// has expire or not at the time of the function call
func (r *Reservation) Expired() bool {
	expire := r.Created.Add(r.Duration)
	return time.Now().After(expire)
}

func (r *Reservation) validate() error {
	// TODO: during testnet phase seems we don't need to verify this
	// if err := Verify(r); err != nil {
	// 	log.Warn().
	// 		Err(err).
	// 		Str("id", string(r.ID)).
	// 		Msg("verification of reservation signature failed")
	// 	return errors.Wrapf(err, "verification of reservation %s signature failed", r.ID)
	// }

	if r.Duration <= 0 {
		return fmt.Errorf("reservation %s has not duration", r.ID)
	}

	if r.Created.IsZero() {
		return fmt.Errorf("wrong creation date in reservation %s", r.ID)
	}

	if r.Expired() {
		return fmt.Errorf("reservation %s has expired", r.ID)
	}

	return nil
}

// ResultState type
type ResultState workloads.TfgridWorkloadsReservationResult1StateEnum

const (
	// StateError constant
	StateError = ResultState(workloads.TfgridWorkloadsReservationResult1StateError)
	// StateOk constant
	StateOk = ResultState(workloads.TfgridWorkloadsReservationResult1StateOk)
	//StateDeleted constant
	StateDeleted = ResultState(workloads.TfgridWorkloadsReservationResult1StateDeleted)
)

func (s ResultState) String() string {
	return generated.TfgridReservationResult1StateEnum(s).String()
}

// Result is the struct filled by the node
// after a reservation object has been processed
type Result struct {
	Type ReservationType `json:"type"`
	//Reservation ID
	ID string `json:"id"`
	// Time when the result is sent
	Created time.Time `json:"created"`
	// State of the deployment (ok,error)
	State ResultState `json:"state"`
	// if State is "error", then this field contains the error
	// otherwise it's nil
	Error string `json:"message"`
	// Data is the information generated by the provisioning of the workload
	// its type depend on the reservation type
	Data json.RawMessage `json:"data_json"`
	// Signature is the signature to the result
	// is generated by signing the bytes returned from call to Result.Bytes()
	// and hex
	Signature string `json:"signature"`
}

// Bytes returns a slice of bytes container all the information
// used to sign the Result object
func (r *Result) Bytes() ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := buf.WriteByte(byte(r.State)); err != nil {
		return nil, err
	}
	if _, err := buf.WriteString(r.Error); err != nil {
		return nil, err
	}
	if _, err := buf.Write(r.Data); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// ToSchemaType converts result to schema type
func (r *Result) ToSchemaType() workloads.TfgridWorkloadsReservationResult1 {
	var rType workloads.TfgridWorkloadsReservationResult1CategoryEnum
	switch r.Type {
	case VolumeReservation:
		rType = workloads.TfgridWorkloadsReservationResult1CategoryVolume
	case ContainerReservation:
		rType = workloads.TfgridWorkloadsReservationResult1CategoryContainer
	case ZDBReservation:
		rType = workloads.TfgridWorkloadsReservationResult1CategoryZdb
	case NetworkReservation:
		rType = workloads.TfgridWorkloadsReservationResult1CategoryNetwork
	default:
		panic(fmt.Errorf("unknown reservation type: %s", r.Type))
	}

	result := workloads.TfgridWorkloadsReservationResult1{
		Category:   rType,
		WorkloadId: r.ID,
		DataJson:   r.Data,
		Signature:  r.Signature,
		State:      workloads.TfgridWorkloadsReservationResult1StateEnum(r.State),
		Message:    r.Error,
		Epoch:      schema.Date{Time: r.Created},
	}

	return result
}
