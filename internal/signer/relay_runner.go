package signer

import (
	"context"
	"errors"
	"fmt"

	"fiatjaf.com/nostr"
)

type relayDialer interface {
	Dial(ctx context.Context, relayURL string) (relayConnection, error)
}

type relayConnection interface {
	Subscribe(ctx context.Context, filter nostr.Filter, opts nostr.SubscriptionOptions) (subscription, error)
	Publish(ctx context.Context, event nostr.Event) error
	Close() error
}

type subscription interface {
	Events() <-chan nostr.Event
	Done() <-chan struct{}
	Unsub()
}

type nostrRelayDialer struct{}

func (nostrRelayDialer) Dial(ctx context.Context, relayURL string) (relayConnection, error) {
	r, err := nostr.RelayConnect(ctx, relayURL, nostr.RelayOptions{})
	if err != nil {
		return nil, err
	}
	return nostrRelayConn{relay: r}, nil
}

type nostrRelayConn struct {
	relay *nostr.Relay
}

func (c nostrRelayConn) Subscribe(ctx context.Context, filter nostr.Filter, opts nostr.SubscriptionOptions) (subscription, error) {
	sub, err := c.relay.Subscribe(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	return nostrSubscription{sub: sub}, nil
}

func (c nostrRelayConn) Publish(ctx context.Context, event nostr.Event) error {
	return c.relay.Publish(ctx, event)
}

func (c nostrRelayConn) Close() error {
	return c.relay.Close()
}

type nostrSubscription struct {
	sub *nostr.Subscription
}

func (s nostrSubscription) Events() <-chan nostr.Event {
	return s.sub.Events
}

func (s nostrSubscription) Done() <-chan struct{} {
	return s.sub.Context.Done()
}

func (s nostrSubscription) Unsub() {
	s.sub.Unsub()
}

// RelayRunner consumes signer requests from one relay and publishes encrypted responses.
type RelayRunner struct {
	service  *Service
	relayURL string
	dialer   relayDialer

	onHandled func(req Request, resp Response)
	onError   func(err error)
}

// NewRelayRunner builds a runner wired to one configurable relay URL.
func NewRelayRunner(service *Service, relayURL string) (*RelayRunner, error) {
	return newRelayRunner(service, relayURL, nostrRelayDialer{})
}

func newRelayRunner(service *Service, relayURL string, dialer relayDialer) (*RelayRunner, error) {
	if service == nil {
		return nil, errors.New("service is required")
	}
	if dialer == nil {
		return nil, errors.New("dialer is required")
	}
	if !nostr.IsValidRelayURL(relayURL) {
		return nil, fmt.Errorf("invalid relay url: %s", relayURL)
	}

	return &RelayRunner{
		service:  service,
		relayURL: nostr.NormalizeURL(relayURL),
		dialer:   dialer,
	}, nil
}

// SetHooks configures optional callbacks for handled requests and async errors.
func (r *RelayRunner) SetHooks(onHandled func(req Request, resp Response), onError func(err error)) {
	r.onHandled = onHandled
	r.onError = onError
}

// Run starts the relay subscription loop and exits cleanly when ctx is canceled.
func (r *RelayRunner) Run(ctx context.Context) error {
	diagf("runner start relay=%s", r.relayURL)
	defer diagf("runner stop relay=%s", r.relayURL)

	relay, err := r.dialer.Dial(ctx, r.relayURL)
	if err != nil {
		diagf("relay connect error relay=%s err=%v", r.relayURL, err)
		return fmt.Errorf("connect relay: %w", err)
	}
	defer relay.Close()

	filter := nostr.Filter{
		Kinds: []nostr.Kind{NostrConnectKind},
		Tags: nostr.TagMap{
			"p": []string{r.service.TransportPublicKey().Hex()},
		},
	}

	sub, err := relay.Subscribe(ctx, filter, nostr.SubscriptionOptions{})
	if err != nil {
		return fmt.Errorf("subscribe relay: %w", err)
	}
	defer sub.Unsub()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-sub.Done():
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("subscription closed")
		case evt, ok := <-sub.Events():
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("subscription events channel closed")
			}

			req, resp, responseEvent, err := r.service.HandleEvent(evt)
			if err != nil {
				if r.onError != nil {
					r.onError(err)
				}
				continue
			}

			if r.onHandled != nil {
				r.onHandled(req, resp)
			}

			if err := relay.Publish(ctx, responseEvent); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				if r.onError != nil {
					r.onError(err)
				}
				return fmt.Errorf("publish response: %w", err)
			}
		}
	}
}

// BunkerURI returns the current connect URI from the underlying service.
func (r *RelayRunner) BunkerURI() (string, error) {
	return r.service.BunkerURI()
}
