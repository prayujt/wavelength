#!/usr/bin/env python3
"""Exercise the real binary with encoded audio. Requires Python 3 and FFmpeg."""
import hashlib
import json
import os
from pathlib import Path
import queue
import signal
import subprocess
import tempfile
import threading
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

BINARY = Path(__file__).resolve().parents[1] / 'bin/wavelength'


def request(url, *, method='GET', headers=None):
    req = urllib.request.Request(url, method=method, headers=headers or {})
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            return response.status, response.headers, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.headers, error.read()


def main():
    # Honor the runtime's scratch location where present.
    with tempfile.TemporaryDirectory(prefix='wavelength-smoke-', dir=os.environ.get('TMPDIR')) as temporary:
        root = Path(temporary)
        music = root / 'music'
        music.mkdir()
        fixtures = []
        for suffix in ('flac', 'mp3'):
            path = music / ('tone.' + suffix)
            subprocess.run([
                'ffmpeg', '-v', 'error', '-f', 'lavfi', '-i',
                'sine=frequency=440:duration=0.25', '-metadata', 'title=Smoke Tone',
                '-metadata', 'artist=Wavelength Test', '-metadata', 'album=Smoke Album',
                '-metadata', 'track=1', str(path),
            ], check=True, capture_output=True)
            fixtures.append(path)
        before = {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in fixtures}
        env = os.environ | {
            'WAVELENGTH_MUSIC_DIR': str(music), 'WAVELENGTH_DATA_DIR': str(root / 'data'),
            'WAVELENGTH_ADDR': '127.0.0.1:0', 'WAVELENGTH_TOKEN': 'smoke-native',
            'WAVELENGTH_SUBSONIC_ADDR': '127.0.0.1:0',
            'WAVELENGTH_SUBSONIC_USER': 'smoke', 'WAVELENGTH_SUBSONIC_PASSWORD': 'smoke-legacy',
        }
        process = subprocess.Popen([str(BINARY)], env=env, stderr=subprocess.PIPE, text=True)
        logs = queue.Queue()

        def read_logs():
            for line in process.stderr:
                logs.put(line)

        reader = threading.Thread(target=read_logs, daemon=True)
        reader.start()
        try:
            addresses = {}
            for _ in range(10):
                line = logs.get(timeout=10)
                if 'wavelength listening' in line:
                    protocol = line.split('protocol=')[1].split()[0]
                    addresses[protocol] = 'http://' + line.split('address=')[1].strip().strip('"')
                if len(addresses) == 2:
                    break
            assert len(addresses) == 2, addresses
            native, legacy = addresses['native'], addresses['legacy-subsonic']
            auth = {'Authorization': 'Bearer smoke-native'}
            status, _, body = request(native + '/api/tracks', headers=auth)
            page = json.loads(body)
            assert status == 200 and page['total'] == 2, page
            assert request(native + '/api/tracks')[0] == 401
            assert request(native + '/rest/ping')[0] == 404
            for track in page['tracks']:
                assert track['title'] == 'Smoke Tone' and track['artist'] == 'Wavelength Test', track
                original = (music / track['path']).read_bytes()
                stream = native + '/api/tracks/' + track['id'] + '/stream'
                status, _, audio = request(stream, headers=auth)
                assert status == 200 and audio == original
                output = root / ('streamed.' + track['path'].split('.')[-1])
                output.write_bytes(audio)
                subprocess.run(['ffmpeg', '-v', 'error', '-i', str(output), '-f', 'null', '-'], check=True, capture_output=True)
                status, headers, audio = request(stream, headers=auth | {'Range': 'bytes=0-15'})
                assert status == 206 and audio == original[:16] and headers['Content-Range'].startswith('bytes 0-15/')
                status, headers, audio = request(stream, method='HEAD', headers=auth)
                assert status == 200 and not audio and int(headers['Content-Length']) == len(original)
            salt = 'smoke-salt'
            token = hashlib.md5(('smoke-legacy' + salt).encode()).hexdigest()
            params = {'u': 'smoke', 't': token, 's': salt, 'v': '1.16.1', 'c': 'smoke', 'f': 'json'}

            def legacy_url(endpoint, **extra):
                return legacy + '/rest/' + endpoint + '.view?' + urllib.parse.urlencode(params | extra)

            status, _, body = request(legacy_url('search3', query='Smoke Tone'))
            songs = json.loads(body)['subsonic-response']['searchResult3']['song']
            assert status == 200 and len(songs) == 2
            status, _, audio = request(legacy_url('stream', id=songs[0]['id']), headers={'Range': 'bytes=0-15'})
            source = next(t for t in page['tracks'] if t['id'] == songs[0]['id'])
            assert status == 206 and audio == (music / source['path']).read_bytes()[:16]
            _, _, body = request(legacy_url('getAlbum', id=songs[0]['albumId']))
            assert json.loads(body)['subsonic-response']['album']['songCount'] == 2
            _, _, body = request(legacy_url('ping', f='xml'))
            assert ET.fromstring(body).attrib['status'] == 'ok'
            assert before == {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in fixtures}
            fixtures[1].unlink()
            status, _, body = request(native + '/api/scan', method='POST', headers=auth)
            assert status == 200 and json.loads(body)['indexed'] == 1
            _, _, body = request(native + '/api/tracks', headers=auth)
            surviving_id = json.loads(body)['tracks'][0]['id']
        finally:
            process.send_signal(signal.SIGTERM)
            try:
                code = process.wait(timeout=12)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
                raise
            process.stderr.close()
        assert code == 0, code
        subprocess.run([str(BINARY), '--scan-only'], env=env, check=True, capture_output=True)
        import sqlite3
        with sqlite3.connect(root / 'data/wavelength.db') as db:
            assert db.execute('SELECT id FROM tracks').fetchall() == [(surviving_id,)]
        print('PASS: tagged FLAC/MP3 indexing; native and legacy streaming; FFmpeg decode; ranges/HEAD; authentication; route isolation; rescan/prune; restart persistence; read-only source; graceful shutdown')


if __name__ == '__main__':
    main()
