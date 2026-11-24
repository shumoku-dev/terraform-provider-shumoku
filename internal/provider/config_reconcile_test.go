package provider

import "testing"

func TestReconcileConfigJSON(t *testing.T) {
	secrets := func() map[string]bool { return map[string]bool{"token": true, "password": true} }
	cases := []struct {
		name, prior, server, want string
	}{
		{"import takes server", "", `{"a":1}`, `{"a":1}`},
		{"identical", `{"a":1}`, `{"a":1}`, `{"a":1}`},
		{"reordered and reformatted", `{"b":2,"a":1}`, `{"a": 1, "b": 2}`, `{"b":2,"a":1}`},
		{"server added key ignored", `{"url":"u","useWebhook":true}`, `{"url":"u","useWebhook":true,"webhookSecret":"x"}`, `{"url":"u","useWebhook":true}`},
		{"configured key drifted", `{"url":"u"}`, `{"url":"v"}`, `{"url":"v"}`},
		{"non-secret configured key removed is drift", `{"url":"u","x":1}`, `{"url":"u"}`, `{"url":"u"}`},
		{"secret masked", `{"url":"u","token":"t"}`, `{"url":"u","token":"********"}`, `{"url":"u","token":"t"}`},
		{"secret omitted", `{"url":"u","token":"t"}`, `{"url":"u"}`, `{"url":"u","token":"t"}`},
		{"secret emptied", `{"url":"u","token":"t"}`, `{"url":"u","token":""}`, `{"url":"u","token":"t"}`},
		{"secret changed to a real value", `{"url":"u","token":"t"}`, `{"url":"u","token":"other"}`, `{"url":"u","token":"other"}`},
		{"nested equal", `{"o":{"a":[1,2]}}`, `{"o":{"a":[1,2]}}`, `{"o":{"a":[1,2]}}`},
		{"not an object", `[1]`, `[1]`, `[1]`},
	}
	for _, c := range cases {
		if got := reconcileConfigJSON(c.prior, c.server, secrets); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestSecretKeysFromConfigSchema(t *testing.T) {
	keys := secretKeysFromConfigSchema([]byte(`{"type":"object","properties":{"url":{"type":"string"},"token":{"type":"string","secret":true},"pw":{"type":"string","format":"password"}}}`))
	if !keys["token"] || !keys["pw"] || keys["url"] {
		t.Errorf("got %v", keys)
	}
	if len(secretKeysFromConfigSchema(nil)) != 0 {
		t.Error("nil schema should yield no keys")
	}
}

func TestCarryOverSecrets(t *testing.T) {
	secrets := func() map[string]bool { return map[string]bool{"webhookSecret": true, "token": true} }
	got := carryOverSecrets(`{"url":"v","useWebhook":true}`, `{"url":"u","useWebhook":true,"webhookSecret":"w","other":1}`, secrets)
	if got != `{"url":"v","useWebhook":true,"webhookSecret":"w"}` {
		t.Errorf("got %s", got)
	}
	if got := carryOverSecrets(`{"token":""}`, `{"token":"t"}`, secrets); got != `{"token":""}` {
		t.Errorf("an explicit empty secret must be sent: %s", got)
	}
	if got := carryOverSecrets(`{"a":1}`, `{"a":2}`, secrets); got != `{"a":1}` {
		t.Errorf("unchanged expected: %s", got)
	}
}
