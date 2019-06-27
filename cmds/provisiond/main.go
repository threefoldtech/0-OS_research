package main

import (
	"context"
	"flag"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/threefoldtech/zbus"
	"github.com/threefoldtech/zosv2/modules/provision"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	var (
		msgBrokerCon string
		resURL       string
	)

	flag.StringVar(&msgBrokerCon, "broker", "unix:///var/run/redis.sock", "connection string to the message broker")
	flag.StringVar(&resURL, "url", "", "reservation url to poll from")

	flag.Parse()

	client, err := zbus.NewRedisClient(msgBrokerCon)
	if err != nil {
		log.Fatal().Msgf("fail to connect to message broker server: %v", err)
	}

	pipe, err := provision.FifoSource("/var/run/reservation.pipe")
	if err != nil {
		log.Fatal().Err(err).Msgf("failed to allocation reservation pipe")
	}

	source := pipe
	if len(resURL) != 0 {
		source = provision.CompinedSource(
			pipe,
			provision.HTTPSource(resURL),
		)
	}

	engine := provision.New(client, source)

	log.Info().
		Str("broker", msgBrokerCon).
		Msg("starting provision module")

	if err := engine.Run(context.Background()); err != nil {
		log.Error().Err(err).Msg("unexpected error")
	}
}
