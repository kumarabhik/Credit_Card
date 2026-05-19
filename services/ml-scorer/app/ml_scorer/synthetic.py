from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from pathlib import Path

import numpy as np
import pandas as pd

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from ml_scorer.dataset import TEST_IDENTITY_FILE, TEST_TRANSACTION_FILE, TRAIN_IDENTITY_FILE, TRAIN_TRANSACTION_FILE
from ml_scorer.log_config import configure_logging


@dataclass(slots=True)
class SyntheticDatasetConfig:
    train_rows: int
    test_rows: int
    seed: int = 42
    fraud_rate: float = 0.035
    chunk_size: int = 100_000
    card_pool_size: int = 75_000


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate a synthetic IEEE-like fraud dataset.")
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=Path("services/ml-scorer/data/processed/synthetic-ieee"),
        help="Directory where synthetic CSV files will be written.",
    )
    parser.add_argument("--train-rows", type=int, default=100_000, help="Number of train rows to generate.")
    parser.add_argument("--test-rows", type=int, default=20_000, help="Number of test rows to generate.")
    parser.add_argument("--chunk-size", type=int, default=100_000, help="Rows per generated write chunk.")
    parser.add_argument("--seed", type=int, default=42, help="Random seed.")
    parser.add_argument("--fraud-rate", type=float, default=0.035, help="Approximate base fraud rate.")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    logger = configure_logging()
    config = SyntheticDatasetConfig(
        train_rows=args.train_rows,
        test_rows=args.test_rows,
        seed=args.seed,
        fraud_rate=args.fraud_rate,
        chunk_size=args.chunk_size,
    )
    paths = write_synthetic_dataset(args.output_dir, config)
    logger.info(
        "generated synthetic dataset",
        output_dir=str(args.output_dir.resolve()),
        train_rows=config.train_rows,
        test_rows=config.test_rows,
        train_transaction=str(paths["train_transaction"]),
        test_transaction=str(paths["test_transaction"]),
    )


def write_synthetic_dataset(output_dir: Path, config: SyntheticDatasetConfig) -> dict[str, str]:
    output_dir.mkdir(parents=True, exist_ok=True)
    _prepare_output(output_dir)

    next_transaction_id = 1
    next_transaction_dt = 0
    for split_name, row_count, include_labels in (
        ("train", config.train_rows, True),
        ("test", config.test_rows, False),
    ):
        remaining = row_count
        while remaining > 0:
            chunk_rows = min(remaining, config.chunk_size)
            transaction_frame, identity_frame, next_transaction_dt = generate_synthetic_chunk(
                row_count=chunk_rows,
                transaction_id_start=next_transaction_id,
                transaction_dt_start=next_transaction_dt,
                seed=config.seed + next_transaction_id,
                fraud_rate=config.fraud_rate,
                include_labels=include_labels,
                card_pool_size=config.card_pool_size,
            )
            next_transaction_id += chunk_rows
            remaining -= chunk_rows

            if split_name == "train":
                _append_frame(output_dir / TRAIN_TRANSACTION_FILE, transaction_frame)
                _append_frame(output_dir / TRAIN_IDENTITY_FILE, identity_frame)
            else:
                test_transaction = transaction_frame.drop(columns=["isFraud"])
                _append_frame(output_dir / TEST_TRANSACTION_FILE, test_transaction)
                _append_frame(output_dir / TEST_IDENTITY_FILE, identity_frame)
                _append_frame(
                    output_dir / "sample_submission.csv",
                    pd.DataFrame(
                        {
                            "TransactionID": test_transaction["TransactionID"],
                            "isFraud": np.zeros(len(test_transaction), dtype=float),
                        }
                    ),
                )

    return {
        "train_transaction": str((output_dir / TRAIN_TRANSACTION_FILE).resolve()),
        "train_identity": str((output_dir / TRAIN_IDENTITY_FILE).resolve()),
        "test_transaction": str((output_dir / TEST_TRANSACTION_FILE).resolve()),
        "test_identity": str((output_dir / TEST_IDENTITY_FILE).resolve()),
        "sample_submission": str((output_dir / "sample_submission.csv").resolve()),
    }


def generate_synthetic_chunk(
    *,
    row_count: int,
    transaction_id_start: int,
    transaction_dt_start: int,
    seed: int,
    fraud_rate: float,
    include_labels: bool,
    card_pool_size: int,
) -> tuple[pd.DataFrame, pd.DataFrame, int]:
    rng = np.random.default_rng(seed)
    transaction_ids = np.arange(transaction_id_start, transaction_id_start + row_count, dtype=np.int64)
    dt_steps = rng.integers(1, 120, size=row_count, dtype=np.int64)
    transaction_dt = np.cumsum(dt_steps) + transaction_dt_start

    card1 = rng.integers(1_000, 1_000 + card_pool_size, size=row_count)
    amount = np.round(rng.lognormal(mean=3.5, sigma=0.9, size=row_count) * 8.0, 2)
    addr1 = rng.integers(100, 600, size=row_count)
    dist1 = np.abs(rng.normal(loc=12.0, scale=18.0, size=row_count))
    dist2 = np.abs(dist1 + rng.normal(loc=4.5, scale=9.0, size=row_count))

    c1 = rng.poisson(lam=np.clip(1.0 + amount / 350.0, 1.0, 12.0)).astype(float)
    c2 = rng.poisson(lam=np.clip(0.8 + amount / 500.0, 1.0, 10.0)).astype(float)
    c5 = rng.poisson(lam=np.clip(0.4 + dist1 / 25.0, 1.0, 8.0)).astype(float)
    c13 = rng.poisson(lam=np.clip(1.0 + amount / 200.0, 1.0, 20.0)).astype(float)
    c14 = rng.poisson(lam=np.clip(0.3 + dist2 / 15.0, 1.0, 20.0)).astype(float)
    d1 = np.abs(rng.normal(loc=7.0, scale=6.0, size=row_count))
    d10 = np.abs(rng.normal(loc=4.0, scale=5.5, size=row_count))

    id_01 = rng.normal(loc=0.0, scale=1.0, size=row_count)
    id_02 = rng.normal(loc=95.0, scale=22.0, size=row_count)
    id_11 = rng.normal(loc=100.0, scale=6.5, size=row_count)
    id_13 = rng.normal(loc=32.0, scale=8.0, size=row_count)
    id_17 = rng.normal(loc=150.0, scale=20.0, size=row_count)

    risk_probability = _risk_probability(
        transaction_dt=transaction_dt,
        amount=amount,
        dist1=dist1,
        dist2=dist2,
        c14=c14,
        d10=d10,
        id_01=id_01,
        id_11=id_11,
        base_rate=fraud_rate,
    )
    is_fraud = rng.binomial(1, risk_probability, size=row_count).astype(np.int32)
    fraud_mask = is_fraud.astype(bool)

    amount = amount.copy()
    dist1 = dist1.copy()
    dist2 = dist2.copy()
    c13 = c13.copy()
    c14 = c14.copy()
    d10 = d10.copy()
    id_01 = id_01.copy()
    id_11 = id_11.copy()

    amount[fraud_mask] *= rng.uniform(1.8, 3.8, size=int(fraud_mask.sum()))
    dist1[fraud_mask] += rng.uniform(35.0, 95.0, size=int(fraud_mask.sum()))
    dist2[fraud_mask] += rng.uniform(55.0, 130.0, size=int(fraud_mask.sum()))
    c13[fraud_mask] += rng.integers(4, 12, size=int(fraud_mask.sum()))
    c14[fraud_mask] += rng.integers(6, 18, size=int(fraud_mask.sum()))
    d10[fraud_mask] += rng.uniform(8.0, 20.0, size=int(fraud_mask.sum()))
    id_01[fraud_mask] *= rng.uniform(2.0, 4.5, size=int(fraud_mask.sum()))
    id_11[fraud_mask] += rng.choice([-1.0, 1.0], size=int(fraud_mask.sum())) * rng.uniform(
        10.0, 25.0, size=int(fraud_mask.sum())
    )

    transaction_frame = pd.DataFrame(
        {
            "TransactionID": transaction_ids,
            "isFraud": is_fraud,
            "TransactionDT": transaction_dt,
            "TransactionAmt": amount,
            "card1": card1,
            "addr1": addr1,
            "dist1": np.round(dist1, 3),
            "dist2": np.round(dist2, 3),
            "C1": c1,
            "C2": c2,
            "C5": c5,
            "C13": c13,
            "C14": c14,
            "D1": np.round(d1, 3),
            "D10": np.round(d10, 3),
        }
    )
    if not include_labels:
        transaction_frame["isFraud"] = 0

    identity_frame = pd.DataFrame(
        {
            "TransactionID": transaction_ids,
            "id_01": np.round(id_01, 4),
            "id_02": np.round(id_02, 4),
            "id_11": np.round(id_11, 4),
            "id_13": np.round(id_13, 4),
            "id_17": np.round(id_17, 4),
        }
    )
    return transaction_frame, identity_frame, int(transaction_dt[-1])


def _risk_probability(
    *,
    transaction_dt: np.ndarray,
    amount: np.ndarray,
    dist1: np.ndarray,
    dist2: np.ndarray,
    c14: np.ndarray,
    d10: np.ndarray,
    id_01: np.ndarray,
    id_11: np.ndarray,
    base_rate: float,
) -> np.ndarray:
    hours = (transaction_dt // 3600) % 24
    clamped_rate = min(max(base_rate, 1e-6), 1.0 - 1e-6)
    intercept = np.log(clamped_rate / (1.0 - clamped_rate))
    risk_logit = np.full(transaction_dt.shape[0], intercept, dtype=float)
    risk_logit += np.where(amount > 1_500.0, 1.35, 0.0)
    risk_logit += np.where(amount > 4_000.0, 1.0, 0.0)
    risk_logit += np.where(hours < 5, 0.75, 0.0)
    risk_logit += np.where(dist1 > 50.0, 0.9, 0.0)
    risk_logit += np.where(dist2 > 75.0, 0.65, 0.0)
    risk_logit += np.where(c14 > 8.0, 0.7, 0.0)
    risk_logit += np.where(d10 > 12.0, 0.55, 0.0)
    risk_logit += np.where(np.abs(id_01) > 2.1, 0.6, 0.0)
    risk_logit += np.where(np.abs(id_11 - 100.0) > 10.0, 0.45, 0.0)
    return 1.0 / (1.0 + np.exp(-risk_logit))


def _append_frame(path: Path, frame: pd.DataFrame) -> None:
    frame.to_csv(path, mode="a", index=False, header=not path.exists())


def _prepare_output(output_dir: Path) -> None:
    for file_name in (
        TRAIN_TRANSACTION_FILE,
        TRAIN_IDENTITY_FILE,
        TEST_TRANSACTION_FILE,
        TEST_IDENTITY_FILE,
        "sample_submission.csv",
    ):
        path = output_dir / file_name
        if path.exists():
            path.unlink()


if __name__ == "__main__":
    main()
