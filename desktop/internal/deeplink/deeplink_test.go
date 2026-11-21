package deeplink

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		kind string
		code string
	}{
		{"czlterm://oauth/callback?code=abc&state=xyz", KindOAuth, ""},
		{"CZLTERM://oauth/callback/?code=abc", KindOAuth, ""},
		{"czlterm://team/join?code=ABCD-EFGH-1234", KindTeamJoin, "ABCD-EFGH-1234"},
		{"czlterm://team/join/?code=%20ab12cd34ef56%20", KindTeamJoin, "ab12cd34ef56"},
		{BillingDoneURL, KindBillingDone, ""},
		{"czlterm://billing/done/?order=x&status=paid", KindBillingDone, ""}, // 参数一律忽略
	}
	for _, c := range cases {
		l, ok := Parse(c.in)
		if !ok || l.Kind != c.kind || l.Code != c.code {
			t.Errorf("Parse(%q) = %+v, %v", c.in, l, ok)
		}
	}
	for _, bad := range []string{
		"czlterm://team/join",               // 没有码
		"czlterm://team/join?code=<script>", // 非法字符
		"czlterm://team/leave?code=ABCD1234",
		"czlterm://oauth/other",
		"czlterm://billing/other",
		"https://connect.czl.net/orgs/join?code=ABCD1234",
		"--flag",
	} {
		if l, ok := Parse(bad); ok {
			t.Errorf("Parse(%q) accepted: %+v", bad, l)
		}
	}
}

func TestTeamJoinURLRoundTrip(t *testing.T) {
	u := TeamJoinURL("ABCD-1234")
	if l, ok := Parse(u); !ok || l.Kind != KindTeamJoin || l.Code != "ABCD-1234" {
		t.Fatalf("round trip %q = %+v %v", u, l, ok)
	}
}
