package main

import "flag"

// options holds the command-line flags. They are defined in one place so the
// help text and its test can see exactly the flags the program has.
type options struct {
	broker    *string
	sensorID  *string
	noMQTT    *bool
	explain   *bool
	listAudio *bool
	version   *bool
	report    *bool
	input     *string
	record    *string
}

func defineFlags(fs *flag.FlagSet) options {
	return options{
		broker:    fs.String("broker", defaultBroker(), "MQTT broker address"),
		sensorID:  fs.String("sensor-id", envOr("SENSOR_ID", "bearing_01"), "Sensor identifier (default: $SENSOR_ID)"),
		noMQTT:    fs.Bool("no-mqtt", false, "Disable MQTT output"),
		explain:   fs.Bool("explain", false, "Print the resolved configuration (profile, windows, bands) and exit"),
		listAudio: fs.Bool("list-audio", false, "List the audio input devices and exit"),
		version:   fs.Bool("version", false, "Print the version and build, then exit"),
		report:    fs.Bool("report", false, "Print a run report to paste into an experiment record, then exit"),
		input:     fs.String("input", "", "Read samples from this file instead of stdin"),
		record:    fs.String("record", "", "Save the raw PCM input to this new file (off by default)"),
	}
}

// usageText is what `edge-filter --help` prints. Keep it in sync with
// edge/.env.example; a test fails when a flag or a setting is missing here.
func usageText() string {
	return `edge-filter - AVENGE edge engine.
Reads a signal (vibration or sound), detects anomalies, sends compact
telemetry over MQTT.

USAGE
  edge-filter [flags]              run (input: stdin, SERIAL_INTERFACE or SENSOR_SOURCE=mic)
  edge-filter --explain            show the resolved configuration, then exit
  edge-filter --list-audio         list microphones (to fill AUDIO_DEVICE), then exit
  edge-filter --report             print a run report for an experiment record, then exit
  edge-filter --version            print the version and build, then exit
  edge-filter --help               show this text

FLAGS
  --broker ADDR       MQTT broker host:port
                      (default: CENTRAL_HOST:MQTT_PORT, else localhost:1883)
  --sensor-id ID      sensor name in MQTT topics (default: SENSOR_ID)
  --no-mqtt           print to the console only, do not connect to a broker
  --explain           print the resolved configuration and exit; use it after
                      every change to the settings below
  --list-audio        list the audio input devices and exit
  --report            print what would run (build, platform, input, resolved
                      configuration) as markdown for an experiment record
  --version           print the version, commit and platform, then exit
  --input FILE        read samples from FILE instead of stdin (same format as
                      stdin: raw PCM for the audio profile, else JSONL); the
                      portable way to replay a recording on Windows too
  --record FILE       save the raw PCM input to a NEW file for replay and
                      re-analysis; off by default, local only, never sent
                      anywhere, never overwrites a file
  --help, -h          this text

SETTINGS
  Put them in edge/.env. An exported variable overrides the file; a flag
  overrides both. Everything not listed has a built-in default.

  What is measured
    SENSOR_PROFILE      vibration (default) | audio
                        sets window, bands, thresholds, time constants
    SENSOR_ID           sensor name (default: bearing_01)
    SENSOR_SAMPLE_RATE  real sample rate of the source, Hz
                        required for SERIAL_INTERFACE; with stdin the
                        profile default applies (vibration 1000, audio 16000)
    SENSOR_INPUT        stdin format: jsonl | pcm_s16le (profile default)

  Where the samples come from (unset = stdin)
    SENSOR_SOURCE       mic = record the microphone itself (implies the audio
                        profile); needs arecord (Linux) or ffmpeg (macOS,
                        Windows); not available in Docker
    AUDIO_DEVICE        microphone for SENSOR_SOURCE=mic; empty = default
                        device, required on Windows; see --list-audio
    SERIAL_INTERFACE    usb | rs485
    USB_PORT            device path for usb   (default /dev/ttyUSB0)
    RS485_PORT          device path for rs485 (default /dev/ttyUSB0)
    RS485_BAUD          RS-485 baud rate      (default 230400)

  Where the data goes
    CENTRAL_HOST        machine running core/ (default localhost)
    MQTT_PORT           broker port           (default 1883)
    MQTT_USER           broker user, normally "edge"
    MQTT_PASSWORD       must equal MQTT_EDGE_PASSWORD in core/.env

EXAMPLES
  # synthetic vibration, no broker
  python3 firmware-sim/signal_generator.py batch --samples 8000 --anomaly | edge-filter --no-mqtt

  # microphone (Linux, macOS, Windows): --explain shows the capture command
  SENSOR_SOURCE=mic edge-filter --explain
  SENSOR_SOURCE=mic edge-filter

  # keep the raw sound of a run, then replay it later with other settings
  SENSOR_SOURCE=mic edge-filter --record room.pcm
  SENSOR_PROFILE=audio edge-filter --no-mqtt --input room.pcm

  # any other raw PCM source
  ffmpeg -i recording.wav -f s16le -ac 1 -ar 16000 - | SENSOR_PROFILE=audio edge-filter

DOCS
  docs/OPERATIONS.md  setup, profiles, sound, troubleshooting
  docs/TESTING.md     tests and how to run them
`
}
