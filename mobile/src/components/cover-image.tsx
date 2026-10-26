import React, { memo, useState } from 'react';
import { StyleSheet } from 'react-native';
import { Image } from 'expo-image';

export type CoverSource = { uri: string; headers?: Record<string, string> };

export const CoverImage = memo(function CoverImage({ source }: { source: CoverSource }) {
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
});
