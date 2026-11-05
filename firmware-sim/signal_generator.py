"""
Signal generator — simulates MPU-6050 vibration output.

Normal mode:   fundamental (50 Hz) + harmonics + white noise
Anomaly mode:  same + high-frequency bearing fault (BPFO 312.5 Hz)
               with strong impulsive character

Output: newline-delimited JSON to stdout, or --file for batch export.

Audio profile (--profile audio): ambient sound for testing a microphone
pipeline: a 120 Hz hum with its third harmonic and slowly drifting noise.
The "anomaly" is a 2.5 kHz tone (a whistle or squeal) that fades in over one
second. Output defaults to raw 16-bit little-endian mono PCM at 16 kHz, which
`edge-filter` reads with SENSOR_PROFILE=audio; --format wav writes a WAV file
you can play through a speaker; --format jsonl gives {t, value, anomaly} lines.

  signal_generator.py --profile audio --seed 1 batch --anomaly > sound.pcm
  signal_generator.py --profile audio batch --anomaly --format wav --file test.wav
  signal_generator.py --profile audio stream --anomaly-after 20 | edge-filter
"""

import argparse
import json
import math
import random
import struct
import sys
import time
import wave


SAMPLE_RATE = 1000

FUNDAMENTAL_HZ = 50.0
HARMONICS      = [1.0, 2.0, 3.0]
HARMONIC_AMPS  = [1.0, 0.35, 0.15]
NOISE_AMPLITUDE = 0.06

# Ball-pass frequency outer race (BPFO) for a typical 6-ball bearing
# at 50 Hz shaft speed: BPFO ≈ 6 * 0.4 * 50 ≈ 120 Hz.
# Using 312.5 Hz here because it lands cleanly in a 1kHz/512-FFT bin.
FAULT_HZ = 312.5

# Fault amplitude at full degradation — 1.8g is a realistic late-stage value
# that produces clearly distinguishable RMS increase (~40% above normal).
FAULT_AMP_FULL = 1.8

GRAVITY_OFFSET = 1.0


def generate_sample(t: float, fault_amp: float) -> float:
    signal = GRAVITY_OFFSET

    for harmonic, amp in zip(HARMONICS, HARMONIC_AMPS):
        signal += amp * math.sin(2 * math.pi * FUNDAMENTAL_HZ * harmonic * t)

    if fault_amp > 0:
        # Continuous component at fault frequency
        signal += fault_amp * 0.5 * math.sin(2 * math.pi * FAULT_HZ * t)

        # Impulsive component: sharp spikes at every fault period
        # Mimics the physical impact of a spall passing under a rolling element
        fault_period = 1.0 / FAULT_HZ
        phase = (t % fault_period) / fault_period
        if phase < 0.08:
            impulse = math.exp(-phase * 80) * math.sin(2 * math.pi * FAULT_HZ * 5 * t)
            signal += fault_amp * impulse

    signal += random.gauss(0, NOISE_AMPLITUDE)
    return signal


# ── Audio profile ────────────────────────────────────────────────────────────
AUDIO_SAMPLE_RATE = 16000
AUDIO_HUM = [(120.0, 0.030), (360.0, 0.010)]   # mains-like hum and its 3rd harmonic
AUDIO_NOISE = 0.010                             # ambient noise, standard deviation
AUDIO_DRIFT = 0.15                              # slow (0.3 Hz) drift of the noise level
AUDIO_TONE_HZ = 2500.0                          # the "anomaly": a whistle / squeal
AUDIO_TONE_AMP = 0.03                           # full amplitude, about as loud as the ambient sound
AUDIO_TONE_RAMP_S = 1.0                         # fade-in time of the tone


def generate_audio_sample(t: float, tone_amp: float) -> float:
    value = sum(amp * math.sin(2 * math.pi * hz * t) for hz, amp in AUDIO_HUM)
    value += random.gauss(0, AUDIO_NOISE * (1 + AUDIO_DRIFT * math.sin(2 * math.pi * 0.3 * t)))
    if tone_amp > 0:
        value += tone_amp * math.sin(2 * math.pi * AUDIO_TONE_HZ * t)
    return value


def tone_amplitude(age_sec: float) -> float:
    """Linear fade-in over AUDIO_TONE_RAMP_S, then constant."""
    return AUDIO_TONE_AMP * min(1.0, max(0.0, age_sec) / AUDIO_TONE_RAMP_S)


def to_pcm16(values) -> bytes:
    """Raw 16-bit little-endian mono PCM, clipped to full scale."""
    ints = [max(-32768, min(32767, int(round(v * 32768)))) for v in values]
    return struct.pack("<%dh" % len(ints), *ints)


def generate_audio_batch(n_samples: int, sample_rate: int, anomaly: bool,
                         anomaly_start_frac: float = 0.5) -> list[tuple[float, float, bool]]:
    start = int(n_samples * anomaly_start_frac)
    out = []
    for i in range(n_samples):
        t = i / sample_rate
        amp = tone_amplitude((i - start) / sample_rate) if anomaly and i >= start else 0.0
        out.append((t, generate_audio_sample(t, amp), amp > AUDIO_TONE_AMP * 0.25))
    return out


def audio_stream_mode(sample_rate: int, anomaly_after_sec: float | None,
                      chunk_size: int, fmt: str) -> None:
    """Real-time audio stream, paced in chunks like stream_mode."""
    chunk_interval = chunk_size / sample_rate
    start = time.monotonic()
    i = 0
    try:
        while True:
            chunk_start_wall = time.monotonic()
            rows = []
            for _ in range(chunk_size):
                t = i / sample_rate
                amp = 0.0
                if anomaly_after_sec is not None:
                    amp = tone_amplitude(time.monotonic() - start - anomaly_after_sec)
                rows.append((t, generate_audio_sample(t, amp), amp > AUDIO_TONE_AMP * 0.25))
                i += 1
            if fmt == "pcm":
                sys.stdout.buffer.write(to_pcm16(v for _, v, _ in rows))
                sys.stdout.buffer.flush()
            else:
                sys.stdout.write("\n".join(json.dumps({"t": round(t, 6), "value": round(v, 6), "anomaly": a})
                                           for t, v, a in rows) + "\n")
                sys.stdout.flush()
            sleep = chunk_interval - (time.monotonic() - chunk_start_wall)
            if sleep > 0:
                time.sleep(sleep)
    except KeyboardInterrupt:
        pass


def generate_batch(
    n_samples: int,
    sample_rate: int,
    anomaly: bool,
    anomaly_start_frac: float = 0.5,
) -> list[dict]:
    records = []
    for i in range(n_samples):
        t = i / sample_rate

        if anomaly and i >= int(n_samples * anomaly_start_frac):
            progress = (i - int(n_samples * anomaly_start_frac)) / (
                n_samples * (1 - anomaly_start_frac)
            )
            # Non-linear ramp: fault grows slowly then accelerates (realistic degradation curve)
            fault_amp = FAULT_AMP_FULL * (progress ** 1.5)
            is_anomaly = fault_amp > FAULT_AMP_FULL * 0.25
        else:
            fault_amp = 0.0
            is_anomaly = False

        records.append({
            "t":       round(t, 6),
            "value":   round(generate_sample(t, fault_amp), 6),
            "anomaly": is_anomaly,
        })
    return records


def stream_mode(sample_rate: int, anomaly_after_sec: float | None, chunk_size: int = 100) -> None:
    """
    Emit samples in chunks of `chunk_size` every (chunk_size / sample_rate) seconds.

    Why chunks instead of one-sample-per-sleep:
      - OS timer resolution is typically 1-15 ms. At 1 kHz, sleeping 1 ms per sample
        accumulates jitter and the stream drifts from wall clock.
      - Sleeping once per chunk (100 ms at default settings) keeps sleep calls well
        above the OS tick rate, so wakeups are reliable.
      - This mirrors what real DMA hardware does: the MCU fires an interrupt and hands
        the host a full buffer, not one float at a time.

    The Go edge-filter sees a burst of `chunk_size` JSON lines every chunk interval,
    exactly like reading from a serial port with a hardware FIFO.
    """
    chunk_interval = chunk_size / sample_rate  # seconds between flushes
    start = time.monotonic()
    i = 0

    try:
        while True:
            chunk_start_wall = time.monotonic()
            lines = []

            for _ in range(chunk_size):
                t = i / sample_rate
                elapsed = time.monotonic() - start

                fault_amp = 0.0
                if anomaly_after_sec is not None and elapsed >= anomaly_after_sec:
                    age = elapsed - anomaly_after_sec
                    fault_amp = min(FAULT_AMP_FULL, FAULT_AMP_FULL * (age / 5.0) ** 1.5)

                lines.append(json.dumps({
                    "t":       round(t, 6),
                    "value":   round(generate_sample(t, fault_amp), 6),
                    "anomaly": fault_amp > FAULT_AMP_FULL * 0.25,
                }))
                i += 1

            # Single write + flush for the whole chunk — one syscall.
            sys.stdout.write("\n".join(lines) + "\n")
            sys.stdout.flush()

            # Sleep for the remainder of the chunk interval.
            elapsed_chunk = time.monotonic() - chunk_start_wall
            sleep = chunk_interval - elapsed_chunk
            if sleep > 0:
                time.sleep(sleep)

    except KeyboardInterrupt:
        pass


def main() -> None:
    parser = argparse.ArgumentParser(description="Vibration signal generator")
    sub = parser.add_subparsers(dest="mode", required=True)

    parser.add_argument("--seed", type=int, default=None,
                        help="Seed the noise generator for reproducible runs")
    parser.add_argument("--profile", choices=["vibration", "audio"], default="vibration",
                        help="Kind of signal to simulate (default: vibration)")
    parser.add_argument("--format", choices=["jsonl", "pcm", "wav"], default=None,
                        help="Output format (default: jsonl for vibration, pcm for audio; "
                             "wav needs --file and the batch mode)")

    b = sub.add_parser("batch")
    b.add_argument("--samples", type=int, default=None,
                   help="Number of samples (default: 4000, audio: 10 s)")
    b.add_argument("--rate",    type=int, default=None,
                   help="Sample rate in Hz (default: 1000, audio: 16000)")
    b.add_argument("--anomaly", action="store_true")
    b.add_argument("--file",    type=str)

    s = sub.add_parser("stream")
    s.add_argument("--rate",          type=int,   default=None,
                   help="Sample rate in Hz (default: 1000, audio: 16000)")
    s.add_argument("--anomaly-after", type=float, default=None, metavar="SEC")
    s.add_argument("--chunk",         type=int,   default=None,
                   help="Samples per flush (default 100 = 100 ms at 1 kHz; audio: 100 ms)")

    # Accept --profile, --format and --seed after the sub-command as well. SUPPRESS keeps
    # the value given before the sub-command when they are not repeated.
    for sub_parser in (b, s):
        sub_parser.add_argument("--profile", choices=["vibration", "audio"], default=argparse.SUPPRESS,
                                help=argparse.SUPPRESS)
        sub_parser.add_argument("--format", choices=["jsonl", "pcm", "wav"], default=argparse.SUPPRESS,
                                help=argparse.SUPPRESS)
        sub_parser.add_argument("--seed", type=int, default=argparse.SUPPRESS, help=argparse.SUPPRESS)

    args = parser.parse_args()
    if args.seed is not None:
        random.seed(args.seed)

    audio = args.profile == "audio"
    fmt = args.format or ("pcm" if audio else "jsonl")
    if not audio and fmt != "jsonl":
        parser.error("--format pcm and wav are only available with --profile audio")
    rate = args.rate or (AUDIO_SAMPLE_RATE if audio else SAMPLE_RATE)

    if args.mode == "batch":
        samples = args.samples or (rate * 10 if audio else 4000)
        if audio:
            rows = generate_audio_batch(samples, rate, args.anomaly)
            if fmt == "wav":
                if not args.file:
                    parser.error("--format wav needs --file (a WAV header cannot be written to a pipe)")
                with wave.open(args.file, "wb") as w:
                    w.setnchannels(1)
                    w.setsampwidth(2)
                    w.setframerate(rate)
                    w.writeframes(to_pcm16(v for _, v, _ in rows))
                print(f"Wrote {len(rows)} samples ({len(rows) / rate:.1f} s) → {args.file}", file=sys.stderr)
            elif fmt == "pcm":
                data = to_pcm16(v for _, v, _ in rows)
                if args.file:
                    with open(args.file, "wb") as f:
                        f.write(data)
                    print(f"Wrote {len(rows)} samples → {args.file}", file=sys.stderr)
                else:
                    sys.stdout.buffer.write(data)
            else:
                output = "\n".join(json.dumps({"t": round(t, 6), "value": round(v, 6), "anomaly": a})
                                   for t, v, a in rows)
                if args.file:
                    with open(args.file, "w") as f:
                        f.write(output + "\n")
                    print(f"Wrote {len(rows)} samples → {args.file}", file=sys.stderr)
                else:
                    print(output)
        else:
            records = generate_batch(samples, rate, args.anomaly)
            output = "\n".join(json.dumps(r) for r in records)
            if args.file:
                with open(args.file, "w") as f:
                    f.write(output + "\n")
                print(f"Wrote {len(records)} samples → {args.file}", file=sys.stderr)
            else:
                print(output)

    elif args.mode == "stream":
        if fmt == "wav":
            parser.error("--format wav is only available in the batch mode")
        if audio:
            audio_stream_mode(rate, args.anomaly_after, args.chunk or rate // 10, fmt)
        else:
            stream_mode(rate, args.anomaly_after, args.chunk or 100)


if __name__ == "__main__":
    main()
