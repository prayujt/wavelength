# /// script
# requires-python = ">=3.10"
# dependencies = ["lxml==5.4.0"]
# ///
"""Independent validation against the official legacy XML schema (run with uv)."""
import argparse
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile
import urllib.request
from lxml import etree

SOURCE = 'https://www.subsonic.org/pages/inc/api/schema/subsonic-rest-api-1.16.1.xsd'
DIGEST = '9baee72fa24087190ad22be4f7674d628acc7a3e0fb65160d74115b166df0387'


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
    root = Path(__file__).resolve().parents[1]
    with tempfile.TemporaryDirectory(prefix='wavelength-xml-', dir=os.environ.get('TMPDIR')) as directory:
        subprocess.run([args.go, 'test', '-timeout', '30s', './internal/subsonic', '-run', '^TestExportXMLContractFixtures$'], cwd=root, env=os.environ | {'WAVELENGTH_XML_FIXTURES': directory}, check=True)
        fixtures = sorted(Path(directory).glob('*.xml'))
        assert fixtures, 'No response fixtures exported'
        failures = []
        for path in fixtures:
            try:
                schema.assertValid(etree.parse(str(path)))
            except etree.DocumentInvalid as error:
                failures.append(f'{path.name}: {error}')
        if failures:
            raise SystemExit('\n'.join(failures))
        print(f'PASS: {len(fixtures)} XML response shapes validated against pinned Subsonic 1.16.1 XSD (documented string-ID queue correction)')


if __name__ == '__main__':
    main()
