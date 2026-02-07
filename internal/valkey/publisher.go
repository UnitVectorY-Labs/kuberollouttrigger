package valkey

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// Publisher publishes messages to a Valkey PubSub channel.
type Publisher struct {
	client  *redis.Client
	channel string
	logger  *slog.Logger
}

// NewPublisher creates a new Valkey publisher.
func NewPublisher(opts *redis.Options, channel string, logger *slog.Logger) *Publisher {
	return &Publisher{
		client:  redis.NewClient(opts),
		channel: channel,
		logger:  logger,
	}
}

// Publish publishes a message to the configured channel.
func (p *Publisher) Publish(ctx context.Context, message string) error {
	result := p.client.Publish(ctx, p.channel, message)
	if result.Err() != nil {
		return fmt.Errorf("failed to publish to channel %s: %w", p.channel, result.Err())
	}
	p.logger.Debug("published message to Valkey", "channel", p.channel)
	return nil
}

// Ping checks the connection to Valkey.
func (p *Publisher) Ping(ctx context.Context) error {
	return p.client.Ping(ctx).Err()
}

// Close closes the Valkey client connection.
func (p *Publisher) Close() error {
	return p.client.Close()
}
