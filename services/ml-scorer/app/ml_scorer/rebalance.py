from __future__ import annotations

from dataclasses import dataclass

import numpy as np
from sklearn.neighbors import NearestNeighbors


@dataclass(slots=True)
class ResampleSummary:
    applied: bool
    original_positive_rate: float
    balanced_positive_rate: float
    original_rows: int
    balanced_rows: int


def apply_smote(
    features: np.ndarray,
    labels: np.ndarray,
    *,
    random_state: int = 42,
    target_ratio: float = 1.0,
    k_neighbors: int = 5,
) -> tuple[np.ndarray, np.ndarray, ResampleSummary]:
    label_array = np.asarray(labels, dtype=int)
    original_rows = int(label_array.shape[0])
    positive_rate = float(label_array.mean()) if original_rows else 0.0

    minority_label, majority_count, minority_indices = _minority_class(label_array)
    minority_count = int(minority_indices.shape[0])
    target_minority = int(round(majority_count * target_ratio))
    rows_needed = target_minority - minority_count

    if rows_needed <= 0 or minority_count == 0:
        summary = ResampleSummary(
            applied=False,
            original_positive_rate=positive_rate,
            balanced_positive_rate=positive_rate,
            original_rows=original_rows,
            balanced_rows=original_rows,
        )
        return features, label_array, summary

    rng = np.random.default_rng(random_state)
    minority_features = np.asarray(features[minority_indices], dtype=float)
    synthetic_rows = _generate_synthetic_rows(
        minority_features,
        rows_needed,
        rng,
        k_neighbors=min(k_neighbors, max(1, minority_count - 1)),
    )
    synthetic_labels = np.full(rows_needed, minority_label, dtype=int)

    balanced_features = np.vstack([features, synthetic_rows])
    balanced_labels = np.concatenate([label_array, synthetic_labels])
    balanced_positive_rate = float(balanced_labels.mean()) if balanced_labels.size else 0.0
    summary = ResampleSummary(
        applied=True,
        original_positive_rate=positive_rate,
        balanced_positive_rate=balanced_positive_rate,
        original_rows=original_rows,
        balanced_rows=int(balanced_labels.shape[0]),
    )
    return balanced_features, balanced_labels, summary


def _minority_class(labels: np.ndarray) -> tuple[int, int, np.ndarray]:
    values, counts = np.unique(labels, return_counts=True)
    if values.size != 2:
        raise ValueError("SMOTE balancing currently expects binary labels.")
    minority_position = int(np.argmin(counts))
    majority_position = int(np.argmax(counts))
    minority_label = int(values[minority_position])
    majority_count = int(counts[majority_position])
    minority_indices = np.flatnonzero(labels == minority_label)
    return minority_label, majority_count, minority_indices


def _generate_synthetic_rows(
    minority_features: np.ndarray,
    row_count: int,
    rng: np.random.Generator,
    *,
    k_neighbors: int,
) -> np.ndarray:
    if minority_features.shape[0] == 1:
        jitter = rng.normal(loc=0.0, scale=0.01, size=(row_count, minority_features.shape[1]))
        return minority_features[0] + jitter
    if minority_features.shape[0] == 2:
        synthetic_rows = np.zeros((row_count, minority_features.shape[1]), dtype=float)
        for row_index in range(row_count):
            blend = float(rng.random())
            synthetic_rows[row_index] = minority_features[0] + blend * (minority_features[1] - minority_features[0])
        return synthetic_rows

    neighbors = NearestNeighbors(n_neighbors=k_neighbors + 1)
    neighbors.fit(minority_features)
    neighbor_indices = neighbors.kneighbors(return_distance=False)

    synthetic_rows = np.zeros((row_count, minority_features.shape[1]), dtype=float)
    for row_index in range(row_count):
        base_index = int(rng.integers(0, minority_features.shape[0]))
        choices = neighbor_indices[base_index, 1:]
        neighbor_index = base_index if choices.size == 0 else int(rng.choice(choices))
        base = minority_features[base_index]
        neighbor = minority_features[neighbor_index]
        blend = float(rng.random())
        synthetic_rows[row_index] = base + blend * (neighbor - base)
    return synthetic_rows
