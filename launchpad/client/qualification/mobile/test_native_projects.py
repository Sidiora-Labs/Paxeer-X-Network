import importlib.util
from pathlib import Path
import tempfile
import unittest
import shutil
import os

spec = importlib.util.spec_from_file_location('native_check', Path(__file__).with_name('check-native-projects.py'))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)

class NativeProjectTests(unittest.TestCase):
    def test_parser_rejects_duplicate_keys_and_trailing_tokens(self):
        for text in ['{ x = 1; x = 2; }', '{ x = 1; } extra', '{ x 1; }']:
            with self.assertRaises((ValueError, IndexError)):
                check.parse_openstep(text)

    def test_parser_handles_shell_paths_and_comments(self):
        self.assertEqual(check.parse_openstep('// header\n{ a = /bin/sh; b = ("quoted path", raw,); /* comment */ }'), {'a':'/bin/sh', 'b':['quoted path','raw']})

    def test_rejects_dangling_project_reference(self):
        mobile = Path(__file__).resolve().parents[2] / 'apps/mobile'
        with tempfile.TemporaryDirectory(prefix='L32-project-negative-') as directory:
            dest = Path(directory)/'mobile'
            shutil.copytree(mobile/'ios',dest/'ios')
            project=dest/'ios/Sidiora.xcodeproj/project.pbxproj'
            project.write_text(project.read_text().replace('rootObject = 83CBB9F71A601CBA00E9B192','rootObject = AAAAAAAAAAAAAAAAAAAAAAAA'))
            with self.assertRaises(KeyError):
                check.check_project(dest,Path('/not-used'),Path('/not-used'))

if __name__ == '__main__':
    unittest.main()
