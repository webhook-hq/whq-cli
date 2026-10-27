package main

import "testing"

func TestTarget(t *testing.T) {
	cases := map[string]string{
		"":                        "http://localhost:3000",
		"4000":                    "http://localhost:4000",
		":5000":                   "http://localhost:5000",
		"127.0.0.1:8080":          "http://127.0.0.1:8080",
		"https://app.test/hooks/": "https://app.test/hooks",
	}
	for in, want := range cases {
		if got, err := target(in); err != nil || got != want {
			t.Errorf("target(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := target("70000"); err == nil {
		t.Error("target(70000) should fail")
	}
}

func TestParseArgsTakesFlagsAnywhere(t *testing.T) {
	positional, flags, err := parseArgs([]string{"3000", "--endpoint", "stripe", "--name=laptop"}, "endpoint", "name")
	if err != nil || len(positional) != 1 || positional[0] != "3000" || flags["endpoint"] != "stripe" || flags["name"] != "laptop" {
		t.Fatalf("got %v %v %v", positional, flags, err)
	}
	if _, _, err := parseArgs([]string{"--nope", "x"}, "endpoint"); err == nil {
		t.Error("unknown flags should fail")
	}
}
