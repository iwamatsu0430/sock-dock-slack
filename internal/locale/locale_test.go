package locale

import "testing"

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"ja-JP": "ja", "ja_JP.UTF-8": "ja", "EN": "en", "en-US": "en", "fr-FR": "", "": "",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetect(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	apple := func(tags ...string) func() []string { return func() []string { return tags } }

	cases := []struct {
		name  string
		env   map[string]string
		apple []string
		want  string
	}{
		{"LANG wins", map[string]string{"LANG": "ja_JP.UTF-8"}, []string{"en-US"}, "ja"},
		{"LC_ALL beats LANG", map[string]string{"LC_ALL": "en_US.UTF-8", "LANG": "ja_JP.UTF-8"}, nil, "en"},
		{"C locale falls through to macOS", map[string]string{"LANG": "C.UTF-8"}, []string{"ja-JP"}, "ja"},
		{"no env uses macOS", nil, []string{"fr-FR", "ja-JP"}, "ja"},
		{"unsupported env is English", map[string]string{"LANG": "fr_FR.UTF-8"}, []string{"ja-JP"}, "en"},
		{"nothing is English", nil, nil, "en"},
	}
	for _, c := range cases {
		if got := detect(env(c.env), apple(c.apple...)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestValid(t *testing.T) {
	for v, want := range map[string]bool{"": true, "auto": true, "en": true, "ja": true, "ja-JP": false, "de": false} {
		if Valid(v) != want {
			t.Errorf("Valid(%q) != %v", v, want)
		}
	}
}
