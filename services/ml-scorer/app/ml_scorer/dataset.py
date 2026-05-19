from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

import numpy as np
import pandas as pd

from ml_scorer.schema import MODEL_FEATURE_NAMES

TRAIN_TRANSACTION_FILE = "train_transaction.csv"
TRAIN_IDENTITY_FILE = "train_identity.csv"
TEST_TRANSACTION_FILE = "test_transaction.csv"
TEST_IDENTITY_FILE = "test_identity.csv"

_TRANSACTION_COLUMNS = [
    "TransactionID",
    "isFraud",
    "TransactionDT",
    "TransactionAmt",
    "card1",
    "addr1",
    "dist1",
    "dist2",
    "C1",
    "C2",
    "C5",
    "C13",
    "C14",
    "D1",
    "D10",
]
_IDENTITY_COLUMNS = [
    "TransactionID",
    "id_01",
    "id_02",
    "id_11",
    "id_13",
    "id_17",
]


@dataclass(slots=True)
class TrainingDataset:
    features: pd.DataFrame
    labels: pd.Series
    source_rows: int


def load_training_dataset(data_dir: Path, *, max_rows: int | None = None, random_state: int = 42) -> TrainingDataset:
    transaction_frame = pd.read_csv(data_dir / TRAIN_TRANSACTION_FILE, usecols=_TRANSACTION_COLUMNS)
    identity_frame = pd.read_csv(data_dir / TRAIN_IDENTITY_FILE, usecols=_IDENTITY_COLUMNS)

    if max_rows is not None and len(transaction_frame) > max_rows:
        transaction_frame = (
            transaction_frame.sample(n=max_rows, random_state=random_state)
            .sort_values("TransactionDT")
            .reset_index(drop=True)
        )
        identity_frame = identity_frame[identity_frame["TransactionID"].isin(transaction_frame["TransactionID"])].copy()

    merged = transaction_frame.merge(identity_frame, on="TransactionID", how="left", sort=False)
    sorted_frame = _sort_source_frame(merged)
    feature_frame = build_feature_frame(sorted_frame)
    labels = sorted_frame["isFraud"].astype("int32").reset_index(drop=True)
    return TrainingDataset(features=feature_frame, labels=labels, source_rows=len(sorted_frame))


def build_feature_frame(raw_frame: pd.DataFrame) -> pd.DataFrame:
    frame = _sort_source_frame(raw_frame)

    features = pd.DataFrame(
        {
            "amount": frame["TransactionAmt"].astype(float),
            "hour_of_day": ((frame["TransactionDT"] // 3600) % 24).astype(float),
            "day_of_week": ((frame["TransactionDT"] // 86400) % 7).astype(float),
            "dist1": frame["dist1"].astype(float),
            "dist2": frame["dist2"].astype(float),
            "addr1": frame["addr1"].astype(float),
            "c1": frame["C1"].astype(float),
            "c2": frame["C2"].astype(float),
            "c5": frame["C5"].astype(float),
            "c13": frame["C13"].astype(float),
            "c14": frame["C14"].astype(float),
            "d1": frame["D1"].astype(float),
            "d10": frame["D10"].astype(float),
            "id_01": frame["id_01"].astype(float),
            "id_02": frame["id_02"].astype(float),
            "id_11": frame["id_11"].astype(float),
            "id_13": frame["id_13"].astype(float),
            "id_17": frame["id_17"].astype(float),
        }
    )
    velocity_features = _build_velocity_features(frame)
    features = pd.concat([features, velocity_features], axis=1)
    return features.loc[:, MODEL_FEATURE_NAMES]


def _sort_source_frame(raw_frame: pd.DataFrame) -> pd.DataFrame:
    return raw_frame.sort_values(["card1", "TransactionDT", "TransactionID"]).reset_index(drop=True)


def _build_velocity_features(frame: pd.DataFrame) -> pd.DataFrame:
    row_count = len(frame)
    count_60s = np.zeros(row_count, dtype=float)
    sum_60s = np.zeros(row_count, dtype=float)
    count_5m = np.zeros(row_count, dtype=float)
    sum_5m = np.zeros(row_count, dtype=float)
    count_24h = np.zeros(row_count, dtype=float)
    sum_24h = np.zeros(row_count, dtype=float)

    windows = (
        (60.0, count_60s, sum_60s),
        (300.0, count_5m, sum_5m),
        (86400.0, count_24h, sum_24h),
    )

    for _, card_frame in frame.groupby("card1", sort=False):
        indices = card_frame.index.to_numpy()
        timestamps = card_frame["TransactionDT"].to_numpy(dtype=float)
        amounts = card_frame["TransactionAmt"].fillna(0.0).to_numpy(dtype=float)
        for window_seconds, counts_target, sums_target in windows:
            counts, sums = _rolling_count_and_sum(timestamps, amounts, window_seconds)
            counts_target[indices] = counts
            sums_target[indices] = sums

    return pd.DataFrame(
        {
            "count_60s": count_60s,
            "sum_60s": sum_60s,
            "count_5m": count_5m,
            "sum_5m": sum_5m,
            "count_24h": count_24h,
            "sum_24h": sum_24h,
        }
    )


def _rolling_count_and_sum(
    timestamps: np.ndarray,
    amounts: np.ndarray,
    window_seconds: float,
) -> tuple[np.ndarray, np.ndarray]:
    counts = np.zeros(len(timestamps), dtype=float)
    sums = np.zeros(len(timestamps), dtype=float)
    left = 0
    running_sum = 0.0

    for right, current_time in enumerate(timestamps):
        running_sum += amounts[right]
        while left < right and current_time - timestamps[left] > window_seconds:
            running_sum -= amounts[left]
            left += 1
        counts[right] = right - left + 1
        sums[right] = running_sum

    return counts, sums
