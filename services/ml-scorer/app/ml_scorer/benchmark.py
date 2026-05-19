from __future__ import annotations

import argparse
import json
import statistics
import sys
import time
from pathlib import Path

import pandas as pd

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from ml_scorer.artifact import load_bundle
from ml_scorer.dataset import load_training_dataset


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Benchmark local ml-scorer inference.")
    parser.add_argument(
        "--model-dir",
        type=Path,
        default=Path("services/ml-scorer/model"),
        help="Directory containing manifest.json and the pickled model artifact.",
    )
    parser.add_argument(
        "--data-dir",
        type=Path,
        default=Path("services/ml-scorer/data/raw"),
        help="Directory containing train_transaction.csv and train_identity.csv for sample features.",
    )
    parser.add_argument("--sample-size", type=int, default=500, help="Number of requests to benchmark.")
    parser.add_argument("--warmup", type=int, default=25, help="Number of warmup requests to discard.")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    bundle = load_bundle(args.model_dir)
    dataset = load_training_dataset(args.data_dir, max_rows=max(args.sample_size, args.warmup) * 2)
    feature_frame = dataset.features.head(args.sample_size)
    result = benchmark_bundle(bundle, feature_frame, warmup=args.warmup)
    print(json.dumps(result, indent=2))


def benchmark_bundle(bundle, feature_frame: pd.DataFrame, *, warmup: int = 25) -> dict[str, float | int | str]:
    feature_maps = [row._asdict() for row in feature_frame.itertuples(index=False, name="FeatureRow")]
    if not feature_maps:
        raise ValueError("Feature frame must contain at least one row for benchmarking.")

    for feature_map in feature_maps[: min(warmup, len(feature_maps))]:
        bundle.score_probability(feature_map)

    elapsed_ms: list[float] = []
    for feature_map in feature_maps:
        started = time.perf_counter()
        bundle.score_probability(feature_map)
        elapsed_ms.append((time.perf_counter() - started) * 1000.0)

    return {
        "model_version": bundle.model_version,
        "sample_size": len(feature_maps),
        "p50_ms": round(_percentile(elapsed_ms, 50), 4),
        "p95_ms": round(_percentile(elapsed_ms, 95), 4),
        "p99_ms": round(_percentile(elapsed_ms, 99), 4),
        "mean_ms": round(float(statistics.fmean(elapsed_ms)), 4),
        "max_ms": round(max(elapsed_ms), 4),
    }


def _percentile(values: list[float], percentile: int) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    position = max(0, min(len(ordered) - 1, int(round((percentile / 100.0) * (len(ordered) - 1)))))
    return float(ordered[position])


if __name__ == "__main__":
    main()
