package provision

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/zos/pkg"
	"github.com/threefoldtech/zos/pkg/app"
	"github.com/threefoldtech/zos/pkg/versioned"
)

// Counter interface
type Counter interface {
	// Increment counter atomically by one
	Increment() int64
	// Decrement counter atomically by one
	Decrement() int64
	// Current returns the current value
	Current() int64

	Add(v int64) int64
	Remove(v int64) int64
}

type counterNop struct{}

func (c *counterNop) Increment() int64 {
	return 0
}

func (c *counterNop) Decrement() int64 {
	return 0
}

func (c *counterNop) Current() int64 {
	return 0
}

func (c *counterNop) Add(v int64) int64 {
	return 0
}
func (c *counterNop) Remove(v int64) int64 {
	return 0
}

// counterImpl value for safe increment/decrement
type counterImpl int64

// Increment counter atomically by one
func (c *counterImpl) Increment() int64 {
	return atomic.AddInt64((*int64)(c), 1)
}

func (c *counterImpl) Add(a int64) int64 {
	return atomic.AddInt64((*int64)(c), a)
}

func (c *counterImpl) Remove(a int64) int64 {
	return atomic.AddInt64((*int64)(c), -a)
}

// Decrement counter atomically by one
func (c *counterImpl) Decrement() int64 {
	return atomic.AddInt64((*int64)(c), -1)
}

// Current returns the current value
func (c *counterImpl) Current() int64 {
	return atomic.LoadInt64((*int64)(c))
}

// FSStore is a in reservation store
// using the filesystem as backend
type FSStore struct {
	sync.RWMutex
	root string

	counters struct {
		containers counterImpl
		volumes    counterImpl
		networks   counterImpl
		zdb        counterImpl
		vm         counterImpl
		debug      counterImpl

		sru counterImpl
		hru counterImpl
		mru counterImpl
		cru counterImpl
	}
}

// NewFSStore creates a in memory reservation store
func NewFSStore(root string) (*FSStore, error) {
	if app.IsFirstBoot("provisiond") {
		log.Info().Msg("first boot, empty reservation cache")
		if err := os.RemoveAll(root); err != nil {
			return nil, err
		}

		if err := app.MarkBooted("provisiond"); err != nil {
			return nil, errors.Wrap(err, "fail to mark provisiond as booted")
		}
	}

	if err := os.MkdirAll(root, 0770); err != nil {
		return nil, err
	}

	log.Info().Msg("restart detected, keep reservation cache intact")

	store := &FSStore{
		root: root,
	}

	return store, store.sync()
}

func (s *FSStore) sync() error {
	s.RLock()
	defer s.RUnlock()

	infos, err := ioutil.ReadDir(s.root)
	if err != nil {
		return err
	}

	for _, info := range infos {
		if info.IsDir() {
			continue
		}

		r, err := s.get(info.Name())
		if err != nil {
			return err
		}

		s.counterFor(r.Type).Increment()
	}

	return nil
}

// Counters returns stats about the cashed reservations
func (s *FSStore) Counters() pkg.ProvisionCounters {
	return pkg.ProvisionCounters{
		Container: s.counters.containers.Current(),
		Volume:    s.counters.volumes.Current(),
		Network:   s.counters.networks.Current(),
		ZDB:       s.counters.zdb.Current(),
		VM:        s.counters.vm.Current(),
		Debug:     s.counters.debug.Current(),
	}
}

func (s *FSStore) counterFor(typ ReservationType) Counter {
	switch typ {
	case ContainerReservation:
		return &s.counters.containers
	case VolumeReservation:
		return &s.counters.volumes
	case NetworkReservation:
		return &s.counters.networks
	case ZDBReservation:
		return &s.counters.zdb
	case DebugReservation:
		return &s.counters.debug
	case KubernetesReservation:
		return &s.counters.vm
	default:
		// this will avoid nil pointer
		return &counterNop{}
	}
}

// Add a reservation to the store
func (s *FSStore) Add(r *Reservation) error {
	s.Lock()
	defer s.Unlock()

	// ensure direcory exists
	if err := os.MkdirAll(s.root, 0770); err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(s.root, r.ID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0660)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("reservation %s already in the store", r.ID)
		}
		return err
	}
	defer f.Close()
	writer, err := versioned.NewWriter(f, reservationSchemaLastVersion)
	if err != nil {
		return err
	}

	if err := json.NewEncoder(writer).Encode(r); err != nil {
		return err
	}

	s.counterFor(r.Type).Increment()
	s.processResourceUnits(r, true)

	return nil
}

// Remove a reservation from the store
func (s *FSStore) Remove(id string) error {
	s.Lock()
	defer s.Unlock()

	r, err := s.get(id)
	if os.IsNotExist(errors.Cause(err)) {
		return nil
	}

	path := filepath.Join(s.root, id)
	err = os.Remove(path)
	if os.IsNotExist(err) {
		// shouldn't happen because we just did a get
		return nil
	} else if err != nil {
		return err
	}

	s.counterFor(r.Type).Decrement()
	s.processResourceUnits(r, false)

	return nil
}

func (s *FSStore) processResourceUnits(r *Reservation, addOrRemoveBool bool) error {
	switch r.Type {
	case VolumeReservation:
		return s.processVolume(r, addOrRemoveBool)
	case ContainerReservation:
		return s.processContainer(r, addOrRemoveBool)
	case ZDBReservation:
		return s.processZdb(r, addOrRemoveBool)
	case KubernetesReservation:
		return s.processKubernetes(r, addOrRemoveBool)
	}

	return nil
}

func (s *FSStore) processVolume(r *Reservation, inc bool) error {
	var volume Volume
	if err := json.Unmarshal(r.Data, &volume); err != nil {
		return err
	}
	var c Counter
	switch volume.Type {
	case SSDDiskType:
		// volume.size in MB, but sru is in GB
		c = &s.counters.sru
	case HDDDiskType:
		c = &s.counters.hru
	}

	if inc {
		c.Add(int64(volume.Size))

	} else {
		c.Remove(int64(volume.Size))
	}

	return nil
}

func (s *FSStore) processContainer(r *Reservation, inc bool) error {
	var contCap ContainerCapacity
	if err := json.Unmarshal(r.Data, &contCap); err != nil {
		return err
	}
	var cpuCounter Counter = &s.counters.cru
	var memoryCounter Counter = &s.counters.mru

	if inc {
		cpuCounter.Add(int64(contCap.CPU))
		memoryCounter.Add(int64(contCap.Memory))
	} else {
		cpuCounter.Remove(int64(contCap.CPU))
		memoryCounter.Remove(int64(contCap.Memory))
	}

	return nil
}

func (s *FSStore) processZdb(r *Reservation, inc bool) error {
	var zdbVolume ZDB
	if err := json.Unmarshal(r.Data, &zdbVolume); err != nil {
		return err
	}
	var volumeCounter Counter
	switch zdbVolume.DiskType {
	case "SSD":
		volumeCounter = &s.counters.sru
	case "HDD":
		volumeCounter = &s.counters.hru
	}

	if inc {
		volumeCounter.Add(int64(zdbVolume.Size))
	} else {
		volumeCounter.Remove(int64(zdbVolume.Size))
	}

	return nil
}

func (s *FSStore) processKubernetes(r *Reservation, inc bool) error {
	var k8s Kubernetes
	if err := json.Unmarshal(r.Data, &k8s); err != nil {
		return err
	}
	var k8sCPUCounter Counter = &s.counters.cru
	var k8sMemoryCounter Counter = &s.counters.mru
	var k8sSSDCounter Counter = &s.counters.sru
	switch k8s.Size {
	case 1:
		if inc {
			k8sCPUCounter.Add(1)
			k8sMemoryCounter.Add(2048)
			k8sSSDCounter.Add(50)
		} else {
			k8sCPUCounter.Remove(1)
			k8sMemoryCounter.Remove(2048)
			k8sSSDCounter.Remove(50)
		}
	case 2:
		if inc {
			k8sCPUCounter.Add(2)
			k8sMemoryCounter.Add(4096)
			k8sSSDCounter.Add(100)
		} else {
			k8sCPUCounter.Remove(2)
			k8sMemoryCounter.Remove(4096)
			k8sSSDCounter.Remove(100)
		}
	}

	return nil
}

// GetExpired returns all id the the reservations that are expired
// at the time of the function call
func (s *FSStore) GetExpired() ([]*Reservation, error) {
	s.RLock()
	defer s.RUnlock()

	infos, err := ioutil.ReadDir(s.root)
	if err != nil {
		return nil, err
	}

	rs := make([]*Reservation, 0, len(infos))
	for _, info := range infos {
		if info.IsDir() {
			continue
		}

		r, err := s.get(info.Name())
		if err != nil {
			return nil, err
		}
		if r.Expired() {
			r.Tag = Tag{"source": "FSStore"}
			rs = append(rs, r)
		}

	}

	return rs, nil
}

// Get retrieves a specific reservation using its ID
// if returns a non nil error if the reservation is not present in the store
func (s *FSStore) Get(id string) (*Reservation, error) {
	s.RLock()
	defer s.RUnlock()

	return s.get(id)
}

// Exists checks if the reservation ID is in the store
func (s *FSStore) Exists(id string) (bool, error) {
	s.RLock()
	defer s.RUnlock()

	path := filepath.Join(s.root, id)
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *FSStore) get(id string) (*Reservation, error) {
	path := filepath.Join(s.root, id)
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, errors.Wrapf(err, "reservation %s not found", id)
	} else if err != nil {
		return nil, err
	}

	defer f.Close()
	reader, err := versioned.NewReader(f)
	if versioned.IsNotVersioned(err) {
		if _, err := f.Seek(0, 0); err != nil { // make sure to read from start
			return nil, err
		}
		reader = versioned.NewVersionedReader(versioned.MustParse("0.0.0"), f)
	}

	validV1 := versioned.MustParseRange(fmt.Sprintf("<=%s", reservationSchemaV1))
	var reservation Reservation

	if validV1(reader.Version()) {
		if err := json.NewDecoder(reader).Decode(&reservation); err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("unknown reservation object version (%s)", reader.Version())
	}
	reservation.Tag = Tag{"source": "FSStore"}
	return &reservation, nil
}

// Close makes sure the backend of the store is closed properly
func (s *FSStore) Close() error {
	return nil
}
