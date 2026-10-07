package browser

import "testing"

func TestParseCookieCandidates_GroupsByHostAndChunks(t *testing.T) {
	const name = "__Secure-next-auth.session-token"
	out := ".chatgpt.com|" + name + ".1|200|BBB\n" +
		".chatgpt.com|" + name + ".0|150|AAA\n" +
		"chatgpt.com|" + name + "|90|OLD\n" +
		".chatgpt.com|" + name + "-other|300|NOPE\n"
	b := BrowserInfo{Name: "Firefox"}
	cands := parseCookieCandidates(b, name, out)
	if len(cands) != 2 {
		t.Fatalf("got %d candidates, want 2: %+v", len(cands), cands)
	}
	sortCookieCandidates(cands)
	if cands[0].host != ".chatgpt.com" || cands[0].lastAccess != 200 {
		t.Fatalf("most recent = %+v", cands[0])
	}
	v, err := cands[0].value(name, map[string][]byte{})
	if err != nil || v != "AAABBB" {
		t.Fatalf("value = %q, %v; want chunks joined in order", v, err)
	}
}

// Two browser profiles signed in to different accounts: the one used most
// recently is the account the user is on.
func TestSortCookieCandidates_MostRecentProfileWins(t *testing.T) {
	const name = "sess"
	def := parseCookieCandidates(BrowserInfo{Name: "Edge Default"}, name, "h|sess|100|A")
	p2 := parseCookieCandidates(BrowserInfo{Name: "Edge Profile 2"}, name, "h|sess|500|B")
	cands := append(def, p2...)
	sortCookieCandidates(cands)
	if cands[0].browser.Name != "Edge Profile 2" {
		t.Fatalf("picked %s, want Edge Profile 2", cands[0].browser.Name)
	}
}
