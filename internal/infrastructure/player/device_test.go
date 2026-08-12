package player

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePactlVerbose(t *testing.T) {
	t.Setenv("PULSE_SERVER", "") // no real pactl: default sink detection is a no-op
	out := `Sink #0
	State: RUNNING
	Name: alsa_output.pci-0000_00_1f.3.analog-stereo
	Description: Built-in Audio Analog Stereo
	Driver: module-alsa-card.c
Sink #1
	State: IDLE
	Name: alsa_output.usb-Device_00d9.analog-stereo
	Description: USB Audio Device
`

	devices := parsePactlVerbose(out)
	require.Len(t, devices, 2)

	assert.Equal(t, "alsa_output.pci-0000_00_1f.3.analog-stereo", devices[0].ID)
	assert.Equal(t, "Built-in Audio Analog Stereo", devices[0].Name)
	assert.False(t, devices[0].IsDefault, "no default sink detected, nothing flagged")

	assert.Equal(t, "alsa_output.usb-Device_00d9.analog-stereo", devices[1].ID)
	assert.Equal(t, "USB Audio Device", devices[1].Name)
	assert.False(t, devices[1].IsDefault)
}

func TestParsePactlVerboseTruncatesDescription(t *testing.T) {
	t.Setenv("PULSE_SERVER", "")
	long := "Sink #0\n\tName: sink-0\n\tDescription: " + repeat("x", 120) + "\n"

	devices := parsePactlVerbose(long)
	require.Len(t, devices, 1)
	assert.Equal(t, 80, len(devices[0].Description)-len("PulseAudio: "))
	assert.True(t, len(devices[0].Description) <= len("PulseAudio: ")+80)
}

func TestParsePactlVerboseMalformed(t *testing.T) {
	t.Setenv("PULSE_SERVER", "")
	assert.Empty(t, parsePactlVerbose(""))
	assert.Empty(t, parsePactlVerbose("State: RUNNING\nName: x\n"), "no Sink marker")
	assert.Empty(t, parsePactlVerbose("Sink #0\n\tState: RUNNING\n"), "no Name → no device")
}

func TestParsePactlVerboseDefaultSink(t *testing.T) {
	out := `Sink #0
	Name: sink-a
	Description: First
Sink #1
	Name: sink-b
	Description: Second
`

	devices := parsePactlVerbose(out)
	require.Len(t, devices, 2)
	for _, d := range devices {
		assert.False(t, d.IsDefault, "no default sink detected, nothing flagged")
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
