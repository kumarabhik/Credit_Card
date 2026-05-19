from __future__ import annotations

import asyncio
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

import numpy as np
import pandas as pd

APP_ROOT = Path(__file__).resolve().parents[1] / "app"
GEN_ROOT = Path(__file__).resolve().parents[3] / "gen" / "python"

sys.path.insert(0, str(APP_ROOT))
sys.path.insert(0, str(GEN_ROOT))

from ml.v1 import score_pb2  # noqa: E402
from ml_scorer.artifact import save_bundle, train_bundle  # noqa: E402
from ml_scorer.dataset import TrainingDataset  # noqa: E402
from ml_scorer.log_config import configure_logging  # noqa: E402
from ml_scorer.schema import MODEL_FEATURE_NAMES  # noqa: E402
from ml_scorer.service import MLScoreService  # noqa: E402


class ServiceTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = Path(tempfile.mkdtemp())

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir)

    def test_score_returns_risk_score_and_model_version(self) -> None:
        rows = 40
        feature_matrix = pd.DataFrame(
            {
                name: np.linspace(0.0, 2.0, rows) + idx
                for idx, name in enumerate(MODEL_FEATURE_NAMES)
            }
        )
        labels = pd.Series(([0] * 20) + ([1] * 20), dtype="int32")
        dataset = TrainingDataset(features=feature_matrix, labels=labels, source_rows=rows)
        bundle = train_bundle(dataset, random_state=9)
        save_bundle(bundle, self.temp_dir, source="unit-test")

        logger = configure_logging()
        service = MLScoreService(bundle, logger)
        request = score_pb2.ScoreRequest(features={name: float(feature_matrix.iloc[0][name]) for name in MODEL_FEATURE_NAMES})

        response = asyncio.run(service.Score(request, None))

        self.assertEqual(bundle.model_version, response.model_version)
        self.assertGreaterEqual(response.risk_score, 0)
        self.assertLessEqual(response.risk_score, 1000)


if __name__ == "__main__":
    unittest.main()
