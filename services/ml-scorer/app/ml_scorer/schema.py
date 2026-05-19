from __future__ import annotations

from collections.abc import Mapping
from typing import Final

import numpy as np
import pandas as pd

MODEL_FEATURE_NAMES: Final[tuple[str, ...]] = (
    "amount",
    "hour_of_day",
    "day_of_week",
    "dist1",
    "dist2",
    "addr1",
    "c1",
    "c2",
    "c5",
    "c13",
    "c14",
    "d1",
    "d10",
    "id_01",
    "id_02",
    "id_11",
    "id_13",
    "id_17",
    "count_60s",
    "sum_60s",
    "count_5m",
    "sum_5m",
    "count_24h",
    "sum_24h",
)


def frame_from_feature_map(features: Mapping[str, float]) -> pd.DataFrame:
    row = {name: float(features.get(name, np.nan)) for name in MODEL_FEATURE_NAMES}
    return pd.DataFrame([row], columns=MODEL_FEATURE_NAMES)


def vector_from_feature_map(features: Mapping[str, float]) -> np.ndarray:
    return np.asarray(
        [[float(features.get(name, np.nan)) for name in MODEL_FEATURE_NAMES]],
        dtype=float,
    )
