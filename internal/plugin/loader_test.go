package plugin

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name: "valid path plugin",
			json: `{"plugins":[{"name":"greet","events":["order_created"],"path":"greet.js"}]}`,
		},
		{
			name: "valid inline script plugin",
			json: `{"plugins":[{"name":"tiny","events":["ping"],"script":"function handle(e){}"}]}`,
		},
		{"empty config", `{}`, false},
		{"malformed json", `{"plugins":[`, true},
		{"unknown field fails closed", `{"plugins":[],"extra":1}`, true},
		{"missing name", `{"plugins":[{"events":["x"],"path":"p.js"}]}`, true},
		{"no events", `{"plugins":[{"name":"n","path":"p.js"}]}`, true},
		{"no source", `{"plugins":[{"name":"n","events":["x"]}]}`, true},
		{"both sources", `{"plugins":[{"name":"n","events":["x"],"path":"p.js","script":"y"}]}`, true},
		{"duplicate name", `{"plugins":[{"name":"n","events":["x"],"path":"a.js"},{"name":"n","events":["y"],"path":"b.js"}]}`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.json))
			if tc.wantErr && err == nil {
				t.Fatalf("Parse(%s) = nil error, want error", tc.json)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Parse(%s) returned error: %v", tc.json, err)
			}
		})
	}
}
