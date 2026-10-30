package app

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.1", "1.0.0", 1},
		{"v1.19.33", "v1.19.4", 1},
		{"0.9.0", "1.0.0", -1},
		{"1.2.3", "v1.2.3", 0},
		{"1.2.3", "0.0.0-dev", 1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compare(%s,%s)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParseUserInfo(t *testing.T) {
	var p Profile
	parseUserInfo("upload=10; download=20; total=1000; expire=1700000000", &p)
	if p.Upload != 10 || p.Download != 20 || p.Total != 1000 || p.Expire != 1700000000 {
		t.Fatalf("%+v", p)
	}
}
