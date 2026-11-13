package camdet

// State is the aggregate audio/video capture state of a host: whether a camera
// is actively streaming and/or a microphone is in use. It is the single source
// of truth for the four-state model, shared by the watch daemon and by any
// external consumer (for example a GORAI sensor) that reacts to capture
// transitions. The string values are stable and form part of the public
// contract (they are the hook event names).
type State string

const (
	StateOff       State = "off"         // no camera streaming and no mic in use
	StateMicOnlyOn State = "mic-only-on" // a mic is in use, no camera streaming
	StateCamOnlyOn State = "cam-only-on" // a camera is streaming, no mic in use
	StateBothOn    State = "both-on"     // a camera is streaming and a mic is in use
)

// AggregateState classifies a detection snapshot into the aggregate capture
// state. A camera counts only when it is actively Streaming — a device that is
// merely open (e.g. a capability probe) does not count; a microphone counts
// when it is InUse.
func AggregateState(result *MicResult) State {
	camerasStreaming := false
	for _, camera := range result.Cameras {
		if camera.Streaming {
			camerasStreaming = true
			break
		}
	}
	micsInUse := false
	for _, mic := range result.Mics {
		if mic.InUse {
			micsInUse = true
			break
		}
	}
	switch {
	case camerasStreaming && micsInUse:
		return StateBothOn
	case camerasStreaming:
		return StateCamOnlyOn
	case micsInUse:
		return StateMicOnlyOn
	default:
		return StateOff
	}
}
