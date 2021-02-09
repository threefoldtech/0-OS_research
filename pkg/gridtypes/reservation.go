package gridtypes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
	// KubernetesReservation type
	KubernetesReservation ReservationType = "kubernetes"
	//PublicIPReservation reservation
	PublicIPReservation ReservationType = "ipv4"
)

var (
	reservationTypes map[ReservationType]struct{} = map[ReservationType]struct{}{
		ContainerReservation:  struct{}{},
		VolumeReservation:     struct{}{},
		NetworkReservation:    struct{}{},
		ZDBReservation:        struct{}{},
		KubernetesReservation: struct{}{},
		PublicIPReservation:   struct{}{},
	}
)

func (t ReservationType) Valid() error {
	if _, ok := reservationTypes[t]; !ok {
		return fmt.Errorf("invalid reservation type")
	}

	return nil
}

func (t ReservationType) String() string {
	return string(t)
}

// Workload struct
type Workload struct {
	//Version is version of reservation object
	Version int `json:"version"`
	// ID of the reservation
	ID ID `json:"id"`
	// Identification of the user requesting the reservation
	User ID `json:"user_id"`
	// Type of the reservation (container, zdb, vm, etc...)
	Type ReservationType `json:"type"`
	// Data is the reservation type arguments.
	Data json.RawMessage `json:"data"`
	// Date of creation
	Created time.Time `json:"created"`
	// TODO: Signature is the signature to the reservation
	// it contains all the field of this struct except the signature itself and the Result field
	// Signature string `json:"signature,omitempty"`
	//ToDelete is set if the user/farmer asked the reservation to be deleted
	ToDelete bool `json:"to_delete"`
	// Metadata is custom user metadata
	Metadata string `json:"metadata"`
	// Tag object is mainly used for debugging.
	Tag Tag `json:"-"`
	// Result of reservation
	Result Result `json:"result"`
}

// Valid validate reservation
func (w *Workload) Valid() error {
	if w.ID.IsEmpty() {
		return fmt.Errorf("invalid workload id")
	}

	if w.User.IsEmpty() {
		return fmt.Errorf("invalid user id")
	}

	if err := w.Type.Valid(); err != nil {
		return err
	}

	return nil
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

// ResultState type
type ResultState string

const (
	//StatusAccepted accepted constant
	StatusAccepted = "accepted"
	// StateError constant
	StateError = "error"
	// StateOk constant
	StateOk = "ok"
	//StateDeleted constant
	StateDeleted = "deleted"
)

// Result is the struct filled by the node
// after a reservation object has been processed
type Result struct {
	// Time when the result is sent
	Created time.Time `json:"created"`
	// State of the deployment (ok,error)
	State ResultState `json:"state"`
	// if State is "error", then this field contains the error
	// otherwise it's nil
	Error string `json:"message"`
	// Data is the information generated by the provisioning of the workload
	// its type depend on the reservation type
	Data json.RawMessage `json:"data"`
	// Signature is the signature to the result
	// is generated by signing the bytes returned from call to Result.Bytes()
	// and hex
	Signature string `json:"signature"`
}

// IsNil checks if Result is the zero values
func (r *Result) IsNil() bool {
	// ideally this should be implemented like this
	// emptyResult := Result{}
	// return reflect.DeepEqual(r, &emptyResult)
	//
	// but unfortunately, the empty Result coming from the explorer already have some fields set
	// (like the type)
	// so instead we gonna check the Data and the Created filed

	return (r.Created.Equal(epoch) || r.Created.Equal(nullTime)) && (len(r.Data) == 0 || bytes.Equal(r.Data, nullRaw))
}

var (
	//emptyResult is the Result zero value
	epoch      = time.Unix(0, 0)
	nullTime   = time.Time{}
	nullRaw, _ = json.Marshal(nil)
)

// Bytes returns a slice of bytes container all the information
// used to sign the Result object
func (r *Result) Bytes() ([]byte, error) {
	buf := &bytes.Buffer{}
	if _, err := buf.WriteString(string(r.State)); err != nil {
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
