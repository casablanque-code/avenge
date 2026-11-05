package main

import "flag"

// options holds the command-line flags. They are defined in one place so the
// help text and its test can see exactly the flags the program has.
type options struct {
	broker   *string
	sensorID *string
	noMQTT   *bool
	explain  *bool
}

func defineFlags(fs *flag.FlagSet) options {
	return options{
		broker:   fs.String("broker", defaultBroker(), "MQTT broker address"),
		sensorID: fs.String("sensor-id", envOr("SENSOR_ID", "bearing_01"), "Sensor identifier (default: $SENSOR_ID)"),
		noMQTT:   fs.Bool("no-mqtt", false, "Disable MQTT output"),
		explain:  fs.Bool("explain", false, "Print the resolved configuration (profile, windows, bands) and exit"),
	}
}

// usageText is what `edge-filter --help` prints. Keep it in sync with
// edge/.env.example; a test fails when a flag or a setting is missing here.
func usageText() string {
	return `edge-filter - AVENGE edge engine.
Reads a signal (vibration or sound), detects anomalies, sends compact
telemetry over MQTT.

USAGE
  edge-filter [flags]              run (input: stdin, or SERIAL_INTERFACE)
  edge-filter --explain            show the resolved configuration, then exit
  edge-filter --help               show this text

FLAGS
  --broker ADDR       MQTT broker host:port
                      (default: CENTRAL_HOST:MQTT_PORT, else localhost:1883)
  --sensor-id ID      sensor name in MQTT topics (default: SENSOR_ID)
  --no-mqtt           print to the console only, do not connect to a broker
  --explain           print the resolved configuration and exit; use it after
                      every change to the settings below
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

  # microphone (Linux), see docs/OPERATIONS.md for Windows and macOS
  SENSOR_PROFILE=audio edge-filter --explain
  arecord -q -f S16_LE -r 16000 -c 1 -t raw | SENSOR_PROFILE=audio edge-filter

DOCS
  docs/OPERATIONS.md  setup, profiles, sound, troubleshooting
  docs/TESTING.md     tests and how to run them
`
}
