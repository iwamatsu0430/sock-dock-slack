package main

import "testing"

func TestClaudeSignInScript(t *testing.T) {
	got, err := claudeSignInScript([]string{"/Users/me/My Tools/claude", "--debug"}, "/usr/bin:/bin")
	if err != nil {
		t.Fatal(err)
	}
	want := "#!/bin/sh\nexport PATH=/usr/bin:/bin\nexec '/Users/me/My Tools/claude' --debug auth login\n"
	if got != want {
		t.Errorf("script =\n%s\nwant\n%s", got, want)
	}
	if _, err := claudeSignInScript(nil, ""); err == nil {
		t.Error("an empty command should fail")
	}
}
