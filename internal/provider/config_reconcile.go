package provider

import (
	"encoding/json"
	"reflect"
	"strings"
)

// reconcileConfigJSON decides which config JSON to keep in state for a data
// source, given the configuration Terraform last wrote (prior) and what the
// server returns now (server).
//
// The server stores the config as given, but it may add keys of its own (a
// Grafana webhookSecret it generates, for example) and it may re-serialize the
// document. A plugin's secret fields may also come back redacted. None of that is
// drift the operator caused, so:
//
//   - keys the server has but prior does not are ignored;
//   - a secret key (per the plugin's configSchema) whose server value is absent,
//     empty or masked is treated as unchanged;
//   - any other difference in a key prior sets is drift: the server's document is returned.
//
// When nothing drifted, prior is returned unchanged, so no diff appears.
// secretKeys is called lazily, only when a difference has to be judged.
func reconcileConfigJSON(prior, server string, secretKeys func() map[string]bool) string {
	if strings.TrimSpace(prior) == "" {
		return server
	}
	var p, s map[string]any
	if json.Unmarshal([]byte(prior), &p) != nil || json.Unmarshal([]byte(server), &s) != nil {
		if jsonEqualStrings(prior, server) {
			return prior
		}
		return server
	}
	var secrets map[string]bool
	for key, want := range p {
		got, ok := s[key]
		if ok && reflect.DeepEqual(want, got) {
			continue
		}
		if secrets == nil {
			secrets = map[string]bool{}
			if secretKeys != nil {
				if k := secretKeys(); k != nil {
					secrets = k
				}
			}
		}
		if secrets[key] && (!ok || looksRedacted(got)) {
			continue
		}
		return server
	}
	return prior
}

func jsonEqualStrings(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return a == b
	}
	return reflect.DeepEqual(x, y)
}

// looksRedacted reports whether a returned secret value is a placeholder rather than a value.
func looksRedacted(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	t := strings.TrimSpace(s)
	if t == "" {
		return true
	}
	switch strings.ToLower(t) {
	case "[redacted]", "<redacted>", "redacted", "__redacted__", "<sensitive>", "(sensitive)":
		return true
	}
	return strings.Trim(t, "*•x") == "" && len(t) >= 3
}

// secretKeysFromConfigSchema returns the top-level property names a plugin's
// configSchema marks secret (secret: true or format: password).
func secretKeysFromConfigSchema(raw json.RawMessage) map[string]bool {
	var schema struct {
		Properties map[string]struct {
			Secret bool   `json:"secret"`
			Format string `json:"format"`
		} `json:"properties"`
	}
	keys := map[string]bool{}
	if len(raw) == 0 || json.Unmarshal(raw, &schema) != nil {
		return keys
	}
	for name, prop := range schema.Properties {
		if prop.Secret || prop.Format == "password" {
			keys[name] = true
		}
	}
	return keys
}
