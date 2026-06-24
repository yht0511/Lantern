package config

import "testing"

func TestParseYAMLSubset(t *testing.T) {
	root, err := parseYAML([]byte(`
version: 1
services:
  - name: bitshare
    protocol: http
    host: 192.168.1.244
    port: 13830
    options:
      buffering: off
      websocket: true
`))
	if err != nil {
		t.Fatal(err)
	}
	services, ok := root["services"].([]any)
	if !ok || len(services) != 1 {
		t.Fatalf("services not parsed: %#v", root["services"])
	}
	service := services[0].(map[string]any)
	if service["name"] != "bitshare" {
		t.Fatalf("unexpected service name: %#v", service["name"])
	}
	options := service["options"].(map[string]any)
	if options["websocket"] != true {
		t.Fatalf("websocket not parsed as true: %#v", options["websocket"])
	}
}
