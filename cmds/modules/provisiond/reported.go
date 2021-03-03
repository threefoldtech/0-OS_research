package provisiond

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/joncrlsn/dque"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/zos/pkg/gridtypes"
	"github.com/threefoldtech/zos/pkg/provision/storage"
)

const (
	every = 5 * 60 // 5 minutes
)

type many []error

func (m many) Error() string {
	return m.WithPrefix("")
}

func (m many) WithPrefix(p string) string {
	var buf bytes.Buffer
	for _, err := range m {
		if buf.Len() > 0 {
			buf.WriteRune('\n')
		}
		if err, ok := err.(many); ok {
			buf.WriteString(err.WithPrefix(p + " "))
			continue
		}

		buf.WriteString(err.Error())
	}

	return buf.String()
}

func (m many) append(err error) many {
	return append(m, err)
}

func (m many) AsError() error {
	if len(m) == 0 {
		return nil
	}

	return m
}

// Consumption struct
type Consumption struct {
	Workloads map[gridtypes.WorkloadType]uint64 `json:"workloads"`
	Capacity  gridtypes.Capacity                `json:"capacity"`
}

// Report is a user report
type Report struct {
	Timestamp   int64 `json:"timestamp"`
	Consumption map[gridtypes.ID]Consumption
}

// Reporter structure
type Reporter struct {
	storage *storage.Fs
	queue   *dque.DQue
}

func reportBuilder() interface{} {
	return &Report{}
}

// NewReported creates a new capacity reporter
func NewReported(s *storage.Fs, report string) (*Reporter, error) {
	if err := os.MkdirAll(report, 0755); err != nil && !os.IsExist(err) {
		return nil, errors.Wrap(err, "failed to create persisted directory for report queue")
	}

	queue, err := dque.NewOrOpen("consumption", report, 1024, reportBuilder)
	if err != nil {
		return nil, errors.Wrap(err, "failed to setup report persisted queue")
	}

	return &Reporter{storage: s, queue: queue}, nil
}

func (r *Reporter) pushOne() error {
	item, err := r.queue.PeekBlock()
	if err != nil {
		return errors.Wrap(err, "failed to peek into capacity queue. #properlyfatal")
	}

	report := item.(Report)

	//TODO: push to farmer here. try forever !
	fmt.Println(report)

	// only removed if report is reported to
	// farmer
	// remove item from queue
	_, err = r.queue.Dequeue()

	return err
}

func (r *Reporter) pusher() {
	for {
		if err := r.pushOne(); err != nil {
			log.Error().Err(err).Msg("error while processing capacity report")
		}
	}
}

// Run runs the reporter
func (r *Reporter) Run(ctx context.Context) error {
	// go over all user reservations
	// take into account the following:
	// every is in seconds.

	go r.pusher()

	ticker := time.NewTicker(every * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case t := <-ticker.C:
			// align time.
			u := t.Unix()
			u = (u / every) * every
			// so any reservation that is deleted but this
			// happened 'after' this time stamp is still
			// considered as consumption because it lies in the current
			// 5 minute slot.
			// but if it was stopped before that timestamp, then it will
			// not be counted.
			report, err := r.collect(time.Unix(u, 0))
			if err != nil {
				log.Error().Err(err).Msg("failed to collect users consumptions")
				continue
			}

			if err := r.push(report); err != nil {
				log.Error().Err(err).Msg("failed to push capacity report")
			}
		}
	}
}

func (r *Reporter) collect(since time.Time) (rep Report, err error) {
	users, err := r.storage.Users()
	if err != nil {
		return rep, err
	}

	rep.Timestamp = since.Unix()
	rep.Consumption = make(map[gridtypes.ID]Consumption)

	for _, user := range users {
		cap, err := r.user(since, user)
		if err != nil {
			log.Error().Err(err).Msg("failed to collect all user capacity")
			// NOTE: we intentionally not doing a 'continue' or 'return'
			// here because even if an error is returned we can have partially
			// collected some of the user consumption, we still can report that
		}

		if cap.Capacity.Zero() {
			continue
		}

		rep.Consumption[user] = cap
	}

	return
}

func (r *Reporter) push(report Report) error {
	return r.queue.Enqueue(report)
}

func (r *Reporter) user(since time.Time, user gridtypes.ID) (Consumption, error) {
	var m many
	types := gridtypes.Types()
	consumption := Consumption{
		Workloads: make(map[gridtypes.WorkloadType]uint64),
	}

	for _, typ := range types {
		consumption.Workloads[typ] = 0
		ids, err := r.storage.ByUser(user, typ)
		if err != nil {
			m = m.append(errors.Wrapf(err, "failed to get reservation for user '%s' type '%s", user, typ))
			continue
		}

		for _, id := range ids {
			wl, err := r.storage.Get(id)
			if err != nil {
				m = m.append(errors.Wrapf(err, "failed to get reservation '%s'", id))
				continue
			}

			if wl.Result.IsNil() {
				// no results yet
				continue
			}

			if r.shouldCount(since, &wl.Result) {
				cap, err := wl.Capacity()
				if err != nil {
					m = m.append(errors.Wrapf(err, "failed to get reservation '%s' capacity", id))
					continue
				}

				consumption.Workloads[typ]++
				consumption.Capacity.Add(&cap)
			}

		}
	}

	return consumption, m.AsError()
}

func (r *Reporter) shouldCount(since time.Time, result *gridtypes.Result) bool {
	if result.State == gridtypes.StateOk {
		return true
	}

	if result.State == gridtypes.StateDeleted {
		// if it was stopped before the 'since' .
		if result.Created.Time().Before(since) {
			return false
		}
		// otherwise it's true
		return true
	}

	return false
}
