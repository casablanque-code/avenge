"""
Quick ASCII-art inspector for the generated signal.
Usage: python3 signal_generator.py batch --samples 200 | python3 inspect.py
"""
import json
import math
import sys


def sparkline(values: list[float], width: int = 60) -> str:
    bars = "▁▂▃▄▅▆▇█"
    lo, hi = min(values), max(values)
    span = hi - lo or 1
    return "".join(bars[int((v - lo) / span * (len(bars) - 1))] for v in values[:width])


def rms(values: list[float]) -> float:
    return math.sqrt(sum(x * x for x in values) / len(values))


def main() -> None:
    records = [json.loads(line) for line in sys.stdin if line.strip()]
    values = [r["value"] for r in records]
    n = len(values)

    n_anomaly = sum(1 for r in records if r["anomaly"])
    anomaly_start = next((r["t"] for r in records if r["anomaly"]), None)

    print(f"\n{'─'*62}")
    print(f"  Samples : {n}")
    print(f"  Duration: {records[-1]['t']:.3f} s")
    print(f"  RMS     : {rms(values):.4f} g")
    print(f"  Range   : [{min(values):.4f}, {max(values):.4f}] g")
    print(f"  Anomaly : {n_anomaly} samples" + (f" (from t={anomaly_start:.3f}s)" if anomaly_start else " (none)"))
    print(f"{'─'*62}")

    # Show signal in three segments
    seg = n // 3
    labels = ["Normal start", "Normal mid  ", "Anomaly zone"]
    for i, label in enumerate(labels):
        chunk = values[i * seg : (i + 1) * seg]
        line = sparkline(chunk, width=54)
        r = rms(chunk)
        print(f"  {label}  {line}  rms={r:.3f}")

    print(f"{'─'*62}\n")


if __name__ == "__main__":
    main()
