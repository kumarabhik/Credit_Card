from __future__ import annotations

import shutil
import sys
import tempfile
import unittest
from pathlib import Path

import numpy as np
import pandas as pd

APP_ROOT = Path(__file__).resolve().parents[1] / "app"

sys.path.insert(0, str(APP_ROOT))

from ml_scorer.artifact import load_bundle, probability_to_risk_score, save_bundle, train_bundle  # noqa: E402
from ml_scorer.dataset import TrainingDataset  # noqa: E402
from ml_scorer.schema import MODEL_FEATURE_NAMES  # noqa: E402


class ArtifactTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = Path(tempfile.mkdtemp())

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir)

    def test_train_save_and_load_bundle(self) -> None:
        rows = 50
        feature_matrix = pd.DataFrame(
            {
                name: np.linspace(0.0, 1.0, rows) + idx
                for idx, name in enumerate(MODEL_FEATURE_NAMES)
            }
        )
        labels = pd.Series(([0] * 25) + ([1] * 25), dtype="int32")
        dataset = TrainingDataset(features=feature_matrix, labels=labels, source_rows=rows)

        bundle = train_bundle(dataset, random_state=7)
        save_bundle(bundle, self.temp_dir, source="unit-test")
        loaded = load_bundle(self.temp_dir)
        response_probability = loaded.score_probability(feature_matrix.iloc[0].to_dict())

        self.assertEqual(bundle.model_version, loaded.model_version)
        self.assertGreaterEqual(response_probability, 0.0)
        self.assertLessEqual(response_probability, 1.0)
        self.assertGreaterEqual(probability_to_risk_score(response_probability), 0)
        self.assertLessEqual(probability_to_risk_score(response_probability), 1000)


if __name__ == "__main__":
    unittest.main()
