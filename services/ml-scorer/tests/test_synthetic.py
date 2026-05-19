from __future__ import annotations

import shutil
import sys
import tempfile
import unittest
from pathlib import Path

import pandas as pd

APP_ROOT = Path(__file__).resolve().parents[1] / "app"

sys.path.insert(0, str(APP_ROOT))

from ml_scorer.synthetic import SyntheticDatasetConfig, write_synthetic_dataset  # noqa: E402


class SyntheticDatasetTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = Path(tempfile.mkdtemp())

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir)

    def test_write_synthetic_dataset_creates_expected_files(self) -> None:
        config = SyntheticDatasetConfig(train_rows=200, test_rows=80, chunk_size=75, seed=11)

        paths = write_synthetic_dataset(self.temp_dir, config)

        train_transaction = pd.read_csv(paths["train_transaction"])
        train_identity = pd.read_csv(paths["train_identity"])
        test_transaction = pd.read_csv(paths["test_transaction"])
        sample_submission = pd.read_csv(paths["sample_submission"])

        self.assertEqual(200, len(train_transaction))
        self.assertEqual(200, len(train_identity))
        self.assertEqual(80, len(test_transaction))
        self.assertEqual(80, len(sample_submission))
        self.assertIn("isFraud", train_transaction.columns)
        self.assertNotIn("isFraud", test_transaction.columns)
        self.assertGreater(train_transaction["isFraud"].sum(), 0)


if __name__ == "__main__":
    unittest.main()
