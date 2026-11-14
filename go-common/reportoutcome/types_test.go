package reportoutcome

import "testing"

func TestHandlingOmissionPreservesPriorPrivateAuthorization(t *testing.T) {
	for _, value := range []*Handling{nil, {}, {Publication: "private"}, {Notices: "none"}} {
		got, err := NormalizeHandling(value)
		if err != nil || got != (Handling{Publication: "private", Notices: "none"}) {
			t.Fatalf("omitted authorization changed: handling=%+v err=%v", got, err)
		}
	}
}

func TestHandlingPermissionsRemainIndependentAndRejectUnknownModes(t *testing.T) {
	for _, value := range []Handling{
		{Publication: "automatic", Notices: "none"},
		{Publication: "private", Notices: "automatic"},
		{Publication: "automatic", Notices: "automatic"},
	} {
		got, err := NormalizeHandling(&value)
		if err != nil || got != value {
			t.Fatalf("explicit authorization changed: handling=%+v err=%v", got, err)
		}
	}
	for _, value := range []Handling{{Publication: "public"}, {Notices: "send_everywhere"}} {
		if _, err := NormalizeHandling(&value); err == nil {
			t.Fatal("unknown handling mode authorized an action")
		}
	}
}
