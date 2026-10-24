"""Seed isolated test databases after server initialization; never used by Wavelength."""
from datetime import datetime, timezone
import hashlib
from pathlib import Path
import sqlite3


def seed_catalog(data, music, records):
    music = Path(music).resolve()
    root_id = hashlib.sha256(str(music).encode()).hexdigest()
    with sqlite3.connect(Path(data) / 'wavelength.db', timeout=10) as db:
        db.execute('PRAGMA foreign_keys=ON')
        db.execute('INSERT INTO library_roots(id,path) VALUES (?,?) ON CONFLICT DO NOTHING', (root_id, str(music)))
        for record in records:
            relative = record['path']
            stat = (music / relative).stat()
            stamp = datetime.fromtimestamp(stat.st_mtime, timezone.utc).isoformat()
            title, artist, album = record['title'], record.get('artist', ''), record.get('album', '')
            db.execute('''INSERT INTO tracks(id,root_id,path,title,artist,album,album_artist,track_number,
                disc_number,year,content_type,size,modified_at,created_at,genre,duration,bit_rate,search_text)
                VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)''', (
                hashlib.sha256((root_id + '\x00' + relative).encode()).hexdigest(), root_id, relative,
                title, artist, album, record.get('album_artist', ''), record.get('track', 0), 0, 0,
                record['content_type'], stat.st_size, stamp, stamp, record.get('genre', ''),
                record.get('duration', 0), record.get('bit_rate', 0), (title + ' ' + artist + ' ' + album).lower(),
            ))
