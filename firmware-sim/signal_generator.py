"""
Signal generator — simulates MPU-6050 vibration output.

Normal mode:   fundamental (50 Hz) + harmonics + white noise
Anomaly mode:  same + high-frequency bearing fault (BPFO 312.5 Hz)
               with strong impulsive character

Output: newline-delimited JSON to stdout, or --file for batch export.
"""

import argparse
import json
import math
import random
import sys
import time


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

    b = sub.add_parser("batch")
    b.add_argument("--samples", type=int, default=4000)
    b.add_argument("--rate",    type=int, default=SAMPLE_RATE)
    b.add_argument("--anomaly", action="store_true")
    b.add_argument("--file",    type=str)

    s = sub.add_parser("stream")
    s.add_argument("--rate",          type=int,   default=SAMPLE_RATE)
    s.add_argument("--anomaly-after", type=float, default=None, metavar="SEC")
    s.add_argument("--chunk",         type=int,   default=100,
                   help="Samples per flush (default 100 = 100 ms at 1 kHz)")

    args = parser.parse_args()

    if args.mode == "batch":
        records = generate_batch(args.samples, args.rate, args.anomaly)
        output = "\n".join(json.dumps(r) for r in records)
        if args.file:
            with open(args.file, "w") as f:
                f.write(output + "\n")
            print(f"Wrote {len(records)} samples → {args.file}", file=sys.stderr)
        else:
            print(output)

    elif args.mode == "stream":
        stream_mode(args.rate, args.anomaly_after, args.chunk)


if __name__ == "__main__":
    main()
