package editor

import (
	"strings"
	"testing"
)

func TestRecheckRequiresDocumentAndLocatorWithoutFrameEvidence(t *testing.T) {
	valid := Inspection{PageStateID: "old-page", DocumentID: strings.Repeat("a", 64), PreviousFingerprint: strings.Repeat("b", 64), Locator: &Locator{Strategy: "css", Expression: ".title"}}
	if err := valid.Validate("locator-recheck"); err != nil {
		t.Fatal(err)
	}
	variants := []Inspection{valid, valid, valid, valid, valid}
	variants[0].Position = &Position{X: 1, Y: 1}
	variants[1].ElementID = "node"
	variants[2].FrameSequence = 1
	variants[3].DocumentID = strings.Repeat("z", 64)
	variants[4].PreviousFingerprint = ""
	for _, input := range variants {
		if input.Validate("locator-recheck") == nil {
			t.Fatalf("accepted invalid recheck: %+v", input)
		}
	}
}
