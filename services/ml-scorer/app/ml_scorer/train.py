from __future__ import annotations

import argparse
import sys
from pathlib import Path

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from ml_scorer.artifact import save_bundle, train_bundle
from ml_scorer.dataset import load_training_dataset
from ml_scorer.log_config import configure_logging


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Train the ml-scorer XGBoost model.")
    parser.add_argument(
        "--data-dir",
        type=Path,
        default=Path("services/ml-scorer/data/raw"),
        help="Directory containing the Kaggle IEEE fraud CSV files.",
    )
    parser.add_argument(
        "--model-dir",
        type=Path,
        default=Path("services/ml-scorer/model"),
        help="Directory where the trained model artifact and manifest will be written.",
    )
    parser.add_argument(
        "--max-rows",
        type=int,
        default=None,
        help="Optional row cap for faster local iteration.",
    )
    parser.add_argument(
        "--random-state",
        type=int,
        default=42,
        help="Random seed used for sampling and train/validation split.",
    )
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    logger = configure_logging()

    dataset = load_training_dataset(
        args.data_dir,
        max_rows=args.max_rows,
        random_state=args.random_state,
    )
    bundle = train_bundle(dataset, random_state=args.random_state)
    artifact_path = save_bundle(bundle, args.model_dir, source=str(args.data_dir.resolve()))

    logger.info(
        "trained ml-scorer model",
        artifact_path=str(artifact_path.resolve()),
        model_version=bundle.model_version,
        validation_auc=round(bundle.validation_auc, 6),
        trained_rows=bundle.trained_rows,
    )


if __name__ == "__main__":
    main()
