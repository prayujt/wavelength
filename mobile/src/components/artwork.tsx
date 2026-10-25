import React, { useId, useState } from 'react';
import { View, StyleSheet, type StyleProp, type ViewStyle } from 'react-native';
import { Image } from 'expo-image';
import Svg, { Defs, LinearGradient, Stop, Rect, Path, Circle, G } from 'react-native-svg';
import { authenticatedURL } from '../core/auth';
import { useSession } from '../lib/session';
import { colors, seed } from '../lib/theme';
import { Txt } from './ui';

export function Artwork({
  path,
  uri,
  title = '',
  id = title,
  size = 56,
  radius = 12,
  style,
  label = true,
}: {
  path?: string;
  uri?: string;
  title?: string;
  id?: string;
  size?: number;
  radius?: number;
  style?: StyleProp<ViewStyle>;
  label?: boolean;
}) {
  const gradient = useId().replace(/:/g, '');
  const sky = gradient + '-sky',
    sun = gradient + '-sun';
  const session = useSession(),
    [dark, mid, light] = colors(id);
  let source: { uri: string; headers?: Record<string, string> } | undefined;
  try {
    if (path && session.grant)
      source = {
        uri: authenticatedURL(session.grant.server, path),
        headers: { Authorization: 'Bearer ' + session.grant.accessToken },
      };
    else if (uri && new URL(uri).protocol === 'https:') source = { uri };
  } catch {
    /* A missing or unsafe image uses a local generative cover. */
  }
  const variant = seed(id) % 3;
  return (
    <View
      accessibilityLabel={title ? `${title} artwork` : undefined}
      style={[
        {
          width: size,
          height: size,
          borderRadius: radius,
          overflow: 'hidden',
          backgroundColor: dark,
        },
        style,
      ]}
    >
      <Svg width="100%" height="100%" viewBox="0 0 300 300" preserveAspectRatio="xMidYMid slice">
        <Defs>
          <LinearGradient id={sky} x1="0" y1="0" x2="0.8" y2="1">
            <Stop offset="0" stopColor={mid} />
            <Stop offset="1" stopColor={dark} />
          </LinearGradient>
          <LinearGradient id={sun} x1="0" y1="0" x2="0" y2="1">
            <Stop offset="0" stopColor={light} />
            <Stop offset="1" stopColor={mid} />
          </LinearGradient>
        </Defs>
        <Rect width="300" height="300" fill={`url(#${sky})`} />
        {variant === 0 ? (
          <>
            <Circle cx="220" cy="102" r="59" fill={`url(#${sun})`} />
            <Path d="M-30 236L85 91l89 116 61-67 98 136V330H-30Z" fill={dark} />
            <Path d="M-20 248L97 124l77 87 62-49 98 118V330H-20Z" fill={light} opacity=".17" />
            <Path d="M-20 273l128-70 85 56 64-32 78 66v50H-20Z" fill={dark} opacity=".75" />
          </>
        ) : variant === 1 ? (
          <>
            <Circle
              cx="145"
              cy="139"
              r="101"
              fill="none"
              stroke={light}
              strokeWidth="1"
              opacity=".35"
            />
            <Circle
              cx="145"
              cy="139"
              r="73"
              fill="none"
              stroke={light}
              strokeWidth="1"
              opacity=".35"
            />
            <Circle cx="145" cy="139" r="43" fill={`url(#${sun})`} />
            <Path d="M0 214Q64 173 142 223T300 208V300H0Z" fill={dark} />
            <Path d="M0 238Q91 211 181 247T320 229" fill="none" stroke={light} opacity=".35" />
          </>
        ) : (
          <>
            <Rect
              x="92"
              y="-30"
              width="92"
              height="430"
              transform="rotate(32 150 150)"
              fill={light}
              opacity=".22"
            />
            <Circle cx="155" cy="140" r="80" fill={`url(#${sun})`} />
            <Path d="M0 236Q102 181 300 228V300H0Z" fill={dark} />
            <Path d="M0 254Q91 207 300 249" fill="none" stroke={light} opacity=".4" />
          </>
        )}
        <G opacity=".1">
          <Path
            fill={light}
            d={Array.from(
              { length: 60 },
              (_, n) =>
                `M ${(n * 71 + 13) % 300} ${(n * 47 + 31) % 300} m -.8 0 a .8 .8 0 1 0 1.6 0 a .8 .8 0 1 0 -1.6 0 Z`,
            ).join(' ')}
          />
        </G>
      </Svg>
      {label && size >= 100 && (
        <View
          style={{
            position: 'absolute',
            left: size * 0.08,
            right: size * 0.08,
            bottom: size * 0.08,
          }}
        >
          <Txt
            weight="bold"
            size={Math.max(10, size * 0.055)}
            numberOfLines={2}
            style={{ color: light, textTransform: 'uppercase', letterSpacing: size * 0.005 }}
          >
            {title}
          </Txt>
        </View>
      )}
      {source && (
        <CoverImage key={source.uri + (source.headers?.Authorization || '')} source={source} />
      )}
    </View>
  );
}

function CoverImage({ source }: { source: { uri: string; headers?: Record<string, string> } }) {
  const [failed, setFailed] = useState(false);
  return failed ? null : (
    <Image
      source={source}
      style={StyleSheet.absoluteFill}
      contentFit="cover"
      transition={200}
      cachePolicy="memory"
      onError={() => setFailed(true)}
    />
  );
}
