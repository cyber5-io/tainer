package doctor

import "testing"

func TestReportHealthy(t *testing.T) {
	cases := []struct {
		name    string
		results []Result
		want    bool
	}{
		{"empty", nil, true},
		{"all ok", []Result{{Status: StatusOK}, {Status: StatusOK}}, true},
		{"warns are healthy", []Result{{Status: StatusOK}, {Status: StatusWarn}}, true},
		{"fail is unhealthy", []Result{{Status: StatusOK}, {Status: StatusFail}}, false},
		{"skip is unhealthy", []Result{{Status: StatusOK}, {Status: StatusSkip}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Report{Results: c.results}
			if got := r.Healthy(); got != c.want {
				t.Errorf("Healthy() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestReportCounts(t *testing.T) {
	r := Report{Results: []Result{
		{Status: StatusOK}, {Status: StatusOK},
		{Status: StatusWarn},
		{Status: StatusFail},
		{Status: StatusSkip}, {Status: StatusSkip}, {Status: StatusSkip},
	}}
	ok, warn, fail, skip := r.Counts()
	if ok != 2 || warn != 1 || fail != 1 || skip != 3 {
		t.Errorf("Counts() = %d/%d/%d/%d, want 2/1/1/3", ok, warn, fail, skip)
	}
}
