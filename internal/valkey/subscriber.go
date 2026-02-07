package valkey

import (
	"context"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// MessageHandler is called for each message received from the subscription.
type MessageHandler func(ctx context.Context, message string)

// Subscriber subscribes to a Valkey PubSub channel and processes messages.
type Subscriber struct {
	client  *redis.Client
	channel string
	logger  *slog.Logger
}

// NewSubscriber creates a new Valkey subscriber.
func NewSubscriber(opts *redis.Options, channel string, logger *slog.Logger) *Subscriber {
	return &Subscriber{
		client:  redis.NewClient(opts),
		channel: channel,
		logger:  logger,
	}
}

// Subscribe starts listening on the configured channel and calls handler for each message.
// This blocks until the context is cancelled.
func (s *Subscriber) Subscribe(ctx context.Context, handler MessageHandler) error {
	pubsub := s.client.Subscribe(ctx, s.channel)
	defer pubsub.Close()

	// Wait for subscription confirmation
	_, err := pubsub.Receive(ctx)
	if err != nil {
		return err
	}

	s.logger.Info("subscribed to Valkey channel", "channel", s.channel)

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("shutting down Valkey subscriber")
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				s.logger.Warn("Valkey subscription channel closed, reconnecting")
				return nil
			}
			handler(ctx, msg.Payload)
		}
	}
}

// Ping checks the connection to Valkey.
func (s *Subscriber) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// Close closes the Valkey client connection.
func (s *Subscriber) Close() error {
	return s.client.Close()
}
