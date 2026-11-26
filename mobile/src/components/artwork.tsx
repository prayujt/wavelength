import { useEffect, useMemo, useState } from 'react';
import { View, type StyleProp, type ViewStyle } from 'react-native';
import { Music2 } from 'lucide-react-native';
import type { Playlist } from '../gen/wavelength/v1/library_pb';
import { authenticatedURL } from '../core/auth';
import { useSession } from '../lib/session';
import { color, colors } from '../lib/theme';
import { installed } from '../lib/device-store';
import { localArtwork } from '../lib/device-library';
import { CoverImage } from './cover-image';

export function Artwork({
  path,
  uri,
  title = '',
  id = title,
  size = 56,
  height = size,
  radius = 12,
  style,
}: {
  path?: string;
  uri?: string;
  title?: string;
  id?: string;
  size?: number;
  height?: number;
  radius?: number;
  style?: StyleProp<ViewStyle>;
}) {
  const session = useSession(),
    [dark, , light] = colors(id);
  const [savedImage, setSavedImage] = useState<{ key: string; uri?: string }>();
  const imageKey = JSON.stringify([session.account?.key, path]);
  const savedUri = savedImage?.key === imageKey ? savedImage.uri : undefined;
  useEffect(() => {
    if (!installed() || !session.account || !path) return;
    let disposed = false,
      uri: string | undefined;
    void localArtwork(session.account, path)
      .then((value) => {
        uri = value;
        if (disposed) {
          if (value) URL.revokeObjectURL(value);
        } else setSavedImage({ key: imageKey, uri: value });
      })
      .catch(() => {});
    return () => {
      disposed = true;
      if (uri) URL.revokeObjectURL(uri);
    };
  }, [path, session.account, imageKey]);
  // expo-image's authenticated web loader keys its fetch effect by source
  // identity. Playback ticks must never create a new request/blob URL.
  const server = session.grant?.server,
    token = session.grant?.accessToken;
  const source = useMemo(() => {
    try {
      if (savedUri) return { uri: savedUri };
      if (path && server && token)
        return {
          uri: authenticatedURL(server, path),
          headers: { Authorization: 'Bearer ' + token },
        };
      if (uri && new URL(uri).protocol === 'https:') return { uri };
    } catch {
      /* A missing or unsafe image uses the local placeholder. */
    }
    return undefined;
  }, [savedUri, path, server, token, uri]);
  return (
    <View
      accessibilityLabel={title ? `${title} artwork` : undefined}
      style={[
        {
          width: size,
          height,
          borderRadius: radius,
          overflow: 'hidden',
          backgroundColor: dark,
          alignItems: 'center',
          justifyContent: 'center',
        },
        style,
      ]}
    >
      <Music2 size={Math.min(46, Math.min(size, height) * 0.38)} color={light} strokeWidth={1.4} />
      {source && (
        <CoverImage key={source.uri + (source.headers?.Authorization || '')} source={source} />
      )}
    </View>
  );
}

export function PlaylistArtwork({
  playlist,
  size,
  radius,
}: {
  playlist: Playlist;
  size: number;
  radius: number;
}) {
  const tracks = playlist.trackIds.slice(0, 4);
  if (!tracks.length)
    return <Artwork title={playlist.name} id={playlist.id} size={size} radius={radius} />;

  return (
    <View
      accessibilityLabel={`${playlist.name} artwork`}
      style={{
        width: size,
        height: size,
        borderRadius: radius,
        overflow: 'hidden',
        backgroundColor: color.surface,
        flexDirection: 'row',
        flexWrap: 'wrap',
      }}
    >
      {tracks.map((id, index) => (
        <Artwork
          key={`${id}:${index}`}
          id={id}
          path={`/media/tracks/${encodeURIComponent(id)}/artwork`}
          size={tracks.length === 1 || (tracks.length === 3 && index === 2) ? size : size / 2}
          height={tracks.length <= 2 ? size : size / 2}
          radius={0}
        />
      ))}
    </View>
  );
}
