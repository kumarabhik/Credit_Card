from __future__ import annotations

import sys
import unittest
from pathlib import Path

import numpy as np

APP_ROOT = Path(__file__).resolve().parents[1] / "app"

sys.path.insert(0, str(APP_ROOT))

from ml_scorer.rebalance import apply_smote  # noqa: E402


class RebalanceTest(unittest.TestCase):
    def test_apply_smote_balances_minority_class(self) -> None:
        features = np.array(
            [
                [0.0, 0.0],
                [0.1, 0.2],
                [0.2, 0.1],
                [1.0, 1.0],
                [1.1, 1.2],
                [1.2, 1.1],
                [1.3, 1.4],
                [1.4, 1.3],
            ],
            dtype=float,
        )
        labels = np.array([1, 1, 0, 0, 0, 0, 0, 0], dtype=int)

        balanced_features, balanced_labels, summary = apply_smote(features, labels, random_state=7)

        self.assertTrue(summary.applied)
        self.assertEqual(len(balanced_features), len(balanced_labels))
        self.assertGreater(summary.balanced_rows, summary.original_rows)
        self.assertAlmostEqual(summary.balanced_positive_rate, 0.5, places=2)


if __name__ == "__main__":
    unittest.main()
