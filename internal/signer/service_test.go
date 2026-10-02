package signer

import (
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip04"
	"fiatjaf.com/nostr/nip44"
)

func TestNewGeneratesOneTimeBunkerSecretInURI(t *testing.T) {
	user := mustSecretFromHex(t, strings.Repeat("a", 64))

	svc, err := New(user, []string{"wss://relay.damus.io"})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	uri, err := svc.BunkerURI()
	if err != nil {
		t.Fatalf("BunkerURI(): %v", err)
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("url.Parse(): %v", err)
	}

	secret := parsed.Query().Get("secret")
	if len(secret) != 32 {
		t.Fatalf("expected 32-char hex secret, got %q", secret)
	}
	if _, err := hex.DecodeString(secret); err != nil {
		t.Fatalf("expected valid hex secret, got error: %v", err)
	}
}

func TestTransportKeyIsSeparateFromUserKey(t *testing.T) {
	svc := mustNewService(t)

	if svc.TransportPublicKey() == svc.UserPublicKey() {
		t.Fatal("expected separate transport pubkey and user pubkey")
	}
}

func TestConnectGetPublicKeyPingPreserveID(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("3", 64))

	connectResp := call(t, svc, clientSK, Request{
		ID:     "req-1",
		Method: "connect",
		Params: []string{svc.TransportPublicKey().Hex(), "bunker-secret"},
	})

	if connectResp.ID != "req-1" {
		t.Fatalf("expected id req-1, got %q", connectResp.ID)
	}
	if connectResp.Result != "ack" || connectResp.Error != "" {
		t.Fatalf("unexpected connect response: %+v", connectResp)
	}

	pkResp := call(t, svc, clientSK, Request{ID: "req-2", Method: "get_public_key"})
	if pkResp.ID != "req-2" {
		t.Fatalf("expected id req-2, got %q", pkResp.ID)
	}
	if pkResp.Result != svc.UserPublicKey().Hex() {
		t.Fatalf("expected pubkey %q, got %q", svc.UserPublicKey().Hex(), pkResp.Result)
	}

	pingResp := call(t, svc, clientSK, Request{ID: "req-3", Method: "ping"})
	if pingResp.ID != "req-3" {
		t.Fatalf("expected id req-3, got %q", pingResp.ID)
	}
	if pingResp.Result != "pong" {
		t.Fatalf("expected pong, got %q", pingResp.Result)
	}
}

func TestSignEvent(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("4", 64))

	_ = call(t, svc, clientSK, Request{
		ID:     "c-1",
		Method: "connect",
		Params: []string{svc.TransportPublicKey().Hex(), "bunker-secret"},
	})

	unsigned := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      1,
		Tags:      nostr.Tags{},
		Content:   "hello",
	}

	j, _ := json.Marshal(unsigned)
	resp := call(t, svc, clientSK, Request{
		ID:     "sig-1",
		Method: "sign_event",
		Params: []string{string(j)},
	})

	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}

	var signed nostr.Event
	if err := json.Unmarshal([]byte(resp.Result), &signed); err != nil {
		t.Fatalf("invalid signed event json: %v", err)
	}

	if signed.PubKey != svc.UserPublicKey() {
		t.Fatalf("expected signed pubkey %q, got %q", svc.UserPublicKey().Hex(), signed.PubKey.Hex())
	}
	if !signed.CheckID() {
		t.Fatal("expected signed event with valid id")
	}
	if !signed.VerifySignature() {
		t.Fatal("expected signed event with valid signature")
	}
}

func TestSwitchRelaysAndLogout(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("5", 64))

	_ = call(t, svc, clientSK, Request{
		ID:     "c-1",
		Method: "connect",
		Params: []string{svc.TransportPublicKey().Hex(), "bunker-secret"},
	})

	resp := call(t, svc, clientSK, Request{
		ID:     "sw-1",
		Method: "switch_relays",
		Params: []string{},
	})
	if resp.Error != "" {
		t.Fatalf("unexpected switch_relays error: %s", resp.Error)
	}

	if resp.Result != `["wss://relay.damus.io"]` {
		t.Fatalf("expected configured relay list, got %q", resp.Result)
	}

	badSwitch := call(t, svc, clientSK, Request{
		ID:     "sw-2",
		Method: "switch_relays",
		Params: []string{"wss://nos.lol"},
	})
	if !strings.Contains(badSwitch.Error, "switch_relays requires no params") {
		t.Fatalf("expected no-params error, got %q", badSwitch.Error)
	}

	oldSecret := svc.BunkerSecret()
	logout := call(t, svc, clientSK, Request{ID: "lo-1", Method: "logout"})
	if logout.Error != "" || logout.Result != "ack" {
		t.Fatalf("unexpected logout response: %+v", logout)
	}

	if svc.BunkerSecret() == "" {
		t.Fatal("expected new bunker secret after logout")
	}
	if svc.BunkerSecret() == oldSecret {
		t.Fatal("expected rotated bunker secret after logout")
	}

	ping := call(t, svc, clientSK, Request{ID: "p-1", Method: "ping"})
	if !strings.Contains(ping.Error, ErrSessionNotReady.Error()) {
		t.Fatalf("expected not connected error after logout, got %q", ping.Error)
	}
}

func TestUnsupportedMethodAndConnectedClientValidation(t *testing.T) {
	svc := mustNewService(t)
	client1 := mustSecretFromHex(t, strings.Repeat("6", 64))
	client2 := mustSecretFromHex(t, strings.Repeat("7", 64))

	_ = call(t, svc, client1, Request{
		ID:     "c-1",
		Method: "connect",
		Params: []string{svc.TransportPublicKey().Hex(), "bunker-secret"},
	})

	unsupported := call(t, svc, client1, Request{ID: "u-1", Method: "unknown"})
	if unsupported.ID != "u-1" {
		t.Fatalf("expected id u-1, got %q", unsupported.ID)
	}
	if !strings.Contains(unsupported.Error, ErrUnsupportedMethod.Error()) {
		t.Fatalf("expected unsupported method error, got %q", unsupported.Error)
	}

	wrongClient := call(t, svc, client2, Request{ID: "w-1", Method: "ping"})
	if wrongClient.ID != "w-1" {
		t.Fatalf("expected id w-1, got %q", wrongClient.ID)
	}
	if !strings.Contains(wrongClient.Error, ErrConnectedClient.Error()) {
		t.Fatalf("expected connected client error, got %q", wrongClient.Error)
	}
}

func TestValidateKindSignatureAndPTag(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("8", 64))

	evt := makeEvent(t, svc, clientSK, Request{
		ID:     "k-1",
		Method: "connect",
		Params: []string{svc.TransportPublicKey().Hex(), "bunker-secret"},
	})

	badKind := evt
	badKind.Kind = 1
	if _, _, _, err := svc.HandleEvent(badKind); err == nil || !strings.Contains(err.Error(), ErrInvalidEventKind.Error()) {
		t.Fatalf("expected invalid kind error, got %v", err)
	}

	badPTag := evt
	badPTag.Tags = nostr.Tags{nostr.Tag{"p", mustSecretFromHex(t, strings.Repeat("9", 64)).Public().Hex()}}
	badPTag.SetID()
	if err := badPTag.Sign(clientSK); err != nil {
		t.Fatalf("sign badPTag: %v", err)
	}
	if _, _, _, err := svc.HandleEvent(badPTag); err == nil || !strings.Contains(err.Error(), ErrInvalidPTag.Error()) {
		t.Fatalf("expected invalid p-tag error, got %v", err)
	}

	badSignature := evt
	badSignature.Sig[0] ^= 0x01
	if _, _, _, err := svc.HandleEvent(badSignature); err == nil || !strings.Contains(err.Error(), ErrInvalidEventSig.Error()) {
		t.Fatalf("expected invalid signature error, got %v", err)
	}

	badSig := evt
	badSig.Content = badSig.Content + "x"
	if _, _, _, err := svc.HandleEvent(badSig); err == nil || !strings.Contains(err.Error(), ErrInvalidEventID.Error()) {
		t.Fatalf("expected invalid event id error, got %v", err)
	}
}

func TestConnectWrongBunkerSecret(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("b", 64))

	resp := call(t, svc, clientSK, Request{
		ID:     "wrong-secret-1",
		Method: "connect",
		Params: []string{svc.TransportPublicKey().Hex(), "wrong-secret"},
	})

	if resp.ID != "wrong-secret-1" {
		t.Fatalf("expected id wrong-secret-1, got %q", resp.ID)
	}
	if !strings.Contains(resp.Error, "invalid bunker secret") {
		t.Fatalf("expected wrong secret error, got %q", resp.Error)
	}
}

func TestRequestBeforeConnect(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("c", 64))

	resp := call(t, svc, clientSK, Request{
		ID:     "before-connect-1",
		Method: "get_public_key",
	})

	if resp.ID != "before-connect-1" {
		t.Fatalf("expected id before-connect-1, got %q", resp.ID)
	}
	if !strings.Contains(resp.Error, ErrSessionNotReady.Error()) {
		t.Fatalf("expected session not ready error, got %q", resp.Error)
	}
}

func TestResponseRequestIDForUnsupportedMethod(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("d", 64))

	_ = call(t, svc, clientSK, Request{
		ID:     "connect-1",
		Method: "connect",
		Params: []string{svc.TransportPublicKey().Hex(), "bunker-secret"},
	})

	resp := call(t, svc, clientSK, Request{
		ID:     "same-id-77",
		Method: "unsupported_method_name",
	})

	if resp.ID != "same-id-77" {
		t.Fatalf("expected same-id-77, got %q", resp.ID)
	}
	if !strings.Contains(resp.Error, ErrUnsupportedMethod.Error()) {
		t.Fatalf("expected unsupported method error, got %q", resp.Error)
	}
}

func TestLogout(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("e", 64))

	_ = call(t, svc, clientSK, Request{
		ID:     "connect-logout-1",
		Method: "connect",
		Params: []string{svc.TransportPublicKey().Hex(), "bunker-secret"},
	})

	before := svc.BunkerSecret()
	resp := call(t, svc, clientSK, Request{ID: "logout-1", Method: "logout"})

	if resp.ID != "logout-1" {
		t.Fatalf("expected logout-1 id, got %q", resp.ID)
	}
	if resp.Error != "" || resp.Result != "ack" {
		t.Fatalf("unexpected logout response: %+v", resp)
	}
	if svc.BunkerSecret() == "" {
		t.Fatal("expected new bunker secret after logout")
	}
	if svc.BunkerSecret() == before {
		t.Fatal("expected rotated bunker secret after logout")
	}
}

func TestHandleEventDecryptsLibraryNIP44PayloadFromRelayMessage(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("a", 64))

	original := makeEvent(t, svc, clientSK, Request{
		ID:     "relay-roundtrip-1",
		Method: "connect",
		Params: []string{svc.TransportPublicKey().Hex(), "bunker-secret"},
	})

	subID := "sub-1"
	env := nostr.EventEnvelope{SubscriptionID: &subID, Event: original}
	raw, err := env.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON(event envelope): %v", err)
	}

	parsedEnvelope, err := nostr.ParseMessage(string(raw))
	if err != nil {
		t.Fatalf("ParseMessage(event envelope): %v", err)
	}

	parsedEventEnvelope, ok := parsedEnvelope.(*nostr.EventEnvelope)
	if !ok {
		t.Fatalf("expected *nostr.EventEnvelope, got %T", parsedEnvelope)
	}

	parsed := parsedEventEnvelope.Event
	if parsed.Content != original.Content {
		t.Fatal("expected inbound relay parse to preserve event content bytes exactly")
	}

	_, resp, _, err := svc.HandleEvent(parsed)
	if err != nil {
		t.Fatalf("HandleEvent(parsed event) error: %v", err)
	}

	if resp.ID != "relay-roundtrip-1" {
		t.Fatalf("expected response id relay-roundtrip-1, got %q", resp.ID)
	}
	if resp.Error != "" || resp.Result != "ack" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHandleEventDecryptFailureInputByte128FromMutatedCiphertext(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("a", 64))

	evt := makeEvent(t, svc, clientSK, Request{
		ID:     "decrypt-fail-128-1",
		Method: "connect",
		Params: []string{svc.TransportPublicKey().Hex(), "bunker-secret"},
	})

	if len(evt.Content) <= 128 {
		t.Fatalf("expected ciphertext length > 128, got %d", len(evt.Content))
	}

	mutatedContent := []byte(evt.Content)
	mutatedContent[128] = '-'
	evt.Content = string(mutatedContent)
	evt.SetID()
	if err := evt.Sign(clientSK); err != nil {
		t.Fatalf("Sign(mutated event): %v", err)
	}

	_, _, _, err := svc.HandleEvent(evt)
	if err == nil {
		t.Fatal("expected decrypt error for non-standard mutated base64 content")
	}
	if !strings.Contains(err.Error(), "invalid base64") {
		t.Fatalf("expected invalid base64 error, got %v", err)
	}
	if !strings.Contains(err.Error(), "input byte 128") {
		t.Fatalf("expected input byte 128 detail, got %v", err)
	}
}

func TestHandleEventNIP44ConnectResponseRoundTrip(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("ab", 32))

	evt := makeEvent(t, svc, clientSK, Request{
		ID:     "nip44-connect-1",
		Method: "connect",
		Params: []string{svc.TransportPublicKey().Hex(), "bunker-secret"},
	})

	if isNIP04Content(evt.Content) {
		t.Fatal("expected NIP-44 content, got legacy NIP-04 marker")
	}

	_, resp, respEvent, err := svc.HandleEvent(evt)
	if err != nil {
		t.Fatalf("HandleEvent() error: %v", err)
	}
	if resp.Error != "" || resp.Result != "ack" {
		t.Fatalf("unexpected connect response: %+v", resp)
	}

	if isNIP04Content(respEvent.Content) {
		t.Fatal("expected response encrypted with NIP-44, got legacy NIP-04 marker")
	}

	conversationKey, err := nip44.GenerateConversationKey(svc.TransportPublicKey(), clientSK)
	if err != nil {
		t.Fatalf("GenerateConversationKey(response): %v", err)
	}

	plain, err := nip44.Decrypt(respEvent.Content, conversationKey)
	if err != nil {
		t.Fatalf("nip44.Decrypt(response): %v", err)
	}

	var decoded Response
	if err := json.Unmarshal([]byte(plain), &decoded); err != nil {
		t.Fatalf("Unmarshal(response): %v", err)
	}
	if decoded.ID != "nip44-connect-1" || decoded.Result != "ack" {
		t.Fatalf("unexpected decoded nip44 response: %+v", decoded)
	}
}

func TestHandleEventNIP04LegacyConnectResponseRoundTrip(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("cd", 32))

	evt := makeNIP04Event(t, svc, clientSK, Request{
		ID:     "nip04-connect-1",
		Method: "connect",
		Params: []string{svc.TransportPublicKey().Hex(), "bunker-secret"},
	})

	if !isNIP04Content(evt.Content) {
		t.Fatal(`expected legacy NIP-04 content marker "?iv="`)
	}

	_, resp, respEvent, err := svc.HandleEvent(evt)
	if err != nil {
		t.Fatalf("HandleEvent() error: %v", err)
	}
	if resp.Error != "" || resp.Result != "ack" {
		t.Fatalf("unexpected connect response: %+v", resp)
	}

	if !isNIP04Content(respEvent.Content) {
		t.Fatal("expected response encrypted with legacy NIP-04 scheme to match request scheme")
	}

	shared, err := nip04.ComputeSharedSecret(svc.TransportPublicKey(), clientSK)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(response): %v", err)
	}

	plain, err := nip04.Decrypt(respEvent.Content, shared)
	if err != nil {
		t.Fatalf("nip04.Decrypt(response): %v", err)
	}

	var decoded Response
	if err := json.Unmarshal([]byte(plain), &decoded); err != nil {
		t.Fatalf("Unmarshal(response): %v", err)
	}
	if decoded.ID != "nip04-connect-1" || decoded.Result != "ack" {
		t.Fatalf("unexpected decoded nip04 response: %+v", decoded)
	}

	pingEvt := makeNIP04Event(t, svc, clientSK, Request{ID: "nip04-ping-1", Method: "ping"})
	_, pingResp, pingRespEvt, err := svc.HandleEvent(pingEvt)
	if err != nil {
		t.Fatalf("HandleEvent(ping) error: %v", err)
	}
	if pingResp.Error != "" || pingResp.Result != "pong" {
		t.Fatalf("unexpected ping response: %+v", pingResp)
	}
	if !isNIP04Content(pingRespEvt.Content) {
		t.Fatal("expected ping response to also use the legacy NIP-04 scheme")
	}
}

func TestNIP04ConnectEmptySignerPubkeyWithValidSecretSucceeds(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("3", 64))

	resp := callNIP04(t, svc, clientSK, Request{
		ID:     "nip04-empty-signer-ok-1",
		Method: "connect",
		Params: []string{"", "bunker-secret"},
	})

	if resp.ID != "nip04-empty-signer-ok-1" {
		t.Fatalf("expected id nip04-empty-signer-ok-1, got %q", resp.ID)
	}
	if resp.Error != "" || resp.Result != "ack" {
		t.Fatalf("unexpected connect response: %+v", resp)
	}
	if !svc.connected {
		t.Fatal("expected connected session")
	}
	if svc.connectedClient != clientSK.Public() {
		t.Fatalf("expected connected client %q, got %q", clientSK.Public().Hex(), svc.connectedClient.Hex())
	}
	if !svc.bunkerSecretUsed || svc.bunkerSecret != "" {
		t.Fatal("expected bunker secret to be consumed after successful connect")
	}
}

func TestNIP04ConnectEmptySignerPubkeyWrongSecretFails(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("4", 64))

	resp := callNIP04(t, svc, clientSK, Request{
		ID:     "nip04-empty-signer-wrong-secret-1",
		Method: "connect",
		Params: []string{"", "wrong-secret"},
	})

	if resp.ID != "nip04-empty-signer-wrong-secret-1" {
		t.Fatalf("expected id nip04-empty-signer-wrong-secret-1, got %q", resp.ID)
	}
	if !strings.Contains(resp.Error, "invalid bunker secret") {
		t.Fatalf("expected wrong secret error, got %q", resp.Error)
	}
}

func TestNIP04ConnectEmptySignerPubkeyReusedSecretFails(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("5", 64))

	first := callNIP04(t, svc, clientSK, Request{
		ID:     "nip04-empty-signer-reuse-1",
		Method: "connect",
		Params: []string{"", "bunker-secret"},
	})
	if first.Error != "" || first.Result != "ack" {
		t.Fatalf("unexpected first connect response: %+v", first)
	}

	// Simulate a disconnected state while preserving consumed secret to verify
	// one-time secret enforcement instead of the session-connected guard.
	svc.connected = false
	svc.connectedClient = nostr.PubKey{}

	reuse := callNIP04(t, svc, clientSK, Request{
		ID:     "nip04-empty-signer-reuse-2",
		Method: "connect",
		Params: []string{"", "bunker-secret"},
	})

	if reuse.ID != "nip04-empty-signer-reuse-2" {
		t.Fatalf("expected id nip04-empty-signer-reuse-2, got %q", reuse.ID)
	}
	if !strings.Contains(reuse.Error, ErrBunkerSecretUsed.Error()) {
		t.Fatalf("expected reused secret error, got %q", reuse.Error)
	}
}

func TestNIP04ConnectNonEmptyInvalidPubkeyStillFails(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("6", 64))

	resp := callNIP04(t, svc, clientSK, Request{
		ID:     "nip04-invalid-pubkey-1",
		Method: "connect",
		Params: []string{"not-a-valid-pubkey", "bunker-secret"},
	})

	if resp.ID != "nip04-invalid-pubkey-1" {
		t.Fatalf("expected id nip04-invalid-pubkey-1, got %q", resp.ID)
	}
	if !strings.Contains(resp.Error, "connect requires valid signer pubkey") {
		t.Fatalf("expected strict invalid signer pubkey error, got %q", resp.Error)
	}
}

func TestNIP44ConnectWithEmptySignerPubkeyRemainsStrict(t *testing.T) {
	svc := mustNewService(t)
	clientSK := mustSecretFromHex(t, strings.Repeat("7", 64))

	resp := call(t, svc, clientSK, Request{
		ID:     "nip44-empty-signer-strict-1",
		Method: "connect",
		Params: []string{"", "bunker-secret"},
	})

	if resp.ID != "nip44-empty-signer-strict-1" {
		t.Fatalf("expected id nip44-empty-signer-strict-1, got %q", resp.ID)
	}
	if !strings.Contains(resp.Error, "connect requires valid signer pubkey") {
		t.Fatalf("expected strict invalid signer pubkey error, got %q", resp.Error)
	}
}

func call(t *testing.T, svc *Service, clientSK nostr.SecretKey, req Request) Response {
	t.Helper()

	evt := makeEvent(t, svc, clientSK, req)

	_, _, respEvent, err := svc.HandleEvent(evt)
	if err != nil {
		t.Fatalf("HandleEvent() error: %v", err)
	}

	if respEvent.Kind != NostrConnectKind {
		t.Fatalf("expected kind %d, got %d", NostrConnectKind, respEvent.Kind)
	}
	if respEvent.PubKey != svc.TransportPublicKey() {
		t.Fatalf("expected response pubkey %q, got %q", svc.TransportPublicKey().Hex(), respEvent.PubKey.Hex())
	}
	if respEvent.Tags.FindWithValue("p", clientSK.Public().Hex()) == nil {
		t.Fatal("expected response p-tag for requesting client")
	}
	if !respEvent.CheckID() || !respEvent.VerifySignature() {
		t.Fatal("expected valid signed response event")
	}

	conversationKey, err := nip44.GenerateConversationKey(svc.TransportPublicKey(), clientSK)
	if err != nil {
		t.Fatalf("GenerateConversationKey(response): %v", err)
	}

	plain, err := nip44.Decrypt(respEvent.Content, conversationKey)
	if err != nil {
		t.Fatalf("Decrypt(response): %v", err)
	}

	var resp Response
	if err := json.Unmarshal([]byte(plain), &resp); err != nil {
		t.Fatalf("Unmarshal(response): %v", err)
	}
	return resp
}

func callNIP04(t *testing.T, svc *Service, clientSK nostr.SecretKey, req Request) Response {
	t.Helper()

	evt := makeNIP04Event(t, svc, clientSK, req)

	_, _, respEvent, err := svc.HandleEvent(evt)
	if err != nil {
		t.Fatalf("HandleEvent() error: %v", err)
	}

	if respEvent.Kind != NostrConnectKind {
		t.Fatalf("expected kind %d, got %d", NostrConnectKind, respEvent.Kind)
	}
	if respEvent.PubKey != svc.TransportPublicKey() {
		t.Fatalf("expected response pubkey %q, got %q", svc.TransportPublicKey().Hex(), respEvent.PubKey.Hex())
	}
	if respEvent.Tags.FindWithValue("p", clientSK.Public().Hex()) == nil {
		t.Fatal("expected response p-tag for requesting client")
	}
	if !respEvent.CheckID() || !respEvent.VerifySignature() {
		t.Fatal("expected valid signed response event")
	}

	shared, err := nip04.ComputeSharedSecret(svc.TransportPublicKey(), clientSK)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(response): %v", err)
	}

	plain, err := nip04.Decrypt(respEvent.Content, shared)
	if err != nil {
		t.Fatalf("Decrypt(response): %v", err)
	}

	var resp Response
	if err := json.Unmarshal([]byte(plain), &resp); err != nil {
		t.Fatalf("Unmarshal(response): %v", err)
	}
	return resp
}

func makeEvent(t *testing.T, svc *Service, clientSK nostr.SecretKey, req Request) nostr.Event {
	t.Helper()

	conversationKey, err := nip44.GenerateConversationKey(svc.TransportPublicKey(), clientSK)
	if err != nil {
		t.Fatalf("GenerateConversationKey(request): %v", err)
	}

	plain, _ := json.Marshal(req)
	ciphertext, err := nip44.Encrypt(string(plain), conversationKey)
	if err != nil {
		t.Fatalf("Encrypt(request): %v", err)
	}

	evt := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      NostrConnectKind,
		Tags:      nostr.Tags{nostr.Tag{"p", svc.TransportPublicKey().Hex()}},
		Content:   ciphertext,
	}
	if err := evt.Sign(clientSK); err != nil {
		t.Fatalf("Sign(request): %v", err)
	}
	return evt
}

func makeNIP04Event(t *testing.T, svc *Service, clientSK nostr.SecretKey, req Request) nostr.Event {
	t.Helper()

	shared, err := nip04.ComputeSharedSecret(svc.TransportPublicKey(), clientSK)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(request): %v", err)
	}

	plain, _ := json.Marshal(req)
	ciphertext, err := nip04.Encrypt(string(plain), shared)
	if err != nil {
		t.Fatalf("nip04.Encrypt(request): %v", err)
	}

	evt := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      NostrConnectKind,
		Tags:      nostr.Tags{nostr.Tag{"p", svc.TransportPublicKey().Hex()}},
		Content:   ciphertext,
	}
	if err := evt.Sign(clientSK); err != nil {
		t.Fatalf("Sign(request): %v", err)
	}
	return evt
}

func mustNewService(t *testing.T) *Service {
	t.Helper()

	user := mustSecretFromHex(t, strings.Repeat("1", 64))
	transport := mustSecretFromHex(t, strings.Repeat("2", 64))

	svc, err := newWithState(user, transport, []string{"wss://relay.damus.io"}, "bunker-secret")
	if err != nil {
		t.Fatalf("newWithState(): %v", err)
	}
	return svc
}

func mustSecretFromHex(t *testing.T, in string) nostr.SecretKey {
	t.Helper()
	sk, err := nostr.SecretKeyFromHex(in)
	if err != nil {
		t.Fatalf("SecretKeyFromHex(%q): %v", in, err)
	}
	return sk
}
