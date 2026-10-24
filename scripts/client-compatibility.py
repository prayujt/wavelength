# /// script
# requires-python = ">=3.10"
# dependencies = ["py-sonic==1.0.3"]
# ///
"""Independent py-sonic interoperability against the real Wavelength binary.

Uses temporary encoded fixtures, loopback listeners and isolated SQLite state.
No existing music, server or identity provider is touched.
"""
from contextlib import closing
import os
from pathlib import Path
import queue
import signal
import subprocess
import tempfile
import threading
import time
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit, urlencode
from urllib.request import urlopen
import libsonic


def run():
    binary = Path(__file__).resolve().parents[1] / 'bin/wavelength'
    with tempfile.TemporaryDirectory(prefix='wavelength-client-', dir=os.environ.get('TMPDIR')) as directory:
        root = Path(directory)
        album = root / 'music' / 'Folder Artist' / 'Folder Album'
        album.mkdir(parents=True)
        source = album / 'tone.flac'
        subprocess.run(['ffmpeg', '-v', 'error', '-f', 'lavfi', '-i', 'sine=duration=1',
                        '-metadata', 'title=Client Tone', '-metadata', 'artist=Tag Artist',
                        '-metadata', 'album=Tag Album', '-metadata', 'genre=Rock', str(source)], check=True)
        video = root / 'music' / 'clip.mp4'
        subprocess.run(['ffmpeg', '-v', 'error', '-f', 'lavfi', '-i', 'color=c=blue:s=160x120:d=1', '-f', 'lavfi', '-i', 'sine=duration=1', '-c:v', 'mpeg4', '-c:a', 'aac', '-shortest', str(video)], check=True)
        video.with_suffix('.vtt').write_text('WEBVTT\n\n00:00.000 --> 00:01.000\nClient caption\n')
        (album / 'tone.lrc').write_text('[00:00.00]Client fixture lyrics')
        # A real tiny PNG, generated with Python's standard library.
        import struct
        import zlib
        def chunk(kind, value):
            return struct.pack('!I', len(value)) + kind + value + struct.pack('!I', zlib.crc32(kind + value))
        png = b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('!2I5B', 1, 1, 8, 2, 0, 0, 0)) + chunk(b'IDAT', zlib.compress(b'\x00\xff\x00\x00')) + chunk(b'IEND', b'')
        (album / 'cover.png').write_bytes(png)
        avatars = root / 'data' / 'avatars'
        avatars.mkdir(parents=True)
        (avatars / 'client.png').write_bytes(png)
        env = os.environ | {
            'WAVELENGTH_MUSIC_DIR': str(root / 'music'), 'WAVELENGTH_DATA_DIR': str(root / 'data'),
            'WAVELENGTH_ADDR': '127.0.0.1:0', 'WAVELENGTH_TOKEN': 'native-is-separate',
            'WAVELENGTH_SUBSONIC_ADDR': '127.0.0.1:0', 'WAVELENGTH_SUBSONIC_USER': 'client',
            'WAVELENGTH_SUBSONIC_PASSWORD': 'client-secret', 'WAVELENGTH_SUBSONIC_PUBLIC_URL': 'https://share.example.test',
        }
        class Feed(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200)
                self.end_headers()
                if self.path == '/episode.flac':
                    self.wfile.write(source.read_bytes())
                else:
                    self.wfile.write(f'<rss version="2.0"><channel><title>Client podcast</title><item><guid>one</guid><title>Episode</title><enclosure url="http://127.0.0.1:{self.server.server_port}/episode.flac" type="audio/flac"/></item></channel></rss>'.encode())
            def log_message(self, *args):
                pass
        feed = ThreadingHTTPServer(('127.0.0.1', 0), Feed)
        feed_thread = threading.Thread(target=feed.serve_forever, daemon=True)
        feed_thread.start()
        process = subprocess.Popen([str(binary)], env=env, stderr=subprocess.PIPE, text=True)
        logs = queue.Queue()
        def collect():
            for line in process.stderr:
                logs.put(line)
        reader = threading.Thread(target=collect, daemon=True)
        reader.start()
        try:
            port = None
            for _ in range(12):
                line = logs.get(timeout=15)
                if 'protocol=legacy-subsonic' in line:
                    port = urlsplit('http://' + line.split('address=')[1].strip().strip('"')).port
                    break
            assert port, 'Legacy listener did not start'
            calls = 0
            covered = set()
            def check(method, *args, **kwargs):
                nonlocal calls
                result = getattr(client, method)(*args, **kwargs)
                assert result is True or result['status'] == 'ok', (method, result)
                calls += 1
                covered.add(method)
                return result
            # Both POST/token auth and GET/legacy-password modes are used by real clients.
            for legacy, get in ((False, False), (True, True)):
                client = libsonic.Connection('http://127.0.0.1', 'client', 'client-secret', port=port,
                                             legacyAuth=legacy, useGET=get)
                check('ping')
                if get:
                    check('getMusicFolders')
                    check('getArtists')
                    # py-sonic's repeated-parameter GET helpers still reference urllib2.
                    # Use its unmodified POST helpers for those operations.
                    client.setGET(False)
                check('updateUser', 'client', adminRole=True, downloadRole=True, commentRole=True, shareRole=True, podcastRole=True)
                check('getLicense')
                check('getMusicFolders')
                indexes = check('getIndexes')['indexes']['index']
                folder_artist = indexes[0]['artist'][0]['id']
                folder_album = check('getMusicDirectory', folder_artist)['directory']['child'][0]['id']
                track = check('getMusicDirectory', folder_album)['directory']['child'][0]['id']
                artist = check('getArtists')['artists']['index'][0]['artist'][0]['id']
                tag_album = check('getArtist', artist)['artist']['album'][0]['id']
                check('getAlbum', tag_album)
                check('getSong', track)
                for method in ('search', 'search2', 'search3'):
                    check(method, **({'any': 'Client'} if method == 'search' else {'query': 'Client'}))
                check('getAlbumList', 'alphabeticalByName')
                check('getAlbumList2', 'alphabeticalByName')
                check('getGenres')
                check('getRandomSongs')
                check('getSongsByGenre', 'Rock')
                check('star', sids=[folder_artist, folder_album, track])
                check('getStarred')
                check('star', albumIds=[tag_album], artistIds=[artist])
                check('getStarred2')
                check('setRating', track, 5)
                check('scrobble', track, submission=False)
                check('getNowPlaying')
                check('scrobble', track)
                for method, identifier in [('getArtistInfo', folder_artist), ('getArtistInfo2', artist),
                                           ('getAlbumInfo', folder_album), ('getAlbumInfo2', tag_album),
                                           ('getSimilarSongs', folder_artist), ('getSimilarSongs2', artist)]:
                    check(method, identifier)
                check('getTopSongs', 'Tag Artist')
                playlist = check('createPlaylist', name='Client list', songIds=[track])['playlist']['id']
                check('updatePlaylist', playlist, name='Renamed', songIdsToAdd=[track], songIndexesToRemove=[0])
                assert len(check('getPlaylist', playlist)['playlist']['entry']) == 1
                check('getPlaylists')
                check('deletePlaylist', playlist)
                check('createBookmark', track, 250, comment='Resume')
                check('getBookmarks')
                check('deleteBookmark', track)
                check('savePlayQueue', [track], current=track, position=250)
                check('getPlayQueue')
                check('getLyrics', artist='Tag Artist', title='Client Tone')
                check('addChatMessage', 'Client integration')
                check('getChatMessages')
                check('getUsers')
                check('getUser', 'client')
                check('createUser', 'temporary', 'password', 't@example.test')
                check('updateUser', 'temporary', maxBitRate=64)
                check('changePassword', 'temporary', 'new-password')
                check('deleteUser', 'temporary')
                check('createInternetRadioStation', 'https://radio.example.test/live', 'Station')
                station = check('getInternetRadioStations')['internetRadioStations']['internetRadioStation'][0]['id']
                check('updateInternetRadioStation', station, 'https://radio.example.test/live', 'Renamed')
                check('deleteInternetRadioStation', station)
                check('createShare', shids=[folder_album], description='Independent client share')
                share = check('getShares')['shares']['share'][0]['id']
                check('updateShare', share, description='Updated share')
                check('deleteShare', share)
                check('createPodcastChannel', f'http://127.0.0.1:{feed.server_port}/feed')
                channel = check('getPodcasts')['podcasts']['channel'][0]
                episode = channel['episode'][0]['id']
                check('downloadPodcastEpisode', episode)
                deadline = time.monotonic() + 10
                while True:
                    channel = check('getPodcasts', pid=channel['id'])['podcasts']['channel'][0]
                    if channel['episode'][0]['status'] == 'completed':
                        break
                    assert channel['episode'][0]['status'] != 'error', channel
                    assert time.monotonic() < deadline, 'Podcast download did not finish'
                    time.sleep(0.02)
                check('getNewestPodcasts')
                check('refreshPodcasts')
                check('deletePodcastEpisode', episode)
                check('deletePodcastChannel', channel['id'])
                video_id = check('getVideos')['videos']['video'][0]['id']
                # py-sonic casts video IDs to int and parses captions as JSON.
                # The protocol defines string IDs and binary captions: check these
                # two operations directly rather than patching the external client.
                for endpoint in ('getVideoInfo', 'getCaptions'):
                    query = urlencode({'u':'client', 'p':'client-secret', 'v':'1.16.1', 'c':'video-contract', 'f':'json', 'id':video_id, 'format':'vtt'})
                    with urlopen(f'http://127.0.0.1:{port}/rest/{endpoint}?{query}', timeout=10) as response:
                        body = response.read()
                    if endpoint == 'getVideoInfo':
                        assert json.loads(body)['subsonic-response']['videoInfo']['id'] == video_id
                    else:
                        assert b'Client caption' in body
                    covered.add(endpoint)
                    calls += 1
                check('getScanStatus')
                check('startScan')
                for method, args in [('download', (track,)), ('stream', (track,)), ('getCoverArt', (folder_album,)), ('getAvatar', ('client',))]:
                    with closing(getattr(client, method)(*args)) as response:
                        body = response.read()
                    assert body, method
                    if method in ('download', 'stream'):
                        assert body == source.read_bytes(), method
                    calls += 1
                    covered.add(method)
                with closing(client.stream(track, maxBitRate=64, tformat='mp3', estimateContentLength=True)) as response:
                    audio = response.read()
                    assert int(response.headers['Content-Length']) == len(audio)
                encoded = root / 'client.mp3'
                encoded.write_bytes(audio)
                subprocess.run(['ffmpeg', '-v', 'error', '-i', str(encoded), '-f', 'null', '-'], check=True)
                assert b'#EXTM3U' in client.hls(track)
                covered.add('hls')
                calls += 2
                check('unstar', sids=[folder_artist, folder_album, track], albumIds=[tag_album], artistIds=[artist])
            contract = json.loads((binary.parent.parent / 'internal/transport/subsonic/testdata/contract.json').read_text())['endpoints']
            supported = set(contract) - {'jukeboxControl'}
            assert covered == supported, f'Endpoint coverage mismatch: missing={supported - covered}, extra={covered - supported}'
            print(f'PASS: all {len(covered)} supported legacy endpoints; {calls} operations (73 endpoints via py-sonic, 2 video endpoints via direct HTTP) against real binary; token/password POST and GET connectivity; filesystem and ID3 browsing, state, accounts, encoded playback, HLS and assets')
        finally:
            process.send_signal(signal.SIGTERM)
            try:
                code = process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
                raise
            reader.join(timeout=2)
            feed.shutdown()
            feed.server_close()
            feed_thread.join(timeout=2)
            assert code == 0, f'Wavelength exited {code}'


if __name__ == '__main__':
    run()
