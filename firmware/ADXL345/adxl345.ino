/*
  AVENGE — ADXL345 firmware for ESP32

  Reads the Z-axis accelerometer register over I2C and streams it to the
  Raspberry Pi as a 3-byte binary packet:

      [0xAA] [hi byte] [lo byte]

  This must match edge/internal/sensor/serial_reader.go exactly:
    - start marker 0xAA
    - int16, big-endian (high byte first)
    - raw ADXL345 register value, ±2g range (1 LSB = 3.9 mg) —
      Go applies the 0.0039 g/LSB scale factor, NOT this firmware.
      Do not convert to g here; send the raw register value as-is.

  IMPORTANT — sample rate ceiling:
    The ADXL345 has a hardware-fixed maximum Output Data Rate of 3200 Hz
    (datasheet, BW_RATE register). 8000 Hz, which earlier drafts of this
    project assumed, is not achievable on this sensor regardless of MCU
    speed or I2C bus speed — the silicon itself only updates its output
    registers up to 3200 times/sec. Polling faster than the sensor's ODR
    just re-reads the same stale value, which would corrupt FFT/anomaly
    results with duplicate "samples".

    This firmware uses 1600 Hz: a supported BW_RATE step with headroom
    below the 3200 Hz ceiling, giving a useful analysis bandwidth up to
    800 Hz (Nyquist) — comfortably above common bearing fault frequencies
    (BPFO/BPFI typically a few hundred Hz at motor shaft speeds).

    edge/.env's SENSOR_SAMPLE_RATE must be changed from 8000 to 1600 to
    match. See edge/internal/sensor/serial_reader.go — its timestamp
    reconstruction divides by SENSOR_SAMPLE_RATE, so a mismatch here
    silently desyncs every Sample.T value from real elapsed time.

  Wiring (default ESP32 I2C pins):
    ADXL345 VCC -> ESP32 3.3V
    ADXL345 GND -> ESP32 GND
    ADXL345 SDA -> ESP32 GPIO21
    ADXL345 SCL -> ESP32 GPIO22

  Library required (Arduino IDE Library Manager):
    "Adafruit ADXL345" by Adafruit, plus its dependency "Adafruit Unified Sensor".
*/

#include <Wire.h>
#include <Adafruit_ADXL345_U.h>

// I2C address: most ADXL345 breakout boards default to 0x53.
// If yours has the SDO/ALT-ADDRESS pin pulled high, use 0x1D instead.
Adafruit_ADXL345_Unified accel = Adafruit_ADXL345_Unified(12345);

// Target sample rate — must be one of the ADXL345's supported BW_RATE
// steps (see setDataRate() below) and must match SENSOR_SAMPLE_RATE in
// edge/.env exactly, or SerialReader's reconstructed timestamps drift
// from real time.
const uint32_t SAMPLE_RATE_HZ = 1600;
const uint32_t SAMPLE_PERIOD_US = 1000000UL / SAMPLE_RATE_HZ; // 625 us

uint32_t nextSampleAt = 0;

void setup() {
  // Must match RS485_BAUD / the implicit USB baud edge-filter expects.
  // 230400 comfortably covers 8000 Hz * 2 bytes = 16000 B/s of payload
  // plus framing overhead (see RS-485.txt for the bandwidth math).
  Serial.begin(230400);

  Wire.begin(21, 22); // SDA, SCL — change if your board wires I2C elsewhere

  if (!accel.begin()) {
    // No sensor detected. Blink built-in LED forever so a wiring mistake
    // is visible immediately instead of silently sending garbage.
    pinMode(LED_BUILTIN, OUTPUT);
    while (true) {
      digitalWrite(LED_BUILTIN, HIGH);
      delay(150);
      digitalWrite(LED_BUILTIN, LOW);
      delay(150);
    }
  }

  // ±2g range — must match the 0.0039 g/LSB scale factor in serial_reader.go.
  // If you change this, update scaleFactor on the Go side too:
  //   ±2g  -> 3.9  mg/LSB (0.0039)
  //   ±4g  -> 7.8  mg/LSB (0.0078)
  //   ±8g  -> 15.6 mg/LSB (0.0156)
  //   ±16g -> 31.2 mg/LSB (0.0312)
  accel.setRange(ADXL345_RANGE_2_G);

  // Explicitly set the output data rate to match SAMPLE_RATE_HZ above.
  // The Adafruit library defaults to 100 Hz if this is not called —
  // leaving it at the default would silently cap every read at 100 Hz
  // regardless of how fast loop() spins.
  accel.setDataRate(ADXL345_DATARATE_1600_HZ);

  nextSampleAt = micros();
}

void loop() {
  // Busy-wait until the next sample boundary. This is deliberately a tight
  // spin rather than delayMicroseconds() — at 8kHz the budget per sample
  // is only 125us, and we want sub-microsecond timing precision rather
  // than however long delayMicroseconds() happens to actually block for.
  uint32_t now = micros();
  if ((int32_t)(now - nextSampleAt) < 0) {
    return;
  }
  nextSampleAt += SAMPLE_PERIOD_US;

  // Read raw Z-axis register value directly — NOT the library's
  // getEvent()/m/s^2-converted value. We want the same raw int16 LSB
  // units that serial_reader.go expects, so the scale factor lives in
  // exactly one place (the Go code), not duplicated here.
  int16_t zRaw = readRawZ();

  uint8_t packet[3];
  packet[0] = 0xAA;
  packet[1] = (zRaw >> 8) & 0xFF; // high byte first — big-endian
  packet[2] = zRaw & 0xFF;
  Serial.write(packet, 3);
}

// readRawZ reads the ADXL345's Z-axis output registers (0x36 low, 0x37 high)
// directly over I2C, bypassing the Adafruit library's unit conversion.
// This keeps firmware and Go in sync on a single source of truth for units.
int16_t readRawZ() {
  const uint8_t ADXL345_ADDR = 0x53;
  const uint8_t REG_DATAZ0 = 0x36;

  Wire.beginTransmission(ADXL345_ADDR);
  Wire.write(REG_DATAZ0);
  Wire.endTransmission(false); // keep connection open for the read

  Wire.requestFrom(ADXL345_ADDR, (uint8_t)2);
  if (Wire.available() < 2) {
    return 0; // I2C read failed this cycle — emit 0 rather than stalling
  }

  uint8_t lo = Wire.read();
  uint8_t hi = Wire.read();
  return (int16_t)((hi << 8) | lo);
}
