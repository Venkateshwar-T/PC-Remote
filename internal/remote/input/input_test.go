package input

import (
	"math"
	"testing"
)

func TestInputValidation(t *testing.T) {
	// 1. Valid Relative Move
	validRel := InputMessage{
		Type: "mouse_move",
		Mode: ModeRelative,
		DX:   15.5,
		DY:   -20.0,
	}
	if err := validRel.Validate(); err != nil {
		t.Fatalf("expected valid relative move to pass, got: %v", err)
	}

	// 2. Reject NaN in Relative Move
	nanRel := InputMessage{
		Type: "mouse_move",
		Mode: ModeRelative,
		DX:   math.NaN(),
		DY:   10,
	}
	if err := nanRel.Validate(); err == nil {
		t.Fatalf("expected error on NaN DX")
	}

	// 3. Reject Infinity in Relative Move
	infRel := InputMessage{
		Type: "mouse_move",
		Mode: ModeRelative,
		DX:   0,
		DY:   math.Inf(1),
	}
	if err := infRel.Validate(); err == nil {
		t.Fatalf("expected error on Inf DY")
	}

	// 4. Valid Absolute Move
	validAbs := InputMessage{
		Type: "mouse_move",
		Mode: ModeAbsolute,
		X:    0.5,
		Y:    0.8,
	}
	if err := validAbs.Validate(); err != nil {
		t.Fatalf("expected valid absolute move to pass, got: %v", err)
	}

	// 5. Reject Out-of-Bounds Absolute Move
	oobAbs := InputMessage{
		Type: "mouse_move",
		Mode: ModeAbsolute,
		X:    1.2,
		Y:    0.5,
	}
	if err := oobAbs.Validate(); err == nil {
		t.Fatalf("expected error on out-of-bounds X > 1.0")
	}

	// 6. Valid Clicks
	for _, btn := range []MouseButton{ButtonLeft, ButtonRight, ButtonMiddle} {
		click := InputMessage{
			Type:   "mouse_click",
			Button: btn,
		}
		if err := click.Validate(); err != nil {
			t.Fatalf("expected button %s to pass, got: %v", btn, err)
		}
	}

	// 7. Reject Invalid Button
	badBtn := InputMessage{
		Type:   "mouse_click",
		Button: "evil_button",
	}
	if err := badBtn.Validate(); err == nil {
		t.Fatalf("expected error on invalid mouse button")
	}

	// 8. Quick Actions: Reject Arbitrary Commands
	badAction := InputMessage{
		Type:   "quick_action",
		Action: "format_c_drive",
	}
	if err := badAction.Validate(); err == nil {
		t.Fatalf("expected error on unpermitted quick action")
	}

	goodAction := InputMessage{
		Type:   "quick_action",
		Action: "lock",
	}
	if err := goodAction.Validate(); err != nil {
		t.Fatalf("expected lock quick action to pass, got: %v", err)
	}
}
