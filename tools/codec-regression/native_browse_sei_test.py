"""Source-authored SEI witnesses and negative controls for browse evidence."""

import copy
import unittest

import native_browse_evidence as check
from paged_codec_oracle import extend_255


def witness(data=b'x' * 670, uuid=bytes(range(16))):
    """Author native fields directly from byte positions, without the SEI oracle."""
    offset = 20
    length = extend_255(16 + len(data))
    rbsp = b'\x05' + length + uuid + data + b'\x80'
    encoded, mapping, zeros = bytearray(), [], 0
    for value in rbsp:
        if zeros >= 2 and value <= 3:
            encoded.append(3)
            zeros = 0
        mapping.append(offset + 1 + len(encoded))
        encoded.append(value)
        zeros = zeros + 1 if value == 0 else 0
    source = bytes(offset) + b'\x06' + encoded
    nals = [{'tagId': 0, 'nalIndex': 0, 'type': 6, 'offset': offset, 'bytes': len(encoded) + 1}]
    fields = []

    def add(parent, name, value, span=None, kind='derived'):
        field = {'id': str(len(fields)), 'parentId': parent, 'name': name, 'type': kind, 'value': value, 'span': span}
        fields.append(field)
        return field['id']

    def mapped(parent, name, value, begin, count):
        spans = []
        for position in mapping[begin:begin + count]:
            if spans and int(spans[-1]['byteOffset']) + spans[-1]['bitLength'] // 8 == position:
                spans[-1]['bitLength'] += 8
            else:
                spans.append({'byteOffset': str(position), 'bitOffset': 0, 'bitLength': 8})
        field = add(parent, name, value, spans[0] if len(spans) == 1 else None)
        if len(spans) > 1:
            for index, span in enumerate(spans):
                add(field, f'{name}.source[{index}]', 'Original bytes excluding emulation-prevention bytes', span)

    add('', 'Parameter context', 'found')
    root = add('', 'SEI messages NAL[0]', 'bounded SEI sequence')
    for name, value in [('seiFramingComplete', 'true'), ('seiComplete', 'true'),
                        ('seiBytesRead', str(len(encoded))), ('seiSyntaxBits', str(len(rbsp) * 8)), ('seiMessageCount', '1')]:
        add(root, name, value)
    group = add(root, 'SEI message[0]', 'userDataUnregistered', kind='SEI message')
    for name in ('syntaxSupported', 'payloadComplete', 'messageComplete'):
        add(group, name, 'true')
    mapped(group, 'payloadType', '5', 0, 1)
    mapped(group, 'payloadSize', str(16 + len(data)), 1, len(length))
    start = 1 + len(length)
    mapped(group, 'seiPayload', 'Inspected payload bytes; unknown types remain opaque', start, 16 + len(data))
    mapped(group, 'uuid', uuid.hex(), start, 16)
    if data:
        mapped(group, 'userDataPreview', data[:64].hex(), start + 16, min(64, len(data)))
    add(group, 'userDataBytes', str(len(data)))
    add(group, 'userDataPreviewTruncated', str(len(data) > 64).lower())
    mapped(root, 'seiTrailingBits', 'Validated trailing bits', len(rbsp) - 1, 1)
    return {'detail': {'detail': {'kind': 'avcVideo', 'complete': True, 'truncated': len(data) > 64,
                                 'fields': fields, 'diagnostics': []}}}, source, nals


class BrowseSEIPreviewTests(unittest.TestCase):
    def test_source_proven_preview_boundary_and_epb_mapping(self):
        for count in (0, 1, 64, 65, 670):
            for epb in (False, True):
                with self.subTest(count=count, epb=epb):
                    snapshot, source, nals = witness((b'\0\0\1' * 224)[:count] if epb else b'x' * count,
                                                     bytes(16) if epb else bytes(range(16)))
                    result = check.verify_avc_preview_omission(snapshot, source, nals)
                    self.assertEqual(result, {'seiMessagesCompared': 1, 'seiUserDataPreviewOmitted': count > 64})

    def test_unexplained_or_missing_top_level_truncation_is_rejected(self):
        for count in (0, 64, 65, 670):
            snapshot, source, nals = witness(b'x' * count)
            snapshot['detail']['detail']['truncated'] = count <= 64
            with self.subTest(count=count), self.assertRaisesRegex(RuntimeError, 'not exactly'):
                check.verify_avc_preview_omission(snapshot, source, nals)
        snapshot = {'detail': {'detail': {'complete': True, 'truncated': True, 'fields': [], 'diagnostics': []}}}
        with self.assertRaisesRegex(RuntimeError, 'not exactly'):
            check.verify_avc_preview_omission(snapshot, b'', [])

    def test_valid_preview_does_not_excuse_semantic_or_source_mutations(self):
        mutations = [('seiFramingComplete', 'value', 'false'), ('seiComplete', 'value', 'false'),
                     ('seiBytesRead', 'value', '1'), ('seiSyntaxBits', 'value', '8'), ('seiMessageCount', 'value', '0'),
                     ('syntaxSupported', 'value', 'false'), ('payloadComplete', 'value', 'false'),
                     ('messageComplete', 'value', 'false'), ('payloadType', 'value', '6'), ('payloadSize', 'value', '685'),
                     ('userDataBytes', 'value', '669'), ('userDataPreviewTruncated', 'value', 'false'),
                     ('uuid', 'value', '00'), ('userDataPreview', 'value', '00'), ('seiPayload', 'span', None),
                     ('seiTrailingBits', 'span', None), ('Parameter context', 'value', 'budget_exhausted')]
        for name, key, value in mutations:
            snapshot, source, nals = witness()
            next(field for field in snapshot['detail']['detail']['fields'] if field['name'] == name)[key] = value
            with self.subTest(name=name, key=key), self.assertRaises(RuntimeError):
                check.verify_avc_preview_omission(snapshot, source, nals)

    def test_missing_duplicate_unexpected_and_misparented_fields_are_rejected(self):
        for name in ('SEI messages NAL[0]', 'SEI message[0]', 'seiTrailingBits', 'uuid', 'userDataPreviewTruncated'):
            for mutation in ('remove', 'duplicate', 'extra', 'reparent'):
                if name.startswith('SEI messages ') and mutation == 'reparent':
                    continue  # The sequence inventory is matched by source NAL name.
                snapshot, source, nals = witness()
                fields = snapshot['detail']['detail']['fields']
                field = next(field for field in fields if field['name'] == name)
                if mutation == 'remove':
                    removed = {field['id']}
                    for candidate in fields:
                        if candidate['parentId'] in removed:
                            removed.add(candidate['id'])
                    fields[:] = [candidate for candidate in fields if candidate['id'] not in removed]
                elif mutation in ('duplicate', 'extra'):
                    other = copy.deepcopy(field)
                    other['id'] = str(len(fields))
                    if mutation == 'extra':
                        other['name'] += '.unexpected'
                    fields.append(other)
                else:
                    field['parentId'] = '0'  # A real earlier parent, but outside the required SEI subtree.
                ids = {candidate['id']: str(index) for index, candidate in enumerate(fields)}
                for candidate in fields:
                    candidate['id'] = ids[candidate['id']]
                    candidate['parentId'] = ids[candidate['parentId']] if candidate['parentId'] else ''
                error = 'SEI sequence inventory' if name.startswith('SEI messages ') else 'missing, duplicate or unexpected fields'
                with self.subTest(name=name, mutation=mutation), self.assertRaisesRegex(RuntimeError, error):
                    check.verify_avc_preview_omission(snapshot, source, nals)

    def test_epb_source_segments_cannot_include_escape_bytes(self):
        snapshot, source, nals = witness(b'\0\0\1' * 24, bytes(16))
        field = next(field for field in snapshot['detail']['detail']['fields'] if field['name'] == 'uuid.source[1]')
        field['span']['byteOffset'] = str(int(field['span']['byteOffset']) - 1)
        with self.assertRaises(RuntimeError):
            check.verify_avc_preview_omission(snapshot, source, nals)

    def test_source_syntax_corruption_is_not_preview_omission(self):
        for mutation in ('header', 'uuid', 'preview', 'size', 'trailing', 'short-uuid', 'second-malformed'):
            snapshot, source, nals = witness()
            changed = bytearray(source)
            if mutation == 'header':
                changed[20] = 0x26
            elif mutation == 'uuid':
                changed[25] ^= 1
            elif mutation == 'preview':
                changed[41] ^= 1
            elif mutation == 'size':
                changed[24] += 1
            elif mutation == 'trailing':
                changed[-1] = 0
            elif mutation == 'short-uuid':
                changed = changed[:20] + b'\x06\x05\x01\x00\x80'
            else:
                changed[-1:] = b'\x05\xff\x80'
            nals[0]['bytes'] = len(changed) - 20
            with self.subTest(mutation=mutation), self.assertRaises(RuntimeError):
                check.verify_avc_preview_omission(snapshot, bytes(changed), nals)

    def test_valid_preview_does_not_excuse_projection_or_parser_budgets(self):
        for mutation in ('incomplete', 'diagnosed', 'field-budget', 'json-budget', 'string-preview', 'string-repair', 'nal-budget', 'message-budget'):
            snapshot, source, nals = witness()
            detail = snapshot['detail']['detail']
            if mutation == 'incomplete':
                detail['complete'] = False
            elif mutation == 'diagnosed':
                detail['diagnostics'] = [{'code': 'AVC_SEI_BYTE_LIMIT_REACHED'}]
            elif mutation in ('field-budget', 'json-budget'):
                count = 512 - len(detail['fields']) if mutation == 'field-budget' else 400
                for index in range(count):
                    detail['fields'].append({'id': str(len(detail['fields'])), 'parentId': '', 'name': str(index),
                                             'type': '\x00' * 256, 'value': '\x00' * 256, 'span': None})
            elif mutation.startswith('string-'):
                detail['fields'][0]['type'] = 'preview\u2026' if mutation == 'string-preview' else 'repaired\ufffd'
            elif mutation == 'nal-budget':
                snapshot, source, nals = witness(b'x' * 65536)
            else:
                raw = b'\x06' + (b'\x05\x10' + bytes(range(16))) * 65 + b'\x80'
                source, nals[0]['bytes'] = bytes(20) + raw, len(raw)
            errors = {'incomplete': 'incomplete/diagnosed', 'diagnosed': 'incomplete/diagnosed',
                      'field-budget': 'field/JSON budget', 'json-budget': 'field/JSON budget',
                      'string-preview': 'string preview', 'string-repair': 'string preview',
                      'nal-budget': 'NAL byte budget', 'message-budget': 'message budget'}
            with self.subTest(mutation=mutation), self.assertRaisesRegex(RuntimeError, errors[mutation]):
                check.verify_avc_preview_omission(snapshot, source, nals)


if __name__ == '__main__':
    unittest.main()
