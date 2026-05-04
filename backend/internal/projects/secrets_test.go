package projects

import (
	"reflect"
	"testing"
)

func TestParseSecretsExample(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"only comments", "# nothing\n# here\n", nil},
		{
			"keys with values",
			"APP_KEY=base64:abc\nDB_PASSWORD=changeme\n",
			[]string{"APP_KEY", "DB_PASSWORD"},
		},
		{
			"keys without values",
			"APP_KEY=\nDB_PASSWORD=\n",
			[]string{"APP_KEY", "DB_PASSWORD"},
		},
		{
			"mixed with comments and blanks",
			"# Required\nAPP_KEY=\n\n# Database\nDB_HOST=localhost\nDB_PASSWORD=\n",
			[]string{"APP_KEY", "DB_HOST", "DB_PASSWORD"},
		},
		{
			"export prefix tolerated",
			"export FOO=bar\nBAZ=qux\n",
			[]string{"FOO", "BAZ"},
		},
		{
			"deduplicates keeping first occurrence",
			"FOO=1\nBAR=2\nFOO=3\n",
			[]string{"FOO", "BAR"},
		},
		{
			"skips lines without =",
			"# header\nORPHAN_LINE\nFOO=ok\n",
			[]string{"FOO"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseSecretsExample([]byte(tc.input))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
