import copy
import hashlib
import unittest

from qualify_phase2 import validate_archive_manifest


class ArchiveManifestTests(unittest.TestCase):
    def test_contiguous_and_explicit_empty_blocks(self):
        manifest = {'version': 1, 'chainId': 125, 'sourceStream': 'canonical', 'deployment': 'deployment',
                    'decoderDigest': 'a' * 64, 'abiDigest': 'b' * 64, 'codeDigest': 'c' * 64,
                    'fromHeight': 10, 'toHeight': 11, 'rawCount': 1, 'complete': True,
                    'blocks': [{'height': 10, 'blockHash': '0x' + 'd' * 64, 'rawCount': 1, 'segmentHash': hashlib.sha256(b'raw\n').hexdigest(), 'segmentBytes': 4},
                               {'height': 11, 'blockHash': '0x' + 'e' * 64, 'rawCount': 0, 'segmentHash': hashlib.sha256(b'').hexdigest(), 'segmentBytes': 0}]}
        validate_archive_manifest(manifest)
        for change in ('missing', 'count', 'incomplete', 'hash'):
            invalid = copy.deepcopy(manifest)
            if change == 'missing':
                invalid['blocks'].pop()
            elif change == 'count':
                invalid['rawCount'] += 1
            elif change == 'incomplete':
                invalid['complete'] = False
            else:
                invalid['blocks'][1]['segmentHash'] = 'invalid'
            with self.assertRaises(ValueError):
                validate_archive_manifest(invalid)


if __name__ == '__main__':
    unittest.main()
