import os
from pathlib import Path
from tempfile import TemporaryDirectory
import unittest
from unittest.mock import patch

import demo


class EnvFileTests(unittest.TestCase):
    def test_loads_values_without_overriding_environment(self):
        with TemporaryDirectory() as folder:
            env_file = Path(folder) / '.env'
            env_file.write_text('# comment\nADMIN_TOKEN=file-secret\nPOSTGRES_PASSWORD="quoted value"\nEMPTY_LINE\n')
            with patch.dict(os.environ, {'ADMIN_TOKEN': 'existing-secret'}, clear=True):
                demo.load_env_file(env_file)
                self.assertEqual(os.environ['ADMIN_TOKEN'], 'existing-secret')
                self.assertEqual(os.environ['POSTGRES_PASSWORD'], 'quoted value')

    def test_missing_file_is_ignored(self):
        with TemporaryDirectory() as folder:
            demo.load_env_file(Path(folder) / '.env')


if __name__ == '__main__':
    unittest.main()
..
----------------------------------------------------------------------
Ran 2 tests in 0.001s

OK
