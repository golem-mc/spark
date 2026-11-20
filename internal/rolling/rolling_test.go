package rolling

import "testing"

func TestAverage(t *testing.T) {
	a := New(3)
	if a.Mean() != 0 || a.Max() != 0 || a.Median() != 0 {
		t.Fatal("empty average is not zero")
	}
	for _, v := range []float64{5, 1, 3, 10} { // the 5 rolls out
		a.Add(v)
	}
	if a.Samples() != 3 || a.Mean() != 14.0/3 || a.Min() != 1 || a.Max() != 10 || a.Median() != 3 || a.Percentile95() != 10 {
		t.Errorf("got n=%d mean=%v min=%v max=%v med=%v p95=%v", a.Samples(), a.Mean(), a.Min(), a.Max(), a.Median(), a.Percentile95())
	}
}
