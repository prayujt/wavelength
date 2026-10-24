package discovery

import "testing"

func TestSelectRequiresAStrongUnambiguousRecording(t *testing.T) {
	source := Ref{Provider: "youtube", ID: "BaW_jenozKc"}
	for _, test := range []struct {
		candidates []Candidate
		want       bool
	}{
		{nil, false},
		{[]Candidate{{Ref: source, Score: .7, Suitable: true}}, false},
		{[]Candidate{{Ref: source, Score: .99, Suitable: false}}, false},
		{[]Candidate{{Ref: source, Score: .99, Suitable: true}, {Ref: source, Score: .98, Suitable: true}}, false},
		{[]Candidate{{Ref: source, Score: .99, Suitable: true}, {Ref: source, Score: .7, Suitable: false}}, true},
	} {
		_, ok := Select(test.candidates)
		if ok != test.want {
			t.Fatal(test, ok)
		}
	}
}
