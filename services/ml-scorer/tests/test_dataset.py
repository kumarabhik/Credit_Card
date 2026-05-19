from __future__ import annotations

import shutil
import sys
import tempfile
import unittest
from pathlib import Path

import pandas as pd

APP_ROOT = Path(__file__).resolve().parents[1] / "app"

sys.path.insert(0, str(APP_ROOT))

from ml_scorer.dataset import load_training_dataset  # noqa: E402
from ml_scorer.schema import MODEL_FEATURE_NAMES  # noqa: E402


class DatasetTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = Path(tempfile.mkdtemp())
        transaction_rows = [
            {
                "TransactionID": 1,
                "isFraud": 0,
                "TransactionDT": 0,
                "TransactionAmt": 10.0,
                "card1": 1001,
                "addr1": 111.0,
                "dist1": 1.0,
                "dist2": 2.0,
                "C1": 1.0,
                "C2": 2.0,
                "C5": 5.0,
                "C13": 13.0,
                "C14": 14.0,
                "D1": 1.0,
                "D10": 10.0,
            },
            {
                "TransactionID": 2,
                "isFraud": 1,
                "TransactionDT": 30,
                "TransactionAmt": 20.0,
                "card1": 1001,
                "addr1": 111.0,
                "dist1": 1.5,
                "dist2": 2.5,
                "C1": 2.0,
                "C2": 3.0,
                "C5": 6.0,
                "C13": 14.0,
                "C14": 15.0,
                "D1": 2.0,
                "D10": 11.0,
            },
            {
                "TransactionID": 3,
                "isFraud": 0,
                "TransactionDT": 4000,
                "TransactionAmt": 50.0,
                "card1": 1001,
                "addr1": 111.0,
                "dist1": 2.0,
                "dist2": 3.0,
                "C1": 3.0,
                "C2": 4.0,
                "C5": 7.0,
                "C13": 15.0,
                "C14": 16.0,
                "D1": 3.0,
                "D10": 12.0,
            },
        ]
        identity_rows = [
            {"TransactionID": 1, "id_01": 0.1, "id_02": 10.0, "id_11": 100.0, "id_13": 5.0, "id_17": 87.0},
            {"TransactionID": 2, "id_01": 0.2, "id_02": 11.0, "id_11": 101.0, "id_13": 6.0, "id_17": 88.0},
            {"TransactionID": 3, "id_01": 0.3, "id_02": 12.0, "id_11": 102.0, "id_13": 7.0, "id_17": 89.0},
        ]
        pd.DataFrame(transaction_rows).to_csv(self.temp_dir / "train_transaction.csv", index=False)
        pd.DataFrame(identity_rows).to_csv(self.temp_dir / "train_identity.csv", index=False)

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir)

    def test_load_training_dataset_builds_expected_features(self) -> None:
        dataset = load_training_dataset(self.temp_dir)

        self.assertEqual(list(MODEL_FEATURE_NAMES), list(dataset.features.columns))
        self.assertEqual(3, dataset.source_rows)
        self.assertEqual(2.0, dataset.features.loc[1, "count_60s"])
        self.assertEqual(30.0, dataset.features.loc[1, "sum_60s"])
        self.assertEqual(1.0, dataset.labels.iloc[1])

    def test_load_training_dataset_keeps_labels_aligned_after_sorting(self) -> None:
        transaction_rows = [
            {
                "TransactionID": 20,
                "isFraud": 1,
                "TransactionDT": 40,
                "TransactionAmt": 99.0,
                "card1": 2002,
                "addr1": 210.0,
                "dist1": 3.0,
                "dist2": 4.0,
                "C1": 2.0,
                "C2": 3.0,
                "C5": 4.0,
                "C13": 5.0,
                "C14": 6.0,
                "D1": 7.0,
                "D10": 8.0,
            },
            {
                "TransactionID": 10,
                "isFraud": 0,
                "TransactionDT": 10,
                "TransactionAmt": 20.0,
                "card1": 1001,
                "addr1": 110.0,
                "dist1": 1.0,
                "dist2": 2.0,
                "C1": 1.0,
                "C2": 1.0,
                "C5": 1.0,
                "C13": 1.0,
                "C14": 1.0,
                "D1": 1.0,
                "D10": 1.0,
            },
            {
                "TransactionID": 30,
                "isFraud": 1,
                "TransactionDT": 20,
                "TransactionAmt": 30.0,
                "card1": 1001,
                "addr1": 110.0,
                "dist1": 1.5,
                "dist2": 2.5,
                "C1": 2.0,
                "C2": 2.0,
                "C5": 2.0,
                "C13": 2.0,
                "C14": 2.0,
                "D1": 2.0,
                "D10": 2.0,
            },
        ]
        identity_rows = [
            {"TransactionID": 10, "id_01": 0.1, "id_02": 10.0, "id_11": 100.0, "id_13": 5.0, "id_17": 87.0},
            {"TransactionID": 20, "id_01": 0.2, "id_02": 11.0, "id_11": 101.0, "id_13": 6.0, "id_17": 88.0},
            {"TransactionID": 30, "id_01": 0.3, "id_02": 12.0, "id_11": 102.0, "id_13": 7.0, "id_17": 89.0},
        ]
        pd.DataFrame(transaction_rows).to_csv(self.temp_dir / "train_transaction.csv", index=False)
        pd.DataFrame(identity_rows).to_csv(self.temp_dir / "train_identity.csv", index=False)

        dataset = load_training_dataset(self.temp_dir)

        self.assertEqual([0, 1, 1], dataset.labels.tolist())


if __name__ == "__main__":
    unittest.main()
