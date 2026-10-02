package signer

import (
	"context"
	"encoding/json"
	"net/url"
	"sync"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip44"
)

func TestBunkerURIContainsSignerPubKeyRelayAndSecret(t *testing.T) {
	svc := mustNewService(t)
	runner, err := newRelayRunner(svc, "wss://relay.damus.io", &fakeDialer{relay: newFakeRelay()})
	if err != nil {
		t.Fatalf("newRelayRunner(): %v", err)
	}

	uri, err := runner.BunkerURI()
	if err != nil {
		t.Fatalf("BunkerURI(): %v", err)
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse bunker uri: %v", err)
	}

	if parsed.Scheme != "bunker" {
		t.Fatalf("expected bunker scheme, got %q", parsed.Scheme)
	}
	if parsed.Host != svc.TransportPublicKey().Hex() {
		t.Fatalf("expected host %q, got %q", svc.TransportPublicKey().Hex(), parsed.Host)
	}

	query := parsed.Query()
	if query.Get("relay") != "wss://relay.damus.io" {
		t.Fatalf("expected relay query to match, got %q", query.Get("relay"))
	}
	if query.Get("secret") != "bunker-secret" {
		t.Fatalf("expected one-time secret in uri, got %q", query.Get("secret"))
	}
}

func TestRelayRunnerSubscribesRelevantFilterAndShutsDownCleanly(t *testing.T) {
	svc := mustNewService(t)
	fakeRelay := newFakeRelay()
	runner, err := newRelayRunner(svc, "wss://relay.damus.io", &fakeDialer{relay: fakeRelay})
	if err != nil {
		t.Fatalf("newRelayRunner(): %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()

	fakeRelay.waitSubscribed(t)

	filter := fakeRelay.lastFilter()
	if len(filter.Kinds) != 1 || filter.Kinds[0] != NostrConnectKind {
		t.Fatalf("expected kinds [%d], got %+v", NostrConnectKind, filter.Kinds)
	}
	pValues := filter.Tags["p"]
	if len(pValues) != 1 || pValues[0] != svc.TransportPublicKey().Hex() {
		t.Fatalf("expected p-tag filter [%q], got %+v", svc.TransportPublicKey().Hex(), pValues)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() unexpected error on shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit after cancel")
	}

	if fakeRelay.closeCount() != 1 {
		t.Fatalf("expected relay close once, got %d", fakeRelay.closeCount())
	}
	if fakeRelay.sub.unsubCount() != 1 {
		t.Fatalf("expected unsub once, got %d", fakeRelay.sub.unsubCount())
	}
}

func TestRelayRunnerPublishesResponseForValidRequest(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, "3333333333333333333333333333333333333333333333333333333333333333")

	fakeRelay := newFakeRelay()
	runner, err := newRelayRunner(svc, "wss://relay.damus.io", &fakeDialer{relay: fakeRelay})
	if err != nil {
		t.Fatalf("newRelayRunner(): %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	fakeRelay.waitSubscribed(t)

	connectReq := Request{
		ID:     "connect-1",
		Method: "connect",
		Params: []string{svc.TransportPublicKey().Hex(), "bunker-secret"},
	}
	fakeRelay.sub.events <- makeEvent(t, svc, clientSK, connectReq)

	published := fakeRelay.waitPublished(t)
	if published.Kind != NostrConnectKind {
		t.Fatalf("expected response kind %d, got %d", NostrConnectKind, published.Kind)
	}

	conversationKey, err := nip44.GenerateConversationKey(svc.TransportPublicKey(), clientSK)
	if err != nil {
		t.Fatalf("GenerateConversationKey(): %v", err)
	}

	plain, err := nip44.Decrypt(published.Content, conversationKey)
	if err != nil {
		t.Fatalf("Decrypt(response): %v", err)
	}

	var resp Response
	if err := json.Unmarshal([]byte(plain), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ID != "connect-1" {
		t.Fatalf("expected response id connect-1, got %q", resp.ID)
	}
	if resp.Result != "ack" {
		t.Fatalf("expected result ack, got %+v", resp)
	}
}

type fakeDialer struct {
	relay *fakeRelay
}

func (f *fakeDialer) Dial(context.Context, string) (relayConnection, error) {
	return f.relay, nil
}

type fakeRelay struct {
	mu sync.Mutex

	sub *fakeSubscription

	subscribed  chan struct{}
	publishedCh chan nostr.Event

	filter     nostr.Filter
	published  []nostr.Event
	closeCalls int
}

func newFakeRelay() *fakeRelay {
	return &fakeRelay{
		sub:         newFakeSubscription(),
		subscribed:  make(chan struct{}),
		publishedCh: make(chan nostr.Event, 8),
	}
}

func (f *fakeRelay) Subscribe(_ context.Context, filter nostr.Filter, _ nostr.SubscriptionOptions) (subscription, error) {
	f.mu.Lock()
	f.filter = filter
	f.mu.Unlock()

	select {
	case <-f.subscribed:
	default:
		close(f.subscribed)
	}

	return f.sub, nil
}

func (f *fakeRelay) Publish(_ context.Context, event nostr.Event) error {
	f.mu.Lock()
	f.published = append(f.published, event)
	f.mu.Unlock()

	f.publishedCh <- event
	return nil
}

func (f *fakeRelay) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCalls++
	return nil
}

func (f *fakeRelay) waitSubscribed(t *testing.T) {
	t.Helper()
	select {
	case <-f.subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("subscribe was not called")
	}
}

func (f *fakeRelay) waitPublished(t *testing.T) nostr.Event {
	t.Helper()
	select {
	case evt := <-f.publishedCh:
		return evt
	case <-time.After(2 * time.Second):
		t.Fatal("expected published response event")
		return nostr.Event{}
	}
}

func (f *fakeRelay) lastFilter() nostr.Filter {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.filter
}

func (f *fakeRelay) closeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeCalls
}

type fakeSubscription struct {
	events chan nostr.Event
	done   chan struct{}

	once sync.Once

	mu         sync.Mutex
	unsubCalls int
}

func newFakeSubscription() *fakeSubscription {
	return &fakeSubscription{
		events: make(chan nostr.Event, 8),
		done:   make(chan struct{}),
	}
}

func (f *fakeSubscription) Events() <-chan nostr.Event {
	return f.events
}

func (f *fakeSubscription) Done() <-chan struct{} {
	return f.done
}

func (f *fakeSubscription) Unsub() {
	f.mu.Lock()
	f.unsubCalls++
	f.mu.Unlock()

	f.once.Do(func() {
		close(f.done)
		close(f.events)
	})
}

func (f *fakeSubscription) unsubCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unsubCalls
}
