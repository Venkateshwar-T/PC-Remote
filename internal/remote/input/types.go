package input

import (
	"errors"
	"fmt"
	"math"
)

const (
	MaxInputMessageSize = 1024 // 1 KB max input message size
	MaxRelativeDelta    = 2000 // Max relative mouse delta per frame
	MaxScrollDelta      = 5000 // Max scroll delta
)

// MouseButton represents supported mouse buttons.
type MouseButton string

const (
	ButtonLeft   MouseButton = "left"
	ButtonRight  MouseButton = "right"
	ButtonMiddle MouseButton = "middle"
)

// MouseMoveMode represents absolute vs relative positioning.
type MouseMoveMode string

const (
	ModeRelative MouseMoveMode = "relative"
	ModeAbsolute MouseMoveMode = "absolute"
)

// InputMessage represents a validated client control/input payload.
type InputMessage struct {
	Type   string        `json:"type"`             // "mouse_move", "mouse_click", "mouse_down", "mouse_up", "scroll", "auth", "quick_action", "ping", "pong", "close"
	Mode   MouseMoveMode `json:"mode,omitempty"`   // "relative" or "absolute"
	DX     float64       `json:"dx,omitempty"`     // Relative movement or scroll
	DY     float64       `json:"dy,omitempty"`     // Relative movement or scroll
	X      float64       `json:"x,omitempty"`      // Absolute normalized coordinate [0.0, 1.0]
	Y      float64       `json:"y,omitempty"`      // Absolute normalized coordinate [0.0, 1.0]
	Button MouseButton   `json:"button,omitempty"` // "left", "right", "middle"
	Action string        `json:"action,omitempty"` // For quick_action: "lock", "sleep", "restart", "shutdown"
	Token     string        `json:"token,omitempty"`      // Challenge token for auth
	Sig       string        `json:"sig,omitempty"`        // Schnorr signature for auth
	CreatedAt int64         `json:"created_at,omitempty"` // Unix timestamp for auth signature verification
}

// Validate checks for invalid numeric representations, bounds violations, and unsupported actions.
func (m *InputMessage) Validate() error {
	switch m.Type {
	case "mouse_move":
		if m.Mode == ModeRelative {
			if math.IsNaN(m.DX) || math.IsInf(m.DX, 0) || math.Abs(m.DX) > MaxRelativeDelta {
				return fmt.Errorf("invalid or out-of-range dx: %v", m.DX)
			}
			if math.IsNaN(m.DY) || math.IsInf(m.DY, 0) || math.Abs(m.DY) > MaxRelativeDelta {
				return fmt.Errorf("invalid or out-of-range dy: %v", m.DY)
			}
		} else if m.Mode == ModeAbsolute {
			if math.IsNaN(m.X) || math.IsInf(m.X, 0) || m.X < 0.0 || m.X > 1.0 {
				return fmt.Errorf("absolute X coordinate must be between 0.0 and 1.0: %v", m.X)
			}
			if math.IsNaN(m.Y) || math.IsInf(m.Y, 0) || m.Y < 0.0 || m.Y > 1.0 {
				return fmt.Errorf("absolute Y coordinate must be between 0.0 and 1.0: %v", m.Y)
			}
		} else {
			return fmt.Errorf("unsupported mouse_move mode: %s", m.Mode)
		}

	case "mouse_click", "mouse_down", "mouse_up":
		if m.Button != ButtonLeft && m.Button != ButtonRight && m.Button != ButtonMiddle {
			return fmt.Errorf("unsupported mouse button: %s", m.Button)
		}

	case "scroll":
		if math.IsNaN(m.DX) || math.IsInf(m.DX, 0) || math.Abs(m.DX) > MaxScrollDelta {
			return fmt.Errorf("invalid scroll dx: %v", m.DX)
		}
		if math.IsNaN(m.DY) || math.IsInf(m.DY, 0) || math.Abs(m.DY) > MaxScrollDelta {
			return fmt.Errorf("invalid scroll dy: %v", m.DY)
		}

	case "quick_action":
		allowed := map[string]bool{"lock": true, "sleep": true, "restart": true, "shutdown": true}
		if !allowed[m.Action] {
			return fmt.Errorf("unpermitted quick action: %s", m.Action)
		}

	case "auth":
		if len(m.Token) == 0 || len(m.Sig) == 0 || len(m.Sig) > 256 {
			return errors.New("invalid auth payload")
		}

	case "ping", "pong", "close":
		// No payload parameters needed

	default:
		return fmt.Errorf("unknown input message type: %s", m.Type)
	}

	return nil
}
