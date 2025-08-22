import json
import os
import unittest
import yaml

class TestResultsJson(unittest.TestCase):
    def setUp(self):
        with open("results.json") as f:
            self.results = json.load(f)
        if os.path.exists("results.json.reference"):
            with open("results.json.reference") as f:
                self.reference = json.load(f)
        else:
            self.reference = None
        if os.path.exists("e2e-testconfig.yaml"):
            with open("e2e-testconfig.yaml") as f:
                self.test_config = yaml.safe_load(f)
        else:
            self.test_config = None

    def test_results_count(self):
        num_tests = len(self.test_config)
        self.assertEqual(
            len(self.results), num_tests, f"results.json should contain {num_tests} test results"
        )

    def test_structure_matches_reference(self):
        if self.reference is None:
            self.fail("results.json.reference not found")
        self.assertEqual(
            len(self.results), len(self.reference), "Length mismatch with reference"
        )
        for i, (gen, ref) in enumerate(zip(self.results, self.reference)):
            self.assertEqual(
                set(gen.keys()), set(ref.keys()), f"Structure mismatch in result {i}"
            )


if __name__ == "__main__":
    unittest.main()
