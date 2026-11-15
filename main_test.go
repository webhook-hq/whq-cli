package main

import "testing"

func TestTarget(t *testing.T) {
	cases := map[string]string{
		"":                        "http://localhost:3000",
		"4000":                    "http://localhost:4000",
		":5000":                   "http://localhost:5000",
		"127.0.0.1:8080":          "http://127.0.0.1:8080",
		"app.webhookhq.test:3000": "http://app.webhookhq.test:3000",
		"https://app.test/hooks/": "https://app.test/hooks",
		"https://app.webhookhq.test:3000/cli-destination": "https://app.webhookhq.test:3000/cli-destination",
		"http://[::1]:3000/webhooks":                      "http://[::1]:3000/webhooks",
		"https://shop.local/api/stripe%20hooks":           "https://shop.local/api/stripe%20hooks",
	}
	for in, want := range cases {
		if got, err := target(in); err != nil || got != want {
			t.Errorf("target(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"70000", "https://", "ftp://app.test", "https://app.test/hooks?x=1", "https://app.test:99999", "https://app.test/#top"} {
		if _, err := target(bad); err == nil {
			t.Errorf("target(%q) should fail", bad)
		}
	}
}

func TestParseArgsTakesFlagsAnywhere(t *testing.T) {
	positional, flags, err := parseArgs([]string{"3000", "--endpoint", "stripe", "--name=laptop"}, "endpoint", "name")
	if err != nil || len(positional) != 1 || positional[0] != "3000" || flags["endpoint"] != "stripe" || flags["name"] != "laptop" {
		t.Fatalf("got %v %v %v", positional, flags, err)
	}
	_, flags, err = parseArgs([]string{"https://app.test", "--insecure", "--endpoint", "stripe"}, "endpoint", "insecure")
	if err != nil || flags["insecure"] != "true" || flags["endpoint"] != "stripe" {
		t.Fatalf("--insecure takes no value: got %v %v", flags, err)
	}
	if _, _, err := parseArgs([]string{"--nope", "x"}, "endpoint"); err == nil {
		t.Error("unknown flags should fail")
	}
}
