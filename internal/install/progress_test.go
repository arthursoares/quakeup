package install

import (
	"testing"
)

func TestParserProgressLine(t *testing.T) {
	p := newSteamParser()
	acts := p.feed(" Update state (0x61) downloading, progress: 52.96 (1022651711 / 1931157552)\n")
	if len(acts) != 1 {
		t.Fatalf("got %d actions, want 1", len(acts))
	}
	a := acts[0]
	if a.kind != actProgress || a.verb != "downloading" || a.done != 1022651711 || a.total != 1931157552 {
		t.Fatalf("unexpected action: %+v", a)
	}
}

func TestParserProgressAcrossChunks(t *testing.T) {
	p := newSteamParser()
	if acts := p.feed(" Update state (0x81) verifying up"); len(acts) != 0 {
		t.Fatalf("acted on incomplete line: %+v", acts)
	}
	acts := p.feed("date, progress: 17.39 (335799161 / 1931157552)\r\n")
	if len(acts) != 1 || acts[0].kind != actProgress || acts[0].verb != "verifying update" {
		t.Fatalf("unexpected actions: %+v", acts)
	}
}

func TestParserSuccess(t *testing.T) {
	p := newSteamParser()
	p.feed("Success! App '2310' fully installed.\n")
	if !p.sawSuccess("2310") {
		t.Fatal("success not recorded")
	}
	if p.sawSuccess("2200") {
		t.Fatal("wrong app marked successful")
	}
}

func TestParserFailures(t *testing.T) {
	cases := []struct{ line, want string }{
		{"ERROR! Failed to install app '2310' (No subscription)\n", "Failed to install app '2310' (No subscription)"},
		{"FAILED (Invalid Password)\n", "Invalid Password"},
		{"Login Failure: Account Logon Denied\n", "Account Logon Denied"},
	}
	for _, c := range cases {
		p := newSteamParser()
		p.feed(c.line)
		if p.failure != c.want {
			t.Errorf("feed(%q): failure = %q, want %q", c.line, p.failure, c.want)
		}
	}
}

func TestParserPasswordPrompt(t *testing.T) {
	p := newSteamParser()
	acts := p.feed("Logging in user 'someone' to Steam Public...\npassword: ")
	var prompt *action
	for i := range acts {
		if acts[i].kind == actPrompt {
			prompt = &acts[i]
		}
	}
	if prompt == nil {
		t.Fatalf("no prompt action in %+v", acts)
	}
	if !prompt.masked {
		t.Error("password prompt should be masked")
	}
	// The echoed answer must not re-trigger the same prompt.
	if acts := p.feed("password: hunter2\n"); hasPrompt(acts) {
		t.Error("echoed input re-triggered the password prompt")
	}
}

func TestParserSteamGuardPrompt(t *testing.T) {
	p := newSteamParser()
	acts := p.feed("This account is protected by a Steam Guard code\nTwo-factor code: ")
	if !hasPrompt(acts) {
		t.Fatalf("no prompt action in %+v", acts)
	}
	for _, a := range acts {
		if a.kind == actPrompt && a.masked {
			t.Error("Steam Guard prompt should not be masked")
		}
	}
}

func hasPrompt(acts []action) bool {
	for _, a := range acts {
		if a.kind == actPrompt {
			return true
		}
	}
	return false
}
