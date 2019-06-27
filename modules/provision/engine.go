package provision

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/threefoldtech/zbus"
)

type defaultEngine struct {
	client zbus.Client
	source ReservationSource
}

// New creates a new engine. Once started, the engine
// will continue processing all reservations from the reservation source
// and try to apply them.
// the default implementation is a single threaded worker. so it process
// one reservation at a time. On error, the engine will log the error. and
// continue to next reservation.
func New(client zbus.Client, source ReservationSource) Engine {
	return &defaultEngine{client, source}
}

// Run starts processing reservation resource. Then try to allocate
// reservations
func (e *defaultEngine) Run(ctx context.Context) error {
	for reservation := range e.source.Reservations(ctx) {
		fn, ok := types[reservation.Type]
		if !ok {
			e.reply(reservation.ReplyTo, reservation.ID, nil, fmt.Errorf("unknown reservation type '%s'", reservation.Type))
			continue
		}

		result, err := fn(e.client, reservation)
		e.reply(reservation.ReplyTo, reservation.ID, result, err)
	}

	return nil
}

func (e *defaultEngine) reply(to ReplyTo, id string, result interface{}, err error) {
	//TODO: actually push the reply to the endpoint defined by `to`
	if err != nil {
		log.Error().Err(err).Str("reply-to", string(to)).
			Str("id", id).Msgf("failed to apply provision")

		return
	}

	log.Info().Str("reservation", id).Str("result", fmt.Sprint(result)).Msg("reservation result")
}
