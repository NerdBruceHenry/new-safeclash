package app

import "testing"

func TestParseTunLogs(t *testing.T) {
	cases := []struct {
		name   string
		lines  []string
		ok     bool
		active bool
		err    string
	}{
		{"no result yet", []string{`time="x" level=info msg="Mixed(http+socks) proxy listening at: 127.0.0.1:7897"`}, false, false, ""},
		{"success", []string{`time="x" level=info msg="[TUN] Tun adapter listening at: Meta([198.18.0.1/30],[])"`}, true, true, ""},
		{"failure", []string{`time="x" level=error msg="Start TUN listening error: configure tun interface: Access is denied."`}, true, false, "configure tun interface: Access is denied."},
	}
	for _, c := range cases {
		st, ok := parseTunLogs(c.lines)
		if ok != c.ok || st.Active != c.active || st.Error != c.err {
			t.Errorf("%s: got ok=%v %+v", c.name, ok, st)
		}
	}
}

func TestRingSince(t *testing.T) {
	r := ring{max: 3}
	r.add("a")
	mark := r.count()
	for _, s := range []string{"b", "c", "d", "e"} {
		r.add(s)
	}
	got := r.since(mark) // 只保留最近 3 行，"b" 已被挤掉
	if len(got) != 3 || got[0] != "c" || got[2] != "e" {
		t.Fatalf("since = %v", got)
	}
	if len(r.since(r.count())) != 0 {
		t.Fatal("since(now) should be empty")
	}
}
