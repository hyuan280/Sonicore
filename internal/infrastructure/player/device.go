package player

import (
	"os"
	"os/exec"
	"strings"
)

type AudioDevice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsDefault   bool   `json:"is_default"`
}

func DetectAudioDevices() []AudioDevice {
	devices := []AudioDevice{
		{ID: "default", Name: "System Default", Description: "Default audio output", IsDefault: true},
	}

	alsaDevs := detectALSA()
	for _, d := range alsaDevs {
		devices = append(devices, d)
	}

	pulseDevs := detectPulse()
	for _, d := range pulseDevs {
		devices = append(devices, d)
	}

	return devices
}

func detectALSA() []AudioDevice {
	data, err := os.ReadFile("/proc/asound/cards")
	if err != nil {
		return nil
	}

	var devices []AudioDevice
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] < '0' || line[0] > '9' {
			continue
		}
		parts := strings.SplitN(line, "[", 2)
		if len(parts) < 2 {
			continue
		}
		nameParts := strings.SplitN(parts[1], "]", 2)
		if len(nameParts) < 1 {
			continue
		}
		cardName := strings.TrimSpace(nameParts[0])
		cardID := strings.TrimSpace(parts[0])
		devices = append(devices, AudioDevice{
			ID:          "hw:" + cardID + ",0",
			Name:        cardName,
			Description: "ALSA hardware device (hw:" + cardID + ",0)",
		})
	}

	return devices
}

func detectPulse() []AudioDevice {
	sock := os.Getenv("PULSE_SERVER")
	if sock == "" {
		return nil
	}
	cmd := exec.Command("pactl", "list", "sinks")
	cmd.Env = append(os.Environ(), "PULSE_SERVER="+sock, "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parsePactlVerbose(string(out))
}

func detectDefaultSink() string {
	sock := os.Getenv("PULSE_SERVER")
	if sock == "" {
		return ""
	}
	cmd := exec.Command("pactl", "info")
	cmd.Env = append(os.Environ(), "PULSE_SERVER="+sock, "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Default Sink:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Default Sink:"))
		}
	}
	return ""
}

func parsePactlVerbose(out string) []AudioDevice {
	var devices []AudioDevice
	defaultSink := detectDefaultSink()

	current := AudioDevice{}
	inSink := false
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "Sink #") {
			if inSink && current.ID != "" {
				devices = append(devices, current)
			}
			current = AudioDevice{}
			inSink = true
			continue
		}

		if !inSink {
			continue
		}

		if strings.HasPrefix(trimmed, "Name:") {
			current.ID = strings.TrimSpace(strings.TrimPrefix(trimmed, "Name:"))
		} else if strings.HasPrefix(trimmed, "Description:") {
			desc := strings.TrimSpace(strings.TrimPrefix(trimmed, "Description:"))
			current.Name = desc
			if len(desc) > 80 {
				desc = desc[:80]
			}
			current.Description = "PulseAudio: " + desc
			current.IsDefault = current.ID == defaultSink
		}
	}
	if inSink && current.ID != "" {
		devices = append(devices, current)
	}

	return devices
}
