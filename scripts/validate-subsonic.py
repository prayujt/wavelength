# /// script
# requires-python = ">=3.10"
# dependencies = ["lxml==5.4.0"]
# ///
"""Validate XML and JSON scalar types against the official legacy schema."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import urllib.request
from lxml import etree

SOURCE = 'https://www.subsonic.org/pages/inc/api/schema/subsonic-rest-api-1.16.1.xsd'
DIGEST = '9baee72fa24087190ad22be4f7674d628acc7a3e0fb65160d74115b166df0387'
NS = {'xs': 'http://www.w3.org/2001/XMLSchema'}


def validate_json(value, type_name, types, path):
    """Keep JSON numbers/booleans typed; XSD validation alone accepts strings."""
    if type_name.startswith('xs:'):
        kind = type_name.removeprefix('xs:')
        if kind == 'boolean':
            valid = type(value) is bool
        elif kind in ('int', 'long', 'integer'):
            valid = type(value) is int
        elif kind in ('float', 'double', 'decimal'):
            valid = type(value) in (int, float)
        else:
            valid = isinstance(value, str)
        if not valid:
            raise ValueError(f'{path}: expected {kind}, got {type(value).__name__}')
        return
    type_name = type_name.removeprefix('sub:')
    definition = types[type_name]
    restriction = definition.find('xs:restriction', NS)
    if restriction is not None:
        validate_json(value, restriction.get('base'), types, path)
        return
    if not isinstance(value, dict):
        raise ValueError(f'{path}: expected object, got {type(value).__name__}')
    extension = definition.find('xs:complexContent/xs:extension', NS)
    if extension is not None:
        validate_json(value, extension.get('base'), types, path)
    text = definition.find('xs:simpleContent/xs:extension', NS)
    if text is not None and 'value' in value:
        validate_json(value['value'], text.get('base'), types, path + '.value')
    if definition.get('mixed') == 'true' and 'value' in value:
        validate_json(value['value'], 'xs:string', types, path + '.value')
    for attribute in definition.findall('.//xs:attribute', NS):
        name = attribute.get('name')
        if attribute.get('use') == 'required' and name not in value:
            raise ValueError(f'{path}.{name}: missing required attribute')
        if name in value:
            validate_json(value[name], attribute.get('type'), types, path + '.' + name)
    for child in definition.findall('.//xs:element', NS):
        name = child.get('name')
        if name not in value:
            if child.get('minOccurs', '1') != '0' and child.getparent().tag != '{' + NS['xs'] + '}choice':
                raise ValueError(f'{path}.{name}: missing required element')
            continue
        content = value[name]
        if child.get('maxOccurs', '1') != '1':
            if not isinstance(content, list):
                raise ValueError(f'{path}.{name}: expected array, got {type(content).__name__}')
            for index, item in enumerate(content):
                validate_json(item, child.get('type'), types, f'{path}.{name}[{index}]')
        else:
            validate_json(content, child.get('type'), types, path + '.' + name)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--go', default='go')
    args = parser.parse_args()
    with urllib.request.urlopen(SOURCE, timeout=15) as response:
        raw = response.read()
    assert hashlib.sha256(raw).hexdigest() == DIGEST, 'Official schema changed; review before updating the pinned digest'
    schema_doc = etree.fromstring(raw)
    # Original XSD declares queue.current xs:int although the API defines it as a
    # media ID and media IDs are strings. Preserve string-ID semantics explicitly.
    namespace = {'xs': 'http://www.w3.org/2001/XMLSchema'}
    current = schema_doc.xpath("//xs:complexType[@name='PlayQueue']/xs:attribute[@name='current']", namespaces=namespace)
    assert len(current) == 1
    current[0].set('type', 'xs:string')
    schema = etree.XMLSchema(schema_doc)
    types = {node.get('name'): node for node in schema_doc if node.get('name')}
    root = Path(__file__).resolve().parents[1]
    with tempfile.TemporaryDirectory(prefix='wavelength-xml-', dir=os.environ.get('TMPDIR')) as directory:
        subprocess.run([args.go, 'test', '-timeout', '30s', './internal/transport/subsonic', '-run', '^TestExportXMLContractFixtures$'], cwd=root, env=os.environ | {'WAVELENGTH_XML_FIXTURES': directory}, check=True)
        fixtures = sorted(Path(directory).glob('*.xml'))
        assert fixtures, 'No response fixtures exported'
        failures = []
        for path in fixtures:
            try:
                schema.assertValid(etree.parse(str(path)))
            except etree.DocumentInvalid as error:
                failures.append(f'{path.name}: {error}')
        json_fixtures = sorted(Path(directory).glob('*.json'))
        assert len(json_fixtures) == len(fixtures), 'Missing JSON response fixtures'
        for path in json_fixtures:
            try:
                response = json.loads(path.read_text())['subsonic-response']
                validate_json(response, 'Response', types, 'subsonic-response')
            except (KeyError, ValueError) as error:
                failures.append(f'{path.name}: {error}')
        if failures:
            raise SystemExit('\n'.join(failures))
        print(f'PASS: {len(fixtures)} XML and {len(json_fixtures)} JSON response shapes validated against pinned Subsonic 1.16.1 XSD (documented string-ID queue correction)')


if __name__ == '__main__':
    main()
