package rmb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const (
	systemLocalBus = "msgbus.system.local"
	replyBus       = "msgbus.system.reply"
	numWorkers     = 5
)

type twinKeyID struct{}
type messageKey struct{}

// Message is an struct used to communicate over the messagebus
type Message struct {
	Version    int      `json:"ver"`
	UID        string   `json:"uid"`
	Command    string   `json:"cmd"`
	Expiration int      `json:"exp"`
	Retry      int      `json:"try"`
	Data       string   `json:"dat"`
	TwinSrc    uint32   `json:"src"`
	TwinDest   []uint32 `json:"dest"`
	Retqueue   string   `json:"ret"`
	Schema     string   `json:"shm"`
	Epoch      int64    `json:"now"`
	Err        string   `json:"err"`
}

// MessageBus is a struct that contains everything required to run the message bus
type MessageBus struct {
	Context  context.Context
	pool     *redis.Pool
	handlers map[string]func(ctx context.Context, payload []byte) (interface{}, error)
}

// New creates a new message bus
func New(ctx context.Context, address string) (*MessageBus, error) {
	pool, err := newRedisPool(address)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to %s", address)
	}

	return &MessageBus{
		pool:     pool,
		Context:  ctx,
		handlers: make(map[string]func(ctx context.Context, payload []byte) (interface{}, error)),
	}, nil
}

// WithHandler adds a topic handler to the messagebus
func (m *MessageBus) WithHandler(topic string, handler func(ctx context.Context, payload []byte) (interface{}, error)) {
	m.handlers[topic] = handler
}

// Run runs listeners to the configured handlers
// and will trigger the handlers in the case an event comes in
func (m *MessageBus) Run(ctx context.Context) error {
	con := m.pool.Get()
	defer con.Close()

	topics := make([]string, len(m.handlers))
	for topic := range m.handlers {
		topics = append(topics, topic)
	}

	jobs := make(chan Message, numWorkers)
	for i := 1; i <= numWorkers; i++ {
		go m.worker(ctx, jobs)
	}

	for {
		if m.Context.Err() != nil {
			return nil
		}

		data, err := redis.ByteSlices(con.Do("BLPOP", redis.Args{}.AddFlat(topics).Add(0)...))
		if err != nil {
			log.Err(err).Msg("failed to read from system local messagebus")
			return err
		}

		var message Message
		err = json.Unmarshal(data[1], &message)
		if err != nil {
			log.Err(err).Msg("failed to unmarshal message")
			continue
		}

		_, ok := m.handlers[string(data[0])]
		if !ok {
			log.Debug().Msg("handler not found")
			continue
		}

		jobs <- message
	}
}

func (m *MessageBus) worker(ctx context.Context, jobs chan Message) {
	for {
		select {
		case <-ctx.Done():
			return
		case message := <-jobs:
			bytes, err := message.GetPayload()
			if err != nil {
				log.Err(err).Msg("err while parsing payload reply")
			}

			handler, ok := m.handlers[message.Command]
			if !ok {
				log.Warn().Msg("handler not found")
			}

			requestCtx := context.WithValue(ctx, twinKeyID{}, message.TwinSrc)
			requestCtx = context.WithValue(requestCtx, messageKey{}, message)

			data, err := handler(requestCtx, bytes)
			if err != nil {
				log.Err(err).Msg("err while handling job")
				// TODO: create an error object
				message.Err = err.Error()
			}

			err = m.sendReply(message, data)
			if err != nil {
				log.Err(err).Msg("err while sending reply")
			}
		}
	}
}

// GetMessage gets a message from the context, panics if it's not there
func GetMessage(ctx context.Context) (*Message, error) {
	message, ok := ctx.Value(messageKey{}).(Message)
	if !ok {
		panic("failed to load message from context")
	}

	return &message, nil
}

// sendReply send a reply to the message bus with some data
func (m *MessageBus) sendReply(message Message, data interface{}) error {
	con := m.pool.Get()
	defer con.Close()

	// reply to source
	message.TwinDest = []uint32{message.TwinSrc}

	// base 64 encode the response data
	// message.Data = base64.StdEncoding.EncodeToString(data)

	// set the time to now
	message.Epoch = time.Now().Unix()

	bytes, err := json.Marshal(message)
	if err != nil {
		return err
	}

	_, err = con.Do("RPUSH", replyBus, bytes)
	if err != nil {
		log.Err(err).Msg("failed to push to reply messagebus")
		return err
	}

	return nil
}

// PushMessage pushes a message to a topic
// for testing purposes
func (m *MessageBus) PushMessage(topic string, message Message) error {
	con := m.pool.Get()
	defer con.Close()

	bytes, err := json.Marshal(message)
	if err != nil {
		return err
	}

	_, err = con.Do("RPUSH", topic, bytes)
	if err != nil {
		log.Err(err).Msg("failed to push to topic")
		return err
	}

	return nil
}

// GetPayload returns the payload for a message's data
func (m *Message) GetPayload() ([]byte, error) {
	return base64.RawStdEncoding.DecodeString(m.Data)
}

func newRedisPool(address string) (*redis.Pool, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	var host string
	switch u.Scheme {
	case "tcp":
		host = u.Host
	case "unix":
		host = u.Path
	default:
		return nil, fmt.Errorf("unknown scheme '%s' expecting tcp or unix", u.Scheme)
	}
	var opts []redis.DialOption

	if u.User != nil {
		opts = append(
			opts,
			redis.DialPassword(u.User.Username()),
		)
	}

	return &redis.Pool{
		Dial: func() (redis.Conn, error) {
			return redis.Dial(u.Scheme, host, opts...)
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			if time.Since(t) > 10*time.Second {
				//only check connection if more than 10 second of inactivity
				_, err := c.Do("PING")
				return err
			}

			return nil
		},
		MaxActive:   5,
		MaxIdle:     3,
		IdleTimeout: 1 * time.Minute,
		Wait:        true,
	}, nil
}
