package camdet

import "testing"

// streamingCamera is a camera snapshot that is actively capturing.
func streamingCamera() Camera {
	return Camera{Name: "HD Pro Webcam C920", InUse: true, Streaming: true}
}

// openCamera is a camera snapshot that is open but not capturing (e.g. a probe).
func openCamera() Camera {
	return Camera{Name: "HD Pro Webcam C920", InUse: true, Streaming: false}
}

func inUseMic() Mic {
	return Mic{Name: "Blue Yeti", InUse: true}
}

func TestAggregateState(t *testing.T) {
	tests := []struct {
		name     string
		cameras  []Camera
		mics     []Mic
		expected State
	}{
		{"nothing active", nil, nil, StateOff},
		{"camera open but not streaming", []Camera{openCamera()}, nil, StateOff},
		{"camera streaming only", []Camera{streamingCamera()}, nil, StateCamOnlyOn},
		{"mic only", nil, []Mic{inUseMic()}, StateMicOnlyOn},
		{"camera streaming and mic", []Camera{streamingCamera()}, []Mic{inUseMic()}, StateBothOn},
		{"open camera plus mic is mic-only", []Camera{openCamera()}, []Mic{inUseMic()}, StateMicOnlyOn},
		{
			"one streaming among several cameras",
			[]Camera{openCamera(), streamingCamera()},
			nil,
			StateCamOnlyOn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &MicResult{Result: &Result{Cameras: tt.cameras}, Mics: tt.mics}
			if got := AggregateState(result); got != tt.expected {
				t.Fatalf("AggregateState(cameras=%d, mics=%d) = %q, want %q",
					len(tt.cameras), len(tt.mics), got, tt.expected)
			}
		})
	}
}
